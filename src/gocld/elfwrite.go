package gocld

import (
	"fmt"
	"sort"
	"strings"
)

// The ELF64 relocatable-object writer -- what `goc -c -target linux` produces.
//
// The ELF *executable* writer is elf.go; this is the other direction. The
// distinction matters because the two disagree about what an address is:
//
//   - In a linked executable a symbol's address is final. A PC-relative field
//     is measured from the byte after itself, and the loader supplies nothing
//     because the layout is already decided.
//   - In a relocatable object nothing is final. Sections have no addresses,
//     only offsets within themselves, and a reference to a symbol in another
//     object cannot be computed at all until that object is read.
//
// So the object records the *formula* rather than the answer: an ELF64 RELA
// entry says "add S to this field's address, adjust by A, relative to P", and
// the job of doing that arithmetic belongs to whoever links. This file's whole
// responsibility is to state each pending Fixup in that form without quietly
// dropping the ones it cannot express.
//
// Structure of the output:
//
//	ELF header
//	.text .rdata .data        (SHT_PROGBITS, or SHT_NOBITS for .bss)
//	.rela.text .rela.<sec>     (SHT_RELA, one per section that has relocations)
//	.symtab .strtab
//	.shstrtab
//	section headers
//
// There is no program header: ET_REL describes bytes to be combined, not an
// image to be loaded, so a p_offset/p_vaddr pair would be a claim about
// addresses the file does not have.

// Symbol binding, type and the special section indices, as st_info/st_shndx
// encode them.
const (
	elfStbLocal  = 0
	elfStbGlobal = 1

	elfSttNotype = 0
	elfSttObject = 1
	elfSttFile   = 4

	shnUndefObj = 0 // this object does not define it; someone else must
)

// elfLibSecName carries the C library symbol list, for the same reason the COFF
// writer rides it along in a symbol's name (see coffLibSymPrefix): goc has no
// library stage, so a link of several units has to be able to tell a second
// inlined copy of printf from a user who defined printf twice. ELF has a
// section to put that in, which is the more honest place for it -- the COFF
// trick exists only because COFF has nowhere else.
const elfLibSecName = ".goc_lib"

// elfSecOut is one section as it will be written.
type elfSecOut struct {
	name  string
	typ   uint32
	flags uint64
	align uint64
	// data is the section's bytes, or nil for SHT_NOBITS (.bss), whose size
	// comes from size and whose bytes the loader supplies.
	data []byte
	size uint64
	// rela holds this section's relocations. A section with none gets no
	// .rela.<name> section at all, which is what keeps an object with no
	// cross-references free of empty metadata.
	rela []elfRelaOut
}

// elfRelaOut is one relocation as it will be written: a field's offset within
// its section, the symbol it wants, how to combine them, and the constant term.
type elfRelaOut struct {
	off    uint64
	symIdx uint32
	typ    uint32
	addend int64
}

// WriteELFObject writes img as a relocatable ELF64 object (ET_REL) and returns
// the bytes.
//
// The Image must be unlinked -- fixups pending, not applied. Resolving them is
// what the linker does, and an object whose fixups had already been applied
// would carry addresses that are about to change: a file that reads back clean
// and links wrong. That is why this is a separate operation rather than
// BuildELF with a flag.
func WriteELFObject(img *Image) []byte {
	w := newELFWriter(img)

	secs := w.collectSections()
	w.collectSymbols(secs)
	w.collectRelocs(secs)
	return w.emit(secs)
}

