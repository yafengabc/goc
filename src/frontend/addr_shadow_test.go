package frontend

import "testing"

// checkSrc lexes, parses and type-checks src, returning the diagnostics.
func checkSrc(t *testing.T, src string) []error {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Check(prog)
}

// TestAddrOfLocalShadowingAFunction: a local variable whose name is also a
// library function must win inside its block -- C block scoping hides file-
// scope names, and function names are file-scope.
//
// The symptom of getting this wrong is narrow and confusing: the variable
// reads and writes perfectly, and only `&x` comes out as the function. Found
// through goclib's fp128 runtime, which keeps its unbiased exponent in a local
// called `exp` -- and math.h declares `double exp(double)`.
func TestAddrOfLocalShadowingAFunction(t *testing.T) {
	src := "double exp(double);\n" +
		"static void takes_int(int *p) { *p = 1; }\n" +
		"int main(void) {\n" +
		"    int exp = 0;\n" +
		"    int plain = 0;\n" +
		"    takes_int(&exp);\n" +
		"    takes_int(&plain);\n" +
		"    takes_int(&exp);\n" +
		"    return exp + plain;\n" +
		"}\n"
	if errs := checkSrc(t, src); len(errs) > 0 {
		t.Fatalf("&exp on a local int: %v", errs)
	}
}

// Taking the address of a genuine function designator still works -- that is
// how a function pointer gets initialised, and the fix must not break it.
func TestAddrOfFunctionDesignator(t *testing.T) {
	src := "static int add(int a, int b) { return a + b; }\n" +
		"int main(void) {\n" +
		"    int (*fp)(int, int) = &add;\n" +
		"    return fp(1, 2);\n" +
		"}\n"
	if errs := checkSrc(t, src); len(errs) > 0 {
		t.Fatalf("&add: %v", errs)
	}
}
