package compiler

import (
	"goc/common"
	"goc/frontend"
	"testing"
)

// parseSrc runs preprocess -> parse and returns the program, so tests can
// inspect the types the parser built rather than just whether it failed.
func parseSrc(t *testing.T, src string) *frontend.Program {
	t.Helper()
	toks, err := common.Preprocess(src, "test.c")
	if err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	prog, err := frontend.Parse(toks)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return prog
}

// TestParseMultiDimArrayOrder pins the dimension order of a multi-dimensional
// array declarator. C reads "int m[2][3]" as 2 elements of int[3]; wrapping
// each dimension as it is read produces 3 elements of int[2] instead, which
// makes every m[i][j] address wrong (m[0][1] landing on m[1][0]'s bytes).
func TestParseMultiDimArrayOrder(t *testing.T) {
	prog := parseSrc(t, "int m[2][3]; int main() { m[1][2] = 5; return m[1][2]; }")
	if len(prog.Globals) != 1 {
		t.Fatalf("expected 1 global, got %d", len(prog.Globals))
	}
	g := prog.Globals[0]
	if g.Name != "m" {
		t.Fatalf("expected global \"m\", got %q", g.Name)
	}
	outer := g.Typ
	if !outer.IsArray() {
		t.Fatalf("expected an array type, got %s", outer)
	}
	if outer.Len != 2 {
		t.Errorf("outer dimension: got %d, want 2", outer.Len)
	}
	inner := outer.Elem
	if inner == nil || !inner.IsArray() {
		t.Fatalf("expected the element to be an array, got %v", inner)
	}
	if inner.Len != 3 {
		t.Errorf("inner dimension: got %d, want 3", inner.Len)
	}
	if inner.Elem == nil || inner.Elem.Kind != frontend.KInt {
		t.Errorf("expected the innermost element to be int, got %v", inner.Elem)
	}
}

// TestParseThreeDimArrayOrder extends the check to three dimensions, where a
// right-to-left wrap is the only thing that keeps every stride correct.
func TestParseThreeDimArrayOrder(t *testing.T) {
	prog := parseSrc(t, "int c[2][3][4]; int main() { c[1][2][3] = 7; return c[0][0][0]; }")
	g := prog.Globals[0]
	if !g.Typ.IsArray() || g.Typ.Len != 2 {
		t.Fatalf("first dimension: got %s (len %d), want 2", g.Typ, g.Typ.Len)
	}
	l2 := g.Typ.Elem
	if l2 == nil || !l2.IsArray() || l2.Len != 3 {
		t.Fatalf("second dimension: got %v, want len 3", l2)
	}
	l3 := l2.Elem
	if l3 == nil || !l3.IsArray() || l3.Len != 4 {
		t.Fatalf("third dimension: got %v, want len 4", l3)
	}
	if l3.Elem == nil || l3.Elem.Kind != frontend.KInt {
		t.Errorf("expected int elements, got %v", l3.Elem)
	}
}
