package gocld

// Tests for the .rsrc resource tree: parsing it, merging two of them, and
// writing one into a PE image.
//
// The trees here are built as bytes by a small builder rather than shipped as
// hex, because the whole point of the format is that the fields are positional
// and a test that reads them back through the same helper that wrote them proves
// only that the helper is self-consistent. buildTree lays the bytes out by hand
// from the documented layout, so a mistake in the parser shows up as a mismatch
// against bytes whose provenance is the specification and not this code.

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

// ---------------------------------------------------------------------------
// A builder for resource trees
// ---------------------------------------------------------------------------

// rsrcNode is one entry under construction: either a child directory or a
// payload, keyed by id or by name.
type rsrcNode struct {
	id    int
	name  string
	sub   *rsrcDirSpec
	leaf  []byte
	isDir bool
}

type rsrcDirSpec struct {
	nodes []rsrcNode
}

func rsrcIDNode(id int, leaf []byte) rsrcNode {
	return rsrcNode{id: id, leaf: leaf}
}

func rsrcIDDir(id int, sub *rsrcDirSpec) rsrcNode {
	return rsrcNode{id: id, sub: sub, isDir: true}
}

func rsrcNameNode(name string, leaf []byte) rsrcNode {
	return rsrcNode{name: name, leaf: leaf}
}

func rsrcNameDir(name string, sub *rsrcDirSpec) rsrcNode {
	return rsrcNode{name: name, sub: sub, isDir: true}
}

// buildTree lays out a resource section the way the format describes it:
// one root table, then for each directory its entries, and every payload after
// all the tables. Directory tables and data entries are 4-aligned; payloads are
// 8-aligned.
//
// The result is deliberately NOT what writeDir produces. A round trip through
// both would agree even if both were wrong in the same way, which is exactly
// the failure this format invites.
func buildTree(root *rsrcDirSpec) []byte {
	var buf []byte
	alignTo := func(n int) {
		for len(buf)%n != 0 {
			buf = append(buf, 0)
		}
	}

	// placeDir lays out one directory and returns its offset. An entry needs
	// its child's offset and an entry's name needs its pool offset, so the
	// table's own bytes cannot be written until the children are placed --
	// but the table's SIZE has to be reserved first, because the children go
	// after it. Hence reserve, recurse, then fill in.
	var placeDir func(d *rsrcDirSpec) int
	placeDir = func(d *rsrcDirSpec) int {
		alignTo(4)
		self := len(buf)
		named, ided := 0, 0
		for _, n := range d.nodes {
			if n.name != "" {
				named++
			} else {
				ided++
			}
		}
		// Reserve the table and its entries.
		buf = append(buf, make([]byte, rscSize+(named+ided)*rscEnt)...)

		// The string pool follows the entries, in the order the entries
		// reference it. Each name is a length in code units, the units, and a
		// NUL.
		poolAt := len(buf)
		for _, n := range d.nodes {
			if n.name == "" {
				continue
			}
			units := utf16.Encode([]rune(n.name))
			buf = appendU16(buf, uint16(len(units)))
			for _, u := range units {
				buf = appendU16(buf, u)
			}
			buf = appendU16(buf, 0)
		}

		// Children next, so their offsets are known when the entries are
		// filled in below.
		subOff := map[*rsrcDirSpec]int{}
		for _, n := range d.nodes {
			if n.isDir {
				subOff[n.sub] = placeDir(n.sub)
			}
		}
		// Then the data entries and their payloads.
		//
		// OffsetToData here is SECTION-relative, which is what a producer
		// emits: in an object file the section has no file position yet, and
		// windres writes the offset within the section. It becomes a file
		// offset only when the section is placed in an image, which is the
		// linker's job and the reason parseRsrc can read an object's tree
		// at all.
		leafAt := map[*rsrcNode]int{}
		alignTo(4)
		for i := range d.nodes {
			n := &d.nodes[i]
			if n.isDir {
				continue
			}
			leafAt[n] = len(buf)
			buf = append(buf, make([]byte, rscData)...)
			alignTo(8)
			// OffsetToData is written AFTER the alignment, because alignment
			// is what decides where the payload starts. Writing it before
			// records the unaligned position.
			putU32at(buf, leafAt[n]+0, uint32(len(buf)))
			putU32at(buf, leafAt[n]+4, uint32(len(n.leaf)))
			buf = append(buf, n.leaf...)
		}

		// Header.
		putU32at(buf, self+0, 0)
		putU32at(buf, self+4, 0)
		putU16at(buf, self+8, 0)
		putU16at(buf, self+10, 0)
		putU16at(buf, self+12, uint16(named))
		putU16at(buf, self+14, uint16(ided))

		// Entries: named first, then ided, matching the counts above.
		e := self + rscSize
		pool := poolAt
		for i := range d.nodes {
			n := &d.nodes[i]
			if n.name != "" {
				putU32at(buf, e, uint32(pool|rscIsString))
				pool += 2 + utf16Len(n.name)*2 + 2
			} else {
				putU32at(buf, e, uint32(n.id))
			}
			e += 4
			if n.isDir {
				putU32at(buf, e, uint32(subOff[n.sub])|rscIsDir)
			} else {
				putU32at(buf, e, uint32(leafAt[n]))
			}
			e += 4
		}
		return self
	}
	placeDir(root)
	return buf
}

