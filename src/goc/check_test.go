package compiler

import (
	"goc/common"
	"goc/frontend"
	"testing"
)

// checkSrc runs the full front-end pipeline (preprocess -> parse -> type
// check) and returns the diagnostics, for use by the tests below.
func checkSrc(t *testing.T, src string) []error {
	t.Helper()
	toks, err := common.Preprocess(src, "test.c")
	if err != nil {
		t.Fatalf("preprocess failed: %v", err)
	}
	prog, err := frontend.Parse(toks)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return frontend.Check(prog)
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

func TestCheckAddressOfArray(t *testing.T) {
	// &array is valid and yields a pointer to the whole array (int (*)[N]),
	// NOT a decayed pointer to element 0. Arrays are lvalues for the purpose
	// of taking their address even though they are not modifiable lvalues.
	src := `
int main() {
    int a[5];
    int (*pa)[5] = &a;
    int *p = a;          // decays to element-0 pointer
    int **ppm = &p;      // taking the address of a pointer is a pointer to pointer
    return (int)(pa) + (int)(p) + (int)(ppm);
}`
	if errs := checkSrc(t, src); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestCheckStringInit(t *testing.T) {
	// A string literal initialises a char array: the length may be inferred
	// from the string ("char s[]") or must leave room for the trailing NUL.
	ok := `
int main() {
    char s[] = "hi";
    char t[8] = "abc";
    char e[] = "";
    char a[] = "x", b[] = "yz";
    return 0;
}`
	if errs := checkSrc(t, ok); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// A string longer than the array (counting the NUL) is rejected...
	big := `int main() { char e[2] = "hi"; return 0; }`
	if errs := checkSrc(t, big); len(errs) == 0 {
		t.Fatal("expected an oversize-string-initialiser error")
	}
	// ...as is a non-char array...
	nonchar := `int main() { int a[2] = 5; return 0; }`
	if errs := checkSrc(t, nonchar); len(errs) == 0 {
		t.Fatal("expected an array-initialiser error for int[]")
	}
	// ...and a char array initialised by something other than a string.
	nonstr := `int main() { char s[3] = 5; return 0; }`
	if errs := checkSrc(t, nonstr); len(errs) == 0 {
		t.Fatal("expected an array-initialiser error for char[] = 5")
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

// A block-scope declaration hides a file-scope name, function names included
// (C 6.2.1p7). The local must win for value uses AND for "fp(x)" call
// spelling; otherwise goclib helpers whose locals collide with user function
// names resolve to the wrong symbol (crash / silent corruption).
func TestLocalShadowsFunction(t *testing.T) {
	src := `
int h(int a) { return a; }
int main() {
    int h = 5;          /* local int shadows function h */
    int (*g)(int) = 0;
    return h + (g == 0);
}`
	if errs := checkSrc(t, src); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestLocalFuncPtrShadowsFunctionCall(t *testing.T) {
	src := `
int h(int a) { return a + 100; }
int g(int a) { return a; }
int main() {
    int (*h)(int) = g;  /* local func-ptr shadows function h */
    int r = h(5);       /* must call through the LOCAL pointer: r == 5 */
    if (r != 5) {
        return 1;       /* would be 105 if the function had won */
    }
    return 0;
}`
	if errs := checkSrc(t, src); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestCheckControlFlow(t *testing.T) {
	src := `
enum Color { RED, GREEN, BLUE };
int main() {
    int i = 0;
    int n = 0;
    switch (i) {
    case 0:
        n = 1;
    case 1:
        n = n + 10;
        break;
    default:
        n = 99;
    }
    switch ('x') {
    case 'a':
        n = 0;
        break;
    default:
        break;
    }
    switch (BLUE) {
    case RED:
        break;
    case GREEN:
        break;
    }
    do {
        i = i + 1;
        if (i == 2) { continue; }
    } while (i < 4);
    i = 0;
again:
    i = i + 1;
    if (i < 3) { goto again; }
    switch (i) {
    case 1:
        switch (n) {
        case 0:
            break;
        default:
            break;
        }
        break;
    }
    return n + i;
}`
	if errs := checkSrc(t, src); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestCheckDuplicateCase(t *testing.T) {
	src := `
int main() {
    int n = 0;
    switch (n) {
    case 1:
        n = 1;
        break;
    case 1:
        n = 2;
        break;
    }
    return n;
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a duplicate-case-value error")
	}
}

func TestCheckMultipleDefault(t *testing.T) {
	src := `
int main() {
    int n = 0;
    switch (n) {
    default:
        n = 1;
        break;
    default:
        n = 2;
        break;
    }
    return n;
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a more-than-one-default error")
	}
}

func TestCheckCaseOutsideSwitch(t *testing.T) {
	src := `
int main() {
    int n = 0;
    case 1:
    n = 2;
    return n;
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a case-outside-switch error")
	}
}

func TestCheckGotoMissingLabel(t *testing.T) {
	src := `
int main() {
    int i = 0;
    goto nowhere;
    return i;
}`
	errs := checkSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a no-such-label error")
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
