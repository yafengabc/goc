// loop analysis for the optimisation passes.
//
// goc's instruction stream is a flat []Inst with labels marking branch
// targets. Before we can do strength reduction or loop-invariant code motion
// we need to *see* the loops: identify loop headers and the instruction
// ranges that make up each natural loop. This file builds a minimal control
// flow graph from the stream, finds back-edges (a branch into an earlier or
// same block -- a loop bottom jumping to its header), and reports the natural
// loop for each.
//
// It is pure analysis: it never rewrites the stream, so it can be unit-tested
// in isolation and slotted into the -O2 pipeline (A-1 strength reduction,
// A-2 LICM) without touching codegen first.
package compiler

import (
	"regexp"
	"strings"
)

// jumpRe matches an x86-64 control-transfer instruction and captures its
// operand (the target label). Unconditional jmp, every conditional jcc, and
// the loop-family instructions all transfer control; `call` does not (it
// always returns to the next instruction) so it is deliberately excluded.
var jumpRe = regexp.MustCompile(`^\t(jmp|loop|loope|loopne|j[a-z]+)\s+(\S+)$`)

// CFGBlock is one straight-line region of the instruction stream. A block begins
// at a label or right after a terminator (a control-transfer instruction),
// and ends at the next label or the next terminator. Start is inclusive, End
// exclusive. Label is the block's leading label ("" for the entry block or a
// block that begins after a terminator with no label).
type CFGBlock struct {
	Idx   int
	Start int
	End   int
	Label string
	// Succs holds the indices of successor blocks: the fall-through block
	// (after a conditional jump) plus the jump target. A block has 0
	// successors when it ends in ret/exit.
	Succs []int
	// Preds holds predecessor block indices (filled by wireSuccs).
	Preds []int
}

// isTerminator reports whether in ends a basic block: a jump/branch/loop
// instruction or a return. `call` is NOT a terminator (control returns to the
// next instruction).
func isTerminator(in Inst) bool {
	if in.Kind != instInstr {
		return false
	}
	if _, ok := jumpTarget(in); ok {
		return true
	}
	t := in.Text
	return strings.HasPrefix(t, "\tret") || strings.HasPrefix(t, "\tsysret") ||
		strings.HasPrefix(t, "\tiret")
}

// jumpTarget reports whether in is a control-transfer instruction and, if so,
// returns its target label.
func jumpTarget(in Inst) (string, bool) {
	if in.Kind != instInstr {
		return "", false
	}
	m := jumpRe.FindStringSubmatch(in.Text)
	if m == nil {
		return "", false
	}
	return m[2], true
}

// buildBlocks splits insts into basic blocks. A new block starts at index 0, at
// every label, and immediately after every terminator (so a conditional jump
// mid-block opens a new fall-through block). Duplicate boundaries are collapsed.
func buildBlocks(insts []Inst) []CFGBlock {
	starts := []int{0}
	seen := map[int]bool{0: true}
	add := func(i int) {
		if !seen[i] {
			seen[i] = true
			starts = append(starts, i)
		}
	}
	for i, in := range insts {
		if i == 0 {
			continue
		}
		if in.Kind == instLabel {
			add(i)
		}
		if isTerminator(insts[i-1]) {
			add(i)
		}
	}
	blocks := make([]CFGBlock, len(starts))
	for bi, s := range starts {
		e := len(insts)
		if bi+1 < len(starts) {
			e = starts[bi+1]
		}
		label := ""
		if insts[s].Kind == instLabel {
			label = strings.TrimSuffix(insts[s].Text, ":")
		}
		blocks[bi] = CFGBlock{Idx: bi, Start: s, End: e, Label: label}
	}
	return blocks
}

// blockOfLabel maps a label name to the block that starts with it.
func blockOfLabel(blocks []CFGBlock) map[string]int {
	m := make(map[string]int, len(blocks))
	for _, b := range blocks {
		if b.Label != "" {
			m[b.Label] = b.Idx
		}
	}
	return m
}

