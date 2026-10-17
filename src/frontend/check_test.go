package frontend

import (
	"strings"
	"testing"
)

// genericChoice type-checks
//
//	int main(void){ <locals> return _Generic(ctrl, arms); }
//
// and reports which association index the checker selected (-1 when the
// selection was never resolved). The locals give the controlling expression
// some named variables of every integer width to work with.
func genericChoice(t *testing.T, ctrl, arms string) int {
	t.Helper()
	src := ("int main(void){ unsigned char uc = 1; unsigned short us = 1; " +
		"unsigned u = 1; unsigned long ul = 1; long l = 1; " +
		"(void)uc; (void)us; (void)u; (void)ul; (void)l; " +
		"return _Generic(" + ctrl + ", " + arms + "); }")
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("lex %s: %v", ctrl, err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse %s: %v", ctrl, err)
	}
	if errs := Check(prog); len(errs) > 0 {
		t.Fatalf("check %s: %v", ctrl, errs)
	}
	got := -1
	WalkStmts(prog.Funcs[0].Body, func(s Stmt) {
		rs, ok := s.(*ReturnStmt)
		if !ok || rs.E == nil {
			return
		}
		if g, ok := rs.E.(*GenericExpr); ok {
			got = g.ChosenIdx
		}
	})
	return got
}

// Which arm of a four-way _Generic an integer expression lands on decides the
// values computed by <stdbit.h>'s type-generic macros, so these cases pin the
// usual arithmetic conversions (C11 6.3.1.8), the integer promotions, and the
// shift rule (C11 6.5.7p3: the result is the promoted *left* operand, not the
// converted pair). Before this, every integer operator returned a plain signed
// int, so `1u << 15` and `~0u` silently matched an int association.
func TestIntegerResultTypes(t *testing.T) {
	// arms: 0 = unsigned char, 1 = unsigned short, 2 = unsigned int,
	//       3 = unsigned long, 4 = long, 5 = int, 6 = default
	arms := ("unsigned char: 0, unsigned short: 1, unsigned int: 2, " +
		"unsigned long: 3, long: 4, int: 5, default: 6")
	cases := []struct {
		ctrl string
		want int
	}{
		// literals carry their suffix into their type
		{"0u", 2},
		{"1U", 2},
		{"5", 5},
		{"0L", 4},
		{"0UL", 3},
		// shifts keep the promoted left operand
		{"1u << 15", 2},
		{"(1u << 15) >> 3", 2},
		{"ul >> 7", 3},
		{"uc << 1", 5}, // unsigned char promotes to int
		// bitwise operators use the usual arithmetic conversions
		{"0x0Fu | 0xF0u", 2},
		{"u & u", 2},
		{"u | 1u", 2},
		// ...and so do the arithmetic ones
		{"u + u", 2},
		{"u * 2u", 2},
		{"uc + uc", 5}, // both operands promote to int
		{"u + ul", 3},  // unsigned long wins over unsigned int
		{"u + l", 4},   // a wider signed type absorbs the narrower unsigned
		// unary operators neither narrow nor re-sign their operand
		{"~0u", 2},
		{"-1u", 2},
		{"~u", 2},
		{"-u", 2},
	}
	for _, c := range cases {
		if got := genericChoice(t, c.ctrl, arms); got != c.want {
			t.Errorf("_Generic(%s): chose arm %d, want %d", c.ctrl, got, c.want)
		}
	}
}

// A C23 "enum Tag : T" fixes the enumeration's own width and signedness, and
// the width has to survive to later references to the tag ("enum Tag x;"),
// otherwise sizeof(enum E : unsigned char) is 4 and a struct member of that
// type is laid out wrong.
func TestEnumUnderlyingType(t *testing.T) {
	src := ("enum Small : unsigned char { S0 = 200, S1 };\n" +
		"enum Big : long long { B0 = 4000000000LL };\n" +
		"enum Plain { P0, P1 };\n" +
		"enum Small s;\n" +
		"enum Big b;\n" +
		"enum Plain p;\n" +
		"int main(void){ return 0; }\n")
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if errs := Check(prog); len(errs) > 0 {
		t.Fatalf("check: %v", errs)
	}
	want := map[string]int{"s": 1, "b": 8, "p": 4}
	for _, g := range prog.Globals {
		w, ok := want[g.Name]
		if !ok {
			continue
		}
		if got := Sizeof(g.Typ); got != w {
			t.Errorf("sizeof(%s) = %d, want %d (type %s)", g.Name, got, w, g.Typ)
		}
	}
	if enumConsts["S1"] != 201 {
		t.Errorf("S1 = %d, want 201", enumConsts["S1"])
	}
	if enumConsts["B0"] != 4000000000 {
		t.Errorf("B0 = %d, want 4000000000", enumConsts["B0"])
	}
}

