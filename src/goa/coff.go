package goa

// COFF object ingestion.
//
// goc has two code paths. The native one is its own assembler: source asm goes
// straight into .text/.data and BuildPE lays out the image. The LLVM backend
// instead hands us a COFF object produced by LLVM's own codegen, and this file
// splices such an object's sections, symbols and relocations into the same
// Section/syms/fixups model BuildPE already consumes. Nothing downstream
// changes: after IngestCOFF the assembler looks exactly as if goa had emitted
// the bytes itself.
//
// What an LLVM-produced x86-64 COFF object actually contains (measured on
// LLVM 23.1.2, not guessed):
//
//   - sections .text .data .bss .rdata .xdata .pdata
//   - relocations of exactly two kinds: IMAGE_REL_AMD64_REL32 (call targets and
//     rip-relative data references in code) and IMAGE_REL_AMD64_ADDR32NB (the
//     RVAs stored in the SEH unwind table). Anything else is rejected rather
//     than silently mis-linked.
//   - undefined symbols (functions satisfied by the goc runtime, e.g. print or
//     __main) that must land in the import address table
//   - one COMDAT selection group per unwind-info contribution
//   - the absolute symbol @feat.00 (the /GS security cookie table)
//
// .pdata/.xdata carry the Win64 SEH unwind tables. They are merged -- symbols
// resolved, relocations applied -- and then left out of the image (see
// Section.Unmapped). The reasoning that used to keep them here was that they
// were nearly free, and that was wrong in a way worth recording: the tables are
// small (12 bytes of .pdata and about 11 of .xdata per function) but a PE
// section takes a whole multiple of FileAlignment in the file, and 512 is the
// smallest value Windows accepts. Three functions cost 1024 bytes to store 72
// bytes of table, so every -fllvm image was at least a kilobyte larger than the
// same program built by the native path, which emits no unwind info either.

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// COFF machine type and the relocation types we accept.
const (
	coffMachineAMD64 = 0x8664

	relAMD64Abs      = 0x0000 // IMAGE_REL_AMD64_ABSOLUTE (padding, no-op)
	relAMD64Addr64   = 0x0001 // IMAGE_REL_AMD64_ADDR64
	relAMD64Addr32   = 0x0002 // IMAGE_REL_AMD64_ADDR32
	relAMD64Addr32NB = 0x0003 // IMAGE_REL_AMD64_ADDR32NB
	relAMD64Rel32    = 0x0004 // IMAGE_REL_AMD64_REL32
)

// COFF symbol storage classes.
const (
	scnClassExternal = 2
	scnClassStatic   = 3
	scnClassFile     = 103 // IMAGE_SYM_CLASS_FILE, the file-name pseudo symbol
)

// coffSec is one parsed section header plus its contents.
type coffSec struct {
	name string
	data []byte
	bss  bool // no file bytes; virtual size comes from SizeOfRawData
	// vsize is the section's virtual size. For a normal section it equals
	// len(data); for .bss it is the only measure of the space the section
	// occupies, since an uninitialised section has no file bytes at all.
	vsize    int
	relOff   int // file offset of the first relocation, 0 if none
	relCount int
}

// coffSym is one parsed symbol-table entry.
type coffSym struct {
	name   string
	value  int32 // offset within its section, or an absolute value
	secNum int32 // 1-based section index; 0 = undefined, -1 = absolute
	class  uint8
	isFunc bool // COFF type 0x20, DTYPE_FUNCTION
}

// coffObj is a parsed COFF object file.
type coffObj struct {
	secs []coffSec
	// syms is every symbol-table entry in table order, including the ones the
	// merge has no use for (segment symbols, the file-name pseudo symbol).
	// Relocation records reference symbols by their raw table index, so the
	// table has to stay index-aligned: dropping entries would silently shift
	// every later reference onto the wrong name.
	syms []coffSym
}

func rd16(b []byte, off int) int {
	if off < 0 || off+2 > len(b) {
		return 0
	}
	return int(binary.LittleEndian.Uint16(b[off:]))
}

func rd32(b []byte, off int) int {
	if off < 0 || off+4 > len(b) {
		return 0
	}
	return int(int32(binary.LittleEndian.Uint32(b[off:])))
}

func rdu32(b []byte, off int) uint32 {
	if off < 0 || off+4 > len(b) {
		return 0
	}
	return binary.LittleEndian.Uint32(b[off:])
}