func appendU16(b []byte, v uint16) []byte {
	return append(b, byte(v), byte(v>>8))
}

// iconTree is the shape windres produces for IDI_ICON ICON "app.ico": an
// RT_ICON payload, and an RT_GROUP_ICON payload filed under the string name
// "IDI_ICON", each with a language level under it.
func iconTree(ico, grp []byte) []byte {
	return buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{ // RT_ICON
			rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{ // icon id 1
				rsrcIDNode(0x0409, ico), // en-US
			}}),
		}}),
		rsrcIDDir(14, &rsrcDirSpec{nodes: []rsrcNode{ // RT_GROUP_ICON
			rsrcNameDir("IDI_ICON", &rsrcDirSpec{nodes: []rsrcNode{
				rsrcIDNode(0x0409, grp),
			}}),
		}}),
	}})
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

func TestParseRsrcTree(t *testing.T) {
	ico := []byte{0x28, 0x00, 0x00, 0x00, 0x01, 0x00}
	grp := []byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x00}
	src := buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{
				rsrcIDNode(0x0409, ico),
			}}),
		}}),
		rsrcIDDir(14, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcNameDir("IDI_ICON", &rsrcDirSpec{nodes: []rsrcNode{
				rsrcIDNode(0x0409, grp),
			}}),
		}}),
	}})

	r, err := parseRsrc(src)
	if err != nil {
		t.Fatalf("parseRsrc: %v", err)
	}
	if len(r.root.ided) != 2 {
		t.Fatalf("root has %d ided entries, want 2", len(r.root.ided))
	}
	icon := r.root.ided[0]
	if icon.id != 3 || icon.sub == nil {
		t.Fatalf("first entry = id %d sub %v, want id 3 with a subdirectory", icon.id, icon.sub)
	}
	if len(icon.sub.ided) != 1 || icon.sub.ided[0].id != 1 {
		t.Fatalf("RT_ICON children = %v, want one child with id 1", icon.sub.ided)
	}
	leaf := icon.sub.ided[0].sub.ided[0]
	if leaf.leaf == nil || !bytes.Equal(leaf.leaf.data, ico) {
		t.Fatalf("icon payload = %v, want %v", leaf.leaf, ico)
	}
	grpEnt := r.root.ided[1]
	if grpEnt.id != 14 || len(grpEnt.sub.named) != 1 {
		t.Fatalf("RT_GROUP_ICON = id %d with %d named, want id 14 with one named", grpEnt.id, len(grpEnt.sub.named))
	}
	if grpEnt.sub.named[0].name != "IDI_ICON" {
		t.Fatalf("named entry = %q, want IDI_ICON", grpEnt.sub.named[0].name)
	}
	// And the language level under it carries the group icon payload.
	lang := grpEnt.sub.named[0].sub.ided[0]
	if lang.id != 0x0409 || lang.leaf == nil || !bytes.Equal(lang.leaf.data, grp) {
		t.Fatalf("IDI_ICON/1033 = id %d leaf %v, want id 1033 with %v", lang.id, lang.leaf, grp)
	}
}