// elfWriter accumulates an object file. The construction order is forced: a
// relocation names a symbol by index, so the symbol table must be complete
// before the relocations are numbered; and the section headers quote file
// offsets, so nothing can be placed until every section's contents are known.
type elfWriter struct {
	img *Image

	strTab  []byte // .strtab payload
	strUsed map[string]uint32

	symtab []elfSymOut
	// symIndex maps a name to its symbol index. Undefined symbols are entered
	// here too, so a second reference to the same undefined name shares one
	// index -- which is what makes it one symbol to be defined elsewhere
	// rather than two.
	symIndex map[string]uint32

	// secIdx maps an image section index to its object section index, so the
	// symbol and relocation passes can name a section without threading the
	// mapping through both.
	secIdx map[int]int

	// firstGlobal is the index in symtab where global symbols begin, which ELF
	// records in .symtab's sh_info. Computed while encoding.
	firstGlobal uint32

	shstr    []byte
	shstrUse map[string]uint32
}

// elfSymOut is one symbol as it will be written.
type elfSymOut struct {
	name  string
	info  uint8
	shndx uint16
	value uint64
}

func newELFWriter(img *Image) *elfWriter {
	return &elfWriter{
		img: img,
		// The string tables open with a NUL, because offset 0 means "no name"
		// in both of them: the null symbol's st_name and the NULL section's
		// sh_name. Starting the payload at offset 0 would make the first real
		// name unreadable -- a reader looking up offset 0 for the null symbol
		// would find a name there, and offset 1 for the next symbol would land
		// one byte into the middle of the first name.
		strTab:  []byte{0},
		strUsed: map[string]uint32{},
		// symIndex maps a name to its symbol index. Undefined symbols are
		// entered here too, so a second reference to the same undefined name
		// shares one index -- which is what makes it one symbol to be defined
		// elsewhere rather than two.
		symIndex: map[string]uint32{},
		shstrUse: map[string]uint32{},
		shstr:    []byte{0},
	}
}

// strOffset returns the .strtab offset of name, storing it once. Offset 0 is the
// table's leading NUL, which is the empty name -- the null symbol's -- so a real
// name never collides with it.
func (w *elfWriter) strOffset(name string) uint32 {
	if name == "" {
		return 0
	}
	if off, ok := w.strUsed[name]; ok {
		return off
	}
	off := uint32(len(w.strTab))
	w.strUsed[name] = off
	w.strTab = append(w.strTab, name...)
	w.strTab = append(w.strTab, 0)
	return off
}

// shstrOffset is strOffset for .shstrtab: the section names, whose offsets every
// section header quotes.
func (w *elfWriter) shstrOffset(name string) uint32 {
	if name == "" {
		return 0 // the NULL section has no name, and offset 0 is the empty string
	}
	if off, ok := w.shstrUse[name]; ok {
		return off
	}
	off := uint32(len(w.shstr))
	w.shstrUse[name] = off
	w.shstr = append(w.shstr, name...)
	w.shstr = append(w.shstr, 0)
	return off
}

// elfSectionFlags derives a section's sh_flags from what the image knows about
// it. ELF records permissions per section, where COFF records them per symbol
// type -- so the question "may this be written" is answered by the section here.
func elfSectionFlags(s *Section) (flags uint64, align uint64) {
	switch s.Name {
	case ".text":
		return shfAlloc | shfExecInstr, 16
	case ".rdata":
		return shfAlloc, 8
	case ".bss":
		return shfAlloc | shfWrite, 8
	default:
		return shfAlloc | shfWrite, 8
	}
}

// elfSectionName maps an image section to the name it carries in the object.
//
// TLS has its own sections here, and the distinction is not cosmetic: .tdata
// and .tbss are what a linker looks for when it builds a thread's TLS block,
// and folding them into .data/.bss would produce an object whose TLS references
// resolve into ordinary data. What the *linked* image then does with them is
// another question -- see BuildELF, which lays every section out in one RWX
// segment -- but the object should at least describe itself correctly, so that
// anything reading it learns what it was built from.
func elfSectionName(s *Section) string {
	switch s.Name {
	case ".tls":
		return ".tdata"
	case ".tbss":
		return ".tbss"
	}
	return s.Name
}