// Unary minus must not drag a floating operand down to int: "-1.5f" is still a
// float and "-2.0" is still a double. Both are observable through _Generic,
// and the double case used to break print()'s float formatting (it printed a
// 64-bit garbage value for -2.0).
func TestUnaryMinusKeepsFloatingType(t *testing.T) {
	arms := "float: 0, double: 1, int: 2, default: 3"
	if got := genericChoice(t, "-1.5f", arms); got != 0 {
		t.Errorf("_Generic(-1.5f): chose arm %d, want 0 (float)", got)
	}
	if got := genericChoice(t, "-2.0", arms); got != 1 {
		t.Errorf("_Generic(-2.0): chose arm %d, want 1 (double)", got)
	}
}

// TestAtomicBuiltins pins the type-checking rules of the <stdatomic.h> builtins
// declared in src/frontend/atomic.go. The fetch family and atomic_exchange
// return the object's plain value type -- the atomic flag is stripped, so a
// fetch on an _Atomic char yields char, not int -- and atomic_compare_exchange_*
// returns _Bool. A first argument that is not a pointer to an atomic integer is
// rejected. The names are the raw __goc_atomic_* builtins (no <stdatomic.h>
// macro expansion), which is exactly what the checker recognises.
func TestAtomicBuiltins(t *testing.T) {
	// atomicBuiltinChoice checks a _Generic over one of the builtins and
	// reports which association index was selected, plus any diagnostics.
	atomicBuiltinChoice := func(t *testing.T, ctrl, arms string) (int, []error) {
		t.Helper()
		src := "int main(void){ _Atomic int a; _Atomic int b; " +
			"_Atomic signed char c; " +
			"return _Generic(" + ctrl + ", " + arms + "); }"
		toks, err := Lex(src)
		if err != nil {
			t.Fatalf("lex %q: %v", ctrl, err)
		}
		prog, err := Parse(toks)
		if err != nil {
			t.Fatalf("parse %q: %v", ctrl, err)
		}
		errs := Check(prog)
		got := -1
		WalkStmts(prog.Funcs[0].Body, func(s Stmt) {
			rs, ok := s.(*ReturnStmt)
			if !ok || rs.E == nil {
				return
			}
			if g, ok := rs.E.(*GenericExpr); ok {
				got = g.ChosenIdx
			}
		})
		return got, errs
	}

	// The fetch family returns the object's value type (int for _Atomic int),
	// never an atomic lvalue.
	if got, errs := atomicBuiltinChoice(t, "__goc_atomic_fetch_add(&a, 1)", "int: 0, _Bool: 1, default: 2"); got != 0 || len(errs) != 0 {
		t.Errorf("fetch_add: chose arm %d (errs=%v), want 0 (int)", got, errs)
	}
	for _, op := range []string{"__goc_atomic_fetch_sub", "__goc_atomic_fetch_and",
		"__goc_atomic_fetch_or", "__goc_atomic_fetch_xor"} {
		if got, errs := atomicBuiltinChoice(t, op+"(&a, 1)", "int: 0, _Bool: 1, default: 2"); got != 0 || len(errs) != 0 {
			t.Errorf("%s: chose arm %d (errs=%v), want 0 (int)", op, got, errs)
		}
	}
	// atomic_exchange likewise returns the value type.
	if got, errs := atomicBuiltinChoice(t, "__goc_atomic_exchange(&a, 1)", "int: 0, _Bool: 1, default: 2"); got != 0 || len(errs) != 0 {
		t.Errorf("exchange: chose arm %d (errs=%v), want 0 (int)", got, errs)
	}
	// compare-exchange returns _Bool.
	if got, errs := atomicBuiltinChoice(t, "__goc_atomic_compare_exchange(&a, &b, 1)", "_Bool: 0, int: 1, default: 2"); got != 0 || len(errs) != 0 {
		t.Errorf("compare_exchange: chose arm %d (errs=%v), want 0 (_Bool)", got, errs)
	}
	// Narrow object: fetch on _Atomic signed char yields char, not int.
	if got, errs := atomicBuiltinChoice(t, "__goc_atomic_fetch_add(&c, 1)", "signed char: 0, int: 1, default: 2"); got != 0 || len(errs) != 0 {
		t.Errorf("narrow fetch_add: chose arm %d (errs=%v), want 0 (signed char)", got, errs)
	}

	// A non-pointer first argument must be rejected with a diagnostic.
	src := "int main(void){ int x; __goc_atomic_fetch_add(x, 1); return 0; }"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("lex error case: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse error case: %v", err)
	}
	if errs := Check(prog); len(errs) == 0 {
		t.Errorf("expected a diagnostic for a non-pointer first argument, got none")
	}
}

