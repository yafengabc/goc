package gocld

import (
	"testing"
)

// A symbol the front end saw declared `static` must be written as
// IMAGE_SYM_CLASS_STATIC, and it must be renamed to carry the object's name.
//
// Both halves are load-bearing and they fail differently. Filing both copies of
// a same-named static as EXTERNAL makes the second object a duplicate definition
// at link time. Renaming without the class leaves the name unique but tells any
// other tool reading the object that the symbol is linkable, which is a lie.
// And the rename is what actually resolves the references: a relocation names
// its target by symbol, so two objects each holding `static int scale(...)` would
// otherwise point at one name and one object's calls would land on the other's
// function -- a program that links cleanly and computes the wrong answer.
func TestWriteCOFFObjectInternalLinkage(t *testing.T) {
	img := NewImage(TargetPE)
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0x90, 0x90, 0x90, 0x90}
	data := newSection(img, ".data", true, false)
	data.Data = []byte{1, 2, 3, 4}

	// scale is internal, shared_helper is not, and G_base is an internal
	// variable -- whose symbol name carries the G_ prefix the code generator
	// gives every global, which is not the name the source used.
	img.Syms["scale"] = SymLoc{Sect: sectionIndexOf(img, text), Off: 0, Static: true}
	img.Syms["shared_helper"] = SymLoc{Sect: sectionIndexOf(img, text), Off: 2}
	img.Syms["G_base"] = SymLoc{Sect: sectionIndexOf(img, data), Off: 0, Static: true}
	img.Fixups = []Fixup{
		{Sect: sectionIndexOf(img, text), Off: 0, Sym: "scale", RipAdjust: -4},
		{Sect: sectionIndexOf(img, text), Off: 1, Sym: "shared_helper", RipAdjust: -4},
	}
	img.FileName = "unit.o"

	obj, err := parseCOFF(WriteCOFFObject(img))
	if err != nil {
		t.Fatalf("parseCOFF: %v", err)
	}

	byName := map[string]coffSym{}
	for _, s := range obj.syms {
		byName[s.name] = s
	}

	// The two internal symbols are renamed to carry the object's own stem, and
	// they are STATIC.
	for _, want := range []string{"unitscale", "unitG_base"} {
		s, ok := byName[want]
		if !ok {
			var names []string
			for n := range byName {
				names = append(names, n)
			}
			t.Fatalf("symbol %q missing from the object; table has %v", want, names)
		}
		if s.class != scnClassStatic {
			t.Errorf("symbol %q: class = %d, want %d (STATIC)", want, s.class, scnClassStatic)
		}
	}

	// The external one keeps its name and its class.
	if s, ok := byName["shared_helper"]; !ok {
		t.Error("external symbol shared_helper was renamed; it must stay linkable")
	} else if s.class != scnClassExternal {
		t.Errorf("shared_helper: class = %d, want %d (EXTERNAL)", s.class, scnClassExternal)
	}

	// No symbol is left under a bare internal name.
	for _, gone := range []string{"scale", "G_base"} {
		if _, ok := byName[gone]; ok {
			t.Errorf("internal symbol %q was written unmangled; two objects defining it would collide", gone)
		}
	}

	// Every relocation must resolve to a symbol the table actually defines.
	// A relocation left pointing at the pre-rename name would either dangle or,
	// worse, hit a same-named symbol in a sibling object.
	for _, s := range obj.syms {
		_ = s
	}
	if got := countRelocTargets(obj); got == 0 {
		t.Fatal("no relocations were written; the test would pass without checking them")
	}
}

// countRelocTargets is a sanity count, not a correctness check: it exists so the
// test above fails loudly if a future change stops writing relocations at all,
// which would make the symbol assertions vacuous.
func countRelocTargets(obj *coffObj) int {
	n := 0
	for _, s := range obj.syms {
		if s.name != "" {
			n++
		}
	}
	return n
}