// collectSections turns the image's sections into the object layout, and returns
// the image-section index -> object-section index mapping.
func (w *elfWriter) collectSections() []*elfSecOut {
	secs := []*elfSecOut{nil} // index 0 is the NULL section, which has no header entry
	idx := make(map[int]int, len(w.img.Sections))
	for i, s := range w.img.Sections {
		if s.Unmapped {
			continue
		}
		flags, al := elfSectionFlags(s)
		es := &elfSecOut{
			name:  elfSectionName(s),
			flags: flags,
			align: al,
		}
		if s.Bss {
			// SHT_NOBITS: the section occupies address space but no file bytes.
			// Its size is the virtual size, since VSize advances for .bss while
			// Data stays empty -- that gap is exactly what NOBITS means.
			es.typ = shtNoBits
			es.size = uint64(s.VSize)
		} else {
			es.typ = shtProgBits
			es.data = s.Data
			es.size = uint64(len(s.Data))
		}
		idx[i] = len(secs)
		secs = append(secs, es)
	}
	w.secIdx = idx
	w.resolveShorts(secs)
	return secs
}

// resolveShorts writes every short fixup's displacement into the section bytes
// and returns; those fixups then need no relocation, which is why
// elfRelocFor refuses them rather than describing them.
//
// A one-byte displacement is PC-relative inside a single section, and both ends
// of it are offsets this file already knows, so the value is fully determined
// here and does not depend on where the link puts the section. ELF has no rel8
// relocation -- but it does not need one either.
//
// Dropping the fixup instead leaves the code generator's placeholder in the
// byte, and the program jumps to the wrong address: silent, and not an
// approximation but simply a wrong number.
func (w *elfWriter) resolveShorts(secs []*elfSecOut) {
	bySec := map[int][]Fixup{}
	for _, f := range w.img.Fixups {
		if f.Short {
			bySec[f.Sect] = append(bySec[f.Sect], f)
		}
	}
	if len(bySec) == 0 {
		return
	}
	for imgSect, fs := range bySec {
		si, ok := w.secIdx[imgSect]
		if !ok {
			continue
		}
		es := secs[si]
		if es.typ == shtNoBits || len(es.data) == 0 {
			continue
		}
		// Copy before writing: the Image belongs to the caller, which may still
		// link it, and a relocatable object must not consume pending fixups.
		data := append([]byte(nil), es.data...)
		touched := false
		for _, f := range fs {
			tgt, ok := w.img.Syms[f.Sym]
			if !ok || tgt.Sect != f.Sect || f.Off+1 > len(data) {
				continue
			}
			disp := tgt.Off + f.Addend - (f.Off + 1 + f.RipAdjust)
			if disp < -128 || disp > 127 {
				continue // out of range; the link reports it by name
			}
			data[f.Off] = byte(int8(disp))
			touched = true
		}
		if touched {
			es.data = data
		}
	}
}

// addSymbol appends a symbol and returns its index.
func (w *elfWriter) addSymbol(s elfSymOut) uint32 {
	w.symtab = append(w.symtab, s)
	return uint32(len(w.symtab) - 1)
}

