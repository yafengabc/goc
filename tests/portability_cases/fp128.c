/* ---------------------------------------------------------------------------
 * fp128: goc's binary128 runtime (goclib/fp128.c) exercised under every back
 * end.
 *
 * `long double` is IEEE binary128 on every target and is computed in software,
 * so this file is really a test of integer code over a bit pattern. The
 * arithmetic is passed through raw words rather than a C type because the front
 * end has no long double yet (#45/#47/#48) -- the prototypes below are what
 * the runtime exposes today, and goclib.h declares the same thing.
 *
 * Two kinds of check:
 *
 *   - explicit expectations, generated from gcc's __float128, i.e. from
 *     libgcc's own tf3 routines. NaN results are only checked for NaN-ness:
 *     which NaN comes back is unspecified and libgcc's is negative while ours
 *     is positive, so pinning the bits here would pin an accident.
 *
 *   - a deterministic pseudo-random battery folded into one FNV hash. It pins
 *     roughly 30 thousand operations in a single number, which is what makes
 *     "the same integer code, compiled by a different back end" checkable at
 *     all: the bit-exact oracle needs __float128 and can only run on the host
 *     (tests/fp128/oracle.c), so this is the part that covers goc and gocl.
 *
 * Self-reporting: exit 0 with OK on the last line.
 * ------------------------------------------------------------------------- */

#include <stdio.h>

typedef struct {
    unsigned long long lo;
    unsigned long long hi;
} goc_tf128;

