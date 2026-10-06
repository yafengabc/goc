package gocld

import (
	"encoding/binary"
	"testing"
)

// A round trip through the writer and the reader is the strongest thing this
// test can assert: the writer's whole job is to emit bytes the reader (and
// every real linker) accepts, and the fields most likely to be wrong are the
// ones a byte-for-byte comparison of a golden would happily lock in.

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func le64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

// elfSections parses the section headers of an ELF64 little-endian file.
func elfSections(t *testing.T, b []byte) map[string]struct {
	typ     uint32
	link    uint32
	info    uint32
	offset  uint64
	size    uint64
	entsize uint64
} {
	t.Helper()
	if len(b) < 64 {
		t.Fatalf("file is %d bytes, too short for an ELF header", len(b))
	}
	if b[0] != 0x7f || string(b[1:4]) != "ELF" {
		t.Fatalf("bad magic % x %q", b[:4], b[1:4])
	}
	if b[4] != 2 {
		t.Errorf("EI_CLASS = %d, want 2 (ELFCLASS64)", b[4])
	}
	if b[5] != 1 {
		t.Errorf("EI_DATA = %d, want 1 (little-endian)", b[5])
	}
	if typ := le16(b[16:]); typ != 1 {
		t.Errorf("e_type = %d, want 1 (ET_REL)", typ)
	}

	shoff := le64(b[40:])
	shentsize := le16(b[58:])
	shnum := le16(b[60:])
	shstrndx := le16(b[62:])
	if shoff == 0 || shnum == 0 {
		t.Fatalf("no section headers: shoff=0x%x shnum=%d", shoff, shnum)
	}
	if shstrndx >= shnum {
		t.Fatalf("e_shstrndx = %d, but there are only %d sections", shstrndx, shnum)
	}

	hdr := func(i int) []byte {
		off := shoff + uint64(i)*uint64(shentsize)
		if off+64 > uint64(len(b)) {
			t.Fatalf("section header %d at 0x%x extends past end of file (%d bytes)", i, off, len(b))
		}
		return b[off : off+64]
	}

	// The names all live in .shstrtab, so that one has to be located before
	// anything else can be labelled.
	sh := hdr(int(shstrndx))
	names := b[le64(sh[24:]):][:le64(sh[32:])]
	cstr := func(off uint32) string {
		if off >= uint32(len(names)) {
			t.Fatalf("string offset %d is past the end of a %d-byte table", off, len(names))
		}
		s := names[off:]
		for i, c := range s {
			if c == 0 {
				return string(s[:i])
			}
		}
		t.Fatalf("unterminated string at offset %d", off)
		return ""
	}

	out := map[string]struct {
		typ     uint32
		link    uint32
		info    uint32
		offset  uint64
		size    uint64
		entsize uint64
	}{}
	for i := 0; i < int(shnum); i++ {
		h := hdr(i)
		name := cstr(le32(h[0:]))
		off := le64(h[24:])
		size := le64(h[32:])
		if off+size > uint64(len(b)) {
			t.Errorf("section %q extends past end of file (offset 0x%x + size 0x%x > %d)",
				name, off, size, len(b))
		}
		out[name] = struct {
			typ     uint32
			link    uint32
			info    uint32
			offset  uint64
			size    uint64
			entsize uint64
		}{
			typ:     le32(h[4:]),
			link:    le32(h[40:]),
			info:    le32(h[44:]),
			offset:  off,
			size:    size,
			entsize: le64(h[56:]),
		}
	}
	return out
}