// collectSymbols builds .symtab.
//
// ELF requires local symbols to precede global ones and records where the
// globals start in sh_info, so the table is built in that order rather than
// sorted: a section symbol out of place would make a linker treat every
// following symbol as local, and the object's references would resolve to
// nothing.
//
// The order within each group is sorted by name for the reason the COFF writer
// sorts: two builds of one source should produce identical bytes, or "did
// anything change?" cannot be answered by comparing two files.
func (w *elfWriter) collectSymbols(secs []*elfSecOut) {
	// Symbol 0 is the null symbol, which every table starts with. Relocation
	// entry 0 conventionally means "no symbol", and indexing from 1 past it
	// keeps that meaning available.
	w.addSymbol(elfSymOut{shndx: shnUndefObj})

	// STT_FILE names the object. A duplicate-definition diagnostic quotes it,
	// and readelf -s shows it, which is how a human tells two objects apart.
	if name := w.img.FileName; name != "" {
		w.addSymbol(elfSymOut{
			name:  name,
			info:  elfStbLocal<<4 | elfSttFile,
			shndx: shnUndefObj,
		})
	}

	// One section symbol per allocated section: "the address of this section",
	// which is what a relocation against a section names.
	for i := 1; i < len(secs); i++ {
		w.addSymbol(elfSymOut{
			name:  "",
			info:  elfStbLocal<<4 | sttSection,
			shndx: uint16(i),
		})
	}

	// Defined symbols. The linker's internal prefixes are skipped for the same
	// reason the COFF writer skips them: `IAT:x` is an import-address slot and
	// `thunk:x` a jump stub, both of which exist only in a linked image. An
	// object names the bare symbol and lets the link decide which of the two
	// forms applies.
	var defined []string
	for name := range w.img.Syms {
		if strings.HasPrefix(name, "IAT:") || strings.HasPrefix(name, "thunk:") {
			continue
		}
		if _, isDef := w.symIndex[name]; isDef {
			continue
		}
		defined = append(defined, name)
	}
	sort.Strings(defined)
	for _, name := range defined {
		loc := w.img.Syms[name]
		si, ok := w.secIdx[loc.Sect]
		if !ok {
			// The symbol lives in a section that was not emitted (Unmapped).
			// Its address still means something inside this object, but there
			// is nothing to anchor it to, and writing it as undefined would be a
			// lie -- it would look like something this object expects a linker
			// to supply. The unwind tables are the only sections in this state
			// and nothing refers to their symbols from outside.
			continue
		}
		w.addSymbol(elfSymOut{
			name:  name,
			info:  elfStbGlobal<<4 | elfSttObject,
			shndx: uint16(si),
			value: uint64(loc.Off),
		})
		w.symIndex[name] = uint32(len(w.symtab) - 1)
	}

	// Undefined symbols: what the object references and does not define. A
	// cross-unit call is one of these, and so is a name that a sibling object
	// will define -- at this stage the object cannot tell the difference, and
	// neither can it: that is the link's question, not this file's.
	//
	// Global, not local: a local undefined symbol would be unresolvable by
	// construction, and a static declaration cannot refer to something outside
	// this translation unit in the first place.
	seen := map[string]bool{}
	for _, f := range w.img.Fixups {
		for _, name := range []string{coffExternName(f.Sym), coffExternName(f.Sym2)} {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if _, already := w.symIndex[name]; already {
				continue
			}
			w.addSymbol(elfSymOut{
				name:  name,
				info:  elfStbGlobal<<4 | elfSttObject,
				shndx: shnUndefObj,
			})
			w.symIndex[name] = uint32(len(w.symtab) - 1)
		}
	}
}

