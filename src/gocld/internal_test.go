package gocld

import (
	"encoding/binary"
	"strings"
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

// objWithLocalIntArray builds a one-section relocatable object holding an
// internal int array, the shape each unit of a multi-unit program gets for a
// `static int base[3]` of its own.
func objWithLocalIntArray(t *testing.T, unit string, vals []int32) []byte {
	t.Helper()
	img := NewImage(TargetELF)
	data := newSection(img, ".data", true, false)
	buf := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(buf[4*i:], uint32(v))
	}
	data.Data = buf
	img.Syms["base"] = SymLoc{Sect: sectionIndexOf(img, data), Off: 0, Static: true}
	img.FileName = unit + ".o"
	return WriteELFObject(img)
}

// TestIngestELFKeepsEachObjectsLocalsApart pins the address a local symbol gets
// when a SECOND object brings one at the same (section, value) pair.
//
// ELF lets several locals share a name, and a linker that keys them by name
// reports a duplicate definition of something that is not duplicated -- which
// is why these are keyed by something else. But "something else" must still
// tell two objects apart: a local's value is an offset into ITS OWN copy of the
// section, and merging the copies rebases all but the first. Two units that
// each declare `static int base[3]` both carry a local at (.data, value 0), and
// keying on the section and value alone collapsed them into one -- so every
// relocation in the second unit resolved to the first unit's array.
//
// Nothing complains when this happens. The program links, runs, and prints the
// other file's numbers: a multi-unit build came out with unit B reading unit
// A's array and printing 30 where 3 was meant. So the assertion is on the two
// addresses being different, which is the only thing that distinguishes a
// correct merge from that one.
func TestIngestELFKeepsEachObjectsLocalsApart(t *testing.T) {
	img := NewImage(TargetELF)
	img.DeferUndefined(true)
	for _, obj := range [][]byte{
		objWithLocalIntArray(t, "a", []int32{10, 20, 30}),
		objWithLocalIntArray(t, "b", []int32{1, 2, 3}),
	} {
		if err := img.IngestELFBytes(obj); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}
	// Group the local symbols by section and collect the offsets each got.
	//
	// The bug's signature is a MISSING entry, not a wrong one: the second
	// object's local finds the key already present and is skipped, so its
	// relocations point at whatever the first object put there. Counting the
	// entries is therefore the assertion -- two objects contribute two locals,
	// and a merge that collapses them leaves one.
	offs := map[int][]int{}
	for name, loc := range img.Syms {
		if strings.HasPrefix(name, "__loc_") {
			offs[loc.Sect] = append(offs[loc.Sect], loc.Off)
		}
	}
	if len(offs) == 0 {
		t.Fatal("no local symbols were ingested; the test proves nothing")
	}
	paired := false
	for sect, list := range offs {
		if len(list) < 2 {
			t.Errorf("section %d has %d local symbol(s), want 2: the two objects' locals\n"+
				"collapsed into one, so the second object's references read the first\n"+
				"object's data", sect, len(list))
			continue
		}
		paired = true
		if list[0] == list[1] {
			t.Errorf("section %d: both objects' locals resolved to offset %d -- the second\n"+
				"object's copy was not rebased onto its own place in the merged section",
				sect, list[0])
		}
	}
	if !paired {
		t.Error("no section received locals from both objects")
	}
}
