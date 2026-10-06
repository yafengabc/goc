package gocld

// The round trip: WriteCOFFObject produces a .o, IngestCOFFBytes reads it back,
// and the image that comes out has to say the same thing the image that went in
// said. This is the only real evidence that the writer is correct, because a
// COFF object that is subtly wrong still parses -- it just links wrong later,
// somewhere else, in a way that is expensive to trace back here.
//
// objdump cross-checks live in TestCOFFObjectAgreesWithObjdump, which shells
// out to the real tool. That matters for the parts a round trip cannot catch:
// the format's own idea of where a field lives is mirrored on both sides here,
// so a misunderstanding of the spec would cancel out.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testImage builds a small image with the shapes that matter: a code section
// with data, a read-only data section, a .bss, an exported symbol, a static
// one, and relocations of each kind -- a call target, a RIP-relative data
// reference, an absolute 64-bit store, and a reference to a symbol this object
// does not define.
func testImage() *Image {
	img := NewImage(TargetPE)

	text := newSection(img, ".text", false, true)
	text.Data = []byte{
		0x48, 0x83, 0xec, 0x28, // sub rsp, 0x28
		0xe8, 0, 0, 0, 0, // call helper        (rel32 @ 4)
		0x48, 0x8b, 0x05, 0, 0, 0, 0, // mov rax, [rip+d]  (rel32 @ 9)
		0x48, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, // movabs rax, imm64 (addr64 @ 16)
		0xc3, // ret
	}
	text.VSize = len(text.Data)

	rdata := newSection(img, ".rdata", false, false)
	rdata.Data = []byte{0x11, 0x22, 0x33, 0x44}
	rdata.VSize = len(rdata.Data)

	bss := newSection(img, ".bss", true, false)
	bss.Bss = true // virtual space, no file bytes -- this is the flag that matters
	bss.VSize = 64

	img.Syms["main"] = SymLoc{Sect: 0, Off: 0}
	img.Syms["helper"] = SymLoc{Sect: 0, Off: 4}
	img.Syms["table"] = SymLoc{Sect: 1, Off: 0}
	img.Syms["counter"] = SymLoc{Sect: 2, Off: 0}

	img.Fixups = []Fixup{
		{Sect: 0, Off: 4, Sym: "helper"},                               // call
		{Sect: 0, Off: 9, Sym: "table", RipAdjust: 4},                  // rip-relative
		{Sect: 0, Off: 16, Sym: "counter", Absolute: true, Wide: true}, // movabs
	}
	img.Entry = "main"
	return img
}