// TestWriteELFObjectHeader lays out a small image and checks the file is a
// well-formed ET_REL object with the sections, types and links it should have.
func TestWriteELFObjectHeader(t *testing.T) {
	img := NewImage(TargetELF)
	img.FileName = "unit.o"
	img.LibSyms = map[string]bool{"printf": true, "fwrite": true}

	text := newSection(img, ".text", false, true)
	text.Data = []byte{0x48, 0x89, 0xe5, 0xc3} // mov rbp,rsp ; ret
	text.VSize = len(text.Data)
	textIdx := sectionIndexOf(img, text)
	img.Syms["fa"] = SymLoc{Sect: textIdx, Off: 0}

	bss := newSection(img, ".bss", true, false)
	bss.Bss = true
	bss.VSize = 64 // virtual only: no bytes
	bssIdx := sectionIndexOf(img, bss)
	img.Syms["counter"] = SymLoc{Sect: bssIdx, Off: 16}

	obj := WriteELFObject(img)
	secs := elfSections(t, obj)

	for _, want := range []string{".text", ".bss", ".symtab", ".strtab", ".shstrtab", elfLibSecName} {
		if _, ok := secs[want]; !ok {
			t.Errorf("missing section %s; got %v", want, sectionNames(secs))
		}
	}

	// .text has no relocation yet (nothing in it refers out), so no .rela
	// section is emitted for it. Emitting an empty one is legal but wasteful,
	// and a reader that indexes the section headers by position has to cope.

	// .bss is SHT_NOBITS and must occupy no file bytes. Writing it out would
	// put zeroes in the file that the loader then zero-fills again -- harmless
	// for correctness, but it makes every object carry a copy of a buffer the
	// size of which is usually chosen at runtime.
	if got := secs[".bss"].typ; got != 8 {
		t.Errorf(".bss sh_type = %d, want 8 (SHT_NOBITS)", got)
	}
	if got := secs[".bss"].size; got != 64 {
		t.Errorf(".bss sh_size = %d, want 64", got)
	}

	// Every section index the headers quote is a FILE index, not an image one:
	// index 0 is the NULL section, so the first real section lands at 1. Writing
	// the image index instead points every reference one section early, and
	// since a section header is 64 bytes of plausible-looking fields, the file
	// still passes a header check and then resolves everything wrongly.
	//
	// So the check is positional: the section .symtab names as sh_link has to be
	// .strtab in the file's own section header table.
	idx := elfIndexOf(t, obj)
	sym := secs[".symtab"]
	link := int(sym.link)
	if link >= len(idx) {
		t.Fatalf(".symtab sh_link = %d, past the section header table", link)
	}
	if got := idx[link]; got != ".strtab" {
		t.Errorf(".symtab sh_link = %d, which is %q, want .strtab", link, got)
	}
	if sym.entsize != 24 {
		t.Errorf(".symtab sh_entsize = %d, want 24 (Elf64_Sym)", sym.entsize)
	}
	if sym.info == 0 {
		t.Errorf(".symtab sh_info = 0, but a global symbol must exist")
	}

	// .rela.text: entsize 24, sh_info naming the section it describes.
	if rela, ok := secs[".rela.text"]; ok {
		if rela.entsize != 24 {
			t.Errorf(".rela.text sh_entsize = %d, want 24 (Elf64_Rela)", rela.entsize)
		}
		if got := elfIndexOf(t, obj)[int(rela.info)]; got != ".text" {
			t.Errorf(".rela.text sh_info = %d, which is %q, want .text", rela.info, got)
		}
	}

	// .text must actually be there, byte for byte.
	th := secs[".text"]
	if got := obj[th.offset : th.offset+th.size]; string(got) != string(text.Data) {
		t.Errorf(".text = % x, want % x", got, text.Data)
	}

	// The library list rides along so that a later link can recognise a second
	// inlined copy of printf instead of calling it a duplicate definition.
	lib := secs[elfLibSecName]
	body := string(obj[lib.offset : lib.offset+lib.size])
	for _, want := range []string{"printf", "fwrite"} {
		if !containsStr(body, want) {
			t.Errorf(".goc_lib = %q, missing %q", body, want)
		}
	}
	// The object must not claim a library function is already there: it does
	// define inlined copies, but they are ordinary definitions, and the
	// deduplication is a link-time judgement.
	if _, listed := img.Syms["printf"]; listed {
		t.Error("printf was added to Syms by the writer; it should stay out")
	}
}

