package gocld

// IngestELF splices a relocatable ELF64 object -- the output of the LLVM
// backend's codegen for a Linux target -- into this assembler, exactly the way
// IngestCOFF does for a Windows target. Nothing downstream changes: after
// IngestELF the assembler looks as though goa had emitted the bytes itself, and
// BuildELF lays out the result.
//
// Two jobs, mirroring the COFF path:
//
//  1. Sections. An ELF section is appended to the matching goa section
//     (.text/.rodata/.data/.bss); .tdata/.tbss are best-effort routed into
//     .data/.bss for now (TLS semantics are not modelled -- no goclib example
//     needs them yet). Non-allocated sections (.comment, .note.*, .debug*, the
//     symbol/string tables, and .eh_frame) are ignored.
//
//  2. Symbols and relocations. Every defined symbol enters img.Syms at its new
//     offset. Every undefined symbol must already resolve to something the
//     assembly half defined -- a goa syscall stub or a global -- or the link
//     fails naming it. An ELF target has no import table to catch a miss, so a
//     guessed import would build and then fault, which is worse. Section
//     symbols (STT_SECTION) become a synthetic "__secbase_<n>" key so a
//     relocation that names "address of section N plus addend" resolves to the
//     section's base plus that addend.
//
// Relocation arithmetic matches the COFF path's model: a PC-relative fixup
// stores S + A - P (P = the byte after the field), which is what goa's
// applyFixup computes for a relative fixup with RipAdjust 0; an absolute 64-bit
// fixup stores S + A; an absolute 32-bit stores the low 32 bits of S + A.

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// ELF64 file/section/symbol constants used by the parser. The base ELF
// constants shared with the writer (elfClass64, emX8664, sht*, shfAlloc, ...)
// live in elf.go; these are the object-specific ones the parser needs on top.
const (
	etRel      = 1 // ET_REL: a relocatable object
	shtRela    = 4
	shtRel     = 9
	sttFunc    = 2
	sttSection = 3
	sttFile    = 4 // STT_FILE: the object's own name, not a thing inside it

	shnUndef = 0
	shnAbs   = 0xfff1

	// x86-64 ELF relocation types we accept.
	rX8664None  = 0
	rX8664_64   = 1  // S + A (absolute 64-bit)
	rX8664PC32  = 2  // S + A - P (32-bit PC-relative)
	rX8664PLT32 = 4  // S + A - P (calls; same arithmetic as PC32 here)
	rX8664_32   = 10 // S + A (absolute 32-bit)
	rX8664_32S  = 11 // S + A sign-extended (absolute 32-bit)
	rX8664PC64  = 24 // S + A - P (64-bit PC-relative)
)

// rd64 reads a little-endian u64 from b at off, or 0 past the end.
func rd64(b []byte, off int) uint64 {
	if off < 0 || off+8 > len(b) {
		return 0
	}
	return binary.LittleEndian.Uint64(b[off:])
}

// elfsym is one parsed ELF symbol-table entry (the parts the merge needs).
type elfsym struct {
	name   string
	value  uint64
	shndx  uint16 // 0 = undefined, 0xfff1 = absolute, else 1-based section
	isFunc bool
	isSect bool // STT_SECTION: names "address of section N"
	isFile bool // STT_FILE: names the object, not anything in it
}

// elfsec is one parsed section header plus its contents.
type elfsec struct {
	name   string
	typ    uint32
	flags  uint64
	align  uint64
	data   []byte
	bss    bool // SHT_NOBITS: no file bytes, virtual size from sh_size
	vsize  uint64
	relOff uint64 // file offset of the first relocation
	relCnt uint64 // number of relocation entries
	relSec uint32 // section index the relocations apply to (sh_info)
}

// elfObj is a parsed ELF64 relocatable object.
type elfObj struct {
	secs   []elfsec
	syms   []elfsym
	strOff uint64 // file offset of .strtab
	strSz  uint64
	symKey []string // fixup key for each symbol index
}

// IngestELF merges the object at path into the assembler.
func (img *Image) IngestELF(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return img.IngestELFBytes(src)
}

