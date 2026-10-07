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
	stbLocal   = 0 // STB_LOCAL: st_info's bind field, the top four bits

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

// ELF machine types the linker accepts. ELF32 machines (EM_386, EM_ARM) arrive
// with the 32-bit ELF reader in a later phase; they are listed so parseELF
// rejects an unknown machine with a clear message instead of misreading it.
const (
	em386     = 3    // EM_386
	emARM     = 40   // EM_ARM
	emAArch64 = 0xB7 // EM_AARCH64
	emRISCV   = 0xF3 // EM_RISCV
)

// AArch64 ELF relocation types we accept. The numeric values are fixed by the
// AArch64 ELF psABI and were read back from a real object with readelf; a
// hand-typed table that disagrees with the tool is the usual way these get
// wrong, which is why they are spelled out rather than derived.
const (
	rAARCH64None                = 0
	rAARCH64_ABS64              = 0x101
	rAARCH64_PREL32             = 0x105
	rAARCH64_ADR_PREL_PG_HI21   = 0x113
	rAARCH64_ADD_ABS_LO12_NC    = 0x115
	rAARCH64_LDST8_ABS_LO12_NC  = 0x111
	rAARCH64_LDST16_ABS_LO12_NC = 0x112
	rAARCH64_LDST32_ABS_LO12_NC = 0x11d
	rAARCH64_LDST64_ABS_LO12_NC = 0x11e
	rAARCH64_JUMP26             = 0x11a
	rAARCH64_CALL26             = 0x11b
)

// ARM32 ELF relocation types we accept. The numeric values are fixed by the
// ARM ELF ABI; the grouping mirrors the instruction forms LLVM emits for a
// static ARMv7 binary (branch, movw/movt immediate, literal-pool / ALU
// pc-relative, and the exception-table PREL31). When the object uses REL rather
// than RELA -- the normal GNU arrangement for ARM32 -- the addend lives in the
// instruction field itself, so it is decoded per type during ingest.
const (
	rARMNone          = 0
	rARMAbs32         = 2  // S + A (absolute 32-bit)
	rARMRel32         = 3  // S + A - P (32-bit pc-relative)
	rARMLdrPCG0       = 4  // pc-relative LDR (literal pool): (S + A - P) as a 12-bit imm
	rARMALUPcRel7_0   = 32 // add/sub/... pc-relative, imm bits 0..7
	rARMALUPcRel19_12 = 33 // imm bits 19..12
	rARMALUPcRel11_8  = 34 // imm bits 11..8
	rARMALUPcRel15_12 = 35 // imm bits 15..12
	rARMLdrPcG1       = 37 // pc-relative LDR, rotated imm
	rARMLdrPcG2       = 38 // pc-relative LDR, rotated imm
	rARMCall          = 28 // BL: ((S + A - P) >> 2) in the 24-bit field
	rARMJump24        = 29 // B:  same, bit 24 cleared
	rARMV4BX          = 40 // BX -> no-op on ARMv5+ (BX is legal there)
	rARMPrel31        = 42 // exception-table entry: (S + A - P) as 31-bit signed
	rARMMovwAbsNC     = 43 // movw: low 16 bits of (S + A)
	rARMMovtAbs       = 44 // movt: high 16 bits of (S + A)
)