// TestWriteELFObjectStrings checks the one convention both string tables share:
// offset 0 is the empty name, so the tables have to begin with a NUL byte.
//
// Getting this wrong is the quietest failure in the format. Nothing complains:
// the first real name lands at offset 0, the null symbol points at a name
// instead of at nothing, and every symbol after the first reads one byte into
// the middle of someone else's name. readelf prints nonsense and the linker
// resolves references to whatever letters happen to be there.
func TestWriteELFObjectStrings(t *testing.T) {
	img := NewImage(TargetELF)
	img.FileName = "strings.o"
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0xc3}
	text.VSize = 1
	img.Syms["main"] = SymLoc{Sect: sectionIndexOf(img, text), Off: 0}

	obj := WriteELFObject(img)
	secs := elfSections(t, obj)

	for _, name := range []string{".strtab", ".shstrtab"} {
		s, ok := secs[name]
		if !ok {
			t.Fatalf("no %s", name)
		}
		if s.size == 0 {
			t.Fatalf("%s is empty", name)
		}
		if obj[s.offset] != 0 {
			t.Errorf("%s[0] = 0x%02x, want 0x00: offset 0 has to mean \"no name\"", name, obj[s.offset])
		}
	}

	// And the names really are readable through it.
	for _, want := range []string{".text", ".symtab", ".strtab", ".shstrtab"} {
		if _, ok := secs[want]; !ok {
			t.Errorf("section name %q did not resolve (got %v)", want, sectionNames(secs))
		}
	}

	sym := secs[".symtab"]
	syms := obj[sym.offset : sym.offset+sym.size]
	if len(syms)%24 != 0 {
		t.Fatalf(".symtab is %d bytes, not a multiple of 24", len(syms))
	}
	// Symbol 0 is the null symbol: st_name must be 0, or it names something.
	if n := le32(syms[0:]); n != 0 {
		t.Errorf("symbol 0 st_name = %d, want 0", n)
	}
	if shndx := le16(syms[6:]); shndx != 0 {
		t.Errorf("symbol 0 st_shndx = %d, want 0 (SHN_UNDEF)", shndx)
	}
}

// TestWriteELFObjectSymbolLayout pins the Elf64_Sym field offsets by looking up
// a symbol whose every field is known.
//
// The layout is st_name(4) st_info(1) st_other(1) st_shndx(2) st_value(8)
// st_size(8) = 24 bytes. st_other is one byte in the middle of it, which is
// exactly the kind of field that gets dropped in a rewrite: the entry then
// still totals 24 bytes -- st_size can absorb the loss -- so nothing looks
// wrong, and every symbol instead names a section one byte off and carries a
// value one byte off, which reads as a program that links cleanly and computes
// every address wrong.
func TestWriteELFObjectSymbolLayout(t *testing.T) {
	img := NewImage(TargetELF)
	img.FileName = "layout.o"
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0x90, 0xc3, 0x90, 0xc3} // nop; ret; nop; ret
	text.VSize = 4
	ti := sectionIndexOf(img, text)

	img.Syms["first"] = SymLoc{Sect: ti, Off: 0}
	img.Syms["second"] = SymLoc{Sect: ti, Off: 2}

	obj := WriteELFObject(img)
	secs := elfSections(t, obj)
	sym := secs[".symtab"]
	syms := obj[sym.offset : sym.offset+sym.size]

	strtab := secs[".strtab"]
	strs := obj[strtab.offset : strtab.offset+strtab.size]

	// The string table is addressed by offset, not NUL-terminated as a whole,
	// so a name runs from its st_name to the next NUL byte.
	for i := 0; i*24 < len(syms); i++ {
		e := syms[i*24 : i*24+24]
		nameOff := le32(e[0:])
		if nameOff >= uint32(len(strs)) {
			t.Fatalf("symbol %d st_name = %d, past the %d-byte string table", i, nameOff, len(strs))
		}
		name := cstringAt(strs[nameOff:])
		if name == "" {
			continue // the null symbol, and the unnamed section symbols
		}
		info := e[4]
		other := e[5]
		shndx := le16(e[6:])
		value := le64(e[8:])
		// st_other is visibility; nothing here overrides it, so it is zero.
		if other != 0 {
			t.Errorf("%s: st_other = %d, want 0", name, other)
		}
		stt := info & 0xf
		// The writer marks image symbols STT_OBJECT rather than NOTYPE: these
		// are code and data, and saying so is more useful than admitting ignorance.
		// Section symbols and the STT_FILE entry are the other two types here.
		if stt != sttSection && stt != elfSttObject && stt != elfSttFile {
			t.Errorf("%s: st_info type = %d, want OBJECT, SECTION or FILE", name, stt)
		}
		if stt == sttSection || stt == elfSttFile {
			continue
		}
		loc, isOurs := img.Syms[name]
		if !isOurs {
			t.Errorf("symbol %q is not in the image but was written as defined", name)
			continue
		}
		if shndx == shnUndefObj {
			t.Errorf("%s: st_shndx = 0, but the image defines it", name)
		}
		// st_shndx is a FILE index, so the image index plus one: index 0 is the
		// NULL section.
		if want := uint16(ti + 1); shndx != want {
			t.Errorf("%s: st_shndx = %d, want %d (.text)", name, shndx, want)
		}
		// And the value has to be the offset the image recorded, byte for byte
		// -- which is exactly what a one-byte shift in the layout would break.
		if want := loc.Off; want != int(value) {
			t.Errorf("%s: st_value = %d, want %d -- the symbol table is shifted by %d byte(s)",
				name, value, want, int(value)-want)
		}
	}

	// Round trip: the reader must find both names with their offsets.
	obj2, err := parseELF(obj)
	if err != nil {
		t.Fatalf("the writer's own reader rejects the object: %v", err)
	}
	for name, want := range map[string]int{"first": 0, "second": 2} {
		found := false
		for _, s := range obj2.syms {
			if s.name != name {
				continue
			}
			found = true
			if int(s.value) != want {
				t.Errorf("round trip %s: st_value = %d, want %d", name, s.value, want)
			}
			if int(s.shndx) != ti+1 {
				t.Errorf("round trip %s: st_shndx = %d, want %d", name, s.shndx, ti+1)
			}
		}
		if !found {
			t.Errorf("round trip: symbol %q missing from the parsed object", name)
		}
	}
}