// IngestELFBytes merges an in-memory ELF object. It is the form the compiler
// uses, which already holds the object.
func (img *Image) IngestELFBytes(src []byte) error {
	o, err := parseELF(src)
	if err != nil {
		return err
	}
	return img.ingestParsedELF(o, src)
}

func parseELF(src []byte) (*elfObj, error) {
	if len(src) < 64 {
		return nil, fmt.Errorf("elf: file too short (%d bytes)", len(src))
	}
	if src[0] != 0x7f || src[1] != 'E' || src[2] != 'L' || src[3] != 'F' {
		return nil, fmt.Errorf("elf: bad magic")
	}
	if src[4] != elfClass64 || src[5] != elfDataLSB {
		return nil, fmt.Errorf("elf: not a little-endian ELF64 object")
	}
	if rd16(src, 16) != etRel {
		return nil, fmt.Errorf("elf: not a relocatable object (e_type=%d)", rd16(src, 16))
	}
	if rd16(src, 18) != emX8664 {
		return nil, fmt.Errorf("elf: machine 0x%x is not x86-64", rd16(src, 18))
	}
	shoff := rd64(src, 40)
	shnum := int(rd16(src, 60))
	shentsz := int(rd16(src, 58))
	shstrndx := int(rd16(src, 62))
	if shentsz < 64 || shnum == 0 || shoff == 0 || shoff+uint64(shnum)*uint64(shentsz) > uint64(len(src)) {
		return nil, fmt.Errorf("elf: missing or malformed section header table")
	}

	// Pull section names out of the section-header string table.
	var shstr []byte
	if shstrndx >= 0 && shstrndx < shnum {
		sh := shoff + uint64(shstrndx)*uint64(shentsz)
		stroff := rd64(src, int(sh)+24)
		strsz := rd64(src, int(sh)+32)
		if stroff+strsz <= uint64(len(src)) {
			shstr = src[stroff : stroff+strsz]
		}
	}
	secName := func(hdr int) string {
		off := int(rd32(src, hdr))
		if off <= 0 || off >= len(shstr) {
			return ""
		}
		end := off
		for end < len(shstr) && shstr[end] != 0 {
			end++
		}
		return string(shstr[off:end])
	}

	o := &elfObj{}
	for i := 0; i < shnum; i++ {
		h := shoff + uint64(i)*uint64(shentsz)
		typ := uint32(rd32(src, int(h)+4))
		es := elfsec{name: secName(int(h)), typ: typ, flags: rd64(src, int(h)+8)}
		es.align = rd64(src, int(h)+48)
		es.data = nil
		es.bss = typ == shtNoBits
		es.vsize = rd64(src, int(h)+32)
		if typ == shtRela || typ == shtRel {
			es.relOff = rd64(src, int(h)+24)
			es.relCnt = rd64(src, int(h)+32) / 24    // SHT_RELA entries are 24 bytes
			es.relSec = uint32(rd32(src, int(h)+44)) // sh_info
		}
		if typ == shtProgBits && es.bss == false {
			off := rd64(src, int(h)+24)
			sz := rd64(src, int(h)+32)
			if off+sz <= uint64(len(src)) {
				es.data = append([]byte(nil), src[off:off+sz]...)
			}
		}
		o.secs = append(o.secs, es)
	}

	// Locate .symtab and its string table (sh_link on the symtab header).
	var symOff, symCnt, strOff, strSz uint64
	for i := 0; i < shnum; i++ {
		h := shoff + uint64(i)*uint64(shentsz)
		if rd32(src, int(h)+4) == shtSymTab {
			symOff = rd64(src, int(h)+24)
			symCnt = rd64(src, int(h)+32) / 24
			lnk := rd32(src, int(h)+40) // sh_link -> .strtab
			if int(lnk) < shnum {
				lh := shoff + uint64(lnk)*uint64(shentsz)
				strOff = rd64(src, int(lh)+24)
				strSz = rd64(src, int(lh)+32)
			}
			break
		}
	}
	o.strOff, o.strSz = strOff, strSz
	if symCnt == 0 {
		return o, nil
	}

	for i := uint64(0); i < symCnt; i++ {
		ent := symOff + i*24
		nm := rd32(src, int(ent))
		info := src[ent+4]
		shndx := rd16(src, int(ent)+6)
		val := rd64(src, int(ent)+8)
		name := ""
		if nm != 0 && uint64(nm) < strSz {
			base := strOff + uint64(nm)
			end := int(base)
			for end < len(src) && src[end] != 0 {
				end++
			}
			if int(base) < len(src) {
				name = string(src[base:uint64(end)])
			}
		}
		o.syms = append(o.syms, elfsym{
			name:   name,
			value:  val,
			shndx:  uint16(shndx),
			isFunc: info&0xf == sttFunc,
			isSect: info&0xf == sttSection,
			isFile: info&0xf == sttFile,
		})
	}
	o.symKey = make([]string, len(o.syms))
	return o, nil
}

