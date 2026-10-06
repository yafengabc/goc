package gocld

// COFF object emission -- the other direction of coff.go.
//
// coff.go reads a COFF object (LLVM's, or one of ours) and splices it into an
// Image. This file writes one: it takes an Image whose fixups have NOT been
// applied and produces a relocatable COFF .o holding the section bytes, the
// symbol table and the relocation table.
//
// This is what makes gocld a linker rather than a packaging step. Until now the
// only producer of an Image was goa's assembler and the only consumer was
// BuildPE/BuildELF, so the two were always adjacent: a program existed only as
// a finished exe. With a writer the pipeline can stop in the middle:
//
//	.c -> goa -> Image -> .o     (relocations pending, addresses unknown)
//	.o + .o -> gocld -> exe      (addresses decided, relocations applied)
//
// which is the whole reason `-c` could not mean "produce an object".
//
// The format is standard COFF as documented in the PE/COFF spec and as emitted
// by LLVM and MSVC, so the .o files are inspectable with `objdump -h/-t/-r` and
// usable by any COFF-aware tool. It is the classic 18-byte symbol record rather
// than bigobj: goc's objects hold a handful of symbols and the read path
// already handles both widths.
//
// The round trip is the correctness argument. IngestCOFFBytes is the inverse of
// this file, so writing an object and reading it straight back must reproduce
// the same sections, symbols and relocations -- which is what
// TestCOFFObjectRoundTrip checks. A writer nobody reads back is a writer nobody
// knows is correct.

import (
	"encoding/binary"
	"errors"
	"sort"
	"strings"
)

// coffLibSymPrefix marks the static symbol whose name is the list of C library
// symbols this object defines. COFF has no metadata section, and a symbol is
// the one place the format lets a producer say something the linker reads but
// no instruction refers to. The prefix cannot be a C identifier's, so the name
// can never collide with a real symbol.
const coffLibSymPrefix = ".goc_lib:"

// coffDllSymPrefix marks the static symbol whose name maps this object's
// imported Win32 entry points to the DLL each comes from, as "Name=user32" pairs
// joined by commas.
//
// It rides along for the same reason as .goc_lib, and it is not optional: a
// COFF object leaves every symbol it does not define undefined, with nothing in
// the file saying which DLL would export it. That knowledge exists only in the
// compiler, which read it off the `extern Name, user32` prototype in the
// goclib header. Drop it and the link has to rediscover it from a table of
// every Win32 entry point the linker has heard of -- which is how a program
// that compiled and ran in one step stopped linking the moment it went through
// an object file, and why the failure named symbols like GetSystemMetrics
// rather than anything about imports.
const coffDllSymPrefix = ".goc_dll:"

// COFF section characteristics. The read path tests for the CNT bits, so an
// object written here is recognised the same way when it comes back.
const (
	scnCntCode              = 0x00000020 // IMAGE_SCN_CNT_CODE
	scnCntInitializedData   = 0x00000040 // IMAGE_SCN_CNT_INITIALIZED_DATA
	scnCntUninitializedData = 0x00000080 // IMAGE_SCN_CNT_UNINITIALIZED_DATA
	scnMemExecute           = 0x20000000
	scnMemRead              = 0x40000000
	scnMemWrite             = 0x80000000
	scnAlign1Bytes          = 0x00100000 // IMAGE_SCN_ALIGN_1BYTES
	scnAlign8Bytes          = 0x00400000 // IMAGE_SCN_ALIGN_8BYTES
)

// imageFileExecutableImage is the one file characteristic worth setting: the
// object holds code, and tools that filter on it expect it.
const imageFileExecutableImage = 0x0002

// coffTypeFunction is IMAGE_SYM_DTYPE_FUNCTION. A debugging nicety rather
// than something a linker needs, but it is what makes `objdump -t` list
// symbols with an upper-case 'F' and it costs nothing to be right about.
const coffTypeFunction = 0x20

// coffRelOut is one relocation as it will be written.
type coffRelOut struct {
	secNum int32 // 1-based section number the site is in
	off    int   // byte offset of the field
	symIdx int32 // index into the symbol table
	typ    uint16
}

