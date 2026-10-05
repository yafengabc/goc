package compiler

import (
	"strings"
	"testing"
)

// --- print(array identifier): Python-style "[1, 2, 3]" thin lowering -------

// print(int[N]) lowers to int_array_print with the array identifier and its
// compile-time length; the vfmt interpreter is not linked in.
func TestPrintIntArrayThin(t *testing.T) {
	src := `int main() {
    int a[5] = {1, 2, 3, 4, 5};
    print(a);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call int_array_print"); got != 1 {
		t.Fatalf("print(int[]) must lower to one int_array_print call, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("print(int[]) must not drag in printf/vfmt:\n%s", asm)
	}
}

// print(long[N]) lowers to long_array_print, again without vfmt.
func TestPrintLongArrayThin(t *testing.T) {
	src := `int main() {
    long a[3] = {10, 20, 30};
    print(a);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call long_array_print"); got != 1 {
		t.Fatalf("print(long[]) must lower to one long_array_print call, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("print(long[]) must not drag in printf/vfmt:\n%s", asm)
	}
}

// A char array is a string first: print(s) stays on str_print, never the
// array printer.
func TestPrintCharArrayStillString(t *testing.T) {
	src := `int main() {
    char s[] = "hi";
    print(s);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call str_print"); got != 1 {
		t.Fatalf("print(char[]) must lower to one str_print call, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call int_array_print") || strings.Contains(asm, "call long_array_print") {
		t.Fatalf("print(char[]) must not hit the array printers:\n%s", asm)
	}
}

// A non-identifier array expression carries no compile-time length, so it
// keeps the %p / printf path (the array decays to a pointer).
func TestPrintArrayExprStillPointer(t *testing.T) {
	src := `int main() {
    int a[3] = {1, 2, 3};
    print(a + 1);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call printf"); got != 1 {
		t.Fatalf("print(a+1) must keep the printf (%%p) lowering, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call int_array_print") {
		t.Fatalf("print(a+1) must not dispatch to the array printer:\n%s", asm)
	}
}

// short[] has its own printer (a short is read at its true 2-byte width --
// the int printer would over-read). It lowers to short_array_print.
func TestPrintShortArrayThin(t *testing.T) {
	src := `int main() {
    short a[3] = {1, 2, 3};
    print(a);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call short_array_print"); got != 1 {
		t.Fatalf("print(short[]) must lower to one short_array_print call, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call int_array_print") || strings.Contains(asm, "call long_array_print") {
		t.Fatalf("print(short[]) must not hit a wider array printer:\n%s", asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("print(short[]) must not drag in printf/vfmt:\n%s", asm)
	}
}

// float[] lowers to float_array_print (elements widen to double internally).
func TestPrintFloatArrayThin(t *testing.T) {
	src := `int main() {
    float a[3] = {1.5, 2.5, 3.5};
    print(a);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call float_array_print"); got != 1 {
		t.Fatalf("print(float[]) must lower to one float_array_print call, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("print(float[]) must not drag in printf/vfmt:\n%s", asm)
	}
}

// double[] lowers to double_array_print.
func TestPrintDoubleArrayThin(t *testing.T) {
	src := `int main() {
    double a[2] = {1.25, 2.5};
    print(a);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call double_array_print"); got != 1 {
		t.Fatalf("print(double[]) must lower to one double_array_print call, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("print(double[]) must not drag in printf/vfmt:\n%s", asm)
	}
}

// An unsigned int[] reuses the same-width signed printer.
func TestPrintUIntArrayUsesIntPrinter(t *testing.T) {
	src := `int main() {
    unsigned int a[3] = {1, 2, 3};
    print(a);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call int_array_print"); got != 1 {
		t.Fatalf("print(unsigned int[]) must reuse int_array_print, got %d:\n%s", got, asm)
	}
}

// _Bool[] has its own printer -- a _Bool is read at its true 1-byte width,
// never through a wider printer.
func TestPrintBoolArrayThin(t *testing.T) {
	src := `int main() {
    _Bool b[3] = {1, 0, 1};
    print(b);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call bool_array_print"); got != 1 {
		t.Fatalf("print(_Bool[]) must lower to one bool_array_print call, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call int_array_print") || strings.Contains(asm, "call long_array_print") {
		t.Fatalf("print(_Bool[]) must not hit an integer array printer:\n%s", asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("print(_Bool[]) must not drag in printf/vfmt:\n%s", asm)
	}
}

// A struct array dispatches to the user's own T_array_print -- the same T_f
// naming convention as UFCS methods -- so any nameable element type gets a
// printable array form without touching the compiler's builtin table.
func TestPrintStructArrayToUserPrinter(t *testing.T) {
	src := `struct Point { int x; int y; };
int Point_array_print(struct Point *a, long n) { return 0; }
int main() {
    struct Point pts[2] = {{1, 2}, {3, 4}};
    print(pts);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call Point_array_print"); got != 1 {
		t.Fatalf("print(struct Point[]) must dispatch to the user Point_array_print, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("print(struct Point[]) must not drag in printf/vfmt:\n%s", asm)
	}
}

// A struct array without a user printer is not silently printed as a
// pointer: the checker names the T_array_print function to write.
func TestPrintStructArrayMissingPrinterError(t *testing.T) {
	src := `struct Point { int x; int y; };
int main() {
    struct Point pts[2] = {{1, 2}, {3, 4}};
    print(pts);
    return 0;
}`
	errs := checkSrc(t, src)
	for _, e := range errs {
		if strings.Contains(e.Error(), "Point_array_print") {
			return
		}
	}
	t.Fatalf("print(struct Point[]) without a printer must ask for Point_array_print, got: %v", errs)
}

// &a[0] is a pointer expression, not a bare array identifier: it carries no
// compile-time length and stays on the %p / printf path.
func TestPrintArrayAddrOfElemStillPointer(t *testing.T) {
	src := `int main() {
    int a[3] = {1, 2, 3};
    print(&a[0]);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call printf"); got != 1 {
		t.Fatalf("print(&a[0]) must keep the printf (%%p) lowering, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call int_array_print") {
		t.Fatalf("print(&a[0]) must not dispatch to the array printer:\n%s", asm)
	}
}

// Array content printing is a single-argument specialisation: in a
// multi-argument print the array is just another pointer expression (the
// %p caveat), so print("a is", a) stays on the printf path.
func TestPrintArrayMultiArgStillPointer(t *testing.T) {
	src := `int main() {
    int a[3] = {1, 2, 3};
    print("a is", a);
    print(a, 10);
    return 0;
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call printf"); got != 2 {
		t.Fatalf("multi-arg print must keep the printf lowering, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call int_array_print") {
		t.Fatalf("multi-arg print must not dispatch to the array printer:\n%s", asm)
	}
}

// The user's T_array_print printer and the T_array_* UFCS methods coexist on
// the same struct array: print(pts) reaches the printer, pts.sum() the
// method, and neither steals the other's call site.
func TestPrintStructArrayPrinterAndMethodCoexist(t *testing.T) {
	src := `struct Point { int x; int y; };
int Point_array_print(struct Point *a, long n) { return 0; }
long Point_array_sum(struct Point *a, long n) { return 0; }
int main() {
    struct Point pts[2] = {{1, 2}, {3, 4}};
    print(pts);
    return pts.sum();
}`
	asm := genAsmOpt(t, src, 0)
	if got := strings.Count(asm, "call Point_array_print"); got != 1 {
		t.Fatalf("print(pts) must lower to one Point_array_print call, got %d:\n%s", got, asm)
	}
	if got := strings.Count(asm, "call Point_array_sum"); got != 1 {
		t.Fatalf("pts.sum() must lower to one Point_array_sum call, got %d:\n%s", got, asm)
	}
	if strings.Contains(asm, "call printf") {
		t.Fatalf("neither the printer nor the method may drag in printf/vfmt:\n%s", asm)
	}
}