// genericChoiceWith type-checks a main() whose body starts with the given
// local declarations and reports which association index the checker picked.
func genericChoiceWith(t *testing.T, locals, ctrl, arms string) int {
	t.Helper()
	src := "int main(void){ " + locals + " return _Generic(" + ctrl + ", " + arms + "); }"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("lex %s: %v", ctrl, err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse %s: %v", ctrl, err)
	}
	if errs := Check(prog); len(errs) > 0 {
		t.Fatalf("check %s: %v", ctrl, errs)
	}
	got := -1
	WalkStmts(prog.Funcs[0].Body, func(s Stmt) {
		rs, ok := s.(*ReturnStmt)
		if !ok || rs.E == nil {
			return
		}
		if g, ok := rs.E.(*GenericExpr); ok {
			got = g.ChosenIdx
		}
	})
	return got
}

// Several pairs of standard types share a representation but stay distinct in
// C, and _Generic has to tell them apart: char/signed char/unsigned char
// (C11 6.2.5p15), long/long long (6.2.5p4) and double/long double (6.2.5p10).
// goc used to fold each pair, so a "char" association swallowed a signed char
// controlling expression and a "long"/"long long" list was rejected outright
// with "type long appears twice". The flags now live on Type (PlainChar /
// LongLong / LongDouble) and are consulted only by genericTypeMatch and
// Type.String -- never by typesEqual, so char and signed char stay assignable.
func TestGenericSameRepresentationDistinctTypes(t *testing.T) {
	locals := ("char c = 0; signed char sc = 0; unsigned char uc = 0; " +
		"long l = 0; long long ll = 0; " +
		"double d = 0; long double ld = 0; " +
		"(void)c; (void)sc; (void)uc; (void)l; (void)ll; (void)d; (void)ld; ")
	cases := []struct {
		what string
		ctrl string
		arms string
		want int
	}{
		// arms: 0 = char, 1 = signed char, 2 = default
		{"plain char", "c", "char: 0, signed char: 1, default: 3", 0},
		{"signed char", "sc", "char: 0, signed char: 1, default: 3", 1},
		{"unsigned char falls to default", "uc", "char: 0, signed char: 1, default: 3", 2},
		// arms: 0 = char, 1 = signed char, 2 = unsigned char, 3 = default
		{"unsigned char has its own arm", "uc", "char: 0, signed char: 1, unsigned char: 2, default: 3", 2},
		// arms: 0 = long, 1 = long long, 2 = default
		{"long", "l", "long: 0, long long: 1, default: 2", 0},
		{"long long", "ll", "long: 0, long long: 1, default: 2", 1},
		{"long literal", "0L", "long: 0, long long: 1, default: 2", 0},
		{"long long literal", "0LL", "long: 0, long long: 1, default: 2", 1},
		// arms: 0 = double, 1 = long double, 2 = default
		{"double", "d", "double: 0, long double: 1, default: 2", 0},
		{"long double", "ld", "double: 0, long double: 1, default: 2", 1},
		{"double literal", "0.0", "double: 0, long double: 1, default: 2", 0},
		{"long double literal", "0.0L", "double: 0, long double: 1, default: 2", 1},
		{"hex long double literal", "0x1p0L", "double: 0, long double: 1, default: 2", 1},
	}
	for _, c := range cases {
		if got := genericChoiceWith(t, locals, c.ctrl, c.arms); got != c.want {
			t.Errorf("%s: _Generic(%s) chose arm %d, want %d", c.what, c.ctrl, got, c.want)
		}
	}
}