// TestParseRsrcNamedNameUsesHighBit pins the one field encoding that is easiest
// to get backwards: a string name is flagged by bit 31, not by a high word of
// all ones. Testing it with a name whose offset is non-zero (0x80-ish) and whose
// low word is non-zero is what makes the two encodings distinguishable -- an
// integer id of 3 satisfies "low 16 bits non-zero" too, so a parser testing the
// wrong field reads an id as a name.
func TestParseRsrcNamedNameUsesHighBit(t *testing.T) {
	src := buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcNameNode("MYICON", []byte{1, 2, 3}),
		rsrcIDNode(3, []byte{4, 5, 6}),
	}})
	r, err := parseRsrc(src)
	if err != nil {
		t.Fatalf("parseRsrc: %v", err)
	}
	if len(r.root.named) != 1 || r.root.named[0].name != "MYICON" {
		t.Fatalf("named = %+v, want one entry named MYICON", r.root.named)
	}
	if len(r.root.ided) != 1 || r.root.ided[0].id != 3 {
		t.Fatalf("ided = %+v, want one entry with id 3", r.root.ided)
	}
}

func TestParseRsrcUTF16NameLengthInCodeUnits(t *testing.T) {
	// A rune outside the BMP needs two code units. A length counted in bytes
	// reads half again as much, and a length counted in runes writes short --
	// either way the truncation lands inside the surrogate pair.
	const emoji = "A\U0001F600B"
	r, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcNameNode(emoji, []byte{9}),
	}}))
	if err != nil {
		t.Fatalf("parseRsrc: %v", err)
	}
	if got := r.root.named[0].name; got != emoji {
		t.Fatalf("name = %q (%d runes), want %q", got, len([]rune(got)), emoji)
	}
}

func TestParseRsrcRejects(t *testing.T) {
	good := buildTree(&rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(3, []byte{1, 2, 3})}})
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, errRsrcShort},
		{"short header", good[:8], errRsrcShort},
		{"count past end", func() []byte {
			b := append([]byte(nil), good...)
			putU16at(b, 12, 0x4000)
			putU16at(b, 14, 0)
			return b
		}(), errRsrcShort},
		{"entry data past end", func() []byte {
			b := append([]byte(nil), good...)
			putU32at(b, rscSize+4, 0x7fffffff)
			return b
		}(), errRsrcShort},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseRsrc(tc.data); err != tc.want {
				t.Fatalf("parseRsrc = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestParseRsrcLeafOutsideSection covers the check that keeps a wrong length
// from reading the symbol table: in an object file the bytes after .rsrc are not
// padding, they are someone else's data.
func TestParseRsrcLeafOutsideSection(t *testing.T) {
	src := buildTree(&rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(3, []byte{1, 2, 3})}})
	// Find the one leaf rather than assuming where it landed: the builder
	// aligns, and a test that guesses the offset tests its guess.
	leaf := -1
	for i := rscSize + rscEnt; i+rscData <= len(src); i += 4 {
		if rdU32(src, i) >= rscData && rdU32(src, i) < len(src) {
			leaf = i
			break
		}
	}
	if leaf < 0 {
		t.Fatalf("no data entry found in a %d byte tree", len(src))
	}
	putU32at(src, leaf, uint32(len(src)+100))
	if _, err := parseRsrc(src); err != errRsrcBadLeaf {
		t.Fatalf("parseRsrc = %v, want %v", err, errRsrcBadLeaf)
	}
}