func (img *Image) ingestParsedELF(o *elfObj, src []byte) error {
	// The C library symbol table the writer rode along in (see WriteELFObject).
	// Read before the sections, because recognising a library symbol is what
	// tells a second inlined copy of printf from a user who defined printf
	// twice, and that decision is made while the symbols go in.
	//
	// The section is non-allocated, so the loop below skips it anyway; it is
	// read here rather than treated as an ordinary section precisely because it
	// is metadata, not content.
	objName := ""
	objLib := map[string]bool{}
	for _, es := range o.secs {
		if es.name != elfLibSecName {
			continue
		}
		for _, n := range strings.Split(string(es.data), ",") {
			if n = strings.TrimSpace(n); n != "" {
				objLib[n] = true
			}
		}
	}

	// Map an ELF section name to a goa section. Unmapped but allocated sections
	// are routed to .rdata so their bytes are not lost; everything else
	// (non-alloc) is dropped.
	elfSectionMap := func(name string) (goaName string, ok bool) {
		switch {
		case strings.HasPrefix(name, ".text"):
			return ".text", true
		case strings.HasPrefix(name, ".rodata"), strings.HasPrefix(name, ".rdata"):
			return ".rdata", true
		case strings.HasPrefix(name, ".data"), strings.HasPrefix(name, ".tdata"):
			return ".data", true
		case strings.HasPrefix(name, ".bss"), strings.HasPrefix(name, ".tbss"):
			return ".bss", true
		}
		return "", false
	}

	// Align within a goa section, mirroring the COFF merge.
	padTo := func(s *Section, want int) {
		if want <= 1 {
			want = 1
		}
		n := align(s.VSize, want) - s.VSize
		if n > 0 {
			padSection(s, n)
		}
	}

	// --- sections ---
	// sectOf and baseOf are indexed by the object's own section number (shndx).
	// o.secs[i] IS section index i -- index 0 is the NULL section, which the loop
	// below skips -- so the mapping is stored at the same index and a symbol or
	// relocation naming shndx N reads sectOf[N] directly. Indexing these by i+1
	// would shift every lookup by one and silently resolve a .data symbol into
	// .text, which is how a string constant ended up overlapping code.
	//
	// Both slices start at -1, not 0: a section the loop below skips (.eh_frame,
	// .rela.*, .symtab, ...) must stay distinguishable from section index 0,
	// which is a perfectly valid .text. With a zero default, a skipped section's
	// relocations were applied to .text at the object's own offsets -- the
	// .eh_frame FDE fields landed in the middle of the code and overwrote the
	// displacement of a `call`.
	sectOf := make([]int, len(o.secs)+1)
	baseOf := make([]int, len(o.secs)+1)
	for i := range sectOf {
		sectOf[i] = -1
	}
	for i, es := range o.secs {
		if es.typ == shtRela || es.typ == shtRel || es.typ == shtSymTab || es.typ == shtStrTab || es.typ == shtNull {
			continue
		}
		if es.bss && es.vsize == 0 {
			continue
		}
		if es.typ != shtProgBits && es.typ != shtNoBits {
			continue
		}
		mapped, known := elfSectionMap(es.name)
		if !known {
			if es.flags&shfAlloc != 0 {
				mapped, known = ".rdata", true
			} else {
				continue
			}
		}
		gs := sectionByName(img, mapped)
		if gs == nil {
			gs = newSection(img, mapped, mapped == ".data" || mapped == ".bss", mapped == ".text")
		}
		want := int(es.align)
		if want == 0 {
			if mapped == ".text" {
				want = 16
			} else {
				want = 8
			}
		}
		padTo(gs, want)
		if es.bss {
			baseOf[i] = gs.VSize
			gs.VSize += int(es.vsize)
			gs.Bss = true
		} else {
			baseOf[i] = gs.VSize
			gs.Data = append(gs.Data, es.data...)
			gs.VSize += len(es.data)
		}
		sectOf[i] = sectionIndexOf(img, gs)
	}

	// --- symbols ---
	for i, s := range o.syms {
		switch {
		case s.isFile:
			// STT_FILE names the object rather than anything in it. Read here
			// because a duplicate-definition diagnostic quotes it, and a
			// diagnostic that says "an earlier object" when both names were
			// sitting in the files costs the reader a step of looking.
			objName = s.name
			o.symKey[i] = ""
		case s.isSect:
			// A section symbol's value is the section's own address, which
			// after merging is baseOf[shndx] -- NOT zero. The object keeps each
			// section at its own offset, and several of them land in one goa
			// section (.rodata, .rodata.cst8 and .rodata.cst16 all become
			// .rdata), so the base is what tells them apart.
			//
			// Getting this wrong is silent and distant. A jump table is a
			// run of zero bytes in .rodata that R_X86_64_64 relocations fill
			// with code addresses, and the switch that reads it is
			// `jmp *disp32(,%rax,8)` with disp32 pointing at the table. With
			// Off: 0 the table's own entries were fixed up correctly but the
			// *reference* to it resolved to the start of the merged .rdata
			// instead of the table, so the switch indexed into whatever
			// preceded it -- usually a pool of double bit patterns, which are
			// not code addresses, and the jump landed on a non-executable
			// value. That surfaces as SIGSEGV with no relocation error
			// anywhere, which is why it is worth stating the rule here.
			key := fmt.Sprintf("__secbase_%d", s.shndx)
			o.symKey[i] = key
			if s.shndx >= 1 && int(s.shndx) <= len(o.secs) {
				if secIdx := sectOf[s.shndx]; secIdx >= 0 {
					if _, ok := img.Syms[key]; !ok {
						img.Syms[key] = SymLoc{Sect: secIdx, Off: baseOf[s.shndx]}
					}
				}
			}
		case s.shndx == shnUndef && s.name == "":
			o.symKey[i] = ""
		case s.shndx == shnUndef:
			// Undefined but named: must resolve to a symbol the assembler half
			// already defined (a goa syscall stub or a global). Record the name;
			// if it is not satisfied, the relocation loop reports it.
			o.symKey[i] = s.name
		case s.shndx == shnAbs:
			key := fmt.Sprintf("__abs_%d", s.value)
			o.symKey[i] = key
			if _, ok := img.Syms[key]; !ok {
				img.Syms[key] = SymLoc{Sect: 0, Off: int(s.value)}
			}
		default:
			if s.shndx >= 1 && int(s.shndx) <= len(o.secs) {
				gi := sectOf[s.shndx]
				if gi >= 0 {
					// A name two objects both define is an error, unless both
					// are the same C library inlined twice -- see isCLibSymbol
					// for why those are equivalent rather than conflicting.
					// The ELF rules mirror the COFF ones, and the reasoning is
					// written there rather than repeated.
					if _, dup := img.Syms[s.name]; dup {
						if isCLibSymbol(s.name, objLib) {
							if img.deduped == nil {
								img.deduped = map[string]bool{}
							}
							img.deduped[s.name] = true
							o.symKey[i] = s.name
							continue
						}
						first := img.definedIn[s.name]
						if first == "" {
							first = "an earlier object"
						}
						if objName == "" {
							objName = "this object"
						}
						return fmt.Errorf("duplicate definition of symbol %q: defined in %s and in %s", s.name, first, objName)
					}
					img.Syms[s.name] = SymLoc{Sect: gi, Off: baseOf[s.shndx] + int(s.value)}
					if img.definedIn == nil {
						img.definedIn = map[string]string{}
					}
					if s.name != "" {
						img.definedIn[s.name] = objName
					}
					o.symKey[i] = s.name
				}
			}
		}
	}

	// --- relocations ---
	var unresolved []string
	for _, es := range o.secs {
		if es.typ != shtRela {
			continue
		}
		target := int(es.relSec)
		if target < 1 || target > len(o.secs) {
			continue
		}
		tsGi := sectOf[target]
		tsBase := baseOf[target]
		if tsGi < 0 {
			continue
		}
		for r := uint64(0); r < es.relCnt; r++ {
			ent := int(es.relOff + r*24)
			if ent+24 > len(src) {
				return fmt.Errorf("elf: relocation %d of %s out of range", r, es.name)
			}
			off := int(rd64(src, ent))
			info := rd64(src, ent+8)
			addend := int64(rd64(src, ent+16))
			symIdx := int(info >> 32)
			typ := uint32(info & 0xffffffff)
			if symIdx < 0 || symIdx >= len(o.syms) {
				return fmt.Errorf("elf: relocation %d of %s names bad symbol %d", r, es.name, symIdx)
			}
			key := o.symKey[symIdx]
			if key == "" {
				return fmt.Errorf("elf: relocation %d of %s references an unresolved symbol", r, es.name)
			}
			if o.syms[symIdx].shndx == shnUndef && !img.definesSymbol(key) {
				// A link over several objects cannot judge this name yet: the
				// object that defines it may not have been read. Note it for
				// Resolve to settle, but keep going -- the relocation still has
				// to be recorded, because by then the name will be defined and
				// the field will want the address.
				//
				// Dropping it here is the tempting mistake, and it produces a
				// program that links cleanly and calls the next instruction
				// instead: the field keeps whatever placeholder bytes it had,
				// there is no relocation left to complain, and the bug surfaces
				// as a wrong answer rather than as a link error.
				if img.deferred {
					img.AddPending(map[string]bool{key: true})
				} else {
					unresolved = append(unresolved, key)
					continue
				}
			}
			at := tsBase + off
			switch typ {
			case rX8664None:
				// padding, no-op
			case rX8664PC32, rX8664PLT32:
				// PC-relative: the field wants S + A - P, with P the field's own
				// address and the "next instruction" already folded into A (-4 for
				// a call/jmp, 0 for a RIP-relative data ref). goa instead measures
				// from the byte after the field -- S + Addend - (P + 4) -- so the
				// addend has to be shifted by the field width or every PC-relative
				// reference lands 4 bytes low (a `call write` reaching write-4).
				img.Fixups = append(img.Fixups, Fixup{
					Sect: tsGi, Off: at, Sym: key, Addend: int(addend) + 4,
				})
			case rX8664PC64:
				// Same rule with an 8-byte field. (The large code model that needs
				// this is not exercised by goclib today.)
				img.Fixups = append(img.Fixups, Fixup{
					Sect: tsGi, Off: at, Sym: key, Addend: int(addend) + 8, Wide: true,
				})
			case rX8664_64:
				img.Fixups = append(img.Fixups, Fixup{
					Sect: tsGi, Off: at, Sym: key, Addend: int(addend),
					Absolute: true, Wide: true,
				})
			case rX8664_32, rX8664_32S:
				img.Fixups = append(img.Fixups, Fixup{
					Sect: tsGi, Off: at, Sym: key, Addend: int(addend),
					Absolute: true,
				})
			default:
				return fmt.Errorf("elf: unsupported relocation type %d for %s (%s)", typ, es.name, key)
			}
		}
	}
	if len(unresolved) > 0 {
		sortStringsELF(unresolved)
		return fmt.Errorf("elf: undefined symbol(s): %s", strings.Join(unresolved, ", "))
	}
	return nil
}

func sortStringsELF(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// UndefinedELFSymbols returns the names of the undefined (external) symbols in
// an ELF64 object -- the ones the object's relocations reference but does not
// define itself. gocl uses this to decide which `extern` lines to hand goa:
// every undefined name that is a raw Linux syscall (goa.IsLinuxSyscall) becomes
// a `mov rax,N; syscall; ret` stub, so the object's calls resolve against it.
// The C library's own functions (memcpy, __goclib_exit, ...) are defined in the
// same object, so they are not undefined and need no extern.
func UndefinedELFSymbols(obj []byte) ([]string, error) {
	o, err := parseELF(obj)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range o.syms {
		if s.shndx == shnUndef && s.name != "" {
			out = append(out, s.name)
		}
	}
	return out, nil
}
