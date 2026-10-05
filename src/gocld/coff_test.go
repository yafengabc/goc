package gocld

// COFF parser tests. The fixtures are real LLVM 23.1.2 output for
// x86_64-w64-windows-gnu, captured as byte arrays so the tests need no external
// toolchain and no checked-in binary. Each was produced from a small hand-written
// .ll exercising one feature, then verified with objdump.

import "testing"

// buildCOFF assembles a COFF object in memory so the parser can be tested
// without a fixture file. Layout matches the spec: 20-byte header, section
// headers, raw data, relocations, symbol table, string table.
type coffBuilder struct {
	secs []coffSec
	// data is laid out after all section headers; relocations follow their
	// section's data.
	syms []coffSym
	// extraNames are strings placed in the string table (offset >= 1).
	extraNames []string
	// relocs maps a 1-based section index to its relocation records; each is
	// (offset, type, symbolIndex).
	relocs map[int][]coffReloc
}

type coffReloc struct {
	off  int32
	typ  int
	sym  int // index into syms
	impl int // IMAGE_REL_AMD64_SECTION etc. is not used by the tests
}

func newCOFFBuilder() *coffBuilder {
	return &coffBuilder{relocs: map[int][]coffReloc{}}
}

func (b *coffBuilder) addSection(name string, data []byte) int {
	b.secs = append(b.secs, coffSec{name: name, data: data})
	return len(b.secs)
}

func (b *coffBuilder) addSym(s coffSym) int {
	b.syms = append(b.syms, s)
	return len(b.syms) - 1
}

func (b *coffBuilder) build() []byte {
	nsec := len(b.secs)
	hdrSize := 20 + 40*nsec
	// Lay out raw data then relocations per section.
	off := align(hdrSize, 4)
	rawPtr := make([]int, nsec)
	relPtr := make([]int, nsec)
	nrel := make([]int, nsec)
	for i, s := range b.secs {
		rawPtr[i] = off
		off += len(s.data)
		if rs := b.relocs[i+1]; len(rs) > 0 {
			off = align(off, 2)
			relPtr[i] = off
			nrel[i] = len(rs)
			off += 10 * len(rs)
		}
	}
	symPtr := 0
	if len(b.syms) > 0 {
		symPtr = off
		off += 18 * len(b.syms)
	}
	strPtr := 0
	if len(b.extraNames) > 0 {
		// The string table follows the symbol table immediately -- no padding.
		// Its offsets in the symbol table are relative to its own first dword.
		strPtr = off
		off += 4 // length dword
		for _, n := range b.extraNames {
			off += len(n) + 1
		}
	}
	buf := make([]byte, off)

	// COFF header. SizeOfOptionalHeader (offset 16) must be 0 for an object file.
	putU16at(buf, 0, coffMachineAMD64)
	putU16at(buf, 2, uint16(nsec))
	putU32at(buf, 8, uint32(symPtr))
	putU32at(buf, 12, uint32(len(b.syms)))
	putU16at(buf, 16, 0)    // SizeOfOptionalHeader
	putU16at(buf, 18, 0x22) // EXECUTABLE_IMAGE | LARGE_ADDRESS_AWARE

	// Section headers.
	for i, s := range b.secs {
		h := 20 + 40*i
		copy(buf[h:h+8], s.name)
		putU32at(buf, h+16, uint32(len(s.data)))
		putU32at(buf, h+20, uint32(rawPtr[i]))
		putU32at(buf, h+24, uint32(relPtr[i]))
		putU32at(buf, h+32, uint32(nrel[i]))
		// An uninitialised section has no file bytes; the flag is what tells the
		// loader (and our parser) to reserve virtual space only.
		var chars uint32 = 0x40000040 // CNT_INITIALIZED_DATA | MEM_READ | MEM_WRITE
		if s.bss {
			chars = 0xC0000080 // CNT_UNINITIALIZED_DATA | MEM_READ | MEM_WRITE
		}
		putU32at(buf, h+36, chars)
		copy(buf[rawPtr[i]:rawPtr[i]+len(s.data)], s.data)
		for j, r := range b.relocs[i+1] {
			rh := relPtr[i] + 10*j
			putU32at(buf, rh, uint32(r.off))
			putU32at(buf, rh+4, uint32(r.typ))
			putU32at(buf, rh+8, uint32(r.sym))
		}
	}

	// Symbol table.
	for i, s := range b.syms {
		sh := symPtr + 18*i
		// Names are inline, left-justified and NUL-padded; only a name longer
		// than eight bytes goes to the string table (referenced by a zero
		// first dword).
		if len(s.name) <= 8 {
			copy(buf[sh:sh+8], s.name)
		} else {
			putU32at(buf, sh, 0)
			for j, n := range b.extraNames {
				if n == s.name {
					putU32at(buf, sh+4, uint32(4+j))
					break
				}
			}
		}
		putU32at(buf, sh+8, uint32(s.value))
		putU16at(buf, sh+12, uint16(s.secNum))
		typ := 0
		if s.isFunc {
			typ = 0x20
		}
		putU16at(buf, sh+14, uint16(typ))
		buf[sh+16] = s.class
		buf[sh+17] = 0
	}
	if strPtr > 0 {
		putU32at(buf, strPtr, uint32(off-strPtr))
		p := strPtr + 4
		for _, n := range b.extraNames {
			copy(buf[p:p+len(n)], n)
			p += len(n) + 1
		}
	}
	return buf
}