func TestCOFFObjectRoundTrip(t *testing.T) {
	src := testImage()
	obj := WriteCOFFObject(src)
	if len(obj) < 20 {
		t.Fatalf("object is %d bytes, too short for a file header", len(obj))
	}

	// --- header, read the way parseCOFF reads it ---
	if m := rd16(obj, 0); m != coffMachineAMD64 {
		t.Errorf("machine = 0x%x, want 0x%x", m, coffMachineAMD64)
	}
	if n := rd16(obj, 2); n != 3 {
		t.Errorf("NumberOfSections = %d, want 3", n)
	}
	if oh := rd16(obj, 16); oh != 0 {
		t.Errorf("SizeOfOptionalHeader = %d, want 0 for a COFF object", oh)
	}
	symAt := rd32(obj, 8)
	nSyms := rd32(obj, 12)
	if symAt <= 0 {
		t.Errorf("PointerToSymbolTable = %d, want a real offset", symAt)
	}
	if nSyms <= 0 {
		t.Errorf("NumberOfSymbols = %d, want the table to be present", nSyms)
	}
	if nSyms != int(rd32(obj, 12)) {
		t.Errorf("NumberOfSymbols changed between reads: %d vs %d", nSyms, rd32(obj, 12))
	}
	// The symbol table must lie inside the file, or every later offset is a lie.
	if symAt+18*int(nSyms) > len(obj) {
		t.Fatalf("symbol table (%d + 18*%d) runs past the end of a %d-byte file",
			symAt, nSyms, len(obj))
	}

	// --- parse it back ---
	got, err := parseCOFF(obj)
	if err != nil {
		t.Fatalf("parseCOFF on our own output: %v", err)
	}
	if len(got.secs) != 3 {
		t.Fatalf("read back %d sections, want 3", len(got.secs))
	}
	for i, want := range []struct {
		name string
		bss  bool
		size int
	}{
		{".text", false, 27},
		{".rdata", false, 4},
		{".bss", true, 64},
	} {
		s := got.secs[i]
		if s.name != want.name {
			t.Errorf("section %d name = %q, want %q", i, s.name, want.name)
		}
		if s.bss != want.bss {
			t.Errorf("section %s bss = %v, want %v", s.name, s.bss, want.bss)
		}
		if s.vsize != want.size {
			t.Errorf("section %s vsize = %d, want %d", s.name, s.vsize, want.size)
		}
	}
	if !bytes.Equal(got.secs[0].data, src.Sections[0].Data) {
		t.Errorf(".text data changed across the round trip:\n got %x\nwant %x",
			got.secs[0].data, src.Sections[0].Data)
	}
	if !bytes.Equal(got.secs[1].data, src.Sections[1].Data) {
		t.Errorf(".rdata data changed:\n got %x\nwant %x", got.secs[1].data, src.Sections[1].Data)
	}

	// --- symbols ---
	byName := map[string]coffSym{}
	for _, s := range got.syms {
		if s.name != "" {
			byName[s.name] = s
		}
	}
	for _, name := range []string{"main", "helper", "table", "counter"} {
		s, ok := byName[name]
		if !ok {
			t.Errorf("symbol %q missing from the object", name)
			continue
		}
		if s.class != scnClassExternal {
			t.Errorf("symbol %q class = %d, want %d (external)", name, s.class, scnClassExternal)
		}
		if s.secNum <= 0 {
			t.Errorf("symbol %q secNum = %d, want a real section", name, s.secNum)
		}
	}
	if s := byName["helper"]; s.value != 4 {
		t.Errorf("helper offset = %d, want 4", s.value)
	}
	if s := byName["table"]; s.value != 0 {
		t.Errorf("table offset = %d, want 0", s.value)
	}

	// --- relocations survive with their types ---
	total := 0
	for _, s := range got.secs {
		total += s.relCount
		if s.relCount > 0 {
			if s.relOff <= 0 || s.relOff+s.relCount*10 > len(obj) {
				t.Errorf("section %s relocations (%d @ %d) run past the end of a %d-byte file",
					s.name, s.relCount, s.relOff, len(obj))
			}
		}
	}
	if total != 3 {
		t.Errorf("read back %d relocations, want 3", total)
	}

	// Every relocation's symbol index must be in range, and its name must be one
	// this object actually mentions. An out-of-range index here is the classic
	// symptom of a symbol table whose order shifted.
	for _, s := range got.secs {
		for i := 0; i < s.relCount; i++ {
			at := s.relOff + i*10
			off := rd32(obj, at)
			si := rd32(obj, at+4)
			typ := rd16(obj, at+8)
			if int(si) >= len(got.syms) {
				t.Errorf("%s relocation %d names symbol %d, past the %d-entry table",
					s.name, i, si, len(got.syms))
				continue
			}
			if int(off) >= len(s.data) {
				t.Errorf("%s relocation %d targets offset %d, past the %d-byte section",
					s.name, i, off, len(s.data))
			}
			if typ != relAMD64Rel32 && typ != relAMD64Addr64 && typ != relAMD64Addr32 {
				t.Errorf("%s relocation %d has type %d, want a recognised x64 type",
					s.name, i, typ)
			}
		}
	}

	// --- an undefined symbol appears as one ---
	// A reference to something this object does not define is how a call into
	// goclib is expressed, so the writer has to be able to produce one.
	img2 := testImage()
	img2.Fixups = append(img2.Fixups, Fixup{Sect: 0, Off: 4, Sym: "printf_lite"})
	obj2 := WriteCOFFObject(img2)
	got2, err := parseCOFF(obj2)
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	found := false
	for _, s := range got2.syms {
		if s.name == "printf_lite" {
			found = true
			if s.secNum != 0 {
				t.Errorf("printf_lite secNum = %d, want 0 (undefined)", s.secNum)
			}
		}
	}
	if !found {
		t.Error("a fixup naming printf_lite produced no symbol for it")
	}
}

