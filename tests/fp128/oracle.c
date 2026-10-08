/* ---------------------------------------------------------------------------
 * fp128 oracle: bit-compares src/goclib/fp128.c against gcc's __float128.
 *
 * HOST ONLY. This file is deliberately not a portability case: the oracle is
 * __float128, which resolves to libgcc's own __addtf3/__multf3/__divtf3, and
 * goc cannot compile __float128 at all. So this runs under gcc alone, and the
 * portability side is covered separately by tests/portability_cases/fp128.c,
 * which pins the values this file establishes.
 *
 *   gcc -std=c11 -O2 -I src/goclib oracle.c src/goclib/fp128.c -o oracle
 *
 * Every operation must agree bit for bit. The one deliberate exception is a
 * NaN result: C does not say which NaN comes back, so a NaN is checked for
 * NaN-ness, not for its payload.
 * ------------------------------------------------------------------------- */

#include <stdio.h>
#include <string.h>

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

/* ---- the oracle side ----------------------------------------------------- */

static __float128 q_of(goc_tf128 x) {
    __float128 q;
    memcpy(&q, &x, 16);
    return q;
}

static goc_tf128 f_of(__float128 q) {
    goc_tf128 x;
    memcpy(&x, &q, 16);
    return x;
}

static double d_of(unsigned long long bits) {
    double d;
    memcpy(&d, &bits, 8);
    return d;
}

static unsigned long long bits_of_d(double d) {
    unsigned long long u;
    memcpy(&u, &d, 8);
    return u;
}

static float f32_of(unsigned int bits) {
    float f;
    memcpy(&f, &bits, 4);
    return f;
}

static unsigned int bits_of_f32(float f) {
    unsigned int u;
    memcpy(&u, &f, 4);
    return u;
}

static int isnan_bits(goc_tf128 x) {
    return ((x.hi >> 48) & 0x7FFFull) == 0x7FFFull &&
           (((x.hi & 0x0000FFFFFFFFFFFFull) | x.lo) != 0ull);
}

/* ---- reporting ------------------------------------------------------------ */

static long checks = 0;
static long fails = 0;

static void hex128(char *out, goc_tf128 x) {
    sprintf(out, "%016llx.%016llx", x.hi, x.lo);
}

static void bad(const char *what, goc_tf128 got, goc_tf128 want,
                const char *ctx) {
    char g[40], w[40];
    fails++;
    if (fails > 25) return;
    hex128(g, got);
    hex128(w, want);
    printf("FAIL %-6s got %s want %s   %s\n", what, g, w, ctx);
}

static void ctx(char *out, goc_tf128 a, goc_tf128 b) {
    char x[40], y[40];
    hex128(x, a);
    hex128(y, b);
    sprintf(out, "a=%s b=%s", x, y);
}

/* A NaN result is only checked for NaN-ness: which NaN comes back is
 * unspecified, and libgcc and this implementation do not agree on payloads. */
static void expect(const char *what, goc_tf128 got, goc_tf128 want,
                   goc_tf128 a, goc_tf128 b) {
    char c[128];
    checks++;
    if (isnan_bits(want)) {
        if (!isnan_bits(got)) bad(what, got, want, "");
        return;
    }
    if (got.hi != want.hi || got.lo != want.lo) {
        ctx(c, a, b);
        bad(what, got, want, c);
    }
}

/* ---- random source -------------------------------------------------------- */

static unsigned long long rs = 0x243F6A8885A308D3ull; /* pi, fractional bits */

static unsigned long long rnd(void) {
    rs ^= rs << 13;
    rs ^= rs >> 7;
    rs ^= rs << 17;
    return rs;
}

static goc_tf128 rnd128(void) {
    goc_tf128 x;
    x.lo = rnd();
    x.hi = rnd();
    return x;
}

/* ---- the batteries -------------------------------------------------------- */

static void binops(goc_tf128 a, goc_tf128 b) {
    __float128 x = q_of(a), y = q_of(b);
    expect("add", goc_tf_add(a, b), f_of(x + y), a, b);
    expect("sub", goc_tf_sub(a, b), f_of(x - y), a, b);
    expect("mul", goc_tf_mul(a, b), f_of(x * y), a, b);
    expect("div", goc_tf_div(a, b), f_of(x / y), a, b);
    checks++;
    {
        int want = (x != x || y != y) ? 2 : (x < y ? -1 : (x > y ? 1 : 0));
        int got = goc_tf_cmp(a, b);
        if (got != want) {
            char c[128];
            ctx(c, a, b);
            fails++;
            if (fails <= 25) printf("FAIL cmp   got %d want %d   %s\n", got, want, c);
        }
    }
}