// Qualification is part of type compatibility (C11 6.7.3p11), so it is compared
// at every level of a _Generic match. The controlling expression loses its
// top-level qualifier to lvalue conversion (so "char * const" matches "char *"
// and a const int expression matches "int"), but the pointee's const survives,
// which is what keeps "const char *" apart from "char *".
func TestGenericQualifiedPointees(t *testing.T) {
	locals := ("char *cp = 0; const char *ccp = 0; int *ip = 0; const int *cip = 0; " +
		"char * const pcp = 0; const int ci = 0; " +
		"(void)cp; (void)ccp; (void)ip; (void)cip; (void)pcp; (void)ci; ")
	cases := []struct {
		what string
		ctrl string
		arms string
		want int
	}{
		// arms: 0 = char*, 1 = const char*, 2 = default
		{"char*", "cp", "char*: 0, const char*: 1, default: 2", 0},
		{"const char*", "ccp", "char*: 0, const char*: 1, default: 2", 1},
		// arms: 0 = int*, 1 = const int*, 2 = default
		{"int*", "ip", "int*: 0, const int*: 1, default: 2", 0},
		{"const int*", "cip", "int*: 0, const int*: 1, default: 2", 1},
		// a top-level const on the pointer itself is dropped by lvalue
		// conversion, so it still matches the unqualified association
		{"const pointer matches char*", "pcp", "char*: 0, const char*: 1, default: 2", 0},
		// ...and a const-qualified controlling expression matches the plain
		// "int" association, not the "const int" one
		{"const int expression matches int", "ci", "const int: 0, int: 1, default: 2", 1},
	}
	for _, c := range cases {
		if got := genericChoiceWith(t, locals, c.ctrl, c.arms); got != c.want {
			t.Errorf("%s: _Generic(%s) chose arm %d, want %d", c.what, c.ctrl, got, c.want)
		}
	}
}

// parseTU lexes, parses and type-checks one translation unit, returning the
// diagnostics (empty when the source is accepted).
func parseTU(t *testing.T, src string) []error {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		return []error{err}
	}
	return Check(prog)
}

// A typedef name has block scope, so an ordinary identifier declared in an
// inner block may reuse the spelling (C11 6.2.1p7). Before the scope stack
// existed, a file-scope "typedef int T" permanently claimed the name, and
// "void f(void){ int T; }" was rejected as a redeclaration of a type.
func TestTypedefNameShadowedByOrdinaryIdentifier(t *testing.T) {
	if errs := parseTU(t, "typedef int T;\nint main(void){ int T; T = 7; }\n"); len(errs) > 0 {
		t.Errorf("block-scoped int T shadowing a typedef was rejected: %v", errs)
	}
	// A same-scope clash stays illegal in both directions, exactly as gcc
	// rejects it: the name is already bound in *this* scope, so it cannot be
	// bound again as the other kind of symbol (C11 6.7p3).
	if errs := parseTU(t, "typedef int T;\nint main(void){ typedef char T; int T; }\n"); len(errs) == 0 {
		t.Error("typedef char T; int T; in one scope was accepted, want a diagnostic")
	}
	if errs := parseTU(t, "typedef int T;\nint main(void){ int T; typedef char T; }\n"); len(errs) == 0 {
		t.Error("int T; typedef char T; in one scope was accepted, want a diagnostic")
	}
	if errs := parseTU(t, "typedef int T;\nint main(void){ { int T; typedef char T; } }\n"); len(errs) == 0 {
		t.Error("nested int T; typedef char T; in one scope was accepted, want a diagnostic")
	}
	if errs := parseTU(t, "typedef int T;\nint T;\nint main(void){ return 0; }\n"); len(errs) == 0 {
		t.Error("typedef int T; int T; at file scope was accepted, want a diagnostic")
	}
	// A parameter name hides an outer typedef for the whole body, so using the
	// name as a type inside the body is an error rather than a declaration.
	if errs := parseTU(t, "typedef char T;\nint f(int T){ T x = 1; return sizeof(x); }\nint main(void){ return 0; }\n"); len(errs) == 0 {
		t.Error("parameter shadowing a typedef, then using the name as a type, was accepted")
	}
	// The file-scope alias survives every block untouched.
	if errs := parseTU(t, "typedef int T;\nint main(void){ { typedef char T; } T x; }\n"); len(errs) > 0 {
		t.Errorf("outer typedef lost after an inner block: %v", errs)
	}
	// A parameter may reuse the spelling as long as the body does not then use
	// it as a type name, and one parameter's type may still be the typedef.
	if errs := parseTU(t, "typedef int T;\nint f(int T){ return T + 1; }\nint main(void){ return 0; }\n"); len(errs) > 0 {
		t.Errorf("parameter named like a typedef was rejected: %v", errs)
	}
	if errs := parseTU(t, "typedef int T;\nint f(T a, int T){ return a + T; }\nint main(void){ return 0; }\n"); len(errs) > 0 {
		t.Errorf("typedef as another parameter's type was rejected: %v", errs)
	}
}