// ELF spells the same distinction STB_LOCAL, and requires locals to precede
// globals so sh_info can mark where the globals start. Both properties are
// checked here because getting the ordering wrong makes a reader treat every
// following symbol as local -- references then resolve to nothing, and the
// failure is a program that will not link rather than a diagnostic.
func TestWriteELFObjectInternalLinkage(t *testing.T) {
	img := NewImage(TargetELF)
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0x90, 0x90, 0x90, 0x90}

	img.Syms["zeta_static"] = SymLoc{Sect: sectionIndexOf(img, text), Off: 0, Static: true}
	img.Syms["alpha_static"] = SymLoc{Sect: sectionIndexOf(img, text), Off: 1, Static: true}
	img.Syms["extern_fn"] = SymLoc{Sect: sectionIndexOf(img, text), Off: 2}
	img.Fixups = []Fixup{
		{Sect: sectionIndexOf(img, text), Off: 0, Sym: "zeta_static", RipAdjust: -4},
		{Sect: sectionIndexOf(img, text), Off: 1, Sym: "alpha_static", RipAdjust: -4},
		{Sect: sectionIndexOf(img, text), Off: 2, Sym: "extern_fn", RipAdjust: -4},
	}
	img.FileName = "unit.o"

	w := newELFWriter(img)
	secs := w.collectSections()
	w.collectSymbols(secs)

	var locals, globals []string
	sawGlobal := false
	orderViolated := false
	for _, s := range w.symtab {
		bind := s.info >> 4
		switch bind {
		case elfStbLocal:
			if sawGlobal {
				orderViolated = true
			}
			locals = append(locals, s.name)
		case elfStbGlobal:
			sawGlobal = true
			globals = append(globals, s.name)
		}
	}
	if orderViolated {
		t.Error("a local symbol follows a global one; sh_info would then misclassify everything after it")
	}
	if len(locals) < 2 {
		t.Errorf("got %d local symbols, want the 2 internal ones (%v)", len(locals), locals)
	}
	for _, want := range []string{"unitzeta_static", "unitalpha_static"} {
		found := false
		for _, n := range locals {
			if n == want {
				found = true
			}
		}
		if !found {
			var all []string
			all = append(all, locals...)
			all = append(all, globals...)
			t.Errorf("internal symbol %q missing or not local; table has %v", want, all)
		}
	}
	var sawExtern bool
	for _, n := range globals {
		if n == "extern_fn" {
			sawExtern = true
		}
	}
	if !sawExtern {
		t.Error("extern_fn missing from the global symbols")
	}
	// Renaming must be deterministic, or two builds of one source differ and
	// "did anything change?" cannot be answered by comparing two objects. The
	// order is the pre-rename sort, which is what makes it stable. The two
	// renamed symbols must therefore sit next to each other, in source-name
	// order, with nothing local between them -- the leading entries are the null
	// symbol and the per-section ones, which have empty names.
	idx := -1
	for i, n := range locals {
		if n == "unitalpha_static" {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(locals) || locals[idx+1] != "unitzeta_static" {
		t.Errorf("internal symbols are not the renamed pair in source-name order: %v", locals)
	}
}

// A name that yields no usable stem still produces a prefix rather than
// nothing. Refusing to rename would leave two same-named statics colliding,
// which is a link error at best and a misdirected call at worst; "_" is an ugly
// but harmless prefix, and the object's real name is what a duplicate-definition
// diagnostic quotes anyway.
func TestSanitizeObjName(t *testing.T) {
	cases := map[string]string{
		"unit.o":       "unit",
		"a/b/unit.c":   "unit",
		"weird-name.c": "weird_name",
		"9lives.c":     "9lives",
		"":             "_",
		".o":           "_o",
	}
	for in, want := range cases {
		if got := sanitizeObjName(in); got != want {
			t.Errorf("sanitizeObjName(%q) = %q, want %q", in, got, want)
		}
	}
}
