package main

// Tests for the CFG-based dead-store elimination. Each case is a tiny
// hand-written instruction stream in the exact shapes goc emits; the pass
// must delete only the stores the backward liveness proves unread.

import (
	"strings"
	"testing"
)

// mkInsts turns indented assembly text into an Inst stream: lines ending in
// ':' become labels, everything else becomes an instruction.
func mkInsts(t *testing.T, src string) []Inst {
	t.Helper()
	var out []Inst
	for _, ln := range strings.Split(strings.TrimSpace(src), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.HasSuffix(ln, ":") {
			out = append(out, Inst{Kind: instLabel, Text: ln})
		} else {
			out = append(out, Inst{Kind: instInstr, Text: "\t" + ln})
		}
	}
	return out
}

func countInst(insts []Inst, substr string) int {
	n := 0
	for _, in := range insts {
		if in.Kind == instInstr && strings.Contains(in.Text, substr) {
			n++
		}
	}
	return n
}

// A store whose slot is redefined in a later block, with no read anywhere on
// any path, dies even though a label sits between the two stores.
func TestLivenessCrossBlockCoveredStore(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	mov rbx, 1
.Lnext:
	mov [rbp-8], rcx
	mov rax, [rbp-8]
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 0 {
		t.Fatalf("covered store across label survived: %v", out)
	}
	if got := countInst(out, "mov [rbp-8], rcx"); got != 1 {
		t.Fatalf("the reading store must survive: %v", out)
	}
}

// A chain of covered stores goes in one sweep. ret reads no frame slot, so
// not one store in the chain has a reader anywhere: the whole chain goes.
func TestLivenessCoveredChain(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-16], rax
	mov rbx, 1
	mov [rbp-16], rcx
	mov rbx, 2
	mov [rbp-16], rdx
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-16]"); got != 0 {
		t.Fatalf("a covered chain with no reader must go entirely: %v", out)
	}
}

// A store read back inside a loop stays: the fixpoint must carry the use
// around the back edge.
func TestLivenessLoopUseKeepsStore(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
.Lloop:
	mov rcx, [rbp-8]
	inc rcx
	mov [rbp-8], rcx
	cmp rcx, 10
	jl .Lloop
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 1 {
		t.Fatalf("store feeding a loop-carried use must survive: %v", out)
	}
}

// A store read on only one branch still stays: liveness is a union.
func TestLivenessOneBranchUseKeepsStore(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	cmp rdi, 0
	je .Lskip
	mov rcx, [rbp-8]
	mov [rbp-16], rcx
.Lskip:
	mov [rbp-8], rbx
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 1 {
		t.Fatalf("a store with a reader on one path must survive: %v", out)
	}
	if got := countInst(out, "mov [rbp-8], rbx"); got != 1 {
		t.Fatalf("final store must survive: %v", out)
	}
}

// A store with no reader on either branch of a diamond dies.
func TestLivenessDeadOnBothBranches(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	cmp rdi, 0
	je .Lelse
	mov rbx, 1
	jmp .Ljoin
.Lelse:
	mov rbx, 2
.Ljoin:
	mov [rbp-8], rcx
	mov rax, [rbp-8]
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 0 {
		t.Fatalf("store dead on both paths must go: %v", out)
	}
}

// A lea escapes the slot: even with no textual read, its store stays.
func TestLivenessLeaEscapeKeepsStore(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	lea r10, [rbp-8]
	mov rbx, 1
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 1 {
		t.Fatalf("escaped slot store must survive: %v", out)
	}
}

// The inlined-spill scenario: a store followed by a call, no read. The
// linear pass clears its window at the call; the CFG pass knows a callee
// cannot reach an un-escaped slot.
func TestLivenessCallDoesNotKeepUnescapedSlot(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-24], rax
	call printf
	mov [rbp-8], rbx
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-24], rax"); got != 0 {
		t.Fatalf("store dead across a call must go: %v", out)
	}
	if got := countInst(out, "mov [rbp-8], rbx"); got != 1 {
		t.Fatalf("callee-save spill must never be deleted: %v", out)
	}
}