// TestWriteELFObjectUndefinedSymbols covers the case that makes an object
// useful at all: a name this unit uses and does not define must be recorded as
// undefined, for a sibling object to satisfy.
func TestWriteELFObjectUndefinedSymbols(t *testing.T) {
	img := NewImage(TargetELF)
	img.FileName = "uses.o"
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0xe8, 0, 0, 0, 0, 0xc3}
	text.VSize = 6
	ti := sectionIndexOf(img, text)
	img.Syms["start"] = SymLoc{Sect: ti, Off: 0}

	// A PC-relative call to a function in another translation unit. goa emits
	// the fixup with RipAdjust 0 for a call: the displacement field ends where
	// the next instruction begins, so there is no trailer to skip.
	img.Fixups = append(img.Fixups, Fixup{
		Sect: ti, Off: 1, Sym: "helper",
	})

	obj := WriteELFObject(img)
	secs := elfSections(t, obj)
	rela := secs[".rela.text"]
	if rela.size == 0 {
		t.Fatal("the call produced no relocation")
	}
	if rela.size%24 != 0 {
		t.Fatalf(".rela.text is %d bytes, not a multiple of 24", rela.size)
	}

	// r_offset is where the field is, and it must be the offset inside .text
	// (which is what a linker resolves against the section base), not an
	// address.
	r := obj[rela.offset : rela.offset+24]
	if off := le64(r[0:]); off != 1 {
		t.Errorf("r_offset = %d, want 1 (the displacement field)", off)
	}
	info := le64(r[8:])
	symIdx := info >> 32
	typ := uint32(info)
	if typ != 2 {
		t.Errorf("r_type = %d, want 2 (R_X86_64_PC32)", typ)
	}
	// A call's displacement is measured from the byte after the field, so the
	// addend is -4: P points at the field itself, and S-A-P has to land on the
	// next instruction. goa computes from the end of the field, so the writer
	// folds the two conventions together here (see elfRelocFor).
	if a := int64(le64(r[16:])); a != -4 {
		t.Errorf("r_addend = %d, want -4 for a call", a)
	}

	// The symbol it names has to be undefined, and it has to be findable.
	sym := secs[".symtab"]
	syms := obj[sym.offset : sym.offset+sym.size]
	if int(symIdx)*24+24 > len(syms) {
		t.Fatalf("r_sym = %d is past the %d-entry symbol table", symIdx, len(syms)/24)
	}
	e := syms[int(symIdx)*24 : int(symIdx)*24+24]
	if shndx := le16(e[6:]); shndx != shnUndefObj {
		t.Errorf("helper st_shndx = %d, want 0 (undefined)", shndx)
	}
	if bind := e[4] >> 4; bind != elfStbGlobal {
		t.Errorf("helper binding = %d, want %d (global)", bind, elfStbGlobal)
	}

	// And the published helper says so, which is what a caller reads.
	undef, err := UndefinedELFSymbols(obj)
	if err != nil {
		t.Fatalf("UndefinedELFSymbols: %v", err)
	}
	found := false
	for _, n := range undef {
		if n == "helper" {
			found = true
		}
	}
	if !found {
		t.Errorf("UndefinedELFSymbols = %v, missing \"helper\"", undef)
	}
}

func cstringAt(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return ""
}

