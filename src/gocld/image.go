package gocld

// The image being built, and the pieces a front end fills in.
//
// This is the boundary between producing machine code and packaging it. A
// front end (goa assembling text, or an object file from elsewhere) states
// what the program contains: these sections, these symbols, these
// relocations still to apply, this entry point, these imports. The linker
// decides where each piece lands and writes the headers.
//
// It exists because the two halves answer different questions. Encoding asks
// "what does this instruction mean"; packaging asks "where does this byte go,
// and what does a symbol resolve to". Folding them together meant the container
// code reached into the encoder's state -- a.syms, a.sections, a.fixups -- and
// a change to one was a change to the other, in a different file, with nothing
// to say the coupling was deliberate.
//
// Nothing here knows about x86. An object file produced by LLVM goes through
// the same Image as one goa assembled, which is the point: two front ends, one
// linker, and a program that behaves the same either way.

// Section is one contiguous run of bytes in the finished image, with the
// attributes the image format needs.
type Section struct {
	Name     string
	Writable bool
	Code     bool
	Data     []byte

	// VSize is the section's virtual size. For a normal section it tracks
	// len(Data); for a .bss section it advances without any bytes being
	// appended, so the file stays empty while symbol offsets remain correct.
	VSize int

	// Bss marks an uninitialised-data section. Its bytes are zero-filled by
	// the loader, never written to the file, and its size is VSize (not
	// len(Data), which stays 0).
	Bss bool

	// Unmapped marks a section whose bytes are merged and whose symbols and
	// relocations resolve normally, but which is left out of the finished
	// image.
	//
	// The Win64 unwind sections are the only user. Their contents are small --
	// 12 bytes of .pdata and about 11 of .xdata per function -- but a PE
	// section occupies a whole multiple of FileAlignment (512, the smallest
	// value Windows accepts) in the file, so a program with three functions
	// paid 1024 bytes to store 72 bytes of table, and every image carried at
	// least that. Dropping them is a size decision: a program that crashes
	// cannot be post-mortem walked, which is a cost the native generator was
	// already paying.
	Unmapped bool
}

// SymLoc is where a symbol lives: which section, at what offset within it.
type SymLoc struct {
	Sect int
	Off  int
}

// A pending relocation's field shape. The booleans are not independent: wide
// and virtual only mean anything together, and applyFixup is where that is
// enforced. Making them a single enum would need a "wide and virtual" case,
// which is a longer way of saying the same thing.
// Fixup is one pending relocation: a place in a section whose value is not yet
// known, because it depends on where a symbol lands.
type Fixup struct {
	// Sect is the index of the section holding the site.
	Sect int
	// Off is the byte offset of the field to patch.
	Off int
	// Sym is the symbol whose address the field wants.
	Sym string
	// Sym2 is the symbol a label difference is measured from, for the
	// relocations that encode a jump table entry as "target minus base".
	Sym2 string
	// Addend is a constant added to the symbol's address.
	Addend int
	// RipAdjust is how many bytes past the end of the field the next
	// instruction begins, which a RIP-relative reference has to add back
	// because its displacement is measured from the following instruction.
	RipAdjust int
	// Absolute stores an address rather than a distance from the field, so the
	// image base is added rather than subtracted.
	Absolute bool
	// Wide makes the field eight bytes instead of four (ADDR64).
	Wide bool
	// Virtual writes VirtualOp before the value, for the move-accumulator form
	// that encodes "load an address" as an opcode plus an immediate.
	Virtual bool
	// Short makes the field one byte rather than four, for a short jump.
	Short bool
}

// UWFunc is one function's unwind bookkeeping, captured while its prologue was
// assembled: where it lives, and the shape of the frame it set up.
//
// The Win64 unwind tables (.pdata and .xdata) are what let a post-mortem
// debugger walk the stack. Building them needs the frame's exact shape -- how
// many registers were pushed and how much stack was allocated -- and that is
// known only while the prologue is being encoded, which is why this is
// collected by the assembler and consumed by the linker rather than derived
// later from the bytes.
type UWFunc struct {
	// Sect is the section index the function lives in (.text).
	Sect int
	// Start is the offset of the function label within the section.
	Start int
	// End is the offset of the next label, or the section end, within Sect.
	End int
	// Pushes are the register numbers pushed, in execution order, including
	// rbp. The unwind record stores their total, not the list, but the list is
	// what makes a mismatch detectable.
	Pushes []int
	// Alloc is the `sub rsp, N` amount, 0 if the frame allocates nothing.
	Alloc int
	// HasProlog records that at least a `push rbp` was seen.
	HasProlog bool
	// PrologDone records that the prologue's shape is fully captured.
	PrologDone bool
}