// ---------------------------------------------------------------------------
// Merging
// ---------------------------------------------------------------------------

// TestMergeRsrcSameResourceIsNotAConflict covers the case the merge exists for:
// two objects compiled from the same .rc file each carry the whole resource.
// That is normal and must not be reported.
func TestMergeRsrcSameResourceIsNotAConflict(t *testing.T) {
	a, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(0x0409, []byte{1, 2, 3})}}),
		}}),
	}}))
	if err != nil {
		t.Fatalf("parse a: %v", err)
	}
	b, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(0x0409, []byte{1, 2, 3})}}),
		}}),
	}}))
	if err != nil {
		t.Fatalf("parse b: %v", err)
	}
	if err := a.merge(b); err != nil {
		t.Fatalf("merge of identical trees: %v", err)
	}
	if n := len(a.root.ided); n != 1 {
		t.Fatalf("merged root has %d types, want 1", n)
	}
}

func TestMergeRsrcDifferentBytesIsADuplicate(t *testing.T) {
	mk := func(payload []byte) *rsrc {
		r, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
			rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{
				rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(0x0409, payload)}}),
			}}),
		}}))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return r
	}
	err := mk([]byte{1, 2, 3}).merge(mk([]byte{4, 5, 6, 7}))
	if err == nil {
		t.Fatal("merging different payloads under one key: no error")
	}
	if !strings.Contains(err.Error(), "defined twice") {
		t.Fatalf("merge error = %v, want it to mention a duplicate definition", err)
	}
	// Both sizes belong in the message: which object won is not the question,
	// the fact that there were two is.
	for _, want := range []string{"3", "4"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("merge error = %v, want it to mention size %s", err, want)
		}
	}
}

func TestMergeRsrcDirectoryAgainstLeaf(t *testing.T) {
	dir, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(1, []byte{7})}}),
	}}))
	if err != nil {
		t.Fatalf("parse dir: %v", err)
	}
	leaf, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDNode(3, []byte{7}),
	}}))
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if err := dir.merge(leaf); err != errRsrcShape {
		t.Fatalf("merge = %v, want %v", err, errRsrcShape)
	}
}

// TestMergeRsrcDisjointTypesAccumulate is the case that makes merging worth
// doing at all: one object brings an icon, another a manifest, and the result
// has both.
func TestMergeRsrcDisjointTypesAccumulate(t *testing.T) {
	icon, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(0x0409, []byte{1})}}),
		}}),
	}}))
	if err != nil {
		t.Fatalf("parse icon: %v", err)
	}
	manifest, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDNode(24, []byte("manifest")),
	}}))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if err := icon.merge(manifest); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(icon.root.ided) != 2 {
		t.Fatalf("merged root has %d types, want 2", len(icon.root.ided))
	}
	// And the tree must still be writable, since a merge that loses the ability
	// to emit is not a merge.
	if n := icon.size(); n <= 0 {
		t.Fatalf("merged size = %d, want a positive section size", n)
	}
}

// ---------------------------------------------------------------------------
// Layout and writing
// ---------------------------------------------------------------------------

// TestRsrcSizeIndependentOfPosition is the property the whole two-pass shape
// exists to provide: the section's size depends on the tree alone, so it can be
// settled before the section's file offset is known. If this fails, the cycle
// IMAGE_RESOURCE_DATA_ENTRY.OffsetToData creates cannot be broken.
func TestRsrcSizeIndependentOfPosition(t *testing.T) {
	r, err := parseRsrc(iconTree([]byte{1, 2, 3, 4}, []byte{5, 6}))
	if err != nil {
		t.Fatalf("parseRsrc: %v", err)
	}
	if r.size() != r.size() {
		t.Fatal("size() is not deterministic")
	}
	if got := len(r.emit(0)); got != r.size() {
		t.Fatalf("emit(0) produced %d bytes, size() said %d", got, r.size())
	}
	if got := len(r.emit(0x4000)); got != r.size() {
		t.Fatalf("emit(0x4000) produced %d bytes, size() said %d", got, r.size())
	}
}

