package gocld

// Merge tests. These run against img real LLVM-produced object when GOC_LLVM_OBJ
// points at one, because the whole point of the merge is to cope with what LLVM
// actually emits; the synthetic objects from coff_test.go only pin the parser.

import (
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
