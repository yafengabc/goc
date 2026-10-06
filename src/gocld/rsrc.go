package gocld

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// PE resource section (.rsrc): parsing, merging and writing.
//
// A .rsrc section is not data. Its bytes are a tree of IMAGE_RESOURCE_DIRECTORY
// tables hanging off a single root, and every leaf records where its payload
// lives by a plain offset from the start of the section. That is what makes the
// section a linker problem rather than a copying one:
//
//   - IMAGE_RESOURCE_DATA_ENTRY.OffsetToData is a FILE offset, not an RVA, so
//     it cannot be left as it arrived. Every leaf has to be rewritten once the
//     section's own position in the output file is known -- and that position
//     is only decided after the headers are sized, which depends on the
//     section's size, which is what we are computing. The way out is to note
//     that sizing depends on the tree alone, so the tree can be measured first
//     and emitted second.
//
//   - The tree is keyed by (type, name-or-id, language), so two objects that
//     both carry an icon have to merge into one RT_ICON node with two leaves.
//     Appending the second tree wholesale gives the loader two RT_ICON entries
//     and it shows only the first.
//
//   - The payloads are opaque. An icon is not "data" in any sense a linker can
//     check, so the bytes are moved and never inspected -- which is the only
//     correct thing to do with them.
//
// Merging is by tree key. Two objects carrying the same icon are the same
// resource and the linker keeps one; two objects carrying different bytes under
// one key is a duplicate definition and is reported, because silently keeping
// one is how a program ships the wrong icon.

// Resource tree constants.
//
// Both halves of a directory entry use the top bit as a flag. The Name field's
// top bit says "the low 31 bits are an offset to a string, not an integer id";
// the Data field's top bit says "the low 31 bits are a subdirectory, not a
// data entry". They are the same bit in different fields, and both are read
// here rather than assumed, because a wrong reading does not fail -- it
// produces a plausible number that walks off the section a few bytes later.
const (
	// rscIsString marks a Name field as holding a string offset rather than
	// an integer id. rscNameOff masks that offset back out.
	rscIsString = 0x80000000
	rscNameOff  = 0x7FFFFFFF
	// rscIsDir marks a Data field as pointing at a subdirectory rather than
	// at an IMAGE_RESOURCE_DATA_ENTRY. What is left in the field is the
	// offset: 31 bits of it, and no flag bits -- a synthesised low bit would
	// shift an odd offset onto the wrong byte.
	rscIsDir   = 0x80000000
	rscOffMask = 0x7FFFFFFF
	rscSize    = 16 // IMAGE_RESOURCE_DIRECTORY
	rscEnt     = 8  // IMAGE_RESOURCE_DIRECTORY_ENTRY
	rscData    = 16 // IMAGE_RESOURCE_DATA_ENTRY

	// rsrcMaxDepth bounds the recursion. Three levels is the format's design
	// (type, name, language); a hand-built object with a deeper tree is not
	// rejected outright, but one that describes a cycle would otherwise hang
	// the linker, so the walk needs a limit.
	rsrcMaxDepth = 8

	// rsrcDefaultLang is the language a tree that stops short is filed under.
	// Windows uses en-US for an unlabelled resource, and a tree produced by a
	// tool defaulting to the machine's language is still expected to load.
	rsrcDefaultLang = 0x0409

	// rsrcDefaultNameID is the name-or-id given to a payload that arrived with
	// no level of its own. RT_MANIFEST is looked up as type/1/language, so 1 is
	// the resource's own number rather than anything invented.
	rsrcDefaultNameID = 1
)

var (
	errRsrcShort   = errors.New("rsrc: truncated section")
	errRsrcBadLeaf = errors.New("rsrc: data entry points outside the section")
	errRsrcShape   = errors.New("rsrc: same key is a directory in one object and a resource in another")
	errRsrcCycle   = errors.New("rsrc: tree is deeper than the format allows")
)