static void unops(goc_tf128 a) {
    __float128 x = q_of(a);
    char c[64];

    checks++;
    if (goc_tf_cmp(goc_tf_neg(a), f_of(-x)) != 0 && !isnan_bits(a)) {
        hex128(c, a);
        fails++;
        if (fails <= 25) printf("FAIL neg   %s\n", c);
    }

    /* fp128 -> double / float: libgcc's __trunctfdf2 / __trunctfsf2 */
    checks += 2;
    {
        unsigned long long wd = bits_of_d((double)x);
        unsigned int wf = bits_of_f32((float)x);
        unsigned long long gd = goc_tf_to_double(a);
        unsigned int gf = goc_tf_to_float(a);
        int wnan = (wd & 0x7FF0000000000000ull) == 0x7FF0000000000000ull &&
                   (wd & 0x000FFFFFFFFFFFFFull) != 0ull;
        int gnand = (gd & 0x7FF0000000000000ull) == 0x7FF0000000000000ull &&
                    (gd & 0x000FFFFFFFFFFFFFull) != 0ull;
        if (!(wnan && gnand) && gd != wd) {
            hex128(c, a);
            fails++;
            if (fails <= 25)
                printf("FAIL f2d   %s got %016llx want %016llx\n", c, gd, wd);
        }
        {
            int wnanf = (wf & 0x7F800000u) == 0x7F800000u && (wf & 0x007FFFFFu) != 0u;
            int gnandf = (gf & 0x7F800000u) == 0x7F800000u && (gf & 0x007FFFFFu) != 0u;
            if (!(wnanf && gnandf) && gf != wf) {
                hex128(c, a);
                fails++;
                if (fails <= 25)
                    printf("FAIL f2f   %s got %08x want %08x\n", c, gf, wf);
            }
        }
    }

    /* fp128 -> integer, only where C defines the answer (in range, finite).
     * Out of range and NaN are undefined; goclib saturates and that is a
     * documented choice, not an oracle-comparable one. */
    if (!isnan_bits(a)) {
        int e = (int)((a.hi >> 48) & 0x7FFFull);
        int finite = (e != 0x7FFF);
        if (finite) {
            int ue = (e == 0) ? 1 : e;
            int exp = ue - 16383;
            if (exp >= 0 && exp <= 62) {
                long long w = (long long)x;
                long long g = goc_tf_to_ll(a);
                checks++;
                if (g != w) {
                    hex128(c, a);
                    fails++;
                    if (fails <= 25)
                        printf("FAIL to_ll %s got %lld want %lld\n", c, g, w);
                }
                if (x >= 0) {
                    unsigned long long wu = (unsigned long long)x;
                    unsigned long long gu = goc_tf_to_ull(a);
                    checks++;
                    if (gu != wu) {
                        hex128(c, a);
                        fails++;
                        if (fails <= 25)
                            printf("FAIL to_ull %s got %llu want %llu\n", c, gu, wu);
                    }
                }
            }
        }
    }
}

/* Widen a double or a float and compare with the oracle's own widening. */
static void widen_double(unsigned long long bits) {
    goc_tf128 g = goc_tf_from_double(bits);
    goc_tf128 w = f_of((__float128)d_of(bits));
    char c[64];
    checks++;
    if (isnan_bits(w)) {
        if (!isnan_bits(g)) { fails++; if (fails <= 25) printf("FAIL d2f nan %016llx\n", bits); }
        return;
    }
    if (g.hi != w.hi || g.lo != w.lo) {
        hex128(c, g);
        fails++;
        if (fails <= 25)
            printf("FAIL d2f   %016llx got %s want %016llx.%016llx\n", bits, c, w.hi, w.lo);
    }
}

static void widen_float(unsigned int bits) {
    goc_tf128 g = goc_tf_from_float(bits);
    goc_tf128 w = f_of((__float128)f32_of(bits));
    char c[64];
    checks++;
    if (isnan_bits(w)) {
        if (!isnan_bits(g)) { fails++; if (fails <= 25) printf("FAIL f2tf nan %08x\n", bits); }
        return;
    }
    if (g.hi != w.hi || g.lo != w.lo) {
        hex128(c, g);
        fails++;
        if (fails <= 25)
            printf("FAIL f2tf  %08x got %s want %016llx.%016llx\n", bits, c, w.hi, w.lo);
    }
}

static void widen_int(unsigned long long v) {
    goc_tf128 g;
    goc_tf128 w;
    char c[64];
    checks++;
    g = goc_tf_from_ull(v);
    w = f_of((__float128)v);
    if (g.hi != w.hi || g.lo != w.lo) {
        hex128(c, g);
        fails++;
        if (fails <= 25)
            printf("FAIL u2tf  %llu got %s want %016llx.%016llx\n", v, c, w.hi, w.lo);
    }
    checks++;
    g = goc_tf_from_ll((long long)v);
    w = f_of((__float128)(long long)v);
    if (g.hi != w.hi || g.lo != w.lo) {
        hex128(c, g);
        fails++;
        if (fails <= 25)
            printf("FAIL i2tf  %llu got %s want %016llx.%016llx\n", v, c, w.hi, w.lo);
    }
}