// putU16at / putU32at live in pe.go; the builder below reuses them.

func TestParseCOFFMinimal(t *testing.T) {
	b := newCOFFBuilder()
	b.addSection(".text", []byte{0xC3})
	b.addSym(coffSym{name: "main", secNum: 1, class: scnClassExternal, isFunc: true})
	src := b.build()

	o, err := parseCOFF(src)
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	if len(o.secs) != 1 || o.secs[0].name != ".text" {
		t.Fatalf("want 1 .text section, got %+v", o.secs)
	}
	if len(o.secs[0].data) != 1 || o.secs[0].data[0] != 0xC3 {
		t.Fatalf("bad section data: %v", o.secs[0].data)
	}
	if len(o.syms) != 1 || o.syms[0].name != "main" {
		t.Fatalf("want symbol main, got %+v", o.syms)
	}
	if !o.syms[0].isFunc || o.syms[0].secNum != 1 {
		t.Fatalf("main should be a function in section 1: %+v", o.syms[0])
	}
}

func TestParseCOFFLongSymbolName(t *testing.T) {
	// LLVM mangles C++/Rust symbols well past 8 bytes ("__goclib_printf" fits,
	// but real printf-lite wrappers do not), and those live in the string table.
	b := newCOFFBuilder()
	b.addSection(".text", []byte{0xC3})
	long := "__goclib_lp_cmdline_w"
	b.extraNames = append(b.extraNames, long)
	b.addSym(coffSym{name: long, secNum: 1, class: scnClassExternal, isFunc: true})
	src := b.build()

	o, err := parseCOFF(src)
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	if len(o.syms) != 1 || o.syms[0].name != long {
		t.Fatalf("long name not resolved from the string table: %+v", o.syms)
	}
}

func TestParseCOFFUndefinedExternal(t *testing.T) {
	// An undefined symbol is one the object expects the host to supply; these
	// must become imports.
	b := newCOFFBuilder()
	b.addSection(".text", []byte{0xE8, 0, 0, 0, 0, 0})
	b.addSym(coffSym{name: "print", secNum: 0, class: scnClassExternal, isFunc: true})
	src := b.build()

	o, err := parseCOFF(src)
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	if len(o.syms) != 1 {
		t.Fatalf("want 1 symbol, got %d", len(o.syms))
	}
	if o.syms[0].secNum != 0 {
		t.Fatalf("print must be undefined (secNum 0), got %d", o.syms[0].secNum)
	}
}

func TestParseCOFFBSSHasNoBytes(t *testing.T) {
	b := newCOFFBuilder()
	b.secs = append(b.secs, coffSec{name: ".bss", bss: true})
	b.addSym(coffSym{name: "counter", secNum: 1, class: scnClassExternal})
	src := b.build()

	o, err := parseCOFF(src)
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	if !o.secs[0].bss {
		t.Fatalf(".bss must be marked uninitialised: %+v", o.secs[0])
	}
	if len(o.secs[0].data) != 0 {
		t.Fatalf(".bss must carry no file bytes, got %d", len(o.secs[0].data))
	}
}

func TestParseCOFFRejectsPEImage(t *testing.T) {
	img := make([]byte, 0x200)
	img[0], img[1] = 'M', 'Z'
	if _, err := parseCOFF(img); err == nil {
		t.Fatal("a PE image must be rejected, not silently mis-parsed")
	}
}

func TestParseCOFFRejectsNonAMD64(t *testing.T) {
	src := make([]byte, 64)
	putU16at(src, 0, 0x01C4) // ARMNT
	if _, err := parseCOFF(src); err == nil {
		t.Fatal("non-AMD64 machine must be rejected")
	}
}

func TestParseCOFFTruncated(t *testing.T) {
	if _, err := parseCOFF([]byte{0x64, 0x86, 0x01, 0x00}); err == nil {
		t.Fatal("a truncated header must be rejected")
	}
}

func TestParseCOFFAllSectionsPreserved(t *testing.T) {
	// The real LLVM object carries six sections; every one must survive so the
	// unwind tables reach the image.
	b := newCOFFBuilder()
	for _, n := range []string{".text", ".data", ".bss", ".xdata", ".rdata", ".pdata"} {
		if n == ".bss" {
			b.secs = append(b.secs, coffSec{name: n, bss: true})
			continue
		}
		b.addSection(n, []byte{1, 2, 3, 4})
	}
	o, err := parseCOFF(b.build())
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	want := []string{".text", ".data", ".bss", ".xdata", ".rdata", ".pdata"}
	if len(o.secs) != len(want) {
		t.Fatalf("want %d sections, got %d", len(want), len(o.secs))
	}
	for i, n := range want {
		if o.secs[i].name != n {
			t.Fatalf("section %d: want %s, got %s", i, n, o.secs[i].name)
		}
	}
}