// rsrcLeaf is the payload of a resource plus the two fields the format states
// about it beyond its content.
type rsrcLeaf struct {
	data []byte
	// codepage is the CodePage field. Zero is the overwhelmingly common value
	// and means "no opinion", which is what windres emits; a resource that
	// declares a real code page keeps it.
	codepage int
	// reserved is written back as it arrived. It is documented as reserved and
	// always zero, so a producer that sets it is either mistaken or knows
	// something the format does not say -- either way not ours to drop.
	reserved int
}

// rsrcDir is one directory in the tree.
//
// named and ided hold the two spaces the format keeps apart, and they are not
// interleaved because the format does not require it: a loader binary-searches
// only the ID list, and only when the name list is empty. Keeping them
// separate makes that invariant visible here rather than something to
// rediscover at write time.
type rsrcDir struct {
	// Characteristics and TimeDateStamp are the two leading dwords of the
	// table. Only the root's are kept, and they are written back unchanged: both
	// are cosmetic, and a merge has no more right to invent a value for them
	// than to erase one.
	characteristics int
	timestamp       int

	named []rsrcEnt
	ided  []rsrcEnt
}

// subs returns the child directories this one points at, in the order they are
// written: named entries first, then ided, mirroring the two lists.
//
// It is derived rather than stored. A parallel list of children is the same
// information twice, and it is information that has to be kept in step -- a
// child added to an entry and not to the list is written nowhere at all, and
// the section comes out short and valid, which is the worst way to fail.
func (d *rsrcDir) subs() []*rsrcDir {
	var out []*rsrcDir
	for _, e := range d.named {
		if e.sub != nil {
			out = append(out, e.sub)
		}
	}
	for _, e := range d.ided {
		if e.sub != nil {
			out = append(out, e.sub)
		}
	}
	return out
}

// rsrcEnt is one entry: a key, and either a child directory or a leaf. The key
// is a name if name is set, else an id -- never both, which mergeRsrcLevel
// relies on when it looks for a duplicate.
type rsrcEnt struct {
	name string
	id   int // -1 when keyed by name
	sub  *rsrcDir
	leaf *rsrcLeaf
}

// rsrc is a whole resource tree, ready to be written.
type rsrc struct {
	// root carries the root table's Characteristics and TimeDateStamp; the
	// first tree to arrive decides them and later ones are ignored, rather than
	// inventing a rule that looks like a merge but is arbitrary.
	root rsrcDir

	// dup records keys two objects filed different bytes under, and shape
	// records a key that is a directory in one object and a leaf in another.
	// They are collected rather than returned immediately because a merge
	// visits many keys and the first failure is rarely the informative one;
	// the caller reports them all at once, which is what makes a duplicate
	// resource diagnosable at all.
	dup   []rsrcDupKey
	shape bool
}

// parseRsrc reads a .rsrc section into a tree.
//
// The three levels are type -> name/id -> language, but a producer may stop
// early: windres emits all three, a hand-built object may emit two. A missing
// level is filled in rather than rejected, because the loader tolerates it and
// refusing would be a stricter rule than the platform imposes.
//
// A malformed tree -- truncated, cyclic, a leaf pointing outside the section --
// is an error, not a best-effort parse. Silently dropping half a resource is how
// a program ships with a missing icon.
//
// Short trees are left as they are found and filled in by fillMissingRsrcLevels
// once every object has been merged. Doing it here would be wrong: filling
// invents an id, and a merge that matched an invented id against a real one
// would either miss a genuine conflict or invent a false one.
func parseRsrc(data []byte) (*rsrc, error) {
	if len(data) < rscSize {
		return nil, errRsrcShort
	}
	r := &rsrc{}
	r.root.characteristics = rdU32(data, 0)
	r.root.timestamp = rdU32(data, 4)
	if err := parseRsrcDir(data, 0, &r.root, 0); err != nil {
		return nil, err
	}
	return r, nil
}