// wireSuccs fills each block's Succs and Preds from terminators.
func wireSuccs(blocks []CFGBlock, insts []Inst) {
	byLabel := blockOfLabel(blocks)
	for bi := range blocks {
		b := &blocks[bi]
		if b.Start >= b.End {
			continue
		}
		last := insts[b.End-1]
		if tgt, ok := jumpTarget(last); ok {
			if ti, found := byLabel[tgt]; found {
				b.Succs = append(b.Succs, ti)
			}
			// Unconditional jump/loop: no fall-through.
			if strings.HasPrefix(last.Text, "\tjmp ") || strings.HasPrefix(last.Text, "\tloop") {
				continue
			}
			// Conditional jump: fall through to the next block.
			if bi+1 < len(blocks) {
				b.Succs = append(b.Succs, bi+1)
			}
			continue
		}
		if isTerminator(last) {
			// ret / sysret / iret: no successors.
			continue
		}
		// Plain fall-through to the next block.
		if bi+1 < len(blocks) {
			b.Succs = append(b.Succs, bi+1)
		}
	}
	for bi := range blocks {
		for _, s := range blocks[bi].Succs {
			blocks[s].Preds = append(blocks[s].Preds, bi)
		}
	}
}

// Loop is one natural loop discovered in the stream. Header is the block index
// of the loop header (the target of a back-edge); Body holds every block index
// that belongs to the loop (Header included). InstRange is the inclusive
// [start, end) span of instructions covered by the loop body, convenient for
// passes that scan the linear stream.
type Loop struct {
	Header    int
	Body      []int
	InstRange [2]int
}

// findBackEdges returns every back-edge (tail -> head) where the head block
// index is <= the tail's: goc emits structured loops, so any branch into an
// earlier-or-equal block is a genuine loop back-edge (including self-loops
// where a header block ends by jumping to its own label).
func findBackEdges(blocks []CFGBlock) [][2]int {
	edges := [][2]int{}
	for ui := range blocks {
		for _, vi := range blocks[ui].Succs {
			if vi <= ui {
				edges = append(edges, [2]int{ui, vi})
			}
		}
	}
	return edges
}

// naturalLoop collects the blocks of the natural loop for a back-edge
// (tail -> head). The body is head plus every block that can reach tail
// without passing through head (a reverse reachability from tail, with head
// treated as the stop node).
func naturalLoop(blocks []CFGBlock, tail, head int) []int {
	body := map[int]bool{head: true, tail: true}
	stack := []int{tail}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, p := range blocks[n].Preds {
			if p == head {
				continue
			}
			if !body[p] {
				body[p] = true
				stack = append(stack, p)
			}
		}
	}
	out := make([]int, 0, len(body))
	for b := range body {
		out = append(out, b)
	}
	return out
}

// FindLoops returns every natural loop in insts, sorted by header block index.
// It is the entry point A-1 (strength reduction) and A-2 (LICM) will build on.
func FindLoops(insts []Inst) []Loop {
	blocks := buildBlocks(insts)
	wireSuccs(blocks, insts)
	edges := findBackEdges(blocks)
	seen := map[[2]int]bool{}
	loops := []Loop{}
	for _, e := range edges {
		if seen[e] {
			continue
		}
		seen[e] = true
		body := naturalLoop(blocks, e[0], e[1])
		lo, hi := len(insts), 0
		for _, b := range body {
			if blocks[b].Start < lo {
				lo = blocks[b].Start
			}
			if blocks[b].End > hi {
				hi = blocks[b].End
			}
		}
		loops = append(loops, Loop{Header: e[1], Body: body, InstRange: [2]int{lo, hi}})
	}
	for i := 1; i < len(loops); i++ {
		for j := i; j > 0 && loops[j-1].Header > loops[j].Header; j-- {
			loops[j-1], loops[j] = loops[j], loops[j-1]
		}
	}
	return loops
}