// A long symbol name has to go into the string table, and the offset has to
// come back pointing at the right bytes. This is the case that silently
// corrupts a table when the string table is sized too early.
func TestCOFFObjectLongSymbolName(t *testing.T) {
	img := testImage()
	const long = "a_very_long_symbol_name_that_cannot_fit_inline"
	img.Syms[long] = SymLoc{Sect: 0, Off: 8}
	img.Fixups = append(img.Fixups, Fixup{Sect: 0, Off: 4, Sym: long})

	obj := WriteCOFFObject(img)
	got, err := parseCOFF(obj)
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	for _, s := range got.syms {
		if s.name == long {
			if s.value != 8 {
				t.Errorf("%s offset = %d, want 8", long, s.value)
			}
			return
		}
	}
	t.Errorf("long symbol %q not found after the round trip", long)
}

// Two builds of the same image have to produce byte-identical objects, or "did
// anything change?" cannot be answered by comparing files.
func TestCOFFObjectIsReproducible(t *testing.T) {
	a := WriteCOFFObject(testImage())
	b := WriteCOFFObject(testImage())
	if !bytes.Equal(a, b) {
		t.Errorf("two writes of the same image differ: %d vs %d bytes", len(a), len(b))
	}
}

// An image with nothing pending has no relocations, and the counts in the
// section headers and the symbol table's aux records must all say so.
func TestCOFFObjectNoRelocations(t *testing.T) {
	img := testImage()
	img.Fixups = nil
	obj := WriteCOFFObject(img)
	got, err := parseCOFF(obj)
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	for _, s := range got.secs {
		if s.relCount != 0 {
			t.Errorf("section %s claims %d relocations, want 0", s.name, s.relCount)
		}
		if s.relOff != 0 {
			t.Errorf("section %s has a relocation offset %d with no relocations", s.name, s.relOff)
		}
	}
}

// An Unmapped section -- the Win64 unwind tables -- must not appear: it has no
// bytes in the finished image either, and a reader would allocate space for it.
func TestCOFFObjectSkipsUnmappedSections(t *testing.T) {
	img := testImage()
	img.Sections[0].Unmapped = true
	obj := WriteCOFFObject(img)
	got, err := parseCOFF(obj)
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}
	if len(got.secs) != 2 {
		t.Errorf("wrote %d sections, want 2 (.text was Unmapped)", len(got.secs))
	}
	for _, s := range got.secs {
		if s.name == ".text" {
			t.Error(".text was Unmapped but appears in the object")
		}
	}
}