// fillMissingRsrcLevels gives a tree that stops short its remaining levels.
//
// The design is three deep -- type, name-or-id, language -- but a producer may
// stop earlier, and the shape that occurs in practice is a payload filed
// directly under its type. The loader walks whatever depth it finds, so such a
// tree is valid input and rejecting it would be a stricter rule than the
// platform imposes.
//
// What the missing middle level is filled with follows the convention for the
// types that take one: RT_MANIFEST is looked up as type/1/language, so id 1 is
// the name-or-id of the resource itself. The language is en-US, which is what
// Windows files an unlabelled resource under.
//
// Only the middle level can be missing. A leaf under the type has exactly one
// level short of it, and a leaf one level below that has none; a leaf at the
// language level is where a leaf belongs and is left strictly alone.
func fillMissingRsrcLevels(root *rsrcDir) {
	fillRsrcLevel(root, 0)
}

func fillRsrcLevel(d *rsrcDir, depth int) {
	wrap := func(e *rsrcEnt) {
		if e.sub != nil {
			fillRsrcLevel(e.sub, depth+1)
			return
		}
		if e.leaf == nil || depth >= 1 {
			return
		}
		e.sub = &rsrcDir{ided: []rsrcEnt{{
			id:  rsrcDefaultNameID,
			sub: &rsrcDir{ided: []rsrcEnt{{id: rsrcDefaultLang, leaf: e.leaf}}},
		}}}
		e.leaf = nil
	}
	for i := range d.named {
		wrap(&d.named[i])
	}
	for i := range d.ided {
		wrap(&d.ided[i])
	}
}

func parseRsrcDir(data []byte, off int, dir *rsrcDir, depth int) error {
	if depth > rsrcMaxDepth {
		return errRsrcCycle
	}
	if off < 0 || off+rscSize > len(data) {
		return errRsrcShort
	}
	// NumberOfNamedEntries and NumberOfIdEntries are WORDs, not dwords. Reading
	// them as one dword each does not fail -- it produces a count large enough
	// to run the bounds check off the end of the section, so a tree that is
	// perfectly good comes back as "truncated". There are no fields after them,
	// so the dword read also swallows the first entry's name.
	named := rd16(data, off+12)
	ided := rd16(data, off+14)
	if off+rscSize+(named+ided)*rscEnt > len(data) {
		return errRsrcShort
	}
	p := off + rscSize
	for i := 0; i < named; i++ {
		e, next, err := parseRsrcEntry(data, p, depth)
		if err != nil {
			return err
		}
		p = next
		dir.named = append(dir.named, e)
	}
	for i := 0; i < ided; i++ {
		e, next, err := parseRsrcEntry(data, p, depth)
		if err != nil {
			return err
		}
		p = next
		dir.ided = append(dir.ided, e)
	}
	return nil
}

func parseRsrcEntry(data []byte, p, depth int) (rsrcEnt, int, error) {
	var e rsrcEnt
	if p < 0 || p+rscEnt > len(data) {
		return e, 0, errRsrcShort
	}
	nameField := rdU32(data, p)
	offField := rdU32(data, p+4)
	e.id = -1

	// The entry is a fixed 8 bytes whatever the name is: a string name's offset
	// points into the pool that follows the entry array, not at the entry's own
	// tail, so the cursor for the next entry never depends on the name.
	next := p + rscEnt
	if nameField&rscIsString != 0 {
		s, err := readRsrcName(data, nameField&rscNameOff)
		if err != nil {
			return e, 0, err
		}
		e.name = s
	} else {
		e.id = nameField
	}

	childOff := offField & rscOffMask
	if offField&rscIsDir == 0 {
		leaf, err := parseRsrcLeaf(data, childOff)
		if err != nil {
			return e, 0, err
		}
		e.leaf = leaf
		return e, next, nil
	}
	sub := &rsrcDir{}
	if err := parseRsrcDir(data, childOff, sub, depth+1); err != nil {
		return e, 0, err
	}
	e.sub = sub
	return e, next, nil
}

