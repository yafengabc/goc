package main

import (
	"strings"
	"testing"
)

func lbl(s string) Inst { return Inst{Kind: instLabel, Text: s} }
func ins(s string) Inst { return Inst{Kind: instInstr, Text: "\t" + s} }

// asmToInsts turns genAsm()'s textual output into the []Inst the analysis
// passes operate on. A line at column 0 ending in ':' is a label; a tab-
// indented line is an instruction. Anything else (empty lines, section
// directives, comments) is dropped -- they never carry a branch target.
func asmToInsts(asm string) []Inst {
	out := []Inst{}
	for _, line := range strings.Split(asm, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "\t") {
			out = append(out, Inst{Kind: instInstr, Text: line})
			continue
		}
		if strings.HasSuffix(line, ":") {
			out = append(out, Inst{Kind: instLabel, Text: line})
		}
	}
	return out
}

// nestedLoopsASM is a minimal two-level loop nest (outer i, inner j) that
// mirrors how goc emits `for` loops: each header is a label, the loop test is a
// conditional jump, and the loop bottom jumps back to its own header.
func nestedLoopsASM() []Inst {
	return []Inst{
		ins("mov ecx, 0"), // 0  entry
		lbl(".Lout:"),     // 1  outer header
		ins("cmp ecx, 10"),
		ins("jge .Lexit"), // 3  -> exit (conditional)
		ins("mov edx, 0"), // 4
		lbl(".Lin:"),      // 5  inner header
		ins("cmp edx, 10"),
		ins("jge .Lcont"), // 7  -> outer continue (conditional)
		ins("inc edx"),    // 8  inner body
		ins("jmp .Lin"),   // 9  back-edge (inner)
		lbl(".Lcont:"),    // 10 outer continue
		ins("inc ecx"),    // 11
		ins("jmp .Lout"),  // 12 back-edge (outer)
		lbl(".Lexit:"),    // 13
		ins("ret"),        // 14
	}
}

func TestFindLoopsNested(t *testing.T) {
	loops := FindLoops(nestedLoopsASM())
	if len(loops) != 2 {
		t.Fatalf("want 2 loops, got %d: %+v", len(loops), loops)
	}
	// Sorted by header index: outer (header .Lout) before inner (header .Lin).
	outer, inner := loops[0], loops[1]
	if outer.Header != 1 {
		t.Errorf("outer header block = %d, want 1 (.Lout)", outer.Header)
	}
	if inner.Header != 3 {
		t.Errorf("inner header block = %d, want 3 (.Lin)", inner.Header)
	}
	// Outer loop body must contain the inner loop's blocks too (nesting).
	has := func(loop Loop, b int) bool {
		for _, x := range loop.Body {
			if x == b {
				return true
			}
		}
		return false
	}
	for _, b := range []int{1, 2, 3, 4, 5} { // every live block except the exit (6)
		if !has(outer, b) {
			t.Errorf("outer loop body missing block %d (body=%v)", b, outer.Body)
		}
	}
	if !has(inner, 3) || !has(inner, 4) {
		t.Errorf("inner loop body should cover its header+body blocks (body=%v)", inner.Body)
	}
	// Instruction spans.
	if outer.InstRange != [2]int{1, 13} {
		t.Errorf("outer InstRange = %v, want [1,13]", outer.InstRange)
	}
	if inner.InstRange != [2]int{5, 10} {
		t.Errorf("inner InstRange = %v, want [5,10]", inner.InstRange)
	}
}

// TestFindLoopsSelfLoop checks a single self-contained loop (header block ends
// by jumping to its own label) is reported as one loop.
func TestFindLoopsSelfLoop(t *testing.T) {
	asm := []Inst{
		ins("mov ecx, 0"),
		lbl(".Lh:"),
		ins("cmp ecx, 5"),
		ins("jge .Ld"),
		ins("inc ecx"),
		ins("jmp .Lh"), // back-edge to .Lh (self loop)
		lbl(".Ld:"),
		ins("ret"),
	}
	loops := FindLoops(asm)
	if len(loops) != 1 {
		t.Fatalf("want 1 loop, got %d", len(loops))
	}
	if loops[0].Header != 1 {
		t.Errorf("header = %d, want 1", loops[0].Header)
	}
	if len(loops[0].Body) != 2 { // header block + the inc block
		t.Errorf("body = %v, want 2 blocks", loops[0].Body)
	}
}

// TestFindLoopsNone confirms a straight-line function reports no loops.
func TestFindLoopsNone(t *testing.T) {
	asm := []Inst{ins("mov eax, 1"), ins("ret")}
	if n := len(FindLoops(asm)); n != 0 {
		t.Fatalf("want 0 loops, got %d", n)
	}
}

// TestFindLoopsOnRealBSort runs the detector over goc's actual emitted asm for
// the bubble-sort benchmark, confirming it locates at least the nested loops in
// real generated code (not just the synthetic fixture).
func TestFindLoopsOnRealBSort(t *testing.T) {
	asm := genAsm(t, `void bsort(int a[], int n) {
		for (int i = 0; i < n-1; i++)
			for (int j = 0; j < n-1-i; j++)
				if (a[j] > a[j+1]) { int t = a[j]; a[j] = a[j+1]; a[j+1] = t; }
	}
	int main(void) { bsort(0, 0); return 0; }`)
	loops := FindLoops(asmToInsts(asm))
	if len(loops) < 2 {
		t.Fatalf("want >=2 loops in bsort, got %d", len(loops))
	}
	// The two deepest loops should report non-trivial instruction spans.
	for _, l := range loops {
		if l.InstRange[1] <= l.InstRange[0] {
			t.Errorf("loop %+v has empty span", l)
		}
	}
}
