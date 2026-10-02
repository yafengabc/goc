/* C23 feature: _BitInt(N) edge cases fixed in the 2026-10 P0 _BitInt repair
 * Clause:     C23 6.3.1.3 / 6.5.8 (conversions & relational ops) -- the three
 *             defects fixed: (a) compare with a bare literal no longer panics
 *             (nil type was dereferenced in the big cmp path); (b) non-zero
 *             file-scope / static _BitInt initialisers are no longer silently
 *             dropped into .bss (bigInitWords now unwraps cast chains and
 *             applies mod-2^N wrap + sign extension); (c) same-width big->big
 *             casts (e.g. (U128)(S128)x) now actually copy the value (the
 *             copyBytes call was missing, so nested casts yielded 0).
 * Strategy:   bare-literal comparisons on S8/U64/U128; global + function-static
 *             non-zero initialisers with wrap and nested cast; global
 *             arithmetic; nested same-width cast in decl/assign/return;
 *             narrowing 6-arg conv (stack-arg channel). All printed values
 *             verified against gcc -std=c2x.
 * Status:     PASS (goc == gcc)
 * EXPECT: PASS
 */
#include <stdio.h>

typedef   signed   _BitInt(8)    S8;
typedef unsigned _BitInt(8)    U8;
typedef   signed   _BitInt(64)   S64;
typedef unsigned _BitInt(64)   U64;
typedef   signed   _BitInt(128)  S128;
typedef unsigned _BitInt(128)  U128;

/* file-scope non-zero initialisers: 200 wraps to -56, 300 wraps to 44,
 * -12345 sign-extends into 128 bits. */
S8   g_s8   = 200;
U8   g_u8   = 300;
S128 g_s128 = -12345;
U128 g_u128 = (U128)(S128)-1;

static void pu128(U128 v) {
    char buf[45]; int i = 0; U128 ten = (U128)10;
    if (v == (U128)0) { printf("0"); return; }
    while (v > (U128)0) { buf[i++] = (char)('0' + (int)(v % ten)); v = v / ten; }
    while (i > 0) putchar(buf[--i]);
}

static U128 id128(U128 v) { return v; }

int main(void) {
    int passed = 0, total = 0;
    int ok;

    /* case1: bare-literal comparisons on S8 (previously a compiler panic) */
    {
        S8 a = 200;                     /* -56 */
        ok = (a == -56) && (a != 0) && (a < 100) && (a >= -100)
          && (a == 200 - 256)           /* 200-256 == -56, literal arithmetic */
          && !(a == 44);
        printf("case1: litcmp a=%lld ok=%d\n", (long long)a, ok);
        passed += ok; total++;
    }

    /* case2: bare-literal comparisons on U64 with an unsigned literal */
    {
        U64 u = 18446744073709551615ULL;  /* all ones */
        ok = (u == 18446744073709551615ULL) && (u != 0ULL) && (u > 1ULL)
          && !(u < 18446744073709551615ULL);
        printf("case2: litcmp u=%llu ok=%d\n", (unsigned long long)u, ok);
        passed += ok; total++;
    }

    /* case3: file-scope non-zero initialisers (wrap + sign extension) */
    {
        ok = (g_s8 == -56) && (g_u8 == 44) && ((long long)g_s128 == -12345);
        printf("case3: global init g_s8=%lld g_u8=%llu g_s128=%lld ok=%d\n",
               (long long)g_s8, (unsigned long long)g_u8,
               (long long)g_s128, ok);
        passed += ok; total++;
    }

    /* case4: global arithmetic on wrapped globals */
    {
        S8  r1 = g_s8 + 1;              /* -55 */
        U8  r2 = g_u8 * 2;              /* 88 */
        S64 r3 = (S64)g_s128 + 1;       /* -12344 */
        ok = (r1 == -55) && (r2 == 88) && (r3 == -12344);
        printf("case4: global arith r1=%lld r2=%llu r3=%lld ok=%d\n",
               (long long)r1, (unsigned long long)r2, (long long)r3, ok);
        passed += ok; total++;
    }

    /* case5: function-static non-zero initialisers, incl. nested cast */
    {
        static S8   s_s8   = 200;        /* -56 */
        static U128 s_u128 = (U128)(S128)-1;   /* 2^128-1 */
        ok = (s_s8 == -56) && (s_u128 == (U128)0 - (U128)1);
        printf("case5: static init s_s8=%lld ok=%d\n", (long long)s_s8, ok);
        passed += ok; total++;
    }

    /* case6: nested same-width cast in decl / assign / return */
    {
        S64 x = -12345;
        U128 w1 = (U128)(S128)x;          /* 2^128 - 12345 */
        U128 w2; S128 tmp = (S128)x; w2 = (U128)tmp;
        U128 w3 = id128((U128)(S128)x);
        ok = (w1 == w2) && (w2 == w3)
          && (w1 == (U128)0 - (U128)12345);
        printf("case6: nested cast ok=%d w1=", ok); pu128(w1);
        printf(" w2="); pu128(w2);
        printf(" w3="); pu128(w3); printf("\n");
        passed += ok; total++;
    }

    /* case7: narrowing 6-arg conv (stack-arg channel), incl. global source */
    {
        S128 big = (S128)700;
        S8   small = (S8)big;            /* 700 mod 256 = 188 -> -68 */
        U8   u8b = (U8)(U128)300;        /* 44 */
        U8   ug = (U8)g_s128;            /* -12345 mod 256 = 199 */
        ok = (small == -68) && (u8b == 44) && (ug == 199);
        printf("case7: narrow conv small=%lld u8b=%llu ug=%llu ok=%d\n",
               (long long)small, (unsigned long long)u8b,
               (unsigned long long)ug, ok);
        passed += ok; total++;
    }

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