// coffWriter accumulates an object file. The order of construction matters and
// is not arbitrary: a relocation names a symbol by its table index, so the
// symbol table's order must be fixed before the relocations are built; and a
// name longer than eight bytes lands in the string table, whose offsets depend
// on which names are present, so the table is built as symbols are added and
// only its length is known at the end.
type coffWriter struct {
	strTab  []byte           // string-table payload, without its length dword
	strUsed map[string]int32 // name -> offset, so a shared name is stored once

	// syms is the symbol table in file order, excluding the auxiliary records.
	// Its index is what a relocation names.
	syms     []coffSymOut
	symIndex map[string]int32

	// relocs accumulates every relocation, grouped by section when written.
	relocs []coffRelOut
}

// coffSymOut is one symbol as it will be written.
type coffSymOut struct {
	name  string
	value int32
	// secNum is 1-based, as COFF counts sections from 1. Zero means undefined: a
	// symbol this object expects someone else to provide, which is how a call
	// into goclib or an imported Windows API appears before the link.
	secNum int32
	class  uint8
	typ    uint16
	// isFunc marks DTYPE_FUNCTION.
	isFunc bool
	// aux is the NumberOfAuxSymbols field. Only the per-section definition
	// symbols carry one.
	aux byte
	// slot is the symbol's index as a file offset into the symbol table --
	// that is, its own position plus the auxiliary records that precede it. A
	// relocation names a symbol this way, not by its position in w.syms.
	//
	// The two differ, and using the wrong one is silent: it shifts every
	// relocation by the number of section-definition aux records in front of
	// it, so a call to `helper` resolves to whichever symbol happens to sit one
	// or two slots earlier. The object still parses, still links, and computes
	// the wrong addresses -- the worst failure mode there is, because nothing
	// complains.
	slot int32
}

func newCoffWriter() *coffWriter {
	return &coffWriter{
		strUsed:  make(map[string]int32),
		symIndex: make(map[string]int32),
	}
}

// strOffset returns the string-table offset of name, appending it if new.
//
// Offsets are relative to the table's own length dword, so the first name
// stored sits at 4 -- the same convention the read path's coffStrBase expects.
func (w *coffWriter) strOffset(name string) int32 {
	if off, ok := w.strUsed[name]; ok {
		return off
	}
	off := int32(4 + len(w.strTab))
	w.strUsed[name] = off
	w.strTab = append(w.strTab, name...)
	w.strTab = append(w.strTab, 0)
	return off
}

// writeName fills an 8-byte COFF name field: the name inline when it fits,
// otherwise a zero dword followed by the string-table offset. The two cases are
// told apart by the first dword alone, which is exactly how parseCOFF reads it.
func (w *coffWriter) writeName(dst []byte, name string) {
	if len(name) <= 8 {
		copy(dst, name)
		return
	}
	binary.LittleEndian.PutUint32(dst, 0)
	binary.LittleEndian.PutUint32(dst[4:], uint32(w.strOffset(name)))
}

// addSymbol appends a symbol unless its name is already present, and returns the
// slot index a relocation must use to name it.
//
// A name referenced by several relocations must appear once: a duplicate would
// be a second definition of one name, and the linker would be free to pick
// either. slot is assigned as the table grows, so it accounts for the aux
// records of every symbol already written -- which is the whole reason it is
// tracked here rather than derived from len(w.syms) at the point of use.
func (w *coffWriter) addSymbol(s coffSymOut) int32 {
	if _, ok := w.symIndex[s.name]; ok {
		return w.symIndex[s.name]
	}
	idx := int32(len(w.syms))
	w.symIndex[s.name] = idx
	// The slot is the table position: every previously added symbol contributes
	// one record plus its aux records.
	slot := int32(0)
	for _, prev := range w.syms {
		slot += 1 + int32(prev.aux)
	}
	s.slot = slot
	w.syms = append(w.syms, s)
	return idx
}