// RISC-V relocation types (psABI). RISC-V always uses RELA, so -- unlike
// ARM32 -- the addend travels in the relocation entry and is not read back
// out of the instruction.
//
// The pair HI20/LO12_I is the whole reason this block needs care: a RISC-V
// address is built as `lui` (20 bits) followed by an I-type `addi`/`ld` whose
// 12-bit immediate is SIGN-extended. The low part can therefore be negative,
// down to -2048, and the high part has to be rounded up to compensate -- which
// is what the +0x800 in R_RISCV_HI20 is for. Dropping it puts every symbol
// whose page offset is 2048 or more one page low, and the program then reads
// the wrong data with no complaint from the linker.
const (
	rRISCVNone    = 0
	rRISCV32      = 1  // S + A (absolute 32-bit)
	rRISCV64      = 2  // S + A (absolute 64-bit)
	rRISCVCall    = 18 // auipc+jalr pair: expanded as PCREL_HI20 + LO12_I
	rRISCVCallPLT = 19 // the same thing, routed through the PLT
	rRISCVBranch  = 16 // SB-type branch: (S + A - P) >> 1
	rRISCVJAL     = 17 // UJ-type jal: (S + A - P) >> 1
	rRISCVHI20    = 26 // lui: ((S + A + 0x800) >> 12)
	rRISCVLO12I   = 27 // I-type: (S + A) & 0xfff
	rRISCVLO12S   = 28 // S-type store: (S + A) & 0xfff, split encoding
	rRISCVRelax   = 51 // linker-relaxation hint; safe to ignore
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
	name    string
	value   uint64
	shndx   uint16 // 0 = undefined, 0xfff1 = absolute, else 1-based section
	isFunc  bool
	isSect  bool // STT_SECTION: names "address of section N"
	isFile  bool // STT_FILE: names the object, not anything in it
	isLocal bool // STB_LOCAL: binds only inside this object
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

// elfObj is a parsed ELF relocatable object (either class).
type elfObj struct {
	secs    []elfsec
	syms    []elfsym
	strOff  uint64 // file offset of .strtab
	strSz   uint64
	symKey  []string // fixup key for each symbol index
	machine uint16   // e_machine: selects the relocation encoding
	class   uint8    // elfClass64 or elfClass32
}

// isELFMetaSection reports whether name is one of the metadata sections this
// linker drops (see the section-merge loop for why). None of them hold a C
// symbol or an executable byte; they are unwind and attribute metadata the
// kernel does not consult at load time.
//
// .eh_frame is the DWARF unwind table, and dropping it costs the ability to
// backtrace after a crash -- the same bargain the native backend already made,
// because it never emits unwind data at all. It is listed here rather than
// under an ARM heading because every LLVM target emits it, not just ARM.
func isELFMetaSection(name string) bool {
	switch name {
	case ".ARM.exidx", ".ARM.extab", ".ARM.attributes", ".note.GNU-stack",
		".eh_frame", ".eh_frame_hdr":
		return true
	}
	return false
}

func isARMMetaSection(name string) bool {
	return isELFMetaSection(name)
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
	img.Machine = o.machine
	img.Class = o.class
	return img.ingestParsedELF(o, src)
}

func parseELF(src []byte) (*elfObj, error) {
	if len(src) < elfEhSize32 {
		return nil, fmt.Errorf("elf: file too short (%d bytes)", len(src))
	}
	if src[0] != 0x7f || src[1] != 'E' || src[2] != 'L' || src[3] != 'F' {
		return nil, fmt.Errorf("elf: bad magic")
	}
	cls := src[4]
	if cls != elfClass64 && cls != elfClass32 {
		return nil, fmt.Errorf("elf: unsupported ELF class %d (want 1=ELF32 or 2=ELF64)", cls)
	}
	if src[5] != elfDataLSB {
		return nil, fmt.Errorf("elf: not a little-endian ELF object")
	}
	if rd16(src, 16) != etRel {
		return nil, fmt.Errorf("elf: not a relocatable object (e_type=%d)", rd16(src, 16))
	}
	m := rd16(src, 18)
	switch m {
	case emX8664, emAArch64, emRISCV, em386, emARM:
		// accepted; a machine that has no relocation engine yet errors from
		// the relocation switch with a clear message rather than here.
	default:
		return nil, fmt.Errorf("elf: unsupported ELF machine 0x%x", m)
	}

	is64 := cls == elfClass64

	// ELF32 and ELF64 place the section-header-table locator fields at
	// different byte offsets and widths; route each through the right reader.
	var shoff uint64
	var shnum, shentsz, shstrndx int
	if is64 {
		shoff = rd64(src, 40)
		shnum = int(rd16(src, 60))
		shentsz = int(rd16(src, 58))
		shstrndx = int(rd16(src, 62))
	} else {
		shoff = uint64(rd32(src, 32))
		shnum = int(rd16(src, 48))
		shentsz = int(rd16(src, 46))
		shstrndx = int(rd16(src, 50))
	}
	if shentsz < 40 || shnum == 0 || shoff == 0 || shoff+uint64(shnum)*uint64(shentsz) > uint64(len(src)) {
		return nil, fmt.Errorf("elf: missing or malformed section header table")
	}

	// Section-header field byte offsets, by class. Names/type/link/info are
	// always 32-bit; the rest are 64-bit on ELF64 and 32-bit on ELF32.
	var shTypeOff, shFlagsOff, shOffOff, shSizeOff, shLinkOff, shInfoOff, shAlignOff int
	if is64 {
		shTypeOff, shFlagsOff, shOffOff, shSizeOff, shLinkOff, shInfoOff, shAlignOff =
			4, 8, 24, 32, 40, 44, 48
	} else {
		shTypeOff, shFlagsOff, shOffOff, shSizeOff, shLinkOff, shInfoOff, shAlignOff =
			4, 8, 16, 20, 24, 28, 32
	}
	secF := func(h int, off int) uint64 {
		if is64 {
			return rd64(src, h+off)
		}
		return uint64(rd32(src, h+off))
	}
	hdrF := func(h int, off int) uint32 { // 32-bit header fields
		return uint32(rd32(src, h+off))
	}

	// Pull section names out of the section-header string table.
	var shstr []byte
	if shstrndx >= 0 && shstrndx < shnum {
		sh := shoff + uint64(shstrndx)*uint64(shentsz)
		stroff := secF(int(sh), shOffOff)
		strsz := secF(int(sh), shSizeOff)
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

	o := &elfObj{machine: uint16(m), class: cls}
	for i := 0; i < shnum; i++ {
		h := int(shoff + uint64(i)*uint64(shentsz))
		typ := hdrF(h, shTypeOff)
		es := elfsec{name: secName(h), typ: typ, flags: secF(h, shFlagsOff)}
		es.align = secF(h, shAlignOff)
		es.data = nil
		es.bss = typ == shtNoBits
		es.vsize = secF(h, shSizeOff)
		if typ == shtRela || typ == shtRel {
			es.relOff = secF(h, shOffOff)
			// SHT_RELA is 24 bytes on ELF64 and 12 on ELF32; SHT_REL is 8.
			switch typ {
			case shtRela:
				if is64 {
					es.relCnt = secF(h, shSizeOff) / 24
				} else {
					es.relCnt = secF(h, shSizeOff) / 12
				}
			case shtRel:
				es.relCnt = secF(h, shSizeOff) / 8
			}
			es.relSec = hdrF(h, shInfoOff)
		}
		if typ == shtProgBits && !es.bss {
			off := secF(h, shOffOff)
			sz := secF(h, shSizeOff)
			if off+sz <= uint64(len(src)) {
				es.data = append([]byte(nil), src[off:off+sz]...)
			}
		}
		o.secs = append(o.secs, es)
	}

	// Locate .symtab and its string table (sh_link on the symtab header).
	var symOff, symCnt, strOff, strSz uint64
	symStride := 24
	if !is64 {
		symStride = 16
	}
	for i := 0; i < shnum; i++ {
		h := int(shoff + uint64(i)*uint64(shentsz))
		if hdrF(h, shTypeOff) == shtSymTab {
			symOff = secF(h, shOffOff)
			symCnt = secF(h, shSizeOff) / uint64(symStride)
			lnk := hdrF(h, shLinkOff) // sh_link -> .strtab
			if int(lnk) < shnum {
				lh := int(shoff + uint64(lnk)*uint64(shentsz))
				strOff = secF(lh, shOffOff)
				strSz = secF(lh, shSizeOff)
			}
			break
		}
	}
	o.strOff, o.strSz = strOff, strSz
	if symCnt == 0 {
		return o, nil
	}

	// Symbol-table entries differ in layout: ELF64's st_value is an 8-byte
	// field at +8; ELF32's is 4-byte at +4. st_info is at +4 (ELF64) or +12
	// (ELF32); st_shndx at +6 (ELF64) or +14 (ELF32).
	valOff, shndxOff, infoOff := 8, 6, 4
	if !is64 {
		valOff, shndxOff, infoOff = 4, 14, 12
	}
	for i := uint64(0); i < symCnt; i++ {
		ent := symOff + i*uint64(symStride)
		nm := rd32(src, int(ent))
		info := src[int(ent)+infoOff]
		shndx := rd16(src, int(ent)+shndxOff)
		val := secF(int(ent), valOff)
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
			name:    name,
			value:   val,
			shndx:   uint16(shndx),
			isFunc:  info&0xf == sttFunc,
			isSect:  info&0xf == sttSection,
			isFile:  info&0xf == sttFile,
			isLocal: info>>4 == stbLocal,
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
		// ARM-specific metadata sections (the exception index/table, the build
		// attributes, and the GNU-stack note) carry no executable bytes and no
		// C symbol the linker needs. The kernel loads only PT_LOAD segments and
		// never consults them for a plain C binary, so they are dropped rather
		// than routed into the output. Their own relocation sections then have
		// a dropped target section and are skipped by the relocation loop
		// below, which is the behaviour we want -- generating ARM unwind records
		// this linker does not model would be wrong.
		if isARMMetaSection(es.name) {
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
		case strings.HasPrefix(s.name, "$"):
			// An ARM/AArch64 mapping symbol -- $a, $t, $d, $x and the numbered
			// forms like $d.2 -- marks the bytes that follow it as code or as
			// data, the way the ARM ELF ABI defines them. They are always
			// local, never referenced by a relocation, and there is no C
			// identifier that can collide with one.
			//
			// They must be dropped rather than entered into the symbol table.
			// An AArch64 object has one in every section that holds code or
			// data, so a linker that counts them sees "$d" defined once by
			// .text and again by .rodata and stops with a duplicate-definition
			// error naming a symbol the source never mentioned. Treating them
			// as markers is what every real linker does.
			o.symKey[i] = ""
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
		case s.isLocal && s.shndx >= 1 && int(s.shndx) <= len(o.secs):
			// A local symbol is private to its object, so the ELF rules let
			// several of them share a name -- and LLVM's RISC-V backend takes
			// that option freely: one object emitted for a printf came out
			// with five separate `.L0` labels, all STB_LOCAL in the same
			// section. Treating those as globals made the link report a
			// duplicate definition of a symbol that is not duplicated at all.
			//
			// The key is qualified by the section AND the value rather than by
			// the name, which does two jobs at once: it keeps distinct locals
			// distinct, and it lets a relocation -- which arrives as a symbol
			// *index*, not a name -- reach the one it means. Two locals that
			// land on the same address are the same address, so merging those
			// is correct rather than merely convenient.
			gi := sectOf[s.shndx]
			if gi < 0 {
				o.symKey[i] = ""
				continue
			}
			key := fmt.Sprintf("__loc_%d_%d", s.shndx, s.value)
			o.symKey[i] = key
			if _, ok := img.Syms[key]; !ok {
				img.Syms[key] = SymLoc{Sect: gi, Off: baseOf[s.shndx] + int(s.value)}
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
	// ELF64 objects carry RELA relocations (a 24-byte entry with an explicit
	// addend); ELF32 objects carry REL relocations (an 8-byte entry with no
	// addend -- the addend lives in the instruction field and applyReloc reads
	// it back out). The entry layout and the r_info split differ by class, so
	// each branch below decodes the right width.
	is64 := o.class == elfClass64
	var unresolved []string
	for _, es := range o.secs {
		isRela := es.typ == shtRela
		if !isRela && es.typ != shtRel {
			continue
		}
		target := int(es.relSec)
		if target < 1 || target > len(o.secs) {
			continue
		}
		tsGi := sectOf[target]
		tsBase := baseOf[target]
		if tsGi < 0 {
			// The section the relocations apply to was dropped (an ARM meta
			// section, say), so there is nowhere to patch. Skip the whole table;
			// its fixups would have no target and the entry-point/BSS logic does
			// not depend on it.
			continue
		}
		for r := uint64(0); r < es.relCnt; r++ {
			var off int
			var symIdx int
			var typ uint32
			var addend int64
			if !isRela {
				ent := int(es.relOff + r*8)
				if ent+8 > len(src) {
					return fmt.Errorf("elf: relocation %d of %s out of range", r, es.name)
				}
				off = int(rd32(src, ent))
				info := rd32(src, ent+4)
				symIdx = int(info >> 8)
				typ = uint32(info & 0xff)
				addend = 0 // implicit; applyReloc reads it from the instruction
			} else if is64 {
				ent := int(es.relOff + r*24)
				if ent+24 > len(src) {
					return fmt.Errorf("elf: relocation %d of %s out of range", r, es.name)
				}
				off = int(rd64(src, ent))
				info := rd64(src, ent+8)
				addend = int64(rd64(src, ent+16))
				symIdx = int(info >> 32)
				typ = uint32(info & 0xffffffff)
			} else {
				ent := int(es.relOff + r*12)
				if ent+12 > len(src) {
					return fmt.Errorf("elf: relocation %d of %s out of range", r, es.name)
				}
				off = int(rd32(src, ent))
				info := rd32(src, ent+4)
				addend = int64(int32(rd32(src, ent+8)))
				symIdx = int(info >> 8)
				typ = uint32(info & 0xff)
			}
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
			switch o.machine {
			case emX8664:
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
					return fmt.Errorf("elf: unsupported x86-64 relocation type %d for %s (%s)", typ, es.name, key)
				}
			case emAArch64:
				switch typ {
				case rAARCH64None:
					// padding, no-op
				case rAARCH64_ABS64, rAARCH64_PREL32, rAARCH64_ADR_PREL_PG_HI21,
					rAARCH64_ADD_ABS_LO12_NC, rAARCH64_LDST8_ABS_LO12_NC,
					rAARCH64_LDST16_ABS_LO12_NC, rAARCH64_LDST32_ABS_LO12_NC,
					rAARCH64_LDST64_ABS_LO12_NC, rAARCH64_CALL26, rAARCH64_JUMP26:
					// AArch64 relocations are bit-field patches into instruction
					// words; they cannot live in the x86 Fixup model. Defer them to
					// applyReloc, which knows each type's encoding.
					img.Relocs = append(img.Relocs, Reloc{
						Machine: emAArch64, Type: typ, Sect: tsGi, Off: at, Sym: key, Addend: addend,
					})
				default:
					return fmt.Errorf("elf: unsupported AArch64 relocation type %d for %s (%s)", typ, es.name, key)
				}
			case emARM:
				switch typ {
				case rARMNone:
					// padding, no-op
				case rARMAbs32, rARMRel32, rARMCall, rARMJump24,
					rARMMovwAbsNC, rARMMovtAbs, rARMLdrPCG0, rARMLdrPcG1, rARMLdrPcG2,
					rARMALUPcRel7_0, rARMALUPcRel19_12, rARMALUPcRel11_8, rARMALUPcRel15_12,
					rARMPrel31, rARMV4BX:
					// ARM32 relocations are bit-field or branch-field patches; the
					// REL form carries no addend, so the addend is the value already
					// in the instruction field and applyReloc reads it back. Defer to
					// applyReloc, which knows each type's encoding.
					img.Relocs = append(img.Relocs, Reloc{
						Machine: emARM, Type: typ, Sect: tsGi, Off: at, Sym: key, Addend: addend,
					})
				default:
					return fmt.Errorf("elf: unsupported ARM32 relocation type %d for %s (%s)", typ, es.name, key)
				}
			case emRISCV:
				switch typ {
				case rRISCVNone, rRISCVRelax:
					// padding / relaxation hint -- nothing to patch
				case rRISCV64, rRISCV32, rRISCVHI20, rRISCVLO12I, rRISCVLO12S,
					rRISCVBranch, rRISCVJAL, rRISCVCall, rRISCVCallPLT:
					// RISC-V patch sites are bit fields inside instruction words
					// (or plain absolute data), and the HI20/LO12 pair cannot be
					// expressed in the x86 Fixup model at all. Defer to
					// applyReloc, which knows each type's encoding.
					img.Relocs = append(img.Relocs, Reloc{
						Machine: emRISCV, Type: typ, Sect: tsGi, Off: at, Sym: key, Addend: addend,
					})
				default:
					return fmt.Errorf("elf: unsupported RISC-V relocation type %d for %s (%s)", typ, es.name, key)
				}
			default:
				return fmt.Errorf("elf: unsupported machine 0x%x for relocation in %s", o.machine, es.name)
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
