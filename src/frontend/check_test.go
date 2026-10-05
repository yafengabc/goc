package frontend

import "testing"

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