// symSlot returns the table slot of name, creating it as an undefined external
// if this object does not define it. This is the mechanism by which a reference
// to a goclib function or an imported API becomes a symbol the linker must
// satisfy, and it returns a slot because that is what a relocation records.
func (w *coffWriter) symSlot(name string) int32 {
	if idx, ok := w.symIndex[name]; ok {
		return w.syms[idx].slot
	}
	w.addSymbol(coffSymOut{
		name:   name,
		class:  scnClassExternal,
		secNum: 0, // undefined
	})
	return w.syms[w.symIndex[name]].slot
}

// coffSectionChars picks a section's characteristics from what the Image says
// about it, the way a compiler would.
func coffSectionChars(s *Section) uint32 {
	switch {
	case s.Bss:
		return scnCntUninitializedData | scnMemRead | scnMemWrite | scnAlign8Bytes
	case s.Code:
		return scnCntCode | scnMemExecute | scnMemRead | scnAlign1Bytes
	case s.Writable:
		return scnCntInitializedData | scnMemRead | scnMemWrite | scnAlign1Bytes
	default:
		return scnCntInitializedData | scnMemRead | scnAlign1Bytes
	}
}

// coffRelocType maps a pending fixup to the COFF relocation type that will
// apply the same value at link time.
//
// The mapping cannot be one-to-one, because the two sides answer different
// questions. A Fixup says how the linker computes the value (applyFixup does
// that arithmetic, and it knows about RIP adjustment, addends and
// target-minus-base), while a COFF relocation says only which symbol the field
// wants. So the type here is chosen to match what applyFixup would have
// written, and the shape flags pick it.
// errShortResolved reports a fixup that cloneShortFixed has already written
// into the section bytes, so it needs no relocation record. It is a sentinel
// rather than a plain nil-with-a-comment so the counting and emitting passes
// cannot disagree about which fixups exist: both ask coffRelocType, and both get
// this, and both skip. A separate boolean would be one more thing to keep in
// step; a distinguishable error cannot drift.
var errShortResolved = errors.New("short fixup resolved in place")

func coffRelocType(f Fixup) (uint16, error) {
	switch {
	case f.Short:
		// A one-byte field has no x64 relocation type -- and needs none. A
		// short jump's displacement is relative to a position inside the same
		// section, and both offsets are already fixed in the object, so
		// cloneShortFixed has written the byte. See the comment there: dropping
		// it instead would leave a placeholder and send the jump off target.
		return 0, errShortResolved
	case f.Absolute && f.Wide:
		// A 64-bit address store (mov to an absolute, or an 8-byte pointer).
		return relAMD64Addr64, nil
	case f.Absolute:
		// A 32-bit absolute address.
		return relAMD64Addr32, nil
	default:
		// A call target, a RIP-relative data reference, or a jump-table entry
		// (Sym2 set): all become REL32, which is what LLVM emits for the first
		// two. A jump table's subtraction of its base is not expressible in a
		// COFF relocation, so it is left to applyFixup, which does it properly.
		return relAMD64Rel32, nil
	}
}

// cloneShortFixed returns secs with every short fixup's displacement written
// into the section bytes, and copies only the sections it had to touch.
//
// A short jump is PC-relative within one section, so its value needs nothing
// from the link: the field's offset and the target's offset are both known, and
// the difference between them does not change when sections are laid out. That
// is the whole reason a one-byte displacement is worth keeping rather than
// rejecting -- COFF has no rel8 relocation, but it also does not need one.
//
// A target in another section is a real error, and not a silent one: the jump
// would be out of range by construction, and goa rejects it at link time with
// a number attached.
func cloneShortFixed(secs []*Section, secNum map[int]int32, img *Image) []*Section {
	shorts := map[int32][]Fixup{}
	for _, f := range img.Fixups {
		if f.Short {
			if n, ok := secNum[f.Sect]; ok {
				shorts[n] = append(shorts[n], f)
			}
		}
	}
	if len(shorts) == 0 {
		return secs
	}
	out := make([]*Section, len(secs))
	copy(out, secs)
	for i, s := range out {
		fs, ok := shorts[int32(i+1)]
		if !ok {
			continue
		}
		cp := *s
		cp.Data = append([]byte(nil), s.Data...)
		out[i] = &cp
		for _, f := range fs {
			tgt, ok := img.Syms[f.Sym]
			if !ok || tgt.Sect != f.Sect {
				// Not reachable from here. Leave the byte alone and let the
				// link report it, rather than writing a displacement to
				// somewhere this object cannot describe.
				continue
			}
			// The CPU measures the displacement from the byte after the
			// field, plus any instruction trailer -- the same arithmetic
			// applyFixup does, with the section base cancelling out.
			disp := tgt.Off + f.Addend - (f.Off + 1 + f.RipAdjust)
			if disp < -128 || disp > 127 {
				continue // out of range; the link will say so by name
			}
			if f.Off+1 > len(cp.Data) {
				continue
			}
			cp.Data[f.Off] = byte(int8(disp))
		}
	}
	return out
}