// readTree walks an emitted section back and returns the payloads keyed by the
// path the loader would take to them. It is a reader, not a check: it exists so
// the assertions below can talk about resources rather than about offsets.
func readTree(t *testing.T, blob []byte, fileOff int) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	var walk func(off int, path string)
	walk = func(off int, path string) {
		if off < 0 || off+rscSize > len(blob) {
			t.Fatalf("directory at %#x is outside the section (%d bytes)", off, len(blob))
		}
		nn := rd16(blob, off+12)
		ii := rd16(blob, off+14)
		for i := 0; i < nn+ii; i++ {
			p := off + rscSize + i*rscEnt
			nameField := rdU32(blob, p)
			offField := rdU32(blob, p+4)
			var key string
			if nameField&rscIsString != 0 {
				so := nameField & rscNameOff
				ln := rd16(blob, so)
				raw := blob[so+2 : so+2+ln*2]
				units := make([]uint16, ln)
				for i := range units {
					units[i] = uint16(rd16(raw, i*2))
				}
				key = string(utf16.Decode(units))
			} else {
				key = strconv.Itoa(nameField)
			}
			cur := path + "/" + key
			if offField&rscIsDir != 0 {
				walk(offField&rscOffMask, cur)
				continue
			}
			leaf := offField & rscOffMask
			if leaf+rscData > len(blob) {
				t.Fatalf("leaf at %#x is outside the section", leaf)
			}
			// OffsetToData is a FILE offset, and it must point past the data
			// entry that describes it.
			dataOff := rdU32(blob, leaf)
			size := rdU32(blob, leaf+4)
			if dataOff != fileOff+leaf+rscData {
				t.Fatalf("%s: OffsetToData = %#x, want %#x (the payload, not its own header)",
					cur, dataOff, fileOff+leaf+rscData)
			}
			if dataOff < fileOff || dataOff+size > fileOff+len(blob) {
				t.Fatalf("%s: payload [%#x,%#x) is outside the section [%#x,%#x)",
					cur, dataOff, dataOff+size, fileOff, fileOff+len(blob))
			}
			out[cur] = append([]byte(nil), blob[dataOff-fileOff:dataOff-fileOff+size]...)
		}
	}
	walk(0, "")
	return out
}

