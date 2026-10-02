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

// --- batch: lexer literal cluster (P0.3 octal, P0.4 UCN, P1.1 escapes, P1.2 leading dot) ---

func TestLexOctalLiteral(t *testing.T) {
	// A leading '0' (that is not 0x/0b) denotes an octal constant: 010 is 8,
	// 0777 is 511, not the decimal 10/777 it used to silently misparse to.
	cases := []struct {
		src  string
		want int64
	}{
		{"010", 8},
		{"0777", 511},
		{"0", 0},
		{"007", 7},
		{"077", 63},
	}
	for _, c := range cases {
		if v, _ := numOf(t, c.src); v != c.want {
			t.Errorf("%s lexed as %d, want %d (octal)", c.src, v, c.want)
		}
	}
	// Suffix flags are still recorded on an octal literal.
	toks, _ := Lex("010U")
	for _, tok := range toks {
		if tok.Kind == TNum {
			if tok.Num != 8 || !tok.IsUnsig {
				t.Errorf("010U = %d unsig=%v, want 8 true", tok.Num, tok.IsUnsig)
			}
		}
	}
	toks, _ = Lex("010L")
	for _, tok := range toks {
		if tok.Kind == TNum {
			if tok.Num != 8 || !tok.IsLong {
				t.Errorf("010L = %d long=%v, want 8 true", tok.Num, tok.IsLong)
			}
		}
	}
}

func TestLexOctalInvalidDigitRejected(t *testing.T) {
	// An 8 or 9 directly after a leading zero is an invalid octal constant
	// (gcc rejects); goc must not silently read it as decimal.
	for _, bad := range []string{"018", "019", "0778"} {
		if _, err := Lex(bad); err == nil {
			t.Errorf("Lex(%q) should reject as invalid octal digit", bad)
		}
	}
	// A trailing-dot float still reads its digits as decimal, not octal.
	toks, _ := Lex("010.5")
	for _, tok := range toks {
		if tok.Kind == TNum && tok.IsDbl {
			if tok.Fval != 10.5 {
				t.Errorf("010.5 = %v, want 10.5 (float, decimal)", tok.Fval)
			}
		}
	}
}

func TestLexStringUCN(t *testing.T) {
	// \u00e9 must decode to the UTF-8 encoding of U+00E9 (0xC3 0xA9), not
	// the literal characters u00e9 (the backslash used to be swallowed).
	toks, err := Lex(`"\u00e9"`)
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	want := []byte{0xC3, 0xA9}
	for _, tok := range toks {
		if tok.Kind == TStr {
			if len(tok.Str) != len(want) || tok.Str[0] != want[0] || tok.Str[1] != want[1] {
				t.Fatalf("string u00e9 = % x, want % x", tok.Str, want)
			}
			return
		}
	}
	t.Fatal("no string token found")
}

func TestLexCharOctalHexEscape(t *testing.T) {
	// \ooo (1-3 digits) and \xhh decode to a byte; goc char is signed so
	// \377 (0xFF) is the int -1, matching gcc rather than the raw byte 255.
	cases := []struct {
		src  string
		want int64
	}{
		{`'\101'`, 65},
		{`'\x41'`, 65},
		{`'\0'`, 0},
		{`'\377'`, -1},
		{`'\x7f'`, 127},
		{`'\x7F'`, 127},
		{`'\7'`, 7},
	}
	for _, c := range cases {
		if v, _ := numOf(t, c.src); v != c.want {
			t.Errorf("%s lexed as %d, want %d", c.src, v, c.want)
		}
	}
}

func TestLexStringOctalHexEscape(t *testing.T) {
	toks, err := Lex(`"a\101b\x41"`)
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	want := []byte{'a', 65, 'b', 65}
	for _, tok := range toks {
		if tok.Kind == TStr {
			if len(tok.Str) != len(want) {
				t.Fatalf("bytes %v, want %v", tok.Str, want)
			}
			for i := range want {
				if tok.Str[i] != want[i] {
					t.Fatalf("bytes %v, want %v", tok.Str, want)
				}
			}
			return
		}
	}
	t.Fatal("no string token found")
}

func TestLexHexEscapeNoDigitRejected(t *testing.T) {
	// \x with no hex digit is a clean error (gcc: "'\x' used with no
	// following hex digits"), not a silent byte.
	if _, err := Lex(`"\x"`); err == nil {
		t.Error(`"\x" should reject (no hex digits)`)
	}
}

func TestLexLeadingDotFloat(t *testing.T) {
	// .5 / .5e2 / .1'2 (C23 separator) lex as floating constants starting
	// at the leading dot.
	cases := []struct {
		src  string
		want float64
	}{
		{".5", 0.5},
		{".5e2", 50.0},
		{".1'2", 0.12},
	}
	for _, c := range cases {
		toks, err := Lex(c.src)
		if err != nil {
			t.Fatalf("Lex(%q) error: %v", c.src, err)
		}
		found := false
		for _, tok := range toks {
			if tok.Kind == TNum && tok.IsDbl {
				found = true
				if tok.Fval != c.want {
					t.Errorf("%s = %v, want %v", c.src, tok.Fval, c.want)
				}
			}
		}
		if !found {
			t.Errorf("%s did not lex as a float", c.src)
		}
	}
	// .5f records the float (narrowing) flag.
	toks, _ := Lex(".5f")
	found := false
	for _, tok := range toks {
		if tok.Kind == TNum && tok.IsDbl {
			found = true
			if !tok.IsFloat {
				t.Error(".5f should be a float constant")
			}
		}
	}
	if !found {
		t.Error(".5f did not lex as a float")
	}
}