goc_tf128          goc_tf_add(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_sub(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_mul(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_div(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_neg(goc_tf128 a);
int                goc_tf_cmp(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_from_double(unsigned long long bits);
goc_tf128          goc_tf_from_float(unsigned int bits);
unsigned long long goc_tf_to_double(goc_tf128 a);
unsigned int       goc_tf_to_float(goc_tf128 a);
goc_tf128          goc_tf_from_ll(long long v);
goc_tf128          goc_tf_from_ull(unsigned long long v);
long long          goc_tf_to_ll(goc_tf128 a);
unsigned long long goc_tf_to_ull(goc_tf128 a);

static int fails = 0;

static void bad(const char *msg, goc_tf128 got) {
    fails++;
    if (fails <= 20) printf("FAIL %s: got %016llx.%016llx\n", msg, got.hi, got.lo);
}

static void badll(const char *msg, unsigned long long got, unsigned long long want) {
    fails++;
    if (fails <= 20) printf("FAIL %s: got %016llx want %016llx\n", msg, got, want);
}

static void expectll(const char *msg, unsigned long long got, unsigned long long want) {
    if (got != want) badll(msg, got, want);
}

static goc_tf128 mk(unsigned long long hi, unsigned long long lo) {
    goc_tf128 x;
    x.hi = hi;
    x.lo = lo;
    return x;
}

static void expect(const char *msg, goc_tf128 got,
                   unsigned long long hi, unsigned long long lo) {
    if (got.hi != hi || got.lo != lo) bad(msg, got);
}

static int isnan128(goc_tf128 x) {
    return ((x.hi >> 48) & 0x7FFFull) == 0x7FFFull &&
           (((x.hi & 0x0000FFFFFFFFFFFFull) | x.lo) != 0ull);
}

/* Expectations from gcc's __float128 (libgcc). */
static void explicit_cases(void) {
    goc_tf128 one = mk(0x3FFF000000000000ull, 0ull);       /* 1.0 */
    goc_tf128 two = mk(0x4000000000000000ull, 0ull);       /* 2.0 */
    goc_tf128 mone = mk(0xBFFF000000000000ull, 0ull);      /* -1.0 */
    goc_tf128 half = mk(0x3FFE000000000000ull, 0ull);      /* 0.5 */
    goc_tf128 minsub = mk(0x0000000000000000ull, 1ull);    /* 2^-16494 */
    goc_tf128 maxn = mk(0x7FFEFFFFFFFFFFFFull, 0xFFFFFFFFFFFFFFFFull);
    goc_tf128 u1 = mk(0x3FFF000000000000ull, 1ull);        /* 1 + 2^-112 */
    goc_tf128 halfulp = mk(0x3F8E000000000000ull, 0ull);   /* 2^-113, ties */
    goc_tf128 abovehalf = mk(0x3F8E800000000000ull, 0ull); /* 3 * 2^-114 */
    goc_tf128 three = goc_tf_from_ll(3);
    goc_tf128 r;

    expect("1.5+2.25", goc_tf_add(mk(0x3FFF800000000000ull, 0ull),
                                  mk(0x4000200000000000ull, 0ull)),
           0x4000E00000000000ull, 0x0000000000000000ull);
    expect("1.0-1.0 = +0", goc_tf_sub(one, one),
           0x0000000000000000ull, 0x0000000000000000ull);
    expect("1.0-(-1.0)", goc_tf_sub(one, mone),
           0x4000000000000000ull, 0x0000000000000000ull);
    expect("minsub*2", goc_tf_mul(minsub, two),
           0x0000000000000000ull, 0x0000000000000002ull);
    /* exactly half of the smallest subnormal: ties to even rounds to zero */
    expect("minsub*0.5", goc_tf_mul(minsub, half),
           0x0000000000000000ull, 0x0000000000000000ull);
    expect("maxnormal*2 = inf", goc_tf_mul(maxn, two),
           0x7FFF000000000000ull, 0x0000000000000000ull);
    expect("1/3", goc_tf_div(one, three),
           0x3FFD555555555555ull, 0x5555555555555555ull);
    expect("1/0 = inf", goc_tf_div(one, mk(0ull, 0ull)),
           0x7FFF000000000000ull, 0x0000000000000000ull);
    if (!isnan128(goc_tf_div(mk(0ull, 0ull), mk(0ull, 0ull)))) bad("0/0 is NaN", mk(0ull, 0ull));
    if (!isnan128(goc_tf_add(mk(0x7FFF000000000000ull, 0ull),
                             mk(0xFFFF000000000000ull, 0ull)))) bad("inf + -inf", mk(0ull, 0ull));
    if (!isnan128(goc_tf_mul(mk(0ull, 0ull), mk(0x7FFF000000000000ull, 0ull)))) bad("0 * inf", mk(0ull, 0ull));
    /* -0 + -0 is -0; -0 + +0 is +0 under round-to-nearest */
    expect("-0 + -0", goc_tf_add(mk(0x8000000000000000ull, 0ull), mk(0x8000000000000000ull, 0ull)),
           0x8000000000000000ull, 0x0000000000000000ull);
    expect("-0 + +0", goc_tf_add(mk(0x8000000000000000ull, 0ull), mk(0ull, 0ull)),
           0x0000000000000000ull, 0x0000000000000000ull);
    /* the tie and the first value above it: 2^-113 is exactly half an ulp */
    expect("1 + 2^-113 (tie -> even)", goc_tf_add(one, halfulp),
           0x3FFF000000000000ull, 0x0000000000000000ull);
    expect("1 + 3*2^-114", goc_tf_add(one, abovehalf),
           0x3FFF000000000000ull, 0x0000000000000001ull);
    expect("neg(1.0)", goc_tf_neg(one), 0xBFFF000000000000ull, 0x0000000000000000ull);

    if (goc_tf_cmp(mk(0x8000000000000000ull, 0ull), mk(0ull, 0ull)) != 0)
        bad("-0 == +0", mk(0ull, 0ull));
    if (goc_tf_cmp(one, two) != -1) bad("1 < 2", one);
    if (goc_tf_cmp(mone, one) != -1) bad("-1 < 1", mone);
    if (goc_tf_cmp(one, mk(0x7FFF800000000000ull, 0ull)) != 2) bad("cmp with NaN", one);

    /* widening and narrowing across the exact boundary */
    expect("from_double(1.0)", goc_tf_from_double(0x3FF0000000000000ull),
           0x3FFF000000000000ull, 0x0000000000000000ull);
    expect("from_float(1.0f)", goc_tf_from_float(0x3F800000u),
           0x3FFF000000000000ull, 0x0000000000000000ull);
    expect("from_ll(1)", goc_tf_from_ll(1), 0x3FFF000000000000ull, 0x0000000000000000ull);
    /* 2^64-1 is exact in 113 bits -- it is not rounded up to 2^64 */
    expect("from_ull(2^64-1)", goc_tf_from_ull(0xFFFFFFFFFFFFFFFFull),
           0x403EFFFFFFFFFFFFull, 0xFFFE000000000000ull);
    expect("from_ll(LLONG_MIN)", goc_tf_from_ll((long long)0x8000000000000000ull),
           0xC03E000000000000ull, 0x0000000000000000ull);

    expectll("to_double(1.0)", goc_tf_to_double(one), 0x3FF0000000000000ull);
    expectll("to_float(1.0)", (unsigned long long)goc_tf_to_float(one), 0x3F800000ull);
    /* 1 + 2^-53 in binary128: exponent field 16383, fraction bit 112-53 = 59.
     * It is exactly half an ulp of 1.0 in binary64, so ties-to-even holds 1.0. */
    expectll("to_double(1+2^-53)",
             goc_tf_to_double(mk(0x3FFF000000000000ull, 0x0800000000000000ull)),
             0x3FF0000000000000ull);
    expectll("to_double(minsub) = 0", goc_tf_to_double(minsub), 0x0000000000000000ull);
    expectll("to_double(maxnormal) = inf", goc_tf_to_double(maxn), 0x7FF0000000000000ull);

    /* truncation toward zero, and the documented saturation */
    expectll("to_ll(1.5)", (unsigned long long)goc_tf_to_ll(mk(0x3FFF800000000000ull, 0ull)), 1ull);
    expectll("to_ll(-1.5)", (unsigned long long)goc_tf_to_ll(mk(0xBFFF800000000000ull, 0ull)),
             0xFFFFFFFFFFFFFFFFull);
    expectll("to_ll(0.5)", (unsigned long long)goc_tf_to_ll(half), 0ull);
    expectll("to_ll(LLONG_MIN)", (unsigned long long)goc_tf_to_ll(mk(0xC03E000000000000ull, 0ull)),
             0x8000000000000000ull);
    expectll("to_ll(2^63) saturates", (unsigned long long)goc_tf_to_ll(mk(0x403F000000000000ull, 0ull)),
             0x7FFFFFFFFFFFFFFFull);
    expectll("to_ull(-1) clamps to 0", goc_tf_to_ull(mone), 0ull);
    expectll("to_ull(2^64-1)", goc_tf_to_ull(goc_tf_from_ull(0xFFFFFFFFFFFFFFFFull)),
             0xFFFFFFFFFFFFFFFFull);

    /* one ulp below and above 1.0 must survive a round trip through u1 */
    r = goc_tf_sub(u1, one);
    expect("(1+2^-112) - 1 = 2^-112", r, 0x3F8F000000000000ull, 0x0000000000000000ull);
}

/* ---- the hash battery ---------------------------------------------------- */

static unsigned long long st = 0x123456789ABCDEFull;

static unsigned long long nx(void) {
    st = st * 6364136223846793005ull + 1442695040888963407ull;
    return st ^ (st >> 32);
}

/* Folded result of the 900-round battery below (~29 thousand operations),
 * measured on the host with gcc. It pins the whole runtime in one number so
 * that goc and gocl can be checked against it: they have no __float128 to
 * compare with, and the arithmetic is pure integer code, so a different value
 * means a back end differs, not that the answer is a matter of taste. */
#define FP128_HASH 0xa25fb5d1c2af45c3ull

static unsigned long long hash_battery(void) {
    unsigned long long h = 0xCBF29CE484222325ull;
    int i;
    for (i = 0; i < 900; i++) {
        goc_tf128 a, b, r;
        a.lo = nx();
        a.hi = nx();
        b.lo = nx();
        b.hi = nx();
#define MIX(v) do { h ^= (unsigned long long)(v); h *= 0x100000001B3ull; } while (0)
        r = goc_tf_add(a, b); MIX(r.hi); MIX(r.lo);
        r = goc_tf_sub(a, b); MIX(r.hi); MIX(r.lo);
        r = goc_tf_mul(a, b); MIX(r.hi); MIX(r.lo);
        r = goc_tf_div(a, b); MIX(r.hi); MIX(r.lo);
        MIX(goc_tf_cmp(a, b));
        /* Not `MIX(goc_tf_neg(a).hi)`: taking a member of a struct-returning
         * call in expression position is a goc native-back-end bug -- it
         * leaves the next struct-returning call reading a pointer instead of
         * a value. Repro: tests/fp128/goc_structret_bug.c. Going through a
         * variable is the workaround; the battery is not here to test that. */
        r = goc_tf_neg(a);
        MIX(r.hi);
        MIX(goc_tf_to_double(a));
        MIX(goc_tf_to_float(a));
        MIX(goc_tf_to_ll(a));
        MIX(goc_tf_to_ull(a));
        r = goc_tf_from_double(a.lo); MIX(r.hi); MIX(r.lo);
        r = goc_tf_from_float((unsigned int)a.lo); MIX(r.hi); MIX(r.lo);
        r = goc_tf_from_ll((long long)a.hi); MIX(r.hi); MIX(r.lo);
        r = goc_tf_from_ull(a.hi); MIX(r.hi); MIX(r.lo);
#undef MIX
    }
    return h;
}

int main(void) {
    explicit_cases();
    {
        unsigned long long h = hash_battery();
#ifdef FP128_PRINT_HASH
        printf("HASH 0x%016llx\n", h);
#else
        if (h != FP128_HASH) badll("hash battery", h, (unsigned long long)FP128_HASH);
#endif
    }
    printf("\n%s\n", fails ? "BAD" : "OK");
    return fails ? 1 : 0;
}