// elfIndexOf returns the section names of an ELF file in file-index order, so a
// header field that claims to point at section N can be resolved and named.
func elfIndexOf(t *testing.T, b []byte) []string {
	t.Helper()
	shoff := le64(b[40:])
	shentsize := le16(b[58:])
	shnum := le16(b[60:])
	shstrndx := le16(b[62:])
	hdr := func(i int) []byte {
		off := shoff + uint64(i)*uint64(shentsize)
		if off+64 > uint64(len(b)) {
			t.Fatalf("section header %d at 0x%x extends past end of file", i, off)
		}
		return b[off : off+64]
	}
	sh := hdr(int(shstrndx))
	names := b[le64(sh[24:]):][:le64(sh[32:])]
	out := make([]string, shnum)
	for i := range out {
		out[i] = cstringAt(names[le32(hdr(i)[0:]):])
	}
	return out
}

func sectionNames(secs map[string]struct {
	typ     uint32
	link    uint32
	info    uint32
	offset  uint64
	size    uint64
	entsize uint64
}) []string {
	out := make([]string, 0, len(secs))
	for n := range secs {
		out = append(out, n)
	}
	return out
}

func containsStr(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
// TestWriteELFObjectShndxFollowsLayout pins st_shndx to the index the section
// has in the *file*, not the provisional index it had before the layout was
// chosen.
//
// A section with relocations is followed by its own .rela section, which takes
// a slot in the section header table. So .text is at index 1 either way, but
// .data moves from 2 to 3, .bss from 3 to 4 and .rdata from 4 to 5 -- and every
// symbol in them keeps the old number unless something corrects it.
//
// The result is a file in which nothing looks wrong: the section headers are
// right, the symbol table is well-formed, and each symbol's offset and value
// are right. Only *which section* each symbol claims to be in is wrong, so a
// `lea [rip+string]` resolves to the bytes of a function in .text and the
// program prints the right number of whatever instructions live there. The
// single-section tests cannot see this: with no relocations there is no .rela
// section, nothing shifts, and the provisional indices happen to be right.
func TestWriteELFObjectShndxFollowsLayout(t *testing.T) {
	img := NewImage(TargetELF)
	img.FileName = "layout.o"

	// The image's sections, in the order goa emits them: .text first, then the
	// data sections. The object will interleave .rela.text right after .text.
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0xe8, 0, 0, 0, 0, 0xc3} // call rel32; ret
	text.VSize = 6
	ti := sectionIndexOf(img, text)
	img.Syms["start"] = SymLoc{Sect: ti, Off: 0}
	// A RIP-relative reference from .text, so .text has a .rela section.
	img.Fixups = append(img.Fixups, Fixup{Sect: ti, Off: 1, Sym: "helper"})

	// The string literal lives in .rdata, which is the section most likely to be
	// reached by the shift: it is last in the image, so it moves the most.
	rdata := newSection(img, ".rdata", false, false)
	rdata.Data = []byte("hello, world\n\x00")
	rdata.VSize = len(rdata.Data)
	img.Syms["msg"] = SymLoc{Sect: sectionIndexOf(img, rdata), Off: 0}

	data := newSection(img, ".data", true, false)
	data.Data = []byte{1, 2, 3, 4}
	data.VSize = 4
	img.Syms["counter"] = SymLoc{Sect: sectionIndexOf(img, data), Off: 0}

	bss := newSection(img, ".bss", true, false)
	bss.Bss = true
	bss.VSize = 32
	img.Syms["buffer"] = SymLoc{Sect: sectionIndexOf(img, bss), Off: 16}

	obj := WriteELFObject(img)
	idx := elfIndexOf(t, obj)
	secs := elfSections(t, obj)

	// The file's section order, stated so a change in layout shows up here
	// rather than as a mysterious test failure further down.
	want := []string{"", ".text", ".rela.text", ".rdata", ".data", ".bss", ".symtab", ".strtab", ".shstrtab"}
	if len(idx) != len(want) {
		t.Fatalf("object has %d sections (%v), want %d (%v)", len(idx), idx, len(want), want)
	}
	for i := range want {
		if idx[i] != want[i] {
			t.Errorf("section %d is %q, want %q (full: %v)", i, idx[i], want[i], idx)
			break
		}
	}

	// Every defined symbol must name the section it is actually in.
	strtab := secs[".strtab"]
	strs := obj[strtab.offset : strtab.offset+strtab.size]
	sym := secs[".symtab"]
	syms := obj[sym.offset : sym.offset+sym.size]

	for name, wantSec := range map[string]string{
		"start":   ".text",
		"msg":     ".rdata",
		"counter": ".data",
		"buffer":  ".bss",
	} {
		loc := img.Syms[name]
		found := false
		for i := 0; i*24 < len(syms); i++ {
			e := syms[i*24 : i*24+24]
			if cstringAt(strs[le32(e[0:]):]) != name {
				continue
			}
			found = true
			if got := idx[le16(e[6:])]; got != wantSec {
				t.Errorf("%s: st_shndx names section %q, want %q -- it names the wrong section,"+
					" so every reference to it resolves into %q", name, got, wantSec, got)
			}
			if int(le64(e[8:])) != loc.Off {
				t.Errorf("%s: st_value = %d, want %d", name, le64(e[8:]), loc.Off)
			}
		}
		if !found {
			t.Errorf("symbol %q missing from the object", name)
		}
	}

	// The section symbols have to move too: one names a section by carrying its
	// index, so a stale one sends a relocation against "this section" into a
	// different section entirely.
	sectSyms := 0
	for i := 0; i*24 < len(syms); i++ {
		e := syms[i*24 : i*24+24]
		if e[4]&0xf != sttSection {
			continue
		}
		sectSyms++
		shndx := int(le16(e[6:]))
		if shndx >= len(idx) || idx[shndx] == "" {
			t.Errorf("section symbol %d carries index %d, which is not a section", i, shndx)
			continue
		}
		// st_value of a section symbol is the section's address, which is
		// meaningless in a relocatable object -- but it must be 0 rather than
		// an offset from some other section.
		if v := le64(e[8:]); v != 0 {
			t.Errorf("section symbol %d (section %q) has st_value = %d, want 0",
				i, idx[shndx], v)
		}
	}
	if sectSyms == 0 {
		t.Error("no section symbols in the object")
	}
}