// readRsrcName reads the UTF-16 name at off: a WORD length in code units
// followed by that many UTF-16 units. The length prefix is part of the name's
// encoding, so the caller passes only the offset and does not learn that it
// exists -- which is also why a length in code units rather than bytes is
// never in danger of being read as a byte count.
//
// Surrogate pairs are combined, and that is not tidiness: two objects spelling
// the same name differently must produce the same key, or the merge silently
// splits one resource into two and the loader shows only the first.
func readRsrcName(data []byte, off int) (string, error) {
	nchars := rd16(data, off)
	need := off + 2 + nchars*2
	if off < 0 || nchars < 0 || need > len(data) {
		return "", errRsrcShort
	}
	u := make([]uint16, nchars)
	for i := range u {
		u[i] = uint16(rd16(data, off+2+i*2))
	}
	return string(utf16.Decode(u)), nil
}

// parseRsrcLeaf reads one IMAGE_RESOURCE_DATA_ENTRY and the payload it points
// at. Only the header is interpreted; the bytes after it are the resource, and
// the linker has no business looking inside.
func parseRsrcLeaf(data []byte, off int) (*rsrcLeaf, error) {
	if off < 0 || off+rscData > len(data) {
		return nil, errRsrcShort
	}
	dataOff := rdU32(data, off)
	size := rdU32(data, off+4)
	if dataOff < 0 || size < 0 || dataOff+size > len(data) {
		// A wrong length here would otherwise read whatever follows in the
		// file -- in an object, that is the symbol table.
		return nil, errRsrcBadLeaf
	}
	return &rsrcLeaf{
		data:     append([]byte(nil), data[dataOff:dataOff+size]...),
		codepage: rdU32(data, off+8),
		reserved: rdU32(data, off+12),
	}, nil
}

// merge folds other's tree into r, matching entries by key at every level.
//
// The levels are type -> name/id -> language, and each is keyed, so a duplicate
// at any level means the leaves below it are the same resource. That is the
// normal case when two objects share a resource file, and it is why an
// identical payload is not treated as a conflict. Different bytes under one key
// is a duplicate definition, and a key that is a directory in one object and a
// leaf in another has no resolution at all -- both are recorded and reported
// once the whole merge is done, so the user sees every collision rather than
// only the first one the walk happened to reach.
func (r *rsrc) merge(o *rsrc) error {
	r.mergeDir(&r.root, &o.root, "", 0)
	if r.shape {
		return errRsrcShape
	}
	if len(r.dup) > 0 {
		keys := make([]string, 0, len(r.dup))
		for _, d := range r.dup {
			keys = append(keys, d.String())
		}
		sort.Strings(keys)
		return fmt.Errorf("rsrc: resource defined twice with different contents: %s",
			strings.Join(keys, "; "))
	}
	return nil
}

// mergeDir merges one directory's entries and recurses into the directories
// they name. A key that is a directory on one side and a leaf on the other has
// no correct resolution, so it stops the merge with an error rather than
// picking one.
//
// prefix is the path to dst itself -- the keys of the directories above it,
// joined by '/'. It is carried down rather than reconstructed because a
// conflict is only diagnosable if the user is told WHICH resource it is, and
// the leaf key alone ("id 1033") appears once per resource type.
func (r *rsrc) mergeDir(dst, src *rsrcDir, prefix string, depth int) {
	for _, e := range src.named {
		r.mergeEntry(dst, e, prefix, depth)
	}
	for _, e := range src.ided {
		r.mergeEntry(dst, e, prefix, depth)
	}
}