// TestRsrcShortTreeIsFilled covers the producer that files a payload directly
// under its type. The loader looks for type/1/language, so a tree left as it
// arrived is a resource that cannot be found -- present in the file, absent to
// everything that asks for it.
func TestRsrcShortTreeIsFilled(t *testing.T) {
	man := []byte("short tree payload")
	src := buildTree(&rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(24, man)}})

	// Parsing must NOT fill: a merge has to compare what the objects said.
	r, err := parseRsrc(src)
	if err != nil {
		t.Fatalf("parseRsrc: %v", err)
	}
	if len(r.root.ided) != 1 || r.root.ided[0].leaf == nil {
		t.Fatal("parseRsrc filled the tree; a merge would compare invented keys")
	}

	blob := r.emit(0)
	got := readTree(t, blob, 0)
	if !bytes.Equal(got["/24/1/1033"], man) {
		t.Fatalf("filled tree has %v, want the payload at /24/1/1033", keysOf(got))
	}
	// And a full tree must come through untouched: filling a tree that is
	// already the right shape would add a level nobody asked for.
	full := buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(24, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcIDDir(7, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(0x0409, man)}}),
		}}),
	}})
	fr, err := parseRsrc(full)
	if err != nil {
		t.Fatalf("parseRsrc full: %v", err)
	}
	fgot := readTree(t, fr.emit(0), 0)
	if !bytes.Equal(fgot["/24/7/1033"], man) {
		t.Fatalf("full tree has %v, want the payload at /24/7/1033 unchanged", keysOf(fgot))
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestRsrcEmitPayloadsSurviveRoundTrip is the end-to-end statement of the format:
// every payload comes back byte-identical, at an offset the loader can use,
// regardless of where the section landed in the file.
func TestRsrcEmitPayloadsSurviveRoundTrip(t *testing.T) {
	ico := []byte{0x28, 0, 0, 0, 1, 0, 0, 0}
	grp := []byte{0, 0, 1, 0, 1, 0}
	man := []byte("<?xml version=\"1.0\"?>")
	src := buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(0x0409, ico)}}),
		}}),
		// RT_GROUP_ICON is keyed by a NAME, and still has a language level
		// under it -- the levels are type, name-or-id, language, and which of
		// the two is a string varies by type.
		rsrcIDDir(14, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcNameDir("IDI_ICON", &rsrcDirSpec{nodes: []rsrcNode{
				rsrcIDNode(0x0409, grp),
			}}),
		}}),
		rsrcIDNode(24, man),
	}})
	r, err := parseRsrc(src)
	if err != nil {
		t.Fatalf("parseRsrc: %v", err)
	}
	for _, fileOff := range []int{0, 0x200, 0x600, 0x1F00} {
		blob := r.emit(fileOff)
		if len(blob) != r.size() {
			t.Fatalf("emit(%#x): %d bytes, size() said %d", fileOff, len(blob), r.size())
		}
		got := readTree(t, blob, fileOff)
		want := map[string][]byte{
			"/3/1/1033":         ico,
			"/14/IDI_ICON/1033": grp,
			// The manifest was filed straight under its type, so it arrived a
			// level short. It is written as type/1/language -- the shape
			// Windows looks RT_MANIFEST up under -- which is the whole point of
			// filling a short tree rather than emitting it as it came.
			"/24/1/1033": man,
		}
		for k, w := range want {
			if !bytes.Equal(got[k], w) {
				t.Fatalf("emit(%#x): %s = %x, want %x", fileOff, k, got[k], w)
			}
		}
	}
}

// TestRsrcEmitIsDeterministic guards the property that lets the linker reason
// about the section at all: emitting the same tree twice gives the same bytes.
// A map iterated in random order would not.
func TestRsrcEmitIsDeterministic(t *testing.T) {
	r, err := parseRsrc(iconTree([]byte{1, 2, 3}, []byte{4, 5}))
	if err != nil {
		t.Fatalf("parseRsrc: %v", err)
	}
	a, b := r.emit(0x600), r.emit(0x600)
	if !bytes.Equal(a, b) {
		t.Fatal("emit is not deterministic")
	}
}

// ---------------------------------------------------------------------------
// Into a PE image
// ---------------------------------------------------------------------------