// TestWriteELFObjectShortJumpIsResolvedInPlace pins the same property on the ELF
// side that TestCOFFObjectShortJumpHasNoRelocation pins on the COFF one: a
// one-byte displacement appears in the section bytes and gets no relocation.
//
// ELF has no rel8 relocation type, and the easy thing to do with a fixup that
// has no type is drop it. Dropping it leaves the code generator's placeholder
// in the byte, so `jmp short` lands one instruction past its target -- and
// nothing says so. The program links, loads and runs; it just takes the wrong
// branch, which is why this shows up as a failing self-check in a test program
// rather than as a link error.
//
// The value is knowable here for the same reason it is on the COFF side: a jump
// that short is relative to a position inside its own section, and both offsets
// are settled before anything is linked.
func TestWriteELFObjectShortJumpIsResolvedInPlace(t *testing.T) {
	img := NewImage(TargetELF)
	img.FileName = "short.o"

	text := newSection(img, ".text", false, true)
	// eb 00   jmp short +0     (fixup at offset 1)
	// 90      nop
	// 90      nop<- label
	text.Data = []byte{0xEB, 0x00, 0x90, 0x90}
	text.VSize = len(text.Data)
	ti := sectionIndexOf(img, text)
	img.Syms["start"] = SymLoc{Sect: ti, Off: 0}
	img.Syms["label"] = SymLoc{Sect: ti, Off: 3}
	img.Fixups = []Fixup{{Sect: ti, Off: 1, Sym: "label", Short: true}}

	obj := WriteELFObject(img)
	idx := elfIndexOf(t, obj)
	secs := elfSections(t, obj)
	text_ := secs[".text"]
	body := obj[text_.offset : text_.offset+text_.size]

	if len(body) != 4 {
		t.Fatalf(".text is %d bytes, want 4", len(body))
	}
	if body[1] != 0x01 {
		t.Errorf("short jump displacement = %#02x, want 0x01 -- a zero here is the"+
			" placeholder the code generator left, and the jump lands on the byte after the label", body[1])
	}

	// And no relocation may name it: a rel8 has no type, so a record here would
	// be a record the linker cannot apply.
	for i, name := range idx {
		if name != ".rela.text" {
			continue
		}
		if ents := secs[name].size / 24; ents != 0 {
			t.Errorf(".rela.text has %d entries, want 0: the short jump is already in the bytes", ents)
		}
		_ = i
	}
}