func (r *rsrc) mergeEntry(dst *rsrcDir, e rsrcEnt, prefix string, depth int) {
	list := dst.ided
	if e.name != "" {
		list = dst.named
	}
	for i := range list {
		cur := &list[i]
		if cur.name != e.name || cur.id != e.id {
			continue
		}
		here := prefix + "/" + rsrcKey(e, depth)
		if cur.sub != nil && e.sub != nil {
			r.mergeDir(cur.sub, e.sub, here, depth+1)
			return
		}
		if cur.leaf != nil && e.leaf != nil {
			// The same resource twice. Keep the one already here and say so,
			// because identical bytes under one key is expected while
			// different ones is a duplicate definition the user needs to see.
			//
			// The sizes are reported rather than a diff: a linker that printed
			// icon bytes would be unreadable, and "same length, different
			// content" is the case where a size alone tells the user nothing
			// useful -- so the path is what carries the diagnosis, and it is
			// spelled out in full for that reason.
			if string(cur.leaf.data) != string(e.leaf.data) {
				r.dup = append(r.dup, rsrcDupKey{
					path: strings.TrimPrefix(here, "/"),
					oldN: len(cur.leaf.data),
					newN: len(e.leaf.data),
				})
			}
			return
		}
		// A directory against a leaf: nothing to merge into.
		r.shape = true
		return
	}
	if e.name != "" {
		dst.named = append(dst.named, e)
	} else {
		dst.ided = append(dst.ided, e)
	}
}

// rsrcKey names one entry: the string name if it has one, else the id.
//
// Only the type level has conventional names -- RT_ICON and the rest are
// defined for that level only, and a language id of 3 means nothing at any
// other. Applying the type table at every level produced messages like
// "RT_ICON/type#1033", where the second component is a language wearing a
// type's name.
func rsrcKey(e rsrcEnt, depth int) string {
	if e.name != "" {
		return e.name
	}
	if depth == 0 {
		return rsrcTypeName(e.id)
	}
	return "id " + strconv.Itoa(e.id)
}

func rsrcTypeName(id int) string {
	switch id {
	case 1:
		return "RT_CURSOR"
	case 2:
		return "RT_BITMAP"
	case 3:
		return "RT_ICON"
	case 4:
		return "RT_MENU"
	case 5:
		return "RT_DIALOG"
	case 6:
		return "RT_STRING"
	case 7:
		return "RT_FONTDIR"
	case 8:
		return "RT_FONT"
	case 9:
		return "RT_ACCELERATOR"
	case 10:
		return "RT_RCDATA"
	case 11:
		return "RT_MESSAGETABLE"
	case 12:
		return "RT_GROUP_CURSOR"
	case 14:
		return "RT_GROUP_ICON"
	case 16:
		return "RT_VERSION"
	case 24:
		return "RT_MANIFEST"
	}
	return "type#" + strconv.Itoa(id)
}

// size is the section's length, which depends on the tree alone -- not on where
// the section lands in the file. That independence is what lets the caller
// reserve the right number of bytes before emitting: the leaf offsets need the
// file position, and the file position needs the size, and a value that could
// depend on both would be circular.
func (r *rsrc) size() int {
	fillMissingRsrcLevels(&r.root)
	l := newRsrcLayout(0)
	l.place(&r.root)
	return l.next
}

// emit lays the tree out for a section that will start at fileOff in the output
// file, and returns the section's bytes.
//
// fileOff is an input because IMAGE_RESOURCE_DATA_ENTRY.OffsetToData is a file
// offset: no leaf can be written until the section's position is known. The
// section's SIZE, though, depends only on the tree, so it can be settled first
// and the bytes follow.
//
// The fill runs here as well as in size(), so that emitting an unfilled tree
// cannot produce a buffer whose length disagrees with the length the caller
// reserved from an earlier size(). It is idempotent, so calling both costs
// nothing and the order the caller uses does not matter.
func (r *rsrc) emit(fileOff int) []byte {
	fillMissingRsrcLevels(&r.root)
	l := newRsrcLayout(fileOff)
	l.place(&r.root)
	buf := make([]byte, l.next)
	l.writeDir(buf, &r.root)
	return buf
}