// elfRelocFor translates one pending Fixup into an ELF relocation type and the
// addend that goes with it.
//
// The arithmetic the two halves use is stated here because it is the whole
// difficulty. goa's applyFixup computes
//
//	target + Addend - (base + Off + size + RipAdjust)
//
// measuring from the byte *after* the field, plus any instruction trailer. ELF
// R_X86_64_PC32 computes
//
//	S + A - P
//
// measuring P from the field itself. So for a field of size w the addend must
// absorb the difference:
//
//	A = Addend - w - RipAdjust
//
// Getting this wrong is the classic version of the bug: a `call write` resolves
// to write-4, every cross-section call lands just short of its target, and
// nothing complains until the program jumps into the middle of an instruction.
func elfRelocFor(f Fixup) (typ uint32, addend int64, err error) {
	switch {
	case f.Short:
		// A one-byte displacement has no usable x86-64 relocation -- and needs
		// none: resolveShorts has already written the value, because a jump
		// this short is relative to a position inside its own section and both
		// offsets are known before the link. See the comment there for why
		// dropping it instead would be worse than useless.
		return 0, 0, errShortResolved
	case f.Sym2 != "":
		// A jump-table entry wants target minus the table's base: two symbols,
		// one field. R_X86_64_PC32 names one. It could be worked around -- the
		// base is in the same section as the field, so their difference is a
		// constant this file could compute -- but goa's own code generator does
		// not emit these (it lowers a switch to compares and branches); they
		// arrive only from LLVM's AT&T output, which is ingested rather than
		// written here. So the case is refused instead of guessed.
		return 0, 0, fmt.Errorf("symbol-difference fixup for %q has no ELF relocation", f.Sym)
	case f.Absolute && f.Wide:
		// S + A, an 8-byte pointer.
		return rX8664_64, int64(f.Addend), nil
	case f.Absolute:
		// S + A, a 32-bit absolute address. The signed form rather than the
		// unsigned one: an absolute 32-bit field is a *32-bit* field, and
		// truncating through the unsigned type would turn a high address into a
		// different low one without any complaint.
		return rX8664_32S, int64(f.Addend), nil
	case f.Wide:
		// An 8-byte PC-relative field (the large code model, which goclib does
		// not use, but expressing it costs one line).
		return rX8664PC64, int64(f.Addend) - 8 - int64(f.RipAdjust), nil
	default:
		// The common case: a call, a jump, or a RIP-relative data reference.
		return rX8664PC32, int64(f.Addend) - 4 - int64(f.RipAdjust), nil
	}
}

// collectRelocs numbers every pending fixup against the symbol table.
func (w *elfWriter) collectRelocs(secs []*elfSecOut) {
	for _, f := range w.img.Fixups {
		si, ok := w.secIdx[f.Sect]
		if !ok {
			continue // the site is in a section that was not emitted
		}
		typ, addend, err := elfRelocFor(f)
		if err != nil {
			// Drop it rather than approximate. A wrong relocation links
			// silently and computes a wrong address; a missing one is loud --
			// the symbol stays undefined and the linker reports it by name.
			continue
		}
		// A jump-table style fixup names two symbols but stores one in Sym;
		// Sym2 is checked above and never reaches here, so Sym is the target.
		name := coffExternName(f.Sym)
		si2, known := w.symIndex[name]
		if !known {
			continue
		}
		secs[si].rela = append(secs[si].rela, elfRelaOut{
			off:    uint64(f.Off),
			symIdx: si2,
			typ:    typ,
			addend: addend,
		})
	}
}