/* Values that sit exactly on a rounding boundary, or at the ends of the
 * exponent range: the places where an off-by-one in a shift hides. */
static const unsigned long long specials_hi[] = {
    0x0000000000000000ull, /* +0 */
    0x8000000000000000ull, /* -0 */
    0x3FFF000000000000ull, /* 1.0 */
    0xBFFF000000000000ull, /* -1.0 */
    0x4000000000000000ull, /* 2.0 */
    0x3FFE000000000000ull, /* 0.5 */
    0x7FFF000000000000ull, /* +inf */
    0xFFFF000000000000ull, /* -inf */
    0x7FFF800000000000ull, /* quiet NaN */
    0x7FFF400000000000ull, /* signalling NaN */
    0x0000000000000000ull, /* min subnormal (lo set below) */
    0x0000FFFFFFFFFFFFull, /* max subnormal */
    0x0001000000000000ull, /* min normal */
    0x7FFEFFFFFFFFFFFFull, /* max normal */
    0x4000000000000000ull,
    0x3F8E000000000000ull, /* 2^-113: half an ulp of 1.0 */
    0x3F8D000000000000ull, /* 2^-114: a quarter of an ulp of 1.0 */
};

int main(void) {
    int i, j;
    goc_tf128 a, b;
    long n;

    /* 1. fully random pairs -- the widest net, and it includes NaNs and infs */
    for (n = 0; n < 40000; n++) {
        binops(rnd128(), rnd128());
    }

    /* 2. random, but with the exponents pulled close together: this is what
     *    makes add/sub cancel and exercise the normalize-and-round path */
    for (n = 0; n < 40000; n++) {
        a = rnd128();
        b = a;
        b.hi = (b.hi & 0x8000000000000000ull) |
               (((b.hi & 0x7FFFull) >> 48) << 48) |
               (rnd() & 0x0000FFFFFFFFFFFFull);
        b.lo = rnd();
        /* keep the exponent within a few of a's */
        {
            int ea = (int)((a.hi >> 48) & 0x7FFFull);
            int d = (int)(rnd() % 5) - 2;
            int eb = ea + d;
            if (eb < 0) eb = 0;
            if (eb > 0x7FFE) eb = 0x7FFE;
            b.hi = (b.hi & 0x8000FFFFFFFFFFFFull) | ((unsigned long long)eb << 48);
        }
        binops(a, b);
        /* and the same magnitudes with the opposite sign: exact cancellation */
        b.hi ^= 0x8000000000000000ull;
        binops(a, b);
    }

    /* 3. everything from a double, which is where most real values come from */
    for (n = 0; n < 20000; n++) {
        unsigned long long x = rnd(), y = rnd();
        widen_double(x);
        a = goc_tf_from_double(x);
        b = goc_tf_from_double(y);
        binops(a, b);
    }

    /* 4. the specials, pairwise */
    for (i = 0; i < (int)(sizeof(specials_hi) / sizeof(specials_hi[0])); i++) {
        for (j = 0; j < (int)(sizeof(specials_hi) / sizeof(specials_hi[0])); j++) {
            int k;
            static const unsigned long long los[] = {0ull, 1ull, 0xFFFFFFFFFFFFFFFEull,
                                                     0x8000000000000000ull};
            for (k = 0; k < 4; k++) {
                a.hi = specials_hi[i];
                a.lo = los[k];
                b.hi = specials_hi[j];
                b.lo = los[(k + 1) & 3];
                if (i == 10) a.lo = 1; /* the min subnormal entry */
                if (j == 10) b.lo = 1;
                binops(a, b);
                unops(a);
            }
        }
    }

    /* 5. random values through the unary conversions and back */
    for (n = 0; n < 20000; n++) {
        unops(rnd128());
        widen_float((unsigned int)rnd());
        widen_int(rnd());
        widen_int(rnd() >> 32);
        widen_int(rnd() & 0xFFFFull);
    }

    /* 6. the rounding boundary: 1 + k*2^-114 for the k that decide a tie */
    {
        goc_tf128 one, tiny;
        one.hi = 0x3FFF000000000000ull; one.lo = 0ull;
        tiny.hi = 0x3F8D000000000000ull; tiny.lo = 0ull; /* 2^-114 */
        for (i = 0; i < 8; i++) {
            goc_tf128 k = goc_tf_from_ll((long long)i);
            binops(goc_tf_mul(k, tiny), one);
        }
    }

    printf("%s checks=%ld fails=%ld\n", fails ? "BAD" : "OK", checks, fails);
    return fails ? 1 : 0;
}
