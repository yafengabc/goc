package main

import "testing"

// numOf lexes src and returns the Num field of the first TNum token, along
// with the token's text. Lexing the full source keeps the path identical to
// real compilation (hex/dec/suffix handling included).
func numOf(t *testing.T, src string) (int64, string) {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", src, err)
	}
	for _, tok := range toks {
		if tok.Kind == TNum {
			return tok.Num, tok.Text
		}
	}
	t.Fatalf("Lex(%q): no numeric token found", src)
	return 0, ""
}

func TestLexIntegerMinDec(t *testing.T) {
	// 9223372036854775808 (2^63) is not representable as a signed int64, but
	// its bit pattern is the int64 minimum; -9223372036854775808L is the
	// standard spelling of LONG_MIN. The old Sscanf path left this as 0,
	// turning LONG_MIN into -0 == 0.
	v, _ := numOf(t, "9223372036854775808L")
	if v != -9223372036854775808 {
		t.Fatalf("9223372036854775808L lexed as %d, want int64 min", v)
	}
}

func TestLexIntegerMinHex(t *testing.T) {
	v, _ := numOf(t, "0x8000000000000000")
	if v != -9223372036854775808 {
		t.Fatalf("0x8000000000000000 lexed as %d, want int64 min", v)
	}
}

func TestLexIntegerMaxDec(t *testing.T) {
	v, _ := numOf(t, "9223372036854775807")
	if v != 9223372036854775807 {
		t.Fatalf("9223372036854775807 lexed as %d, want max", v)
	}
}

func TestLexIntegerMaxHex(t *testing.T) {
	v, _ := numOf(t, "0x7fffffffffffffff")
	if v != 9223372036854775807 {
		t.Fatalf("0x7fffffffffffffff lexed as %d, want max", v)
	}
}

func TestLexIntegerUint64MaxBitPattern(t *testing.T) {
	// 2^64-1 fits a uint64 bit pattern and maps to -1 when reinterpreted as
	// signed; no data is lost. Only values beyond 2^64-1 stay 0.
	v, _ := numOf(t, "18446744073709551615")
	if v != -1 {
		t.Fatalf("18446744073709551615 lexed as %d, want -1 (bit pattern)", v)
	}
}

func TestLexIntegerBeyondUint64StaysZero(t *testing.T) {
	// 2^64 overflows the 64-bit bit pattern; falls back to 0 (unchanged).
	v, text := numOf(t, "18446744073709551616")
	if text == "" {
		t.Fatal("no text")
	}
	if v != 0 {
		t.Fatalf("18446744073709551616 lexed as %d, want 0 (overflow)", v)
	}
}

func TestLexIntegerPlain(t *testing.T) {
	v, _ := numOf(t, "42")
	if v != 42 {
		t.Fatalf("42 lexed as %d, want 42", v)
	}
}