// emit lays the object out and returns the bytes.
//
// The order of what follows the section contents is not arbitrary. Relocations
// name symbols by index, so .symtab must be complete; .strtab and .shstrtab
// are referenced by index rather than content, so they may follow; and the
// section headers quote a file offset for every section, so nothing can be
// placed until all of the above has a size.
//
// Which means every name has to be in .shstrtab before anything is placed: its
// length is part of the layout, and a name added while writing a header would
// grow the Go slice after the file was already filled from it. The result is a
// file whose section-name table is shorter than its headers claim -- which
// reads back as a header table full of string fragments, every name empty, and
// objdump printing "no symbols" for an object that has them. So the names go in
// first, as a step of their own, and the layout below can trust the size.
func (w *elfWriter) emit(secs []*elfSecOut) []byte {
	// The library-symbol table, a non-allocated section holding the list.
	libData := w.libSectionData()

	// Sections that go in the file, in order: the image's own, then one .rela
	// per section that has relocations, then the metadata. The .symtab index
	// and the string tables' indices are needed while building headers, so they
	// are assigned as positions are chosen.
	type placed struct {
		es   *elfSecOut
		off  int
		name string
		data []byte
	}
	var body []placed
	// The provisional (secs) index of each body entry, in the same order, so
	// the symbol pass can be told where each section actually landed.
	var bodyIdx []int
	// The .rela payloads, by section name, so the content pass below does not
	// rebuild them.
	relaData := map[string][]byte{}

	cur := elfEhSize
	// put places one section, recording which provisional index it came from so
	// the symbol pass can be told where it landed (see fileIdxOf).
	put := func(name string, es *elfSecOut, data []byte, si int) {
		al := int(es.align)
		if al < 1 {
			al = 1
		}
		cur = align(cur, al)
		body = append(body, placed{es: es, off: cur, name: name, data: data})
		bodyIdx = append(bodyIdx, si)
		cur += len(data)
	}

	for i := 1; i < len(secs); i++ {
		es := secs[i]
		if es.typ == shtNoBits || len(es.data) == 0 {
			// A .bss section occupies no file bytes. It still needs a header,
			// and its offset is where the *next* section begins -- which is
			// correct, since a symbol in it is at that address plus its own
			// offset, and the loader zero-fills from there on. The cursor still
			// aligns first, so the next real section starts aligned rather than
			// inheriting .bss's alignment.
			al := int(es.align)
			if al < 1 {
				al = 1
			}
			cur = align(cur, al)
			body = append(body, placed{es: es, off: cur, name: es.name})
			bodyIdx = append(bodyIdx, i)
			continue
		}
		put(es.name, es, es.data, i)
		if len(es.rela) > 0 {
			// The payload is built now rather than deferred, because its size is
			// what the cursor must advance by. Passing a placeholder length and
			// writing the bytes later would place the next section on top of
			// these -- which reads back as a section running past the end of the
			// file, and is silently wrong rather than loudly broken.
			rela := encodeRela(es)
			put(".rela"+es.name, es, rela, i)
			relaData[".rela"+es.name] = rela
		}
	}

	// The layout is chosen now, so the symbols' st_shndx can be corrected to
	// match it.
	//
	// They were provisionally set to the section's index in secs, and that is
	// almost never the index it ended up with in the file: a section with
	// relocations is followed by its own .rela section, which takes a slot. So
	// .text at secs[1] stays 1, but .data at secs[2] becomes 3 because .rela.text
	// took 2, and .rdata at secs[4] becomes 5. Every symbol in a section past the
	// first relocated one then names a section one or two earlier than its own.
	//
	// Nothing about the file looks wrong: the section headers are all correct,
	// the symbol table is well-formed, and each symbol's offset and value are
	// right. What is wrong is which section each symbol lives in -- so a
	// `lea [rip+X]` resolves to whatever sits at that address in a different
	// section. A string literal in .rdata then reads as the bytes of a function
	// in .text, and the program prints the right number of whatever
	// instructions happened to be there.
	//
	// The section symbols move with it. They name a section by carrying its
	// index, so a stale one sends a relocation against "this section" into a
	// different section entirely.
	fileIdx := fileIdxOf(bodyIdx)
	for i := range w.symtab {
		if real, ok := fileIdx[int(w.symtab[i].shndx)]; ok {
			w.symtab[i].shndx = uint16(real)
		}
	}

	symtab := w.encodeSymtab()
	strtab := w.strTab
	if libData != nil {
		put(elfLibSecName, &elfSecOut{typ: shtProgBits, align: 1}, libData, -1)
	}
	put(".symtab", &elfSecOut{typ: shtSymTab, align: 8}, symtab, -1)
	put(".strtab", &elfSecOut{typ: shtStrTab, align: 1}, strtab, -1)

	// Every section name is now known, so .shstrtab can be sized and placed like
	// any other -- and the header table that follows cannot land on top of it.
	for _, p := range body {
		w.shstrOffset(p.name)
	}
	w.shstrOffset(".shstrtab")
	shstr := w.shstr
	put(".shstrtab", &elfSecOut{typ: shtStrTab, align: 1}, shstr, -1)

	// The section indices the headers quote, taken after everything is placed.
	//
	// Each is len(body) at the moment its own put ran -- the NULL section is index
	// 0 and holds no data, so the first real section lands at 1. Working them out
	// in advance instead is off by one in a way that reads plausibly: e_shstrndx
	// would point at .symtab, whose bytes are a different string table, so every
	// section name would resolve to whatever symbol name happened to sit at that
	// offset -- or to nothing at all, and objdump would list an object with
	// seven unnamed sections.
	index := func(name string) int {
		for i, p := range body {
			if p.name == name {
				return i + 1
			}
		}
		return 0
	}
	symtabIdx := index(".symtab")
	strtabIdx := index(".strtab")
	shstrIdx := index(".shstrtab")

	numSh := len(body) + 1 // +1 for the NULL section, which has no data
	shOff := align(cur, 8)

	buf := make([]byte, shOff+numSh*elfShEntSize)

	// --- ELF header ---
	putU32at(buf, 0, elfIdentMagic)
	buf[4] = elfClass64
	buf[5] = elfDataLSB
	buf[6] = elfVersion
	buf[7] = elfOSABISysV
	buf[8] = 0 // ABI version
	putU16at(buf, 16, etRel)
	putU16at(buf, 18, emX8664)
	putU32at(buf, 20, elfVersion)
	// e_entry is 0: a relocatable object has no entry point. Only a linker,
	// holding every object, knows which symbol the program starts at.
	putU64at(buf, 24, 0)
	putU64at(buf, 32, 0)             // e_phoff: no program headers
	putU64at(buf, 40, uint64(shOff)) // e_shoff
	putU32at(buf, 48, 0)             // e_flags
	putU16at(buf, 52, elfEhSize)
	putU16at(buf, 54, 0) // e_phentsize
	putU16at(buf, 56, 0) // e_phnum
	putU16at(buf, 58, elfShEntSize)
	putU16at(buf, 60, uint16(numSh))
	putU16at(buf, 62, uint16(shstrIdx))

	// --- section contents ---
	for _, p := range body {
		copy(buf[p.off:], p.data)
	}

	// --- section headers ---
	for i, p := range body {
		h := shOff + i*elfShEntSize + elfShEntSize // skip the NULL header at shOff
		var typ uint32
		var flags uint64
		var al uint64
		var size, link, info, entsize uint32
		switch {
		case p.name == ".symtab":
			typ, flags, al = shtSymTab, 0, 8
			size = uint32(len(symtab))
			entsize = elfSymEntSize
			link = uint32(strtabIdx)
			// sh_info is where the global symbols begin. Every symbol from
			// here on is global; everything before is local to this object.
			info = w.firstGlobal
		case p.name == ".strtab":
			typ, flags, al = shtStrTab, 0, 1
			size = uint32(len(strtab))
		case p.name == ".shstrtab":
			typ, flags, al = shtStrTab, 0, 1
			size = uint32(len(shstr))
		case p.name == elfLibSecName:
			typ, flags, al = shtProgBits, 0, 1
			size = uint32(len(libData))
		case strings.HasPrefix(p.name, ".rela"):
			target := p.name[len(".rela"):]
			typ, flags, al = shtRela, 0, 8
			size = uint32(len(relaData[p.name]))
			entsize = 24
			// sh_link is .symtab (where the symbol indices are read from)
			// and sh_info is the section the entries apply to. Both are
			// already 1-based, the same numbering the section headers use.
			link = uint32(symtabIdx)
			info = uint32(w.targetIndex(secs, target))
		default:
			typ, flags, al = p.es.typ, p.es.flags, p.es.align
			if p.es.typ == shtNoBits {
				size = uint32(p.es.size)
			} else {
				size = uint32(len(p.es.data))
			}
		}
		putU32at(buf, h, w.shstrOffset(p.name))
		putU32at(buf, h+4, typ)
		putU64at(buf, h+8, flags)
		putU64at(buf, h+16, 0) // sh_addr: unknown until linked
		putU64at(buf, h+24, uint64(p.off))
		putU64at(buf, h+32, uint64(size))
		// sh_link and sh_info are the two 4-byte fields that follow sh_size.
		// They are 8-byte fields' neighbours here, which is the easy way to
		// be off by four: writing them at +36 and +40 puts them inside sh_size,
		// and a reader then sees a section whose length is billions of bytes.
		putU32at(buf, h+40, link)
		putU32at(buf, h+44, info)
		putU64at(buf, h+48, al)
		putU64at(buf, h+56, uint64(entsize))
	}

	return buf
}

