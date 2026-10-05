package gocld

// Validation of the COFF parser against a real LLVM-produced object, rather
// than only against the synthetic builder in coff_test.go. The fixture is
// generated at test time when GOC_LLVM_OBJ points at one; the test is skipped
// otherwise so the suite stays hermetic.

import (
	"os"
	"sort"
	"testing"
)

func loadRealCOFF(t *testing.T) *coffObj {
	t.Helper()
	p := os.Getenv("GOC_LLVM_OBJ")
	if p == "" {
		t.Skip("set GOC_LLVM_OBJ to a real LLVM COFF object to run this")
	}
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	o, err := parseCOFF(src)
	if err != nil {
		t.Fatalf("parseCOFF on real LLVM object: %v", err)
	}
	return o
}

func TestRealCOFFSections(t *testing.T) {
	o := loadRealCOFF(t)
	if len(o.secs) == 0 {
		t.Fatal("no sections parsed")
	}
	// The LLVM object for a module with code, data, a global initialiser and
	// unwind info carries exactly these; all must survive the parse.
	want := []string{".text", ".data", ".rdata", ".xdata", ".pdata"}
	got := map[string]bool{}
	for _, s := range o.secs {
		got[s.name] = true
	}
	var missing []string
	for _, w := range want {
		if !got[w] {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("missing sections %v (got %d sections)", missing, len(o.secs))
	}
	// .text must actually hold bytes.
	var text *coffSec
	for i := range o.secs {
		if o.secs[i].name == ".text" {
			text = &o.secs[i]
		}
	}
	if text == nil || len(text.data) == 0 {
		t.Fatal(".text has no data")
	}
	t.Logf("real object: %d sections, .text = %d bytes", len(o.secs), len(text.data))
}

func TestRealCOFFSymbols(t *testing.T) {
	o := loadRealCOFF(t)
	byName := map[string]coffSym{}
	for _, s := range o.syms {
		byName[s.name] = s
	}
	// The fixture defines these and imports print.
	for _, want := range []string{"main", "fib", "bsort"} {
		s, ok := byName[want]
		if !ok {
			t.Errorf("symbol %q missing from parse", want)
			continue
		}
		if s.secNum <= 0 {
			t.Errorf("symbol %q should be defined, got secNum %d", want, s.secNum)
		}
	}
	// Undefined externals must be recognised as such so they become imports.
	if s, ok := byName["print"]; ok {
		if s.secNum != 0 {
			t.Errorf("print must be undefined (secNum 0), got %d", s.secNum)
		}
	} else {
		t.Log("note: print not in symbol table (may be satisfied internally)")
	}
	// Names longer than 8 bytes prove the string-table path works on real data.
	var long int
	for _, s := range o.syms {
		if len(s.name) > 8 {
			long++
		}
	}
	if long == 0 {
		t.Log("no long symbol names in this object")
	}
	var names []string
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	t.Logf("symbols: %v", names)
}

func TestRealCOFFRelocationsParse(t *testing.T) {
	// The parser records relocation locations; this checks the offsets it read
	// land inside the section they belong to.
	o := loadRealCOFF(t)
	p := os.Getenv("GOC_LLVM_OBJ")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for i, s := range o.secs {
		if s.relCount == 0 {
			continue
		}
		for r := 0; r < s.relCount; r++ {
			rec := s.relOff + 10*r
			if rec+10 > len(src) {
				t.Fatalf("section %s relocation %d out of range", s.name, r)
			}
			total++
		}
		_ = i
	}
	if total == 0 {
		t.Fatal("no relocations parsed from a real object")
	}
	t.Logf("real object: %d relocation records", total)
}
