package main

import "testing"

// checkSrc runs the full front-end pipeline (preprocess -> parse -> type
// check) and returns the diagnostics, for use by the tests below.
func checkSrc(t *testing.T, src string) []error {
	t.Helper()
	toks, err := Preprocess(src, "test.c")
	if err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return Check(prog)
}

func TestCheckValidPointerArray(t *testing.T) {
	src := `
int sum(int a[], int n) {
    int s = 0, i = 0;
    while (i < n) { s = s + a[i]; i = i + 1; }
    return s;
}
int main() {
    int arr[4];
    int i = 0;
    while (i < 4) { arr[i] = i; i = i + 1; }
    int *p = arr;
    int x = 5;
    int *q = &x;
    *q = 9;
    int t = sum(arr, 4);
    return t + *p + x;
}`
	if errs := checkSrc(t, src); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestCheckUndeclared(t *testing.T) {
	src := `int main() { x = 1; return 0; }`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected an undeclared-identifier error")
	}
}

func TestCheckArgCount(t *testing.T) {
	src := `
int f(int a) { return a; }
int main() { f(1, 2); return 0; }`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a wrong-argument-count error")
	}
}

func TestCheckIndexNonArray(t *testing.T) {
	src := `int main() { int x = 3; return x[0]; }`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected an cannot-index-scalar error")
	}
}

func TestCheckDerefNonPointer(t *testing.T) {
	src := `int main() { int x = 3; return *x; }`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a cannot-dereference-non-pointer error")
	}
}

func TestCheckAssignToArray(t *testing.T) {
	src := `int main() { int a[3]; a = 5; return 0; }`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected an array-is-not-an-lvalue error")
	}
}

func TestCheckReturnFromVoid(t *testing.T) {
	src := `void f() { return 1; } int main() { f(); return 0; }`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a returning-value-from-void error")
	}
}

func TestCheckNoMain(t *testing.T) {
	src := `int f() { return 0; }`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a no-main error")
	}
}

func TestCheckFunctionPointer(t *testing.T) {
	src := `
int add(int a, int b) { return a + b; }
int apply(int (*op)(int, int), int a, int b) { return op(a, b); }
typedef int (*binop)(int, int);
struct box { binop f; };
int main() {
    int (*fp)(int, int) = add;
    int x = fp(1, 2);
    int y = (*fp)(3, 4);
    int z = apply(add, 5, 6);
    binop op = &add;
    struct box b;
    b.f = add;
    int w = b.f(1, 1);
    return x + y + z + w + op(2, 2);
}`
	if errs := checkSrc(t, src); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestCheckFunctionPointerArity(t *testing.T) {
	src := `
int add(int a, int b) { return a + b; }
int main() {
    int (*fp)(int, int) = add;
    return fp(1);
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a wrong-argument-count error through a function pointer")
	}
}

func TestCheckFunctionPointerArgType(t *testing.T) {
	src := `
double half(double x) { return x / 2.0; }
int main() {
    double (*fp)(double) = half;
    return fp(1.5, 2.5);
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected an arity error on a function-pointer call")
	}
}

func TestCheckIncompatibleFunctionPointer(t *testing.T) {
	src := `
int add(int a, int b) { return a + b; }
double avg(double a, double b) { return (a + b) / 2.0; }
int main() {
    int (*fp)(int, int) = add;
    fp = avg;
    return 0;
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected an incompatible-function-pointer assignment error")
	}
}