// buildRsrcPE links a trivial image carrying one .rsrc section and returns the
// bytes.
func buildRsrcPE(t *testing.T, rsrcBytes []byte) []byte {
	t.Helper()
	img := NewImage(TargetPE)
	img.Entry = "_start"
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0x48, 0x31, 0xC0, 0xC3} // xor rax,rax; ret
	text.VSize = len(text.Data)
	img.Syms["_start"] = SymLoc{Sect: 0, Off: 0}
	r, err := parseRsrc(rsrcBytes)
	if err != nil {
		t.Fatalf("parseRsrc: %v", err)
	}
	img.Rsrc = r
	out := filepath.Join(t.TempDir(), "probe.exe")
	if err := img.BuildPE(out); err != nil {
		t.Fatalf("BuildPE: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return b
}

// peSection finds a section by name in a PE image and returns its file offset
// and raw size.
func peSection(t *testing.T, exe []byte, name string) (fileOff, rawSz int) {
	t.Helper()
	if string(exe[0:2]) != "MZ" {
		t.Fatalf("not a PE file")
	}
	nsec := int(rd16(exe, 0x86))
	for i := 0; i < nsec; i++ {
		p := 0x188 + i*40
		if strings.TrimRight(string(exe[p:p+8]), " \x00") == name {
			return int(rdU32(exe, p+20)), int(rdU32(exe, p+16))
		}
	}
	t.Fatalf("no %s section in the image", name)
	return 0, 0
}

// TestBuildPEWritesResourceSection checks the three things that make a resource
// section reachable, all of which fail silently when wrong: the section exists,
// the data directory points at it, and the directory has a non-zero size. A
// correct RVA with a zero size is a directory the loader walks past, so the
// icon is simply absent.
func TestBuildPEWritesResourceSection(t *testing.T) {
	ico := []byte{0x28, 0, 0, 0, 1, 0}
	grp := []byte{0, 0, 1, 0}
	exe := buildRsrcPE(t, buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(0x0409, ico)}}),
		}}),
		rsrcIDDir(14, &rsrcDirSpec{nodes: []rsrcNode{
			rsrcNameDir("IDI_ICON", &rsrcDirSpec{nodes: []rsrcNode{
				rsrcIDNode(0x0409, grp),
			}}),
		}}),
	}}))

	fileOff, rawSz := peSection(t, exe, ".rsrc")
	if rawSz == 0 {
		t.Fatal(".rsrc has SizeOfRawData 0")
	}
	// Data directory 2, at oh+112+2*8.
	dirRVA := rdU32(exe, 0x98+112+2*8)
	dirSize := rdU32(exe, 0x98+112+2*8+4)
	if dirRVA == 0 || dirSize == 0 {
		t.Fatalf("resource data directory = RVA %#x size %#x, want both non-zero", dirRVA, dirSize)
	}
	var rsrcVA int
	if p := 0x188; strings.TrimRight(string(exe[p:p+8]), " \x00") == ".rsrc" {
		rsrcVA = int(rdU32(exe, p+12))
	} else {
		nsec := int(rd16(exe, 0x86))
		for i := 0; i < nsec; i++ {
			p := 0x188 + i*40
			if strings.TrimRight(string(exe[p:p+8]), " \x00") == ".rsrc" {
				rsrcVA = int(rdU32(exe, p+12))
			}
		}
	}
	if dirRVA != rsrcVA {
		t.Fatalf("resource data directory RVA = %#x, want the .rsrc section's %#x", dirRVA, rsrcVA)
	}

	// And the tree inside must be walkable, with payloads where it says.
	blob := exe[fileOff : fileOff+rawSz]
	got := readTree(t, blob, fileOff)
	if !bytes.Equal(got["/3/1/1033"], ico) {
		t.Fatalf("icon payload = %x, want %x", got["/3/1/1033"], ico)
	}
	if !bytes.Equal(got["/14/IDI_ICON/1033"], grp) {
		t.Fatalf("group icon payload = %x, want %x", got["/14/IDI_ICON/1033"], grp)
	}
}

// TestBuildPEWithoutResourcesHasNoResourceDirectory is the other direction: no
// .rsrc must mean no section and a zeroed directory, not an empty one.
func TestBuildPEWithoutResourcesHasNoResourceDirectory(t *testing.T) {
	img := NewImage(TargetPE)
	img.Entry = "_start"
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0xC3}
	text.VSize = 1
	img.Syms["_start"] = SymLoc{Sect: 0, Off: 0}
	out := filepath.Join(t.TempDir(), "plain.exe")
	if err := img.BuildPE(out); err != nil {
		t.Fatalf("BuildPE: %v", err)
	}
	exe, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	nsec := int(rd16(exe, 0x86))
	for i := 0; i < nsec; i++ {
		p := 0x188 + i*40
		if strings.TrimRight(string(exe[p:p+8]), " \x00") == ".rsrc" {
			t.Fatal("an image with no resources grew a .rsrc section")
		}
	}
	if got := rdU32(exe, 0x98+112+2*8); got != 0 {
		t.Fatalf("resource data directory RVA = %#x, want 0", got)
	}
}

