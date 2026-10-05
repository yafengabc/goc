package goa

// Windows stack-probe helper.
//
// LLVM lowers any frame larger than a page (4 KiB) to a call to the CRT's
// stack-probe routine rather than a bare `sub rsp, N`. That is not a stylistic
// choice: Windows commits the stack one guard page at a time, so skipping over
// a page below the current guard faults instead of growing the stack, and the
// only way to grow it is to *touch* each page on the way down.
//
// The name it calls is `___chkstk_ms` -- three underscores, the "managed entry
// point" variant MSVC mangles it to, and the one LLVM emits because the x64
// MSVC ABI is the only Windows ABI LLVM knows. goc's own code generator does
// not need a call (see CG.emitFrameAlloc, which inlines an equivalent probe loop
// per frame), so without this routine any translation unit that goes through
// LLVM fails to link as soon as one function has a large local array -- which
// is most of the examples.
//
// The ABI is fixed by the CRT:
//
//	in:  eax = bytes requested (always a multiple of 16, always > 4096)
//	out: rsp = lowered by exactly the requested amount
//	     r11 = the same value, which the caller adds back after pushing the
//	           registers it wants to keep
//
// rax and r11 are volatile in the Windows x64 ABI, so the loop is free to use
// them. Everything else is left alone.

// chkstkName is the symbol LLVM emits. It is defined rather than imported, so
// no `extern` declaration is needed for it.
const chkstkName = "___chkstk_ms"

// chkstkPage is the unit Windows grows the stack by.
const chkstkPage = 4096

// chkstkLoop is the block label inside the routine. It is a local name, so it
// is qualified with the routine's own name and cannot collide with anything.
const chkstkLoop = chkstkName + "$loop"

// asmStep is one instruction for emitChkstk. Going through encode rather than
// raw bytes keeps the routine inside the same encoder (and the same tests) as
// everything else, so a change to the operand model cannot silently corrupt it.
type asmStep struct {
	mnem string
	ops  []Operand
	ln   string
}

// emitChkstk defines the stack-probe routine on a PE target.
//
// It is emitted unconditionally: the routine is around twenty bytes, and always
// having it means a program that never calls it still links, which removes the
// whole class of "it only failed for this one input" reports. A second call is
// a no-op, so several translation units can share one image.
func (a *Assembler) emitChkstk() error {
	// .text is always section 0 (NewAssembler creates it first), and the
	// routine has to be in the image whether or not anything branches to it.
	if len(a.sections) == 0 || a.sections[0].Name != ".text" {
		return nil
	}
	prev := a.cur
	a.cur = 0
	if _, dup := a.syms[chkstkName]; dup {
		a.cur = prev
		return nil
	}

	r := func(i int) Operand { return Operand{kind: K_REG, reg: i} }
	imm := func(v int64) Operand { return Operand{kind: K_IMM, imm: v} }
	// A plain [rsp] reference: base and reg are both rsp, with no index and an
	// explicit scale so planMem sees a well-formed operand.
	stackSlot := Operand{kind: K_MEM, memReg: 4, memBase: 4, memHasBase: true,
		memIndex: -1, memScale: 1, memWidth: 8}

	a.defineSym(chkstkName)

	// r11 holds the bytes still to commit, eax is the scratch read register.
	// The loop head sits above the first page commit so the backward jump
	// re-enters the probe rather than reloading the count.
	prologue := []asmStep{
		{"mov", []Operand{r(11), r(0)}, "mov r11, rax"},
	}
	a.defineSym(chkstkLoop)
	loop := []asmStep{
		{"sub", []Operand{r(11), imm(chkstkPage)}, "sub r11, 4096"},
		{"sub", []Operand{r(4), imm(chkstkPage)}, "sub rsp, 4096"},
		// The touch. A load faults on an uncommitted page, and unlike most
		// arithmetic it leaves the flags alone -- which matters, because the
		// test below is about the subtraction rather than about the value read.
		{"mov", []Operand{r(0), stackSlot}, "mov rax, [rsp]"},
		{"test", []Operand{r(11), r(11)}, "test r11, r11"},
		{"jne", []Operand{{kind: K_SYM, sym: chkstkLoop}}, "jne " + chkstkLoop},
	}
	// The request is a multiple of 16 but not of 4096, so the last subtraction
	// left r11 negative by the overshoot. Hand the overshoot back: the caller
	// must end up with exactly the frame it asked for.
	epilogue := []asmStep{
		{"sub", []Operand{r(4), r(11)}, "sub rsp, r11"},
		{"mov", []Operand{r(11), r(4)}, "mov r11, rsp"},
		{"ret", nil, "ret"},
	}

	for _, group := range [][]asmStep{prologue, loop, epilogue} {
		for _, s := range group {
			if err := a.encode(s.mnem, s.ops, s.ln); err != nil {
				a.cur = prev
				return err
			}
		}
	}
	a.cur = prev
	return nil
}
