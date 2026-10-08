package frontend

import (
	"math"
	"math/big"
	"strings"
	"testing"
)

// The expected encodings below are hand-computed from the binary128 layout
// (1 sign bit, 15 exponent bits biased by 16383, 112 stored mantissa bits) so
// the test is an independent oracle rather than a recording of whatever the
// implementation happens to produce.

func TestParseFloat128Exact(t *testing.T) {
	cases := []struct {
		in string
		hi uint64
		lo uint64
	}{
		// powers of two: exponent field = e + 16383, mantissa zero
		{"1.0", 0x3FFF000000000000, 0x0000000000000000},
		{"2.0", 0x4000000000000000, 0x0000000000000000},
		{"0.5", 0x3FFE000000000000, 0x0000000000000000},
		{"4", 0x4001000000000000, 0x0000000000000000},
		// 1.5 = 1.1b * 2^0 -> the top mantissa bit is set
		{"1.5", 0x3FFF800000000000, 0x0000000000000000},
		// 3.0 = 1.1b * 2^1
		{"3.0", 0x4000800000000000, 0x0000000000000000},
		// -1.0: same, sign bit set
		{"-1.0", 0xBFFF000000000000, 0x0000000000000000},
		// zeros
		{"0.0", 0x0000000000000000, 0x0000000000000000},
		{"-0.0", 0x8000000000000000, 0x0000000000000000},
		// 0.1 = 1.100110011...b * 2^-4; the repeating "1001" fills all 112
		// mantissa bits and the tail rounds the last nibble up to A.
		{"0.1", 0x3FFB999999999999, 0x999999999999999A},
		// 1/3 = 1.010101...b * 2^-2. Bit 113 of the true value is 0 and the
		// rest of the tail is nonzero but below half an ulp, so it truncates
		// to a mantissa of "01" repeated 56 times. The 40-digit decimal is
		// within 3e-41 of 1/3 -- far below one ulp here (2^-114 ~ 5e-35) --
		// so it must land on exactly the same encoding as 1/3 itself.
		{"0." + strings.Repeat("3", 40), 0x3FFD555555555555, 0x5555555555555555},
	}
	for _, c := range cases {
		got, err := ParseFloat128(c.in)
		if err != nil {
			t.Fatalf("ParseFloat128(%q): %v", c.in, err)
		}
		if got.Hi != c.hi || got.Lo != c.lo {
			t.Errorf("ParseFloat128(%q) = %016x%016x, want %016x%016x",
				c.in, got.Hi, got.Lo, c.hi, c.lo)
		}
	}
}

func TestParseFloat128OverflowAndUnderflow(t *testing.T) {
	// binary128 tops out near 1.19e4932 and bottoms out near 3.4e-4932.
	if got, _ := ParseFloat128("1e5000"); !got.IsInf() {
		t.Errorf("1e5000 = %s, want +inf", got.Bits())
	}
	if got, _ := ParseFloat128("-1e5000"); !got.IsInf() || got.Hi>>63 != 1 {
		t.Errorf("-1e5000 = %s, want -inf", got.Bits())
	}
	if got, _ := ParseFloat128("1e4932"); got.IsInf() {
		t.Errorf("1e4932 = %s, want finite", got.Bits())
	}
	if got, _ := ParseFloat128("1e-5000"); !got.IsZero() {
		t.Errorf("1e-5000 = %s, want zero", got.Bits())
	}
	if got, _ := ParseFloat128("-1e-5000"); !got.IsZero() || got.Hi>>63 != 1 {
		t.Errorf("-1e-5000 = %s, want -zero", got.Bits())
	}
}

func TestParseFloat128Hex(t *testing.T) {
	// 0x1.8p3 = 1.5 * 8 = 12 = 1.1b * 2^3 -> exponent field 16386.
	got, err := ParseFloat128("0x1.8p3")
	if err != nil {
		t.Fatal(err)
	}
	if got.Hi != 0x4002800000000000 || got.Lo != 0 {
		t.Errorf("0x1.8p3 = %s, want 40028000000000000000000000000000", got.Bits())
	}
	// 0x1p0 is exactly 1.0.
	got, _ = ParseFloat128("0x1p0")
	if got.Hi != 0x3FFF000000000000 || got.Lo != 0 {
		t.Errorf("0x1p0 = %s", got.Bits())
	}
	// 0x1.fffffffffffff8p0 = 2 - 2^-53: significand 1.111...1 with 53 ones,
	// so the 112-bit mantissa is 53 ones (bits 111..59) followed by zeros --
	// Hi's mantissa is all ones and Lo is 11111b << 59.
	got, _ = ParseFloat128("0x1.fffffffffffff8p0")
	if got.Hi != 0x3FFFFFFFFFFFFFFF || got.Lo != 0xF800000000000000 {
		t.Errorf("0x1.fffffffffffff8p0 = %s", got.Bits())
	}
}