// targetIndex finds the 1-based object section index of the section a .rela
// applies to. The result goes straight into sh_info, which is a section index
// rather than a position in `secs` -- so the NULL section's offset of one is
// added here rather than at every use.
func (w *elfWriter) targetIndex(secs []*elfSecOut, name string) int {
	for i := 1; i < len(secs); i++ {
		if secs[i].name == name {
			return i
		}
	}
	return 0
}

// encodeSymtab renders .symtab and records where the globals begin.
//
// The globals' position is found in a second pass rather than tracked as they
// are added, because sh_info has to name the first one and "the first global" is
// only knowable once the whole table exists. An object with no globals at all
// gets one past the end, which is the correct value for an empty range.
// fileIdxOf maps each section's provisional index to the index it occupies in
// the laid-out file.
//
// Two body entries can share a section -- it and its own .rela companion -- and
// the section must keep the earlier slot, so the first placement wins.
func fileIdxOf(bodyIdx []int) map[int]int {
	idx := make(map[int]int, len(bodyIdx))
	for file, si := range bodyIdx {
		if _, seen := idx[si]; !seen {
			idx[si] = file + 1
		}
	}
	return idx
}

func (w *elfWriter) encodeSymtab() []byte {
	w.firstGlobal = uint32(len(w.symtab))
	for i, s := range w.symtab {
		if s.info>>4 == elfStbGlobal {
			w.firstGlobal = uint32(i)
			break
		}
	}
	out := make([]byte, 0, len(w.symtab)*elfSymEntSize)
	for _, s := range w.symtab {
		// st_name, st_info, st_other, st_shndx, st_value, st_size -- 24 bytes,
		// and every field's position matters. Leaving st_other out shifts
		// shndx and value by a byte, and a symbol then names the wrong section
		// while looking perfectly well-formed: the object links, and every
		// reference resolves one byte off.
		out = appendU32(out, w.strOffset(s.name))
		out = append(out, s.info)
		out = append(out, 0) // st_other: visibility, which is default here
		out = append(out, byte(s.shndx), byte(s.shndx>>8))
		out = appendU64(out, s.value)
		out = appendU64(out, 0) // st_size: a linker does not need it here
	}
	return out
}