func newRsrcLayout(fileOff int) *rsrcLayout {
	return &rsrcLayout{
		fileOff: fileOff,
		dirAt:   map[*rsrcDir]int{},
		leafAt:  map[*rsrcLeaf]int{},
	}
}

// rsrcLayout holds every offset the two passes agree on. Keeping them in one
// place is the point: an entry's data field, a directory's table, and a
// payload's file offset are three different numbers that must be consistent, and
// when they are computed in three places they drift.
type rsrcLayout struct {
	next    int
	fileOff int
	dirAt   map[*rsrcDir]int
	leafAt  map[*rsrcLeaf]int
}

func (l *rsrcLayout) align4() {
	for l.next%4 != 0 {
		l.next++
	}
}

func (l *rsrcLayout) align8() {
	for l.next%8 != 0 {
		l.next++
	}
}

// place assigns a section-relative offset to d and everything under it, and
// remembers it. It writes no bytes: an entry needs its child's offset, and the
// child may be written later.
func (l *rsrcLayout) place(d *rsrcDir) {
	l.align4()
	l.dirAt[d] = l.next
	l.next += rscSize + (len(d.named)+len(d.ided))*rscEnt
	// The name pool follows the table, in the order the entries reference it.
	//
	// Each name costs a length word, the code units, AND a NUL. Leaving the
	// NUL out makes every offset after the first string name one word short of
	// where it is written -- which reads as a tree whose payloads start in the
	// middle of their own headers, and every such payload comes back as zeros
	// rather than as an error.
	for i := range d.named {
		if d.named[i].name != "" {
			l.next += 2 + utf16Len(d.named[i].name)*2 + 2
		}
	}
	for _, sub := range d.subs() {
		l.place(sub)
	}
	for _, e := range d.named {
		l.placeLeaf(e)
	}
	for _, e := range d.ided {
		l.placeLeaf(e)
	}
}

func (l *rsrcLayout) placeLeaf(e rsrcEnt) {
	if e.leaf == nil {
		return
	}
	l.align4()
	l.leafAt[e.leaf] = l.next
	l.next += rscData
	l.align8()
	l.next += len(e.leaf.data)
}

// writeDir emits one directory: its table, its name pool, its entries, then the
// directories and leaves those entries name -- in the order place assigned them,
// so the offsets recorded above still describe what is written.
func (l *rsrcLayout) writeDir(buf []byte, d *rsrcDir) {
	dOff := l.dirAt[d]
	putU32at(buf, dOff+0, uint32(d.characteristics))
	putU32at(buf, dOff+4, uint32(d.timestamp))
	// Then four words: MajorVersion, MinorVersion, and the two entry counts.
	// Reading any of them as a dword runs into the next field -- and the counts
	// are the last two, so a dword read of them reaches straight into the first
	// entry and reports a count far larger than the table.
	putU16at(buf, dOff+8, 0)
	putU16at(buf, dOff+10, 0)
	putU16at(buf, dOff+12, uint16(len(d.named)))
	putU16at(buf, dOff+14, uint16(len(d.ided)))

	// The name pool, in the same order the entries below will reference it.
	pool := dOff + rscSize + (len(d.named)+len(d.ided))*rscEnt
	nameAt := map[string]int{}
	for i := range d.named {
		n := d.named[i].name
		if n == "" {
			continue
		}
		nameAt[n] = pool
		putU16at(buf, pool, uint16(utf16Len(n)))
		putUTF16at(buf, pool+2, n)
		putU16at(buf, pool+2+utf16Len(n)*2, 0) // NUL terminator
		pool += 2 + utf16Len(n)*2 + 2
	}

	// Entries: named first, then ided -- the order the counts above describe.
	e := dOff + rscSize
	for i := range d.named {
		e = l.writeEntry(buf, d.named[i], e, nameAt)
	}
	for i := range d.ided {
		e = l.writeEntry(buf, d.ided[i], e, nameAt)
	}

	for _, sub := range d.subs() {
		l.writeDir(buf, sub)
	}
	for _, ent := range d.named {
		l.writeLeaf(buf, ent)
	}
	for _, ent := range d.ided {
		l.writeLeaf(buf, ent)
	}
}

