package gocld

// Merge tests. These run against img real LLVM-produced object when GOC_LLVM_OBJ
// points at one, because the whole point of the merge is to cope with what LLVM
// actually emits; the synthetic objects from coff_test.go only pin the parser.

import (
	"bytes"
	"os"
	"testing"
)

func realObj(t *testing.T) []byte {
	t.Helper()
	p := os.Getenv("GOC_LLVM_OBJ")
	if p == "" {
		t.Skip("set GOC_LLVM_OBJ to img real LLVM COFF object to run this")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return b
}

func TestIngestCOFFSectionsMerged(t *testing.T) {
	src := realObj(t)
	img := NewImage(TargetPE)
	if err := img.IngestCOFFBytes(src); err != nil {
		t.Fatalf("IngestCOFFBytes: %v", err)
	}
	// Every section the object carried must now exist in the assembler, with
	// the object's bytes somewhere inside it.
	for _, name := range []string{".text", ".data", ".rdata", ".xdata", ".pdata"} {
		s := sectionByName(img, name)
		if s == nil {
			t.Errorf("section %s missing after ingest", name)
			continue
		}
		if s.VSize == 0 {
			t.Errorf("section %s is empty after ingest", name)
		}
	}
	if s := sectionByName(img, ".bss"); s == nil || !s.Bss {
		t.Errorf(".bss must be present and marked uninitialised")
	}
	t.Logf("merged: .text=%d .data=%d .rdata=%d .xdata=%d .pdata=%d",
		sz(img, ".text"), sz(img, ".data"), sz(img, ".rdata"), sz(img, ".xdata"), sz(img, ".pdata"))
}

func sz(img *Image, name string) int {
	if s := sectionByName(img, name); s != nil {
		return s.VSize
	}
	return -1
}

func TestIngestCOFFSymbolsResolved(t *testing.T) {
	src := realObj(t)
	img := NewImage(TargetPE)
	if err := img.IngestCOFFBytes(src); err != nil {
		t.Fatalf("IngestCOFFBytes: %v", err)
	}
	// Functions the object defines must land in the symbol table, inside .text.
	text := sectionByName(img, ".text")
	if text == nil {
		t.Fatal("no .text")
	}
	textIdx := sectionIndexOf(img, text)
	for _, name := range []string{"main", "fib", "bsort"} {
		loc, ok := img.Syms[name]
		if !ok {
			t.Errorf("symbol %q missing after ingest", name)
			continue
		}
		if loc.Sect != textIdx {
			t.Errorf("symbol %q should be in .text, got section %d", name, loc.Sect)
		}
		if loc.Off < 0 || loc.Off > text.VSize {
			t.Errorf("symbol %q offset %d outside .text (0..%d)", name, loc.Off, text.VSize)
		}
	}
	// Data symbols too.
	for _, name := range []string{"counter", "arr"} {
		if _, ok := img.Syms[name]; !ok {
			t.Errorf("data symbol %q missing after ingest", name)
		}
	}
}

func TestIngestCOFFUndefinedBecomesImport(t *testing.T) {
	src := realObj(t)
	img := NewImage(TargetPE)
	if err := img.IngestCOFFBytes(src); err != nil {
		t.Fatalf("IngestCOFFBytes: %v", err)
	}
	// print is declared but not defined in the object: it has to be registered
	// as an extern so buildIData emits an address-table slot for it.
	if dll, ok := img.Exts["print"]; !ok {
		t.Error("undefined symbol print was not registered as an import")
	} else if dll == "" {
		t.Error("import print has no DLL name")
	}
	// A defined symbol must NOT become an import.
	if _, ok := img.Exts["main"]; ok {
		t.Error("defined symbol main must not become an import")
	}
}

func TestIngestCOFFRelocationsRecorded(t *testing.T) {
	src := realObj(t)
	img := NewImage(TargetPE)
	if err := img.IngestCOFFBytes(src); err != nil {
		t.Fatalf("IngestCOFFBytes: %v", err)
	}
	if len(img.Fixups) == 0 {
		t.Fatal("no fixups recorded from the object's relocations")
	}
	// Every fixup must name img symbol the assembler can resolve, otherwise
	// BuildPE fails with "undefined symbol referenced".
	for _, f := range img.Fixups {
		if f.Sym == "" {
			t.Errorf("fixup with empty symbol at section %d offset %d", f.Sect, f.Off)
		}
	}
	t.Logf("%d fixups recorded", len(img.Fixups))
}

// TestChkstkStubSavesVolatileRegs pins the register behaviour of the
// synthesized __chkstk / ___chkstk_ms stack-probe stub. LLVM lowers a frame
// larger than a page into a call to this helper and models the call as
// preserving every register but rax -- a match for MSVC's __chkstk, which
// push/pops r10/r11. Its register allocator therefore keeps loop-carried
// state in r10/r11 across a VLA's mid-function probe. A stub that treats
// r10/r11 as free scratch silently miscompiles such loops (a continue
// condition computed into r10 never fired; verified 2026-10-16). Asserting
// the save/restore bytes here guards the fix directly -- and independently of
// whether a given LLVM release happens to allocate state to those registers,
// which a C example cannot, because that choice is not stable.
func TestChkstkStubSavesVolatileRegs(t *testing.T) {
	b := newCOFFBuilder()
	b.addSection(".text", []byte{0xC3}) // body is irrelevant; only synthesis runs
	img := NewImage(TargetPE)
	if err := img.IngestCOFFBytes(b.build()); err != nil {
		t.Fatalf("IngestCOFFBytes: %v", err)
	}
	loc, ok := img.Syms["__chkstk"]
	if !ok {
		t.Fatal("__chkstk was not synthesized during merge")
	}
	if loc.Sect < 0 || loc.Sect >= len(img.Sections) {
		t.Fatalf("__chkstk section index %d out of range (have %d sections)", loc.Sect, len(img.Sections))
	}
	sec := img.Sections[loc.Sect]
	if loc.Off < 0 || loc.Off >= len(sec.Data) {
		t.Fatalf("__chkstk offset %d outside .text data (len %d)", loc.Off, len(sec.Data))
	}
	stub := sec.Data[loc.Off:]

	// Prologue: push r10 ; push r11 ; mov r10,rsp ; mov r11,rax. Saving the
	// pair before using r10/r11 as the probe cursor is the whole fix.
	pro := []byte{0x41, 0x52, 0x41, 0x53, 0x4C, 0x8B, 0xD4, 0x49, 0x89, 0xC3}
	if !bytes.HasPrefix(stub, pro) {
		t.Fatalf("__chkstk does not save r10/r11 first:\n got % x\nwant prefix % x", stub, pro)
	}
	// Probe block: sub rsp,0x1000 ; mov [rsp],r11 ; sub r11,0x1000. Without it
	// the routine would be a stub that never touches the guard pages.
	probe := []byte{0x48, 0x81, 0xEC, 0x00, 0x10, 0x00, 0x00, 0x4C, 0x89, 0x1C, 0x24, 0x49, 0x81, 0xEB, 0x00, 0x10, 0x00, 0x00}
	if !bytes.Contains(stub, probe) {
		t.Fatalf("__chkstk probe block missing/malformed:\n got % x\nwant % x", stub, probe)
	}
	// Epilogue: mov rsp,r10 ; pop r11 ; pop r10 ; ret. The restores must be
	// present, or the caller's r10/r11 are still destroyed on the way out.
	epi := []byte{0x4C, 0x89, 0xD4, 0x41, 0x5B, 0x41, 0x5A, 0xC3}
	if !bytes.Contains(stub, epi) {
		t.Fatalf("__chkstk does not restore r11/r10 before ret:\n got % x\nwant % x somewhere", stub, epi)
	}
	// Both spellings must name the same site, or a current-LLVM __chkstk
	// reference finds nothing to bind to.
	if ms, ok := img.Syms["___chkstk_ms"]; !ok || ms != loc {
		t.Fatalf("___chkstk_ms must alias __chkstk (%+v), got %+v ok=%v", loc, ms, ok)
	}
}