// appendU32 and appendU64 grow a byte slice by one little-endian field.
//
// The put*at family writes in place and so needs the destination to already be
// long enough. These build instead, which is what an accumulating buffer wants;
// the two exist because a relocatable object's sections are sized only once
// every symbol is known, and sizing them first would mean guessing.
func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendU64(b []byte, v uint64) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}

// encodeRela renders one section's relocations. es is the section the entries
// apply to -- the same one the .rela section's sh_info will name -- so the
// entries need no lookup of their own.
func encodeRela(es *elfSecOut) []byte {
	out := make([]byte, 0, len(es.rela)*24)
	for _, r := range es.rela {
		out = appendU64(out, r.off)
		// r_info packs the symbol index above the type: the index needs the
		// high 32 bits because it can exceed what the low half holds, and the
		// two are independent.
		out = appendU64(out, uint64(r.symIdx)<<32|uint64(r.typ))
		out = appendU64(out, uint64(r.addend))
	}
	return out
}

// libSectionData renders the C library symbol list, or nil when there is none.
func (w *elfWriter) libSectionData() []byte {
	if len(w.img.LibSyms) == 0 {
		return nil
	}
	names := make([]string, 0, len(w.img.LibSyms))
	for n := range w.img.LibSyms {
		names = append(names, n)
	}
	sort.Strings(names)
	return []byte(strings.Join(names, ",") + "\n")
}