func TestFloat128FromFloat64(t *testing.T) {
	// Every double is exactly representable, so widening is a re-encoding.
	if got := Float128FromFloat64(1); got.Hi != 0x3FFF000000000000 || got.Lo != 0 {
		t.Errorf("float64 1 -> %s", got.Bits())
	}
	if got := Float128FromFloat64(0.5); got.Hi != 0x3FFE000000000000 || got.Lo != 0 {
		t.Errorf("float64 0.5 -> %s", got.Bits())
	}
	if got := Float128FromFloat64(-2); got.Hi != 0xC000000000000000 || got.Lo != 0 {
		t.Errorf("float64 -2 -> %s", got.Bits())
	}
	// The mantissa's 52 stored bits move to positions 111..60, so the low 60
	// bits of Lo are always zero for a widened normal double.
	if got := Float128FromFloat64(3.141592653589793); got.Lo&((uint64(1)<<60)-1) != 0 {
		t.Errorf("float64 pi -> %s: low 60 bits must be zero", got.Bits())
	}
	// A binary64 subnormal becomes a perfectly ordinary binary128 normal:
	// 2^-1074 has unbiased exponent -1074, so the exponent field is 15309 and
	// the mantissa is zero (the leading bit is implicit, not stored).
	got := Float128FromFloat64(math.Float64frombits(1))
	if got.Hi != 0x3BCD000000000000 || got.Lo != 0 {
		t.Errorf("2^-1074 -> %s, want 3BCD0000000000000000000000000000", got.Bits())
	}
	// inf and NaN keep their class.
	if got := Float128FromFloat64(math.Inf(1)); !got.IsInf() || got.Hi>>63 != 0 {
		t.Errorf("+Inf -> %s, want +inf", got.Bits())
	}
	nan := Float128FromFloat64(math.NaN())
	if (nan.Hi>>48)&f128ExpMax != f128ExpMax || nan.Hi&f128QuietHi == 0 {
		t.Errorf("NaN -> %s, want a quiet NaN", nan.Bits())
	}
}

// TestParseFloat128RoundsOnce guards the reason this file exists: a decimal
// constant must be rounded once, straight to 113 bits, and not via a float64.
// The decimals here are generated exactly from big.Rat rather than typed by
// hand, so the expectations are self-evident:
//
//   - 1 + 2^-112 is exactly representable -> mantissa 1.
//   - 1 + 2^-113 is exactly half an ulp above 1.0, a tie that rounds to even,
//     i.e. back down to 1.0. A float64 cannot even hold 2^-113, so any path
//     through a float64 would produce 1.0 here too -- the point of the case is
//     that the tie must resolve *down*, which is the round-to-even rule.
//   - 1 + 2^-112 + 2^-113 is a whisker over the tie and must round up to 2 ulp.
func TestParseFloat128RoundsOnce(t *testing.T) {
	// 2^-k as an exact terminating decimal (denominator 2^k -> k digits).
	pow2Decimal := func(k int) string {
		r := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), uint(k)))
		return r.FloatString(k)
	}
	onePlus := func(ks ...int) string {
		r := new(big.Rat).SetInt64(1)
		for _, k := range ks {
			r.Add(r, new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), uint(k))))
		}
		return r.FloatString(200)
	}
	if s := pow2Decimal(112); !strings.HasSuffix(s, "5") {
		t.Fatalf("2^-112 decimal %q looks wrong", s)
	}
	got, err := ParseFloat128(onePlus(112))
	if err != nil {
		t.Fatal(err)
	}
	if got.Hi != 0x3FFF000000000000 || got.Lo != 1 {
		t.Errorf("1+2^-112 = %s, want mantissa 1", got.Bits())
	}
	got, err = ParseFloat128(onePlus(113))
	if err != nil {
		t.Fatal(err)
	}
	if got.Hi != 0x3FFF000000000000 || got.Lo != 0 {
		t.Errorf("1+2^-113 (a tie) = %s, want to round to even: 1.0", got.Bits())
	}
	got, err = ParseFloat128(onePlus(112, 113))
	if err != nil {
		t.Fatal(err)
	}
	if got.Hi != 0x3FFF000000000000 || got.Lo != 2 {
		t.Errorf("1+2^-112+2^-113 = %s, want mantissa 2", got.Bits())
	}
}