// writeEntry emits one directory entry and returns the offset just past it.
//
// The data field carries a flag in its high bit and a leaf flag in its low bit,
// and the two together say how to read the offset that remains. A stale flag
// here gives the loader an entry it walks as a directory when it is a leaf, or
// the reverse, and nothing complains until the resource is asked for.
func (l *rsrcLayout) writeEntry(buf []byte, ent rsrcEnt, e int, nameAt map[string]int) int {
	if ent.name != "" {
		// A named entry's Name field is an offset into the string pool with
		// the top bit set to say so. Writing the offset bare makes the loader
		// read the name as the integer 96, which is not a resource type.
		putU32at(buf, e, uint32(nameAt[ent.name])|rscIsString)
	} else {
		putU32at(buf, e, uint32(ent.id))
	}
	e += 4
	switch {
	case ent.sub != nil:
		putU32at(buf, e, uint32(l.dirAt[ent.sub])|rscIsDir)
	case ent.leaf != nil:
		// The data entry's own offset, and nothing else. An extra low bit
		// here is not a flag the format defines, and it corrupts the offset
		// rather than describing anything.
		putU32at(buf, e, uint32(l.leafAt[ent.leaf]))
	}
	return e + 4
}

// writeLeaf emits a resource's data entry and its payload.
//
// OffsetToData is where the file-offset trap sits: it is a file offset, so it
// is the section's own position plus the payload's offset within it. Two things
// are easy to get wrong here and both produce a file nothing complains about.
// Using the section-relative offset instead of a file offset, and pointing at
// the data entry rather than at the bytes AFTER it -- the entry describes the
// payload, it is not the payload. The second one is worse: the loader reads the
// entry's own first field as the first four bytes of the icon, so the resource
// exists, has the right size, and is garbage.
//
// A leaf is placed with its header at leafAt and its payload immediately after,
// so the payload is at leafAt+rscData.
func (l *rsrcLayout) writeLeaf(buf []byte, ent rsrcEnt) {
	if ent.leaf == nil {
		return
	}
	d := l.leafAt[ent.leaf]
	if d+rscData > len(buf) {
		return
	}
	putU32at(buf, d+0, uint32(l.fileOff+d+rscData))
	putU32at(buf, d+4, uint32(len(ent.leaf.data)))
	putU32at(buf, d+8, uint32(ent.leaf.codepage))
	putU32at(buf, d+12, uint32(ent.leaf.reserved))
	copy(buf[d+rscData:], ent.leaf.data)
}

// utf16Len returns the number of UTF-16 code units a string needs, which is not
// always the number of characters: a rune outside the BMP takes two. The name
// pool and the entries both count in units, so a length that ignored this would
// cut a long name short -- and the cut lands in the middle of a surrogate pair,
// producing a name that is not the one that was written.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// putUTF16at writes s as UTF-16 code units at off.
func putUTF16at(b []byte, off int, s string) {
	i := off
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			putU16at(b, i, uint16(0xD800+(r>>10)))
			putU16at(b, i+2, uint16(0xDC00+(r&0x3FF)))
			i += 4
			continue
		}
		putU16at(b, i, uint16(r))
		i += 2
	}
}

// rsrcDupKey names a resource two objects define differently.
type rsrcDupKey struct {
	path       string
	oldN, newN int
}

func (d rsrcDupKey) String() string {
	return fmt.Sprintf("%s (%d bytes vs %d bytes)", d.path, d.oldN, d.newN)
}
