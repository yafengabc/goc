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

func TestLexCharEscapeControls(t *testing.T) {
	// '\b' and '\f' used to fall through to "the character itself", so '\b'
	// lexed as 'b' (98) and '\f' as 'f' (102). cJSON's print_string_ptr
	// switches on exactly those two escapes when it counts how many
	// characters a string needs escaped, so every key beginning with a 'b'
	// or an 'f' was counted one too long and the printed JSON lost a byte
	// of alignment ("bits" came out as "bitso").
	cases := []struct {
		src  string
		want int64
	}{
		{"'\\b'", 8},
		{"'\\f'", 12},
		{"'\\a'", 7},
		{"'\\v'", 11},
		{"'\\n'", 10},
		{"'\\r'", 13},
		{"'\\t'", 9},
		{"'\\\\'", 92},
		{"'\\''", 39},
		{"'\\\"'", 34},
		{"'\\0'", 0},
	}
	for _, c := range cases {
		if v, _ := numOf(t, c.src); v != c.want {
			t.Errorf("%s lexed as %d, want %d", c.src, v, c.want)
		}
	}
}

func TestLexStringEscapeControls(t *testing.T) {
	// The same escape table drives string literals, and a string body has to
	// produce the identical bytes.
	toks, err := Lex(`"\b\f\a\v\n\t\r\\\"\0"`)
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	want := []byte{8, 12, 7, 11, 10, 9, 13, '\\', '"', 0}
	for _, tok := range toks {
		if tok.Kind != TStr {
			continue
		}
		if len(tok.Str) != len(want) {
			t.Fatalf("string literal lexed as %v, want %v", tok.Str, want)
		}
		for i := range want {
			if tok.Str[i] != want[i] {
				t.Fatalf("string literal lexed as %v, want %v", tok.Str, want)
			}
		}
		return
	}
	t.Fatal("no string literal token found")
}
