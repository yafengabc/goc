package compiler

import (
	"strings"
	"testing"
)

// T1.6 (C4) ptrCapable regression tests: an int-typed variable holding a
// 64-bit pointer must pass through the narrow consumption sites unextended.
// A movsxd at any of them truncates the pointer and every program below would
// dereference garbage.

// A mixed compare (N5b) must not movsxd a ptrCapable left operand.
func TestC4CompareNoNarrow(t *testing.T) {
	src := `int main(void) {
    int v = "abc";
    long p = (long)v;
    return v == p ? 1 : 0;
}`
	asm := genAsm(t, src)
	if got := strings.Count(asm, "movsxd r10, r10d"); got != 0 {
		t.Fatalf("N5b must not movsxd a ptrCapable compare operand, got %d:\n%s", got, asm)
	}
}

// A widening store (N17) of a ptrCapable int into a long slot must keep the
// pointer whole.
func TestC4StoreNoNarrow(t *testing.T) {
	src := `int main(void) {
    int v = "abc";
    long l = v;
    return ((char *)l)[0] == 'a' ? 0 : 1;
}`
	asm := genAsm(t, src)
	if got := strings.Count(asm, "movsxd rax, eax"); got != 0 {
		t.Fatalf("N17 store must not movsxd a ptrCapable value, got %d:\n%s", got, asm)
	}
}

// A (long) cast (N17) of a ptrCapable int must keep the pointer whole.
func TestC4CastNoNarrow(t *testing.T) {
	src := `int main(void) {
    int v = "abc";
    return ((char *)(long)v)[1] == 'b' ? 0 : 1;
}`
	asm := genAsm(t, src)
	if got := strings.Count(asm, "movsxd rax, eax"); got != 0 {
		t.Fatalf("N17 cast must not movsxd a ptrCapable value, got %d:\n%s", got, asm)
	}
}

// An int->double conversion (N22) of a ptrCapable int must not movsxd first.
// (The `d != 0` compare below legitimately movsxd's its 0 constant, so the
// assertion is scoped to the FIRST cvtsi2sd -- the one fed by v's load.)
func TestC4DoubleNoNarrow(t *testing.T) {
	src := `int main(void) {
    int v = "abc";
    double d = (double)v;
    return d != 0 ? 0 : 1;
}`
	asm := genAsm(t, src)
	i := strings.Index(asm, "cvtsi2sd")
	if i < 0 {
		t.Fatalf("no cvtsi2sd emitted:\n%s", asm)
	}
	pre := asm
	if i-48 > 0 {
		pre = asm[i-48 : i]
	}
	if strings.Contains(pre, "movsxd") {
		t.Fatalf("N22 must not movsxd a ptrCapable value before cvtsi2sd: ...%s...\n%s", pre, asm)
	}
}

// An ordinary negative int must STILL be narrowed at the same sites: the
// ptrCapable analysis must not leak a false mark into a plain int. Without
// the narrowing (N5b) "v == (long)someInt" would compare -1 as a huge
// positive.
func TestC4PlainIntStillNarrows(t *testing.T) {
	src := `int main(void) {
    int v = -1;
    long p = (long)v;
    return v == p ? 1 : 0;
}`
	asm := genAsm(t, src)
	if got := strings.Count(asm, "movsxd r10, r10d"); got < 1 {
		t.Fatalf("a plain negative int must still be movsxd in the compare, got %d:\n%s", got, asm)
	}
}

// A variable seeded from a pointer but later REASSIGNED from an ordinary
// integer is not pointer-carrying any more: the mark must be dropped so the
// narrow sites keep sign-extending it. Without this rule "(long)v" read back
// 4294967295 instead of -1 (the whole point of the mark is per-value, not
// per-lifetime).
func TestC4ReassignedFromIntDropsMark(t *testing.T) {
	src := `int main(void) {
    int v = "abc";
    v = -1;
    long p = (long)v;
    return p == -1 ? 0 : 1;
}`
	asm := genAsm(t, src)
	if got := strings.Count(asm, "movsxd rax, eax"); got < 1 {
		t.Fatalf("a var reassigned from a plain int must still be movsxd, got %d:\n%s", got, asm)
	}
}

// NOTE: a compound assignment ("v += 1") on a marked variable is NOT expected
// to keep the value full-width: N23 emits 32-bit arithmetic for an int lvalue,
// so the stored result is a materialized 32-bit value and the later movsxd is
// correct. That is the documented known-remainder of T1.6 (design doc 3.1:
// "int arithmetic on a pointer-in-int truncates, no regression"). The analysis
// still must not clear the mark there -- see the `plain` argument of the
// frontend.AssignExpr rule in ptrcap.go -- but the emitted code is 32-bit either way,
// so it is deliberately not asserted here.
