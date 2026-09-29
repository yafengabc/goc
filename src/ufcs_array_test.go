package main

import (
	"strings"
	"testing"
)

// --- array methods: arr.f(args) resolves as T_array_f(arr, len, args) ----
//
// The forwarding rule is type_array_function: an array of element type T
// spells its methods T_array_f, the same T_array_ convention the array
// printers use (T_array_print). The checker passes the array identifier
// (which decays to T*) and its compile-time length -- a C array carries no
// runtime length -- then the user's own arguments.

// a.add(10) on an int[3] lowers to a direct call of int_array_add with the
// array and the length 3 prepended.
func TestUFCSArrayMethod(t *testing.T) {
	src := `int int_array_add(int *a, long n, int x) { long i; for (i = 0; i < n; i++) a[i] = a[i] + x; return 0; }
int main() {
    int a[3] = {1, 2, 3};
    a.add(10);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call int_array_add"); got != 1 {
		t.Fatalf("a.add(10) must lower to one call of int_array_add, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("a.add(10) must not drag in printf:\n%s", asm)
	}
}

// A struct array method uses the tag spelling: pts.sum() -> Point_array_sum.
func TestUFCSArrayMethodStruct(t *testing.T) {
	src := `struct Point { int x; int y; };
long Point_array_sum(struct Point *a, long n) { long i; long s = 0; for (i = 0; i < n; i++) s = s + a[i].x; return s; }
int main() {
    struct Point pts[2] = {{1, 2}, {3, 4}};
    return pts.sum();
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call Point_array_sum"); got != 1 {
		t.Fatalf("pts.sum() must lower to one call of Point_array_sum, got %d:\n%s", got, asm)
	}
}

// The element spelling is exact: an unsigned int array needs uint_array_add,
// never int_array_add -- an array method reads and writes its elements.
func TestUFCSArrayUnsignedSpelling(t *testing.T) {
	src := `int int_array_add(int *a, long n, int x) { return 0; }
int uint_array_add(unsigned int *a, long n, int x) { return 1; }
int main() {
    unsigned int u[2] = {1, 2};
    return u.add(5);
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call uint_array_add"); got != 1 {
		t.Fatalf("u.add(5) must lower to one call of uint_array_add, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call int_array_add") {
		t.Fatalf("u.add(5) must not resolve to int_array_add:\n%s", asm)
	}
}

// A char array still carries methods (char_array_*), spelled like any other
// scalar element.
func TestUFCSCharArrayMethod(t *testing.T) {
	src := `int char_array_upper(char *a, long n) { long i; for (i = 0; i < n; i++) a[i] = a[i] - 32; return 0; }
int main() {
    char s[] = "abc";
    s.upper();
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call char_array_upper"); got != 1 {
		t.Fatalf("s.upper() must lower to one call of char_array_upper, got %d:\n%s", got, asm)
	}
}

// Without a T_array_f the call is a plain member error -- but the hint names
// the function to write, so the user is not left guessing.
func TestUFCSArrayMethodMissingError(t *testing.T) {
	src := `int main() {
    int a[3] = {1, 2, 3};
    return a.add(10);
}`
	errs := checkSrc(t, src)
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	if !strings.Contains(joined, "int_array_add") {
		t.Fatalf("error must hint at defining int_array_add, got: %s", joined)
	}
}

// A T_array_f whose first parameter is not a pointer to the element type is
// not a method: the call falls back to the member error.
func TestUFCSArrayMethodWrongReceiver(t *testing.T) {
	src := `int int_array_add(long *a, long n, int x) { return 0; }
int main() {
    int a[3] = {1, 2, 3};
    return a.add(10);
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("int_array_add(long*,...) must not accept an int[] receiver")
	}
}

// A non-identifier base carries no compile-time length: (a+1).add() and
// &a.add() stay plain member errors, never array methods.
func TestUFCSArrayMethodNonIdentBase(t *testing.T) {
	src := `int int_array_add(int *a, long n, int x) { return 0; }
int main() {
    int a[3] = {1, 2, 3};
    int *q = a;
    (a + 1).add(1);
    q->add(1);
    return 0;
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("array methods must require a bare array identifier base")
	}
}

// The length the checker passes is the declared length: add() with no user
// arguments still receives (a, n), so int_array_clear(a, n) works.
func TestUFCSArrayMethodNoUserArgs(t *testing.T) {
	src := `int int_array_clear(int *a, long n) { long i; for (i = 0; i < n; i++) a[i] = 0; return 0; }
int main() {
    int a[3] = {1, 2, 3};
    a.clear();
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call int_array_clear"); got != 1 {
		t.Fatalf("a.clear() must lower to one call of int_array_clear, got %d:\n%s", got, asm)
	}
}