// WriteCOFFObject writes img as a relocatable COFF object and returns the bytes.
//
// The Image must be unlinked -- its fixups are expected to be pending, because
// resolving them is the linker's job and this is the stage before it. Running
// this over an image whose fixups were already applied would emit an object
// whose relocations point at addresses that are about to change: a file that
// reads back clean and links wrong. That is why this is a separate operation
// rather than BuildPE with a flag.
func WriteCOFFObject(img *Image) []byte {
	w := newCoffWriter()

	// Sections that go into the object. An Unmapped section (the Win64 unwind
	// tables) has no bytes in the finished image by design -- it is regenerated
	// at link time from the UWFunc records -- so there is nothing to emit.
	secs := make([]*Section, 0, len(img.Sections))
	secNum := make(map[int]int32, len(img.Sections)) // Image index -> COFF number
	for i, s := range img.Sections {
		if s.Unmapped {
			continue
		}
		secNum[i] = int32(len(secs) + 1)
		secs = append(secs, s)
	}

	// A one-byte displacement has no COFF relocation type, but the value it
	// wants is already known here: a short jump reaches only inside its own
	// section, and both the field and the target have offsets within that
	// section. So the displacement is computed and patched into the section
	// bytes, and no relocation is emitted for it.
	//
	// The alternative -- dropping the fixup the way an unexpressible type
	// would be dropped -- leaves whatever placeholder the code generator wrote
	// in the byte, and the program then jumps to the wrong address. That is
	// silent: the object links, the loader is happy, and `jmp short` lands
	// mid-instruction. It is not an approximation either, so it does not belong
	// with the cases that are refused.
	//
	// The write goes to a copy of the section data. The Image belongs to the
	// caller, which may still link it into an executable afterwards, and a
	// relocatable object must not consume the pending fixups it was handed.
	secs = cloneShortFixed(secs, secNum, img)

	// The symbol table opens with a file-name pseudo symbol, then one
	// section-definition symbol per section. The section definitions are what
	// carry each section's size and relocation count, so the counts are computed
	// before the table is built.
	//
	// The count has to agree with what emit actually writes, and the two are not
	// the same set: a fixup with no COFF relocation type (a short jump) is
	// dropped on the way out. Counting it here anyway inflates the section
	// header, and the reader -- which trusts the header and walks that many
	// 10-byte records -- then runs off the end of the table and into the symbol
	// table, reading symbol bytes as relocation entries. The symptom is a link
	// error naming an absurd symbol index (28462, for a table of 282), which
	// points nowhere near the real cause. So the same predicate that decides
	// whether to write a record decides whether to count it.
	relCount := make([]int, len(secs))
	for _, f := range img.Fixups {
		n, ok := secNum[f.Sect]
		if !ok {
			continue // the site is in an unmapped section
		}
		if _, err := coffRelocType(f); err != nil {
			continue // dropped below, so not counted here either
		}
		relCount[n-1]++
	}

	// The .file symbol carries the object's name, which is what lets a linker say
	// which two files collided. A caller that has no name to give writes the
	// conventional ".file", which every COFF tool also accepts.
	objFile := img.FileName
	if objFile == "" {
		objFile = ".file"
	}
	w.addSymbol(coffSymOut{
		name:   objFile,
		class:  scnClassFile,
		secNum: 0, // the file symbol is absolute, not in a section
		aux:    0,
	})
	// The library-symbol table rides along as a static symbol whose name IS the
	// list. COFF has no metadata section, and a symbol is the one place the
	// format lets a producer say something the linker reads but no instruction
	// refers to. It is a STATIC in section 0 so nothing can link against it, and
	// the name is prefixed so it can never collide with a C identifier.
	if len(img.LibSyms) > 0 {
		names := make([]string, 0, len(img.LibSyms))
		for n := range img.LibSyms {
			names = append(names, n)
		}
		sort.Strings(names)
		w.addSymbol(coffSymOut{
			name:   coffLibSymPrefix + strings.Join(names, ","),
			class:  scnClassStatic,
			secNum: 0,
			aux:    0,
		})
	}
	// The import table rides along the same way: one static symbol naming every
	// Win32 entry point this object calls and the DLL each is imported from.
	// The pairs are sorted so the same source yields the same bytes, for the
	// same reason the library list is.
	if len(img.Exts) > 0 {
		pairs := make([]string, 0, len(img.Exts))
		for n, dll := range img.Exts {
			pairs = append(pairs, n+"="+dll)
		}
		sort.Strings(pairs)
		w.addSymbol(coffSymOut{
			name:   coffDllSymPrefix + strings.Join(pairs, ","),
			class:  scnClassStatic,
			secNum: 0,
			aux:    0,
		})
	}
	for i, s := range secs {
		// class STATIC: these define the section, they are not referenceable by
		// name from another object, and a relocation against one means
		// "this section's address" rather than "this function's address".
		_ = i
		w.addSymbol(coffSymOut{
			name:   s.Name,
			class:  scnClassStatic,
			secNum: int32(i + 1),
			aux:    1,
		})
	}

	// The real symbols. Sorting the names makes the table order deterministic;
	// without it the order would follow Go's map iteration and two builds of the
	// same source would produce different files, which makes "did anything
	// change?" unanswerable by comparing two objects.
	names := make([]string, 0, len(img.Syms))
	for name := range img.Syms {
		// "IAT:name" is a slot in the import address table and "thunk:name" the
		// jump stub that reaches it -- both belong to the linked image, where
		// the import table is built. Neither exists yet, and neither means
		// anything in a relocatable object: writing them out would put names in
		// the symbol table that no COFF-aware tool can resolve, and a link that
		// read them back would look for a function called "IAT:ExitProcess".
		//
		// What does belong here is the bare name the reference is really to.
		// An external call is recorded as `call ExitProcess`; whether that means
		// a sibling object or an import is the link's decision, and it makes it
		// from the undefined symbol this file now emits.
		if strings.HasPrefix(name, "IAT:") || strings.HasPrefix(name, "thunk:") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		loc := img.Syms[name]
		num, ok := secNum[loc.Sect]
		if !ok {
			// The symbol lives in an unmapped section. Its address still means
			// something inside this object, but there is no section to put it in,
			// and recording it as undefined would be a lie. The unwind tables are
			// the only sections in this state and nothing outside them refers to
			// their symbols, so skipping is safe -- asserted rather than assumed.
			continue
		}
		w.addSymbol(coffSymOut{
			name:   name,
			value:  int32(loc.Off),
			secNum: num,
			class:  scnClassExternal,
			// typeForSection is indexed by the COFF section number, not the
			// Image's: Unmapped sections are dropped, so the two numbering
			// schemes differ and using the wrong one reads another section's
			// attributes.
			typ: typeForSection(secs[num-1]),
		})
	}

	// Relocations, one per pending fixup.
	for _, f := range img.Fixups {
		n, ok := secNum[f.Sect]
		if !ok {
			continue // the site is in an unmapped section
		}
		typ, err := coffRelocType(f)
		if err != nil {
			// Drop the relocation rather than approximate it: a wrong type links
			// silently and corrupts the program, while a missing one is loud --
			// the symbol stays undefined and the linker reports it by name.
			continue
		}
		w.relocs = append(w.relocs, coffRelOut{
			secNum: n,
			off:    f.Off,
			symIdx: w.symSlot(coffExternName(f.Sym)),
			typ:    typ,
		})
	}

	return w.emit(secs, relCount)
}

