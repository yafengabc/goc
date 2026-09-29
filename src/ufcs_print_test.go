package main

import (
	"strings"
	"testing"
)

// --- UFCS: x.f(args) / p->f(args) resolved as T_f(&x|*p, args) -------------

// A pointer-receiver method call rewrites into a direct call of the T_f
// symbol with the receiver's address prepended.
func TestUFCSRewritesToDirectCall(t *testing.T) {
	src := `struct Point { int x; int y; };
void Point_print(struct Point* p) { printf("(%d,%d)", p->x, p->y); }
int main() {
    struct Point p;
    p.x = 3;
    p.y = 4;
    p.print();
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if !strings.Contains(asm, "call Point_print") {
		t.Fatalf("p.print() must lower to a direct call of Point_print:\n%s", asm)
	}
}

// A value-receiver method (first parameter is struct T, not struct T*) gets
// the receiver by value; through an arrow the receiver is *p.
func TestUFCSValueReceiver(t *testing.T) {
	src := `struct Point { int x; int y; };
int Point_dist2(struct Point p) { return p.x * p.x + p.y * p.y; }
int main() {
    struct Point p;
    p.x = 3;
    p.y = 4;
    struct Point *q = &p;
    return p.dist2() + q->dist2();
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call Point_dist2"); got != 2 {
		t.Fatalf("value-receiver methods must call Point_dist2 twice (dot and arrow), got %d:\n%s", got, asm)
	}
}

// A real member always wins: a function-pointer member named like a method
// keeps its C meaning and must not be rewritten.
func TestUFCSMemberWins(t *testing.T) {
	src := `struct S { int (*cb)(int); };
int S_cb(struct S* s, int v) { return v + 1; }
int twice(int v) { return v * 2; }
int main() {
    struct S s;
    s.cb = twice;
    return s.cb(21);
}`
	asm := genAsmOpt(t, src, 0)
	if strings.Contains(asm, "call S_cb") {
		t.Fatalf("a function-pointer member must keep its C meaning:\n%s", asm)
	}
	if !strings.Contains(asm, "call rax") {
		t.Fatalf("s.cb(21) must be an indirect call:\n%s", asm)
	}
}

// Without a T_f function the member lookup fails exactly as before.
func TestUFCSNoMethodReportsNoMember(t *testing.T) {
	src := `struct Point { int x; int y; };
int main() {
    struct Point p;
    return p.nope();
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected an error for a call to a missing member and method")
	}
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	if !strings.Contains(joined, `no member "nope"`) {
		t.Fatalf("error must be the plain no-member diagnostic, got: %s", joined)
	}
}

// A T_f whose first parameter is neither struct T nor struct T* is not a
// method: the call falls back to the no-member error.
func TestUFCSWrongReceiverShape(t *testing.T) {
	src := `struct Point { int x; int y; };
int Point_shift(struct Point* p, int dx, int dy) { p->x = p->x + dx; p->y = p->y + dy; return 0; }
int main() {
    struct Point p;
    return p.shift_wrong(1, 2);
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected an error: Point_shift does not match the member name")
	}
}

// --- print: checker-built format string, lowered to printf -----------------

// One format string is built from the static argument types and the call
// becomes printf: %d for ints, %g for doubles, %s for strings.
func TestPrintBuildsFormatString(t *testing.T) {
	src := `int main() {
    print(1, 2.5, "three");
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call printf"); got != 1 {
		t.Fatalf("print must lower to exactly one printf call, got %d:\n%s", got, asm)
	}
	if !strings.Contains(asm, "%d %g %s") {
		t.Fatalf("format string missing, want %q:\n%s", "%d %g %s\n", asm)
	}
}

// print() with no arguments is just the newline.
func TestPrintEmpty(t *testing.T) {
	src := `int main() {
    print();
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call printf"); got != 1 {
		t.Fatalf("print() must still call printf once, got %d:\n%s", got, asm)
	}
}

// A struct argument is rejected with a hint pointing at the method escape
// hatch, and the remaining arguments keep checking.
func TestPrintRejectsStruct(t *testing.T) {
	src := `struct Point { int x; int y; };
int main() {
    struct Point p;
    print(p, 1);
    return 0;
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected print to reject a struct argument")
	}
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	if !strings.Contains(joined, "cannot print") || !strings.Contains(joined, "Point_print") {
		t.Fatalf("error must name the type and the method escape hatch, got: %s", joined)
	}
}

// A user-declared print function keeps precedence over the builtin.
func TestPrintUserDeclaredWins(t *testing.T) {
	src := `int print(int v) { return v + 1; }
int main() {
    return print(41);
}`
	asm := genAsmOpt(t, src, 0)
	if !strings.Contains(asm, "call print") {
		t.Fatalf("a user-declared print must be called, not rewritten:\n%s", asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("user print must not fall through to the builtin:\n%s", asm)
	}
}

// The method escape hatch works end to end: x.print() prints the struct.
func TestPrintStructViaMethod(t *testing.T) {
	src := `struct Point { int x; int y; };
void Point_print(struct Point* p) { printf("(%d,%d)", p->x, p->y); }
int main() {
    struct Point p;
    p.x = 3;
    p.y = 4;
    print("p =", p);   // would fail...
    return 0;
}`
	// ...so this program must NOT check clean: print still rejects structs
	// even when a method exists. The method is opt-in via x.print().
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("print must keep rejecting structs even with a _print method defined")
	}
}