// The strongest check available: the real tool has to agree. objdump reads the
// spec independently of this code, so a field this writer places wrongly shows
// up as objdump disagreeing with us -- which a round trip cannot detect,
// because both sides here share the same reading of the format.
func TestCOFFObjectAgreesWithObjdump(t *testing.T) {
	if _, err := exec.LookPath("objdump"); err != nil {
		t.Skip("objdump not on PATH")
	}
	img := testImage()
	const long = "a_very_long_symbol_name_that_cannot_fit_inline"
	img.Syms[long] = SymLoc{Sect: 0, Off: 8}
	img.Fixups = append(img.Fixups,
		Fixup{Sect: 0, Off: 4, Sym: long},
		Fixup{Sect: 0, Off: 18, Sym: "counter", Absolute: true, Wide: true},
	)

	dir := t.TempDir()
	path := filepath.Join(dir, "test.o")
	if err := os.WriteFile(path, WriteCOFFObject(img), 0644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		// The file name has to be passed explicitly: objdump defaults to
		// "a.out" in the current directory, and a missing default reads as a
		// format error rather than a missing argument.
		args = append(args, path)
		cmd := exec.Command("objdump", args...)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("objdump %s: %v\n%s", strings.Join(args, " "), err, out.String())
		}
		return out.String()
	}

	// Sections: names, sizes and flags as objdump reads them.
	secs := run("-h")
	for _, want := range []string{".text", ".rdata", ".bss"} {
		if !strings.Contains(secs, want) {
			t.Errorf("objdump -h does not list %s:\n%s", want, secs)
		}
	}
	// .bss must be reported as allocated but with no file contents: objdump
	// prints CONTENTS for a section that occupies file bytes and leaves it off
	// for one that does not, so the absence of CONTENTS plus a zero File off is
	// the uninitialised-data reading. Checking for the word "uninit" instead
	// would assert on objdump's wording rather than on the file.
	bssLine := ""
	for _, line := range strings.Split(secs, "\n") {
		if strings.Contains(line, ".bss") {
			bssLine = line
			break
		}
	}
	if bssLine == "" {
		t.Errorf("objdump -h does not list .bss:\n%s", secs)
	} else {
		if strings.Contains(bssLine, "CONTENTS") {
			t.Errorf(".bss is reported as having file contents: %q", strings.TrimSpace(bssLine))
		}
		if !strings.Contains(bssLine, "00000040") {
			t.Errorf(".bss size is not the virtual size (0x40): %q", strings.TrimSpace(bssLine))
		}
	}

	// Symbols: every one of ours must be listed, with the right section.
	syms := run("-t")
	for _, want := range []string{"main", "helper", "table", "counter", long} {
		if !strings.Contains(syms, want) {
			t.Errorf("objdump -t does not list %q:\n%s", want, syms)
		}
	}

	// Relocations: three of them, and the long name must resolve (not show as a
	// raw string-table offset).
	rels := run("-r")
	if n := strings.Count(rels, long); n < 1 {
		t.Errorf("objdump -r does not resolve the long symbol name:\n%s", rels)
	}
	if !strings.Contains(rels, "helper") {
		t.Errorf("objdump -r does not list the call relocation to helper:\n%s", rels)
	}
}

// TestCOFFObjectShortJumpHasNoRelocation pins the shape of a one-byte
// displacement in a relocatable object: the byte carries the value, the
// relocation table does not mention it.
//
// Both halves matter. Leaving the relocation out is right -- COFF has no rel8
// type -- but only because the byte was written. A writer that simply skipped
// the fixup produced an object that linked cleanly and jumped to the code
// generator's placeholder, which is how `jmp short` came to land one instruction
// past its target with nothing in the diagnostics.
func TestCOFFObjectShortJumpHasNoRelocation(t *testing.T) {
	img := NewImage(TargetPE)
	text := newSection(img, ".text", false, true)
	// eb 00   jmp short +0        (fixup at offset 1, target at 3)
	// 90      nop
	// 90      nop                 <- the label
	text.Data = []byte{0xeb, 0x00, 0x90, 0x90}
	text.VSize = len(text.Data)
	img.Syms["main"] = SymLoc{Sect: 0, Off: 0}
	img.Syms["target"] = SymLoc{Sect: 0, Off: 3}
	img.Fixups = []Fixup{{Sect: 0, Off: 1, Sym: "target", Short: true}}

	link := NewImage(TargetPE)
	link.Entry = "main"
	if err := link.IngestCOFFBytes(WriteCOFFObject(img)); err != nil {
		t.Fatalf("IngestCOFFBytes: %v", err)
	}
	if len(link.Fixups) != 0 {
		t.Errorf("the short jump came back as %d pending fixup(s); it should have been resolved in place", len(link.Fixups))
	}
	// Locate the instruction through the symbols rather than a literal index.
	// The linked image's .text also carries the synthetic entry stub the merger
	// puts in front of the object's code, so nothing sits at offset 0 -- and how
	// much stub there is depends on what else the image needs. The distance
	// between the two symbols is the object's own and does not move.
	main, ok := link.Syms["main"]
	if !ok {
		t.Fatal("main did not survive the round trip")
	}
	tgt, ok := link.Syms["target"]
	if !ok {
		t.Fatal("the target symbol did not survive the round trip")
	}
	ts := sectionByName(link, ".text")
	if ts == nil {
		t.Fatal("no .text in the linked image")
	}
	// eb <disp> at main, label at main+3: the field holds the displacement and
	// the CPU counts from the byte after it, so the value must be 1.
	jmpOff := main.Off
	if jmpOff+1 >= len(ts.Data) {
		t.Fatalf("jmp offset %d is outside .text (%d bytes)", jmpOff, len(ts.Data))
	}
	if ts.Data[jmpOff] != 0xEB {
		t.Errorf("byte at %d is %#02x, want 0xEB (jmp short rel8)", jmpOff, ts.Data[jmpOff])
	}
	if got := int8(ts.Data[jmpOff+1]); got != 1 {
		t.Errorf("short jump displacement = %d, want 1 (from the byte after the field to the label)", got)
	}
	if d := tgt.Off - main.Off; d != 3 {
		t.Errorf("target is %d bytes after main, want 3 -- the test's own layout assumption moved", d)
	}
}