// TestMergeRsrcThenBuildPE is the combination the linker actually performs:
// two objects, each with resources, merged and written into one image.
func TestMergeRsrcThenBuildPE(t *testing.T) {
	mk := func(nodes []rsrcNode) *rsrc {
		r, err := parseRsrc(buildTree(&rsrcDirSpec{nodes: nodes}))
		if err != nil {
			t.Fatalf("parseRsrc: %v", err)
		}
		return r
	}
	icon := mk([]rsrcNode{rsrcIDDir(3, &rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDDir(1, &rsrcDirSpec{nodes: []rsrcNode{rsrcIDNode(0x0409, []byte{1, 2, 3})}}),
	}})})
	manifest := mk([]rsrcNode{rsrcIDNode(24, []byte("manifest bytes"))})
	if err := icon.merge(manifest); err != nil {
		t.Fatalf("merge: %v", err)
	}

	img := NewImage(TargetPE)
	img.Entry = "_start"
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0xC3}
	text.VSize = 1
	img.Syms["_start"] = SymLoc{Sect: 0, Off: 0}
	img.Rsrc = icon
	out := filepath.Join(t.TempDir(), "merged.exe")
	if err := img.BuildPE(out); err != nil {
		t.Fatalf("BuildPE: %v", err)
	}
	exe, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	fileOff, rawSz := peSection(t, exe, ".rsrc")
	got := readTree(t, exe[fileOff:fileOff+rawSz], fileOff)
	if !bytes.Equal(got["/3/1/1033"], []byte{1, 2, 3}) {
		t.Fatalf("icon payload = %x, want 010203", got["/3/1/1033"])
	}
	if !bytes.Equal(got["/24/1/1033"], []byte("manifest bytes")) {
		t.Fatalf("manifest payload = %x, want the manifest bytes", got["/24/1/1033"])
	}
}

// TestBuildPESectionVirtualSizes is here because adding .rsrc forced the section
// table to be written in two passes, and the rewrite quietly changed
// VirtualSize from the section's own vsize to len(its data).
//
// Those are the same number for every section except .bss, whose size lives only
// in vsize -- it has no file bytes at all. So a .bss would have been declared
// zero-length, which the loader accepts and then hands its address space to
// whatever comes next. Nothing complains; the corruption shows up much later.
func TestBuildPESectionVirtualSizes(t *testing.T) {
	img := NewImage(TargetPE)
	img.Entry = "_start"
	text := newSection(img, ".text", false, true)
	text.Data = []byte{0x48, 0x31, 0xC0, 0xC3}
	text.VSize = len(text.Data)
	img.Syms["_start"] = SymLoc{Sect: 0, Off: 0}

	bss := newSection(img, ".bss", true, false)
	bss.Bss = true
	bss.VSize = 64 // virtual space only: no Data at all

	img.Rsrc, _ = parseRsrc(buildTree(&rsrcDirSpec{nodes: []rsrcNode{
		rsrcIDNode(24, []byte("manifest")),
	}}))

	out := filepath.Join(t.TempDir(), "sizes.exe")
	if err := img.BuildPE(out); err != nil {
		t.Fatalf("BuildPE: %v", err)
	}
	exe, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	want := map[string]int{".text": 4, ".bss": 64}
	nsec := int(rd16(exe, 0x86))
	found := map[string]int{}
	for i := 0; i < nsec; i++ {
		p := 0x188 + i*40
		name := strings.TrimRight(string(exe[p:p+8]), " \x00")
		found[name] = int(rdU32(exe, p+8)) // VirtualSize
	}
	for name, v := range want {
		if found[name] != v {
			t.Errorf("%s VirtualSize = %d, want %d", name, found[name], v)
		}
	}
	// .rsrc is sized but empty at the point the table is written, and its size
	// comes from the tree -- a zero here would declare an empty section even
	// though the bytes land right after.
	if got := found[".rsrc"]; got == 0 {
		t.Error(".rsrc VirtualSize = 0, want the tree's size")
	}
	if _, ok := found[".bss"]; !ok {
		t.Error("no .bss section in the image")
	}
}