// TestParseFloat128MatchesBigRat checks rounding independently for a batch of
// constants: decode the result back into an exact rational and confirm it is
// within half an ulp of the exact decimal value.
func TestParseFloat128MatchesBigRat(t *testing.T) {
	for _, s := range []string{
		"1.0", "0.1", "1e-30", "123456789.123456789", "1e300",
		"2.718281828459045235360287471352662497757247093699959574966",
		"1e-320", "0.3", "1e100", "12345678901234567890123456789.0",
		"0.000000000000000000000000000000000000000000001",
	} {
		got, err := ParseFloat128(s)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		num, den, err := decRat(s)
		if err != nil {
			t.Fatal(err)
		}
		exact := new(big.Rat).SetFrac(num, den)
		back := f128ToRat(got)
		diff := new(big.Rat).Sub(back, exact)
		if diff.Sign() < 0 {
			diff.Neg(diff)
		}
		// e = floor(log2|back|); half an ulp is 2^(e-113).
		fb := new(big.Float).SetPrec(256).SetRat(back)
		e := fb.MantExp(nil) - 1
		half := pow2Rat(e - 113)
		if diff.Cmp(half) > 0 {
			t.Errorf("%q: error %v exceeds half an ulp %v (got %s)", s, diff, half, got.Bits())
		}
	}
}

// TestF128ToRatRoundTrip is the guard on the guard: the decoder the property
// test above relies on must itself agree with the encoder.
func TestF128ToRatRoundTrip(t *testing.T) {
	for _, s := range []string{"1.0", "0.1", "1e300", "1e-320", "12345.6789"} {
		got, err := ParseFloat128(s)
		if err != nil {
			t.Fatal(err)
		}
		num, den, _ := decRat(s)
		exact := new(big.Rat).SetFrac(num, den)
		back := f128ToRat(got)
		// The decoded value must equal the original to within 2^-113 relative.
		diff := new(big.Rat).Sub(back, exact)
		if diff.Sign() < 0 {
			diff.Neg(diff)
		}
		scale := new(big.Rat).Mul(exact, pow2Rat(-113))
		if scale.Sign() < 0 {
			scale.Neg(scale)
		}
		if diff.Cmp(scale) > 0 {
			t.Errorf("%q: decode(%s) = %v, want within 2^-113 of %v", s, got.Bits(), back, exact)
		}
	}
}

// TestFloat128FromFloat64MatchesParse cross-checks the widening path against
// the decimal parser for the awkward binary64 classes. It exists because a
// single hand-picked subnormal hides a bug rather than exposing one: for
// 2^-1074 the stray bit the widening path can leave set lands on an exponent
// field whose low bit is already 1 (15309 is odd) and vanishes from Lo by
// shifting past 64 bits, so the smallest subnormal alone cannot tell the two
// implementations apart. Feeding the exact decimal of several subnormals
// through the independent parser does.
func TestFloat128FromFloat64MatchesParse(t *testing.T) {
	patterns := []uint64{
		0x0000000000000001, // smallest subnormal: 2^-1074
		0x0000000000000003, // 3 * 2^-1074: normalises with a nonzero fraction
		0x000FFFFFFFFFFFFF, // largest subnormal
		0x0010000000000000, // smallest normal
		0x3FF0000000000000, // 1.0
		0x4000000000000000, // 2.0
		0x7FEFFFFFFFFFFFFF, // largest finite double
		0x3FD5555555555555, // ~1/3
	}
	for _, b := range patterns {
		d := math.Float64frombits(b)
		// The exact value of the double, printed with enough digits that a
		// terminating binary expansion (every double has one) is shown whole.
		exact := new(big.Rat).SetFloat64(d)
		s := exact.FloatString(1200)
		want, err := ParseFloat128(s)
		if err != nil {
			t.Fatalf("bits %016x: %v", b, err)
		}
		if got := Float128FromFloat64(d); got != want {
			t.Errorf("bits %016x: Float128FromFloat64 = %s, ParseFloat128(%q...) = %s",
				b, got.Bits(), s[:min(len(s), 24)], want.Bits())
		}
	}
}

// --- the long double type itself -------------------------------------------