// coffExternName strips the linker's internal prefixes from a fixup target, so
// the object names the symbol the reference is really to. `call [rip+IAT:add]`
// and `call thunk:add` both name add; which of the two forms applies is settled
// at link time, once it is known whether add lives in a sibling object or in an
// import table.
func coffExternName(sym string) string {
	if rest, ok := strings.CutPrefix(sym, "IAT:"); ok {
		return rest
	}
	if rest, ok := strings.CutPrefix(sym, "thunk:"); ok {
		return rest
	}
	return sym
}

// typeForSection returns the COFF symbol type for a symbol living in s: a
// function symbol when the section holds code, 0 otherwise.
func typeForSection(s *Section) uint16 {
	if s != nil && s.Code {
		return coffTypeFunction
	}
	return 0
}

// pad4 rounds n up to a 4-byte boundary, which COFF requires of every file
// offset that follows a block of section data or a relocation table.
func pad4(n int) int { return (n + 3) &^ 3 }

// emit lays the file out: header, section headers, section data, relocation
// tables, symbol table, string table. The file header's symbol-table pointer is
// patched at the end because that position is only known once everything before
// it is sized.
func (w *coffWriter) emit(secs []*Section, relCount []int) []byte {
	nsec := len(secs)

	// The file header, 20 bytes, written straight into `out` rather than into a
	// separate buffer that is appended afterwards. An append that grows the slice
	// reallocates, and a separately-allocated header would then be patching a
	// copy the file no longer points at -- which compiles, runs, and produces an
	// object with PointerToSymbolTable 0, i.e. one no reader can find symbols in.
	//
	// TimeDateStamp is left zero on purpose: stamping the current time would make
	// every build a different file.
	out := make([]byte, 20, 4096)
	binary.LittleEndian.PutUint16(out[0:], coffMachineAMD64)
	binary.LittleEndian.PutUint16(out[2:], uint16(nsec))
	binary.LittleEndian.PutUint32(out[4:], 0) // TimeDateStamp
	// [8] PointerToSymbolTable and [12] NumberOfSymbols are patched at the end.
	binary.LittleEndian.PutUint16(out[16:], 0) // SizeOfOptionalHeader: a COFF
	// object has none, and this is also how a reader tells classic from bigobj.
	binary.LittleEndian.PutUint16(out[18:], imageFileExecutableImage)

	// Section headers, 40 bytes each, patched once the data offsets are known.
	secHdr := make([]int, nsec)
	for i := range secs {
		secHdr[i] = len(out)
		out = append(out, make([]byte, 40)...)
	}

	// Section data. A .bss section contributes no file bytes; its virtual size
	// is recorded in the header and the read path allocates from that.
	dataAt := make([]int, nsec)
	for i, s := range secs {
		if s.Bss {
			continue
		}
		dataAt[i] = len(out)
		out = append(out, s.Data...)
		out = append(out, make([]byte, pad4(len(out))-len(out))...)
	}

	// Relocation tables, grouped by section and placed after all the data.
	relAt := make([]int, nsec)
	for i := range secs {
		if relCount[i] == 0 {
			continue
		}
		relAt[i] = len(out)
		for _, r := range w.relocs {
			if r.secNum != int32(i+1) {
				continue
			}
			rec := make([]byte, 10)
			binary.LittleEndian.PutUint32(rec[0:], uint32(r.off))
			binary.LittleEndian.PutUint32(rec[4:], uint32(r.symIdx))
			binary.LittleEndian.PutUint16(rec[8:], r.typ)
			out = append(out, rec...)
		}
		out = append(out, make([]byte, pad4(len(out))-len(out))...)
	}

	// Symbol table. Its position is the one thing not known until now.
	symTableAt := len(out)
	auxTotal := 0
	for _, s := range w.syms {
		auxTotal += int(s.aux)
	}
	for _, s := range w.syms {
		rec := make([]byte, 18)
		w.writeName(rec, s.name)
		binary.LittleEndian.PutUint32(rec[8:], uint32(s.value))
		binary.LittleEndian.PutUint16(rec[12:], uint16(s.secNum))
		binary.LittleEndian.PutUint16(rec[14:], s.typ)
		rec[16] = s.class
		rec[17] = s.aux
		// An auxiliary record follows its symbol and occupies its own slot --
		// skipping it would shift every later symbol index. So the symbol record
		// goes out first, then its aux records.
		if s.aux > 0 {
			ax := make([]byte, 18*int(s.aux))
			// The section-definition aux record: Length(4) NumberOfRelocations(4)
			// NumberOfLinenumbers(4) then CheckSum(4) and Number/Section(2), all
			// zero here. A reader takes the size and relocation count from here,
			// and it must agree with the section header's own count.
			si := int(s.secNum) - 1
			if si >= 0 && si < nsec {
				binary.LittleEndian.PutUint32(ax[0:], uint32(secs[si].VSize))
				binary.LittleEndian.PutUint32(ax[4:], uint32(relCount[si]))
			}
			out = append(out, rec...)
			out = append(out, ax...)
			continue
		}
		out = append(out, rec...)
	}

	// Section names go into the name field now. A name longer than eight bytes
	// is a string-table offset, and adding it can grow the table -- which is why
	// this happens before the table is appended below rather than after.
	for i, s := range secs {
		w.writeName(out[secHdr[i]:secHdr[i]+40], s.Name)
	}

	// String table: its own length, then the names. Both the symbol names above
	// and the section names just written are already in w.strTab, so the length
	// written here is final.
	strAt := make([]byte, 4)
	binary.LittleEndian.PutUint32(strAt, uint32(4+len(w.strTab)))
	out = append(out, strAt...)
	out = append(out, w.strTab...)

	// Now the section headers, whose data and relocation pointers are known.
	for i, s := range secs {
		h := out[secHdr[i] : secHdr[i]+40]
		raw := 0
		if !s.Bss {
			raw = len(s.Data)
		} else {
			// An uninitialised section has no file bytes, so PointerToRawData
			// is zero -- that is what marks it as .bss. SizeOfRawData, though,
			// carries the virtual size: the space still has to be allocated, and
			// a reader takes the size from this field rather than from
			// VirtualSize (which is not present in a COFF section header at
			// all). Writing zero here would make the section look empty rather
			// than zero-filled, and every symbol in it would resolve to the
			// wrong place.
			raw = s.VSize
		}
		binary.LittleEndian.PutUint32(h[8:], uint32(s.VSize)) // VirtualSize
		binary.LittleEndian.PutUint32(h[12:], 0)              // VirtualAddress: the linker assigns it
		binary.LittleEndian.PutUint32(h[16:], uint32(raw))    // SizeOfRawData
		if s.Bss {
			binary.LittleEndian.PutUint32(h[20:], 0) // no file bytes
		} else {
			binary.LittleEndian.PutUint32(h[20:], uint32(dataAt[i]))
		}
		if relCount[i] > 0 {
			binary.LittleEndian.PutUint32(h[24:], uint32(relAt[i]))
		} else {
			binary.LittleEndian.PutUint32(h[24:], 0)
		}
		binary.LittleEndian.PutUint32(h[28:], 0) // PointerToLinenumbers
		// PointerToRelocations and PointerToLineNumbers are both 0: the
		// PointerToLinenumbers, PointerToRelocations and PointerToLineNumbers
		// are all zero: the relocations live in the single reloc stream, not
		// interleaved between section bytes.
		binary.LittleEndian.PutUint32(h[28:], 0)
		// NumberOfRelocations (16-bit) and NumberOfLinenumbers (16-bit) share
		// the two count words at +32. NumberOfRelocations is also recorded in
		// the symbol table's section-definition aux record; both must agree or a
		// reader walks the wrong number of records.
		binary.LittleEndian.PutUint16(h[32:], uint16(relCount[i]))
		binary.LittleEndian.PutUint16(h[34:], 0)
		binary.LittleEndian.PutUint32(h[36:], coffSectionChars(s))
	}

	// Patch the file header now that the symbol table's position is known. The
	// count includes auxiliary records, since they occupy table slots.
	binary.LittleEndian.PutUint32(out[8:], uint32(symTableAt))
	binary.LittleEndian.PutUint32(out[12:], uint32(len(w.syms)+auxTotal))
	return out
}