// Typedefs() reports the bindings left after parsing, so it also proves that
// every block frame was popped and the outer binding restored: an inner
// "typedef char T" must not outlive its block.
func TestTypedefScopeIsRestoredAfterBlock(t *testing.T) {
	src := "typedef int T;\nint main(void){ typedef char T; { typedef short T; } }\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	if _, err := Parse(toks); err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := Typedefs()["T"]
	if got == nil || got.Width != 4 {
		t.Errorf("after parsing, T = %v, want the file-scope typedef int (width 4)", got)
	}
}

// The one genuinely ambiguous point of C's declaration grammar: whether a
// typedef name is a *type* or the *declarator's name*. gcc's rule -- and the
// one implemented here -- is that a typedef name only counts as a type
// specifier while no other type specifier has been seen in the same list, so
// "int T;" declares a variable while "T x;" uses T as the type. The selected
// _Generic arm reports which reading won.
func TestTypedefNameVersusDeclaratorName(t *testing.T) {
	cases := []struct {
		decl string
		want int // selected _Generic arm; -1 = expect a diagnostic instead
	}{
		// arms: 0 = int, 1 = char, 2 = default
		{"T x;", 0},           // T is the type, so x is an int
		{"T T;", 0},           // first T is the type, second the name
		{"int T;", 0},         // T is the declarator's name, of type int
		{"char T;", 1},        // same, with a different base type
		{"unsigned T;", 2},    // "unsigned" counts as a type specifier: unsigned int
		{"int T, U;", 0},      // U follows a declarator, so it is a name too
		{"const T x;", 0},     // a qualifier does not block the typedef
		{"int T x;", -1},      // gcc: "expected '=' ... before 'x'"
		{"unsigned T u;", -1}, // gcc: same shape
	}
	for _, c := range cases {
		src := "typedef int T;\nint main(void){ " + c.decl +
			" return _Generic(" + pickSubject(c.decl) + ", int: 0, char: 1, default: 2); }\n"
		toks, err := Lex(src)
		if err != nil {
			t.Fatalf("lex %q: %v", c.decl, err)
		}
		prog, err := Parse(toks)
		if c.want < 0 {
			if err == nil && len(Check(prog)) == 0 {
				t.Errorf("%q was accepted, want a diagnostic", c.decl)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q rejected: %v", c.decl, err)
			continue
		}
		if errs := Check(prog); len(errs) > 0 {
			t.Errorf("%q: %v", c.decl, errs)
			continue
		}
		got := -1
		WalkStmts(prog.Funcs[0].Body, func(s Stmt) {
			if rs, ok := s.(*ReturnStmt); ok && rs.E != nil {
				if g, ok := rs.E.(*GenericExpr); ok {
					got = g.ChosenIdx
				}
			}
		})
		if got != c.want {
			t.Errorf("%q: _Generic chose arm %d, want %d", c.decl, got, c.want)
		}
	}
}

// pickSubject returns the identifier whose type the _Generic above inspects:
// the object the declaration actually declared.
func pickSubject(decl string) string {
	if i := strings.IndexByte(decl, ','); i >= 0 {
		return strings.TrimSpace(decl[i+1 : len(decl)-1]) // the second declarator
	}
	// "T x;" / "int T;" -- the name is the last identifier before the ';'.
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(decl), ";"))
	if i := strings.LastIndexByte(body, ' '); i >= 0 {
		return body[i+1:]
	}
	return body
}