func TestLongDoubleLayout(t *testing.T) {
	ld := LongDoubleType()
	if got := sizeOf(ld); got != 16 {
		t.Errorf("sizeof(long double) = %d, want 16", got)
	}
	if got := alignOf(ld); got != 16 {
		t.Errorf("_Alignof(long double) = %d, want 16", got)
	}
	if got := ld.Class(); got != TF128 {
		t.Errorf("long double class = %v, want TF128 (not the 8-byte TInt/TDouble slots)", got)
	}
	if !ld.IsFloating() || !ld.IsArith() {
		t.Error("long double must be a real arithmetic floating type")
	}
	if got := ld.String(); got != "long double" {
		t.Errorf("String() = %q, want \"long double\"", got)
	}
	// An array of them is 16 bytes an element, and a struct containing one
	// inherits the 16-byte alignment.
	if got := sizeOf(ArrType(LongDoubleType(), 3)); got != 48 {
		t.Errorf("sizeof(long double[3]) = %d, want 48", got)
	}
}

// TestLongDoubleUsualArith is deliberately NOT here yet. The ordering
// (long double > double > float) is implemented in check.go's usual arithmetic
// conversions, but it can only be observed through _Generic once "long double"
// and the l/L suffix actually produce the long double type, which waits for
// #47/#48 -- until then the parser maps both to double on purpose, because
// test_c89 uses long double and is compiled as double today.

func TestLongDoubleLiteralEncoding(t *testing.T) {
	// Each literal is lexed on its own: the suffix is not part of the token
	// text, so "1.5", "1.5f" and "1.5L" all carry Text "1.5" and cannot be
	// told apart inside one token stream.
	lexOne := func(s string) Token {
		toks, err := Lex(s)
		if err != nil {
			t.Fatalf("lex %q: %v", s, err)
		}
		for _, tk := range toks {
			if tk.Kind == TNum {
				return tk
			}
		}
		t.Fatalf("no numeric token in %q", s)
		return Token{}
	}

	// 1.5L must carry its 128-bit encoding: 1.1b * 2^0 -> 3FFF8000...0.
	ld := lexOne("1.5L")
	if !ld.IsLongDouble {
		t.Fatalf("1.5L: IsLongDouble = false")
	}
	if ld.F128.Hi != 0x3FFF800000000000 || ld.F128.Lo != 0 {
		t.Errorf("1.5L encoded as %s, want 3fff8000000000000000000000000000", ld.F128.Bits())
	}
	// The unsuffixed and f-suffixed forms must stay double and float.
	if d := lexOne("1.5"); d.IsLongDouble || !d.IsDbl {
		t.Errorf("1.5: IsLongDouble = %v, IsDbl = %v; want false/true", d.IsLongDouble, d.IsDbl)
	}
	if f := lexOne("1.5f"); f.IsLongDouble || !f.IsFloat {
		t.Errorf("1.5f: IsLongDouble = %v, IsFloat = %v; want false/true", f.IsLongDouble, f.IsFloat)
	}
	// On an *integer* literal l/L still means "long", not long double -- the
	// suffix scanner only ever runs on the floating paths.
	if l := lexOne("1L"); l.IsLongDouble || !l.IsLong {
		t.Errorf("1L: IsLongDouble = %v, IsLong = %v; want false/true", l.IsLongDouble, l.IsLong)
	}
	// A hex float constant takes the same path: 0x1.8p3L = 12.0.
	if h := lexOne("0x1.8p3L"); !h.IsLongDouble || h.F128.Hi != 0x4002800000000000 || h.F128.Lo != 0 {
		t.Errorf("0x1.8p3L: IsLongDouble = %v, encoding %s", h.IsLongDouble, h.F128.Bits())
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func pow2Rat(n int) *big.Rat {
	r := new(big.Rat).SetFrac(big.NewInt(1), big.NewInt(1))
	if n >= 0 {
		r.Num().Lsh(r.Num(), uint(n))
	} else {
		r.Denom().Lsh(r.Denom(), uint(-n))
	}
	return r
}

// f128ToRat decodes a finite binary128 into an exact rational (test helper).
func f128ToRat(f Float128) *big.Rat {
	sign := 1
	if f.Hi>>63 != 0 {
		sign = -1
	}
	expField := int((f.Hi >> 48) & f128ExpMax)
	mant := new(big.Int).Lsh(big.NewInt(int64(f.Hi&((uint64(1)<<48)-1))), 64)
	mant.Or(mant, new(big.Int).SetUint64(f.Lo))
	if expField == f128ExpMax {
		panic("f128ToRat: inf/nan")
	}
	e := expField - f128ExpBias
	if expField == 0 {
		e = f128MinExp
	} else {
		mant.SetBit(mant, f128MantBits, 1) // implicit leading bit
	}
	r := pow2Rat(e - f128MantBits)
	r.Mul(r, new(big.Rat).SetInt(mant))
	if sign < 0 {
		r.Neg(r)
	}
	return r
}