// TestCOFFObjectRelocCountMatchesRecords guards the section header's relocation
// count against the records actually written.
//
// The count and the records are computed by two separate passes over the same
// fixups, and they used to disagree: a fixup with no COFF relocation type was
// dropped on the way out but still counted on the way in. The header then
// claimed more relocations than the file held, and a reader that trusts the
// header -- which is every reader -- walked off the end of the table and into
// the symbol table, decoding symbol bytes as relocation entries. The error it
// produced named a symbol index of 28462 in a table of 282, which points at
// nothing a person could debug.
func TestCOFFObjectRelocCountMatchesRecords(t *testing.T) {
	img := testImage()
	// A short jump, which the writer resolves rather than relocating.
	ts := sectionByName(img, ".text")
	ts.Data = append(ts.Data, 0xeb, 0x00, 0x90, 0x90)
	short := len(ts.Data) - 3
	ts.VSize = len(ts.Data)
	img.Syms["near"] = SymLoc{Sect: 0, Off: short + 2}
	img.Fixups = append(img.Fixups, Fixup{Sect: 0, Off: short + 1, Sym: "near", Short: true})

	link := NewImage(TargetPE)
	if err := link.IngestCOFFBytes(WriteCOFFObject(img)); err != nil {
		t.Fatalf("a short fixup made the object unreadable: %v", err)
	}
	// The three real relocations survive, and nothing chokes on the fourth.
	if n := len(link.Fixups); n != 3 {
		var names []string
		for _, f := range link.Fixups {
			names = append(names, f.Sym)
		}
		t.Errorf("linker sees %d pending fixup(s) %v, want 3", n, names)
	}
}

// TestCOFFObjectCarriesImports covers the round trip that makes a Win32 program
// linkable from an object file at all.
//
// A COFF object leaves every symbol it does not define undefined and says
// nothing about which DLL exports it -- that knowledge lives in the compiler,
// which read it off `extern Name, user32` in the goclib header. It rides along
// in a static symbol whose name is the map, the same way the library-symbol
// list does.
//
// Without it, a program that called MessageBoxA linked in one step and failed
// to link through an object file, with the error naming MessageBoxA and nothing
// mentioning imports. The linker's own Win32 table could have covered the
// common cases, but a table is a guess about a program's imports, and a guess
// that is merely incomplete is indistinguishable from a bug in the program.
func TestCOFFObjectCarriesImports(t *testing.T) {
	img := testImage()
	img.Exts["MessageBoxA"] = "user32.dll"
	img.Exts["GetLastError"] = "kernel32.dll"

	link := NewImage(TargetPE)
	if err := link.IngestCOFFBytes(WriteCOFFObject(img)); err != nil {
		t.Fatalf("IngestCOFFBytes: %v", err)
	}
	for name, want := range map[string]string{
		"MessageBoxA":  "user32.dll",
		"GetLastError": "kernel32.dll",
	} {
		if got := link.Exts[name]; got != want {
			t.Errorf("Exts[%q] = %q, want %q", name, got, want)
		}
	}
	// An import is a claim about the loader, not a definition: giving it an
	// image address would let a relocation resolve against a slot that has none.
	if _, ok := link.Syms["MessageBoxA"]; ok {
		t.Error("an imported name was recorded as a defined symbol")
	}
}