// Image is the program to link, stated by whoever produced it.
type Image struct {
	// Sections are the image's sections in the order they were created. Their
	// indices are what SymLoc and Fixup refer to.
	Sections []*Section

	// Syms maps a symbol name to its location. A name the linker cannot find
	// here and cannot satisfy from an imported library is a link error naming
	// the symbol, never a guess: an image built on a guessed import loads fine
	// and then dies with STATUS_ENTRYPOINT_NOT_FOUND and no explanation.
	Syms map[string]SymLoc

	// Fixups are the relocations still to apply, applied once the section
	// layout gives every symbol an address.
	Fixups []Fixup

	// Entry is the symbol at the image's entry point.
	Entry string

	// Exts maps an imported name to the library it comes from, without the
	// ".dll" the loader will match on.
	Exts map[string]string

	// Subsystem is the Windows subsystem: 3 for a console program, 2 for a
	// windowed one. Ignored for ELF.
	Subsystem uint16

	// Target selects the container: targetPE or targetELF.
	Target int

	// UWRecs are the unwind records, one per function that has one.
	UWRecs []*UWFunc

	// deferred makes an ingest hold its undefined names back instead of
	// reporting them, so that a link over several objects can judge them once
	// all of them have been read. See Resolve for why that is a link-wide
	// question rather than a per-object one.
	deferred bool

	// pending is the undefined-name set deferred mode has accumulated.
	pending map[string]bool

	// definedIn records which object first defined each symbol, so a second
	// definition can name both files. A duplicate is a link error rather than a
	// silent "last one wins": the program would run one of the two with nothing
	// to say which, and the usual cause -- the same source compiled into two
	// units, or a helper that lost its `static` -- turns into a bug report
	// instead of a diagnostic.
	definedIn map[string]string

	// deduped names the library symbols that arrived more than once because
	// each unit inlines its own copy of the C library. They are dropped rather
	// than reported: see isCLibSymbol.
	deduped map[string]bool

	// FileName is the object's own name, used for the COFF .file symbol so that a
// duplicate-definition diagnostic can name the two files that collided. Empty
// is fine -- the symbol then reads ".file" and the diagnostic falls back to
// position.
FileName string

// LibSyms names the C library symbols an image defines. The library's
	// function names are fixed by the C ABI and so carry no marker of their
	// own; the set is what lets a link of several units see the same function
	// inlined into two of them and treat the second as another copy rather than
	// as a duplicate definition. It survives the round trip through an object
	// file because a link over separately compiled units has no other way to
	// learn it.
	LibSyms map[string]bool

	// PdataRVA and PdataSize record where the .pdata section landed, which
	// the PE header's exception directory points at. Zero when there is none.
	PdataRVA  int
	PdataSize int
}

// DeferUndefined makes later ingests hold their undefined names back instead of
// reporting them, which is what a link over more than one object needs: a name
// one object leaves undefined may be defined by the next. Call Resolve once
// every object has been ingested.
func (img *Image) DeferUndefined(on bool) { img.deferred = on }

// Container formats.
const (
	TargetPE  = 0
	TargetELF = 1
)

// linkState is the linker's working copy of an Image. It is separate so that
// resolution -- which assigns every symbol an address once the layout is
// fixed -- can be described without touching the input.
type linkState struct {
	img *Image
	// symRVA is the final address of each symbol, filled after layout.
	symRVA map[string]int
	// symBase is each section's address, so a RIP-relative displacement can be
	// computed from a section-relative offset.
	symBase map[string]int
	// base is the image's load address. PE has one; ELF's is 0 because the
	// loader supplies it, which is why a displacement differs between them.
	base int
}
