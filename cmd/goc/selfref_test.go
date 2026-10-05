package main

import "testing"

// genAsmErr runs the frontend and reports the first check/gen error, or "" when
// the program is well formed. Used by the negative cases.
func genAsmErr(t *testing.T, src string) string {
	t.Helper()
	toks, err := Preprocess(src, "test.c")
	if err != nil {
		return "preprocess: " + err.Error()
	}
	prog, err := Parse(toks)
	if err != nil {
		return "parse: " + err.Error()
	}
	if errs := Check(prog); len(errs) > 0 {
		return errs[0].Error()
	}
	if _, err := Gen(prog, false, 0, false); err != nil {
		return "gen: " + err.Error()
	}
	return ""
}

// TestSelfReferentialSizeof covers C 6.2.1p7: an identifier's scope starts at
// the end of its declarator, so the name is already visible inside its own
// initialiser. `T x = { sizeof(x), ... }` is legal C, and it is how every
// Win32 struct with a dwSize field gets initialised -- the callee reads that
// field back to learn how much structure was passed. Rejecting it blocked
// INITCOMMONCONTROLSEX, BITMAPINFOHEADER and the rest of the common controls.
func TestSelfReferentialSizeof(t *testing.T) {
	src := `typedef struct { unsigned int a; unsigned int b; } S;
int main(void) {
  S s = { sizeof(s), 2 };
  return (int)s.a - 8;
}
`
	if msg := genAsmErr(t, src); msg != "" {
		t.Fatalf("self-referential sizeof rejected: %s", msg)
	}
}

// TestSelfRefArrayAndScalar guards the same rule for the other two initialiser
// forms: a scalar initialiser and a string initialiser, both of which are
// checked after the name is now in scope.
func TestSelfRefArrayAndScalar(t *testing.T) {
	for _, src := range []string{
		`int main(void) { int x = sizeof(x); return x != 4; }`,
		`typedef struct { char b[8]; int n; } S;
int main(void) { S s = { "hi", sizeof(s) }; return s.n - 16; }`,
	} {
		if msg := genAsmErr(t, src); msg != "" {
			t.Errorf("rejected: %s\n  source: %s", msg, src)
		}
	}
}

// TestSelfRefStillCatchesRedefinition is the other half of the fix: putting the
// name in scope earlier must not let a genuine redeclaration through. The
// checker reports the duplicate once, and reporting it twice would be a
// separate regression, so assert on the count too.
func TestSelfRefStillCatchesRedefinition(t *testing.T) {
	src := `int main(void) { int x = 1; int x = 2; return x; }`
	toks, err := Preprocess(src, "test.c")
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	errs := Check(prog)
	if len(errs) != 1 {
		t.Fatalf("want exactly 1 redefinition error, got %d: %v", len(errs), errs)
	}
}

// TestSelfRefKeepsShadowing proves the earlier put did not make an inner
// declaration see an outer one's type: the two must stay distinct.
func TestSelfRefKeepsShadowing(t *testing.T) {
	src := `typedef struct { long a; long b; } Wide;
typedef struct { int a; } Narrow;
int main(void) {
  int n = 0;
  Wide w = { sizeof(w), 0 };
  Narrow m = { sizeof(m) };
  n += (int)w.a + (int)m.a;
  return n;
}
`
	if msg := genAsmErr(t, src); msg != "" {
		t.Fatalf("shadowed self-size rejected: %s", msg)
	}
}