// But a call does keep a slot whose address escaped via lea.
func TestLivenessCallKeepsEscapedSlot(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-16], rax
	lea r10, [rbp-16]
	call helper
	mov rbx, 1
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-16], rax"); got != 1 {
		t.Fatalf("escaped slot store must survive a call: %v", out)
	}
}

// Narrow access poisons the slot for deletion (but its uses still work).
func TestLivenessNarrowAccessPoisons(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	mov byte [rbp-8], cl
	mov rbx, 1
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 1 {
		t.Fatalf("narrow overwrite must keep the wide store: %v", out)
	}
}

// A 32-bit register store is a partial write: it may not delete the earlier
// wide store, and is itself never deleted.
func TestLivenessPartialRegStore(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	mov [rbp-8], eax
	mov rcx, [rbp-8]
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8]"); got != 2 {
		t.Fatalf("partial-width store must keep both: %v", out)
	}
}

// Indirect addressing opts the whole function out.
func TestLivenessIndirectOptsOut(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	mov rax, [r10]
	mov [rbp-8], rcx
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 1 {
		t.Fatalf("function with indirect access must be untouched: %v", out)
	}
}

// Inline asm opts the whole function out.
func TestLivenessInlineAsmOptsOut(t *testing.T) {
	in := []Inst{
		{Kind: instLabel, Text: "main:"},
		{Kind: instInstr, Text: "\tmov [rbp-8], rax"},
		{Kind: instRaw, Text: "\t__asm { mov r10, 5 }"},
		{Kind: instInstr, Text: "\tmov [rbp-8], rcx"},
		{Kind: instInstr, Text: "\tret"},
	}
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 1 {
		t.Fatalf("function with inline asm must be untouched: %v", out)
	}
}

// Two functions: one optimised, one opted out; the split must not leak.
func TestLivenessPerFunctionSplit(t *testing.T) {
	in := mkInsts(t, `
f_plain:
	mov [rbp-8], rax
	mov rbx, 1
	mov [rbp-8], rcx
	mov rax, [rbp-8]
	ret
f_pointer:
	mov [rbp-8], rax
	mov rax, [r10]
	mov [rbp-8], rcx
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 1 {
		t.Fatalf("only the plain function's covered store may go: %v", out)
	}
}

// ret reads no frame slot: the frame is dead on return, and every
// legitimate reader (a callee-save restore) is an explicit load the model
// sees. A store with no reader goes even right before ret.
func TestLivenessRetReadsNothing(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	mov rbx, 9
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 0 {
		t.Fatalf("a store with no reader must go even right before ret: %v", out)
	}
}

// Immediate stores define their slot (killing earlier live-ness) but are
// never deleted themselves.
func TestLivenessImmStoreDefinesButStays(t *testing.T) {
	in := mkInsts(t, `
main:
	mov [rbp-8], rax
	mov rbx, 1
	mov [rbp-8], 5
	mov rax, [rbp-8]
	ret`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 0 {
		t.Fatalf("wide store covered by an immediate store must go: %v", out)
	}
	if got := countInst(out, "mov [rbp-8], 5"); got != 1 {
		t.Fatalf("the immediate store itself must stay: %v", out)
	}
}

// Conditional jumps at the very end of a segment (no fall-through) opt out.
func TestLivenessCondAtTailOptsOut(t *testing.T) {
	in := mkInsts(t, `
main:
	cmp rax, 0
	je .Lend
	mov [rbp-8], rax
.Lend:`)
	out := livenessDSE(in)
	if got := countInst(out, "mov [rbp-8], rax"); got != 1 {
		t.Fatalf("segment without closing ret must be untouched: %v", out)
	}
}