// parseCOFF reads a PE/COFF object file -- the bare-header form LLVM emits, not
// a full PE image (which is what BuildPE produces).
func parseCOFF(src []byte) (*coffObj, error) {
	if len(src) < 20 {
		return nil, fmt.Errorf("coff: file too short (%d bytes)", len(src))
	}
	// A PE image starts with "MZ"; a COFF object is a bare header. Rejecting the
	// image form here turns a confusing layout bug into a clear message.
	if src[0] == 'M' && src[1] == 'Z' {
		return nil, fmt.Errorf("coff: this is a PE image, not a COFF object")
	}
	if machine := rd16(src, 0); machine != coffMachineAMD64 {
		return nil, fmt.Errorf("coff: machine 0x%x is not AMD64", machine)
	}
	nsec := rd16(src, 2)
	// IMAGE_FILE_HEADER after Machine+NumberOfSections:
	//   TimeDateStamp(4) PointerToSymbolTable(4) NumberOfSymbols(4)
	//   SizeOfOptionalHeader(2) Characteristics(2)
	// A COFF object has no optional header, so its size is 0. Skipping the
	// timestamp is essential: reading the symbol-table pointer one field early
	// yields the timestamp (0) and silently parses no symbols at all.
	symTableOff := rd32(src, 8)
	nSyms := rd32(src, 12)

	o := &coffObj{}
	// Where the string table begins: immediately after the symbol table, whose
	// first dword is the table's own length. A "/N" section name is an offset
	// into it, counted from that length field, so the base has to be known
	// before the section headers are read.
	coffStrBase := symTableOff + 18*nSyms

	// --- section headers (20-byte COFF header, then 40 bytes each) ---
	pos := 20
	for i := 0; i < nsec; i++ {
		if pos+40 > len(src) {
			return nil, fmt.Errorf("coff: section header %d out of range", i)
		}
		hdr := pos
		// An eight-byte name field holds either the name itself or, for a name
		// that does not fit, a slash followed by a decimal offset into the
		// string table ("/4"). LLVM emits the long form for ".rdata" and for
		// COMDAT members, so reading it as literal text yields a section called
		// "/4" -- which then matches no mapping and lands in the image under a
		// name nothing refers to.
		name := coffSectionName(src, hdr, coffStrBase)
		rawSize := rd32(src, hdr+16)
		rawPtr := rd32(src, hdr+20)
		relPtr := rd32(src, hdr+24)
		nrel := rd32(src, hdr+32)
		chars := rdu32(src, hdr+36)
		pos += 40

		// IMAGE_SCN_CNT_UNINITIALIZED_DATA (0x80) marks .bss: virtual space
		// with no file bytes. A section with no raw pointer is the same thing.
		sec := coffSec{
			name:     name,
			relOff:   relPtr,
			relCount: nrel,
			vsize:    rawSize,
			bss:      chars&0x80 != 0 || rawPtr == 0,
		}
		if !sec.bss {
			if rawPtr < 0 || rawPtr+rawSize > len(src) {
				return nil, fmt.Errorf("coff: section %s data out of range", name)
			}
			sec.data = append([]byte(nil), src[rawPtr:rawPtr+rawSize]...)
		}
		o.secs = append(o.secs, sec)
	}

	if nSyms == 0 || symTableOff <= 0 {
		return o, nil
	}

	// Symbol records come in two widths. Classic COFF uses 18 bytes with the
	// name occupying bytes [0..7]. bigobj -- the format LLVM emits for Windows
	// targets, because a module can exceed the 65,535-symbol classic limit --
	// uses 20 bytes, with a 4-byte flags field in front of the name:
	//
	//   classic: [8]name  [4]value [2]section [2]type [1]class [1]naux
	//   bigobj:  [4]flags [8]name  [4]value [2]section [2]type [4]class+naux
	//
	// The two are told apart by SizeOfOptionalHeader, which is 0 for a COFF
	// object and 0x20 (IMAGE_NT_OPTIONAL_HDR32_MAGIC) for bigobj. Guessing from
	// the data instead is what makes this fragile: an 18-byte read of a bigobj
	// table yields plausible-looking garbage (huge section offsets, nonsense
	// values) rather than an error, so the format must come from the header.
	optHdrSize := rd16(src, 16)
	bigObj := false
	if optHdrSize == 0x20 {
		bigObj = true
	} else if optHdrSize != 0 {
		// A COFF object carries no optional header at all. A non-zero value
		// means this is something else (a PE image, already rejected above).
		return nil, fmt.Errorf("coff: unexpected SizeOfOptionalHeader %d for a COFF object", optHdrSize)
	}
	recSize := 18
	if bigObj {
		recSize = 20
	}

	if symTableOff+recSize*nSyms > len(src) {
		return nil, fmt.Errorf("coff: symbol table out of range (%d records of %d bytes at %d)",
			nSyms, recSize, symTableOff)
	}
	// The string table immediately follows the symbol table; its length is the
	// first dword there.
	strBase := symTableOff + recSize*nSyms
	strEnd := len(src)
	if strBase+4 <= len(src) {
		if n := rd32(src, strBase); n >= 4 && strBase+n <= len(src) {
			strEnd = strBase + n
		}
	}
	// Names are offsets into the string table, whose own indices are relative to
	// its first dword -- the same base the section names used.
	coffStrTab := src[:strEnd]

	// readName pulls the 8-byte name field at rec. A name of eight bytes or
	// fewer is stored inline, left-justified and NUL-padded, so its first dword
	// is non-zero. A longer name lives in the string table and is referenced by
	// a ZERO first dword followed by the offset -- the two cases are therefore
	// told apart by the first dword alone, and reading the offset from the wrong
	// half is what loses the symbol.
	readName := func(rec int) string {
		if binary.LittleEndian.Uint32(src[rec:rec+4]) == 0 {
			if s, ok := coffStringAt(coffStrTab, coffStrBase+rd32(src, rec+4), -1); ok {
				return s
			}
			return ""
		}
		return strings.TrimRight(string(src[rec:rec+8]), "\x00")
	}

	for i := 0; i < nSyms; {
		rec := symTableOff + recSize*i
		if rec+recSize > len(src) {
			return nil, fmt.Errorf("coff: symbol %d out of range", i)
		}
		name := readName(rec)
		var val int32
		var secNum int32
		var typ uint16
		var cls uint8
		var nAux int
		if bigObj {
			// bigobj: [4]flags [8]name [4]value [2]section [2]type [4]class+naux
			flags := rd32(src, rec)
			if flags != 0 {
				// Flags non-zero means the name is a string-table offset, and the
				// high bit set means the name is a section-definition auxiliary.
				off := flags &^ 0x80000000
				if s, ok := coffStringAt(coffStrTab, coffStrBase+off, -1); ok {
					name = s
				} else {
					name = ""
				}
			} else {
				name = strings.TrimRight(string(src[rec+4:rec+12]), "\x00")
			}
			val = int32(rd32(src, rec+12))
			secNum = int32(rd16(src, rec+16))
			typ = uint16(rd16(src, rec+18))
			// StorageClass and NumberOfAuxSymbols share one 4-byte field.
			cls = src[rec+18]
			nAux = int(src[rec+19])
		} else {
			// classic: [8]name [4]value [2]section [2]type [1]class [1]naux
			val = int32(rd32(src, rec+8))
			secNum = int32(rd16(src, rec+12))
			typ = uint16(rd16(src, rec+14))
			cls = src[rec+16]
			nAux = int(src[rec+17])
		}

		// Every entry is recorded, in table order, so relocation symbol indices
		// stay aligned with the file. The merge later ignores the ones it has no
		// use for.
		o.syms = append(o.syms, coffSym{
			name:   name,
			value:  val,
			secNum: secNum,
			class:  cls,
			isFunc: typ&0x20 != 0,
		})
		// Aux records occupy their own slots in the table: a relocation may
		// name one, and an index that skips them would shift every later
		// reference onto the wrong symbol. They carry no usable name or address,
		// so they are recorded as empty placeholders.
		for k := 0; k < nAux; k++ {
			o.syms = append(o.syms, coffSym{})
		}
		i += 1 + nAux
	}
	return o, nil
}

// coffSectionName decodes a section header's eight-byte name field. strBase is
// where the string table starts; a "/N" name is an offset into it measured from
// there, N counting the table's own length dword as zero.
func coffSectionName(src []byte, hdr, strBase int) string {
	raw := string(src[hdr : hdr+8])
	if strings.HasPrefix(raw, "/") {
		off := 0
		for i := 1; i < len(raw); i++ {
			if raw[i] < '0' || raw[i] > '9' {
				break
			}
			off = off*10 + int(raw[i]-'0')
		}
		if off > 0 {
			if s, ok := coffStringAt(src, strBase+off, -1); ok {
				return s
			}
		}
	}
	return strings.TrimRight(raw, "\x00")
}

// coffStringAt reads a NUL-terminated name at offset off within tab. A negative
// want means "to the end of the table".
func coffStringAt(tab []byte, off, want int) (string, bool) {
	if off <= 0 || off >= len(tab) {
		return "", false
	}
	end := off
	for end < len(tab) && tab[end] != 0 {
		if want >= 0 && end-off >= want {
			break
		}
		end++
	}
	return string(tab[off:end]), true
}
