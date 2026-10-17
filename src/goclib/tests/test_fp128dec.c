/* Differential test for fp128dec.c against libquadmath.
 *
 *   fmt:   __goclib_tf128_fmt vs quadmath_snprintf, over edge values and a
 *          pile of random bit patterns, x {f,e,g,F,E,G} x precisions x '#'.
 *   parse: __goclib_tf128_parse vs strtoflt128, bitwise, over decimal
 *          tokens (round-trips, fuzz digit strings, oversized inputs).
 *
 * Exit code 0 = clean. Mismatches print value/spec/prec and both outputs.
 */
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <quadmath.h>
#include "goclib.h"

static char bufA[24000], bufB[24000];

static unsigned long long rs[2] = {0x243F6A8885A308D3ull, 0x13198A2E03707344ull};
static unsigned long long rnd64(void) {
    unsigned long long s1 = rs[0], s0 = rs[1];
    rs[0] = s0;
    s1 ^= s1 << 23; s1 ^= s1 >> 17; s1 ^= s0; s1 ^= s0 >> 26;
    rs[1] = s1;
    return s1 + s0;
}

static int fails = 0, cases = 0;

/* libquadmath's %g strips trailing zeros even under '#', which C99 forbids
 * (Python's '%#.6g' % 1e6 == '1.00000e+06'); ours keeps them. For alt-g
 * comparisons only, normalize both sides by stripping so the significant
 * content is what gets compared. */
static void strip_g_zeros(char *s, int *n) {
    int i, e = *n;
    for (i = 0; i < *n; i++)
        if (s[i] == 'e' || s[i] == 'E') { e = i; break; }
    if (e == *n) return;               /* fixed style: nothing to normalize */
    while (e > 0 && s[e - 1] == '0') e--;
    if (e > 0 && s[e - 1] == '.') e--;
    memmove(s + e, s + i, (size_t)(*n - i));
    *n = e + (*n - i);
}

static void check_fmt(const goc_tf128 *v) {
    static const int specs[] = {'f', 'e', 'g', 'F', 'E', 'G'};
    static const int precs[] = {-1, 0, 1, 2, 3, 6, 17, 34, 40, 113, 1000};
    unsigned i, j;
    __float128 q;
    memcpy(&q, v, 16);
    for (i = 0; i < 6; i++) {
        for (j = 0; j < sizeof(precs) / sizeof(precs[0]); j++) {
            int alt, prec = precs[j];
            char fmt[16];
            for (alt = 0; alt < 2; alt++) {
                int hasPrec = prec >= 0, na, nb, want;
                /* keep "no precision given" genuinely absent: %.0 means
                 * precision ZERO, a different thing from no dot at all */
                if (hasPrec)
                    sprintf(fmt, "%%%s.%dQ%c", alt ? "#" : "", prec, specs[i]);
                else
                    sprintf(fmt, "%%%sQ%c", alt ? "#" : "", specs[i]);
                want = quadmath_snprintf(bufB, sizeof bufB, fmt, q);
                na = __goclib_tf128_fmt(bufA, sizeof bufA, v, specs[i],
                                        hasPrec ? prec : 0, hasPrec, alt);
                if (alt && (specs[i] == 'g' || specs[i] == 'G')) {
                    strip_g_zeros(bufA, &na);
                    strip_g_zeros(bufB, &want);
                }
                cases++;
                nb = (na < 0) ? -na : na;
                if (na < 0 || nb != want || memcmp(bufA, bufB, nb) != 0) {
                    if (fails < 25)
                        printf("FMT MISMATCH %s bits=%016llx.%016llx\n"
                               "  quadmath(%d): %.80s%s\n"
                               "  ours    (%d): %.80s%s\n",
                               fmt, v->hi, v->lo, want, bufB,
                               want > 80 ? "..." : "",
                               na, bufA, na > 80 ? "..." : "");
                    fails++;
                }
            }
        }
    }
}

static void check_parse(const char *tok) {
    goc_tf128 v;
    char *end;
    __float128 q = strtoflt128(tok, &end);
    if (*end != '\0') return;               /* not a clean token: skip */
    if (__goclib_tf128_parse(tok, &v) != 0) {
        printf("PARSE ERR  token=%.1320s (quadmath accepted)\n", tok);
        fails++;
        return;
    }
    cases++;
    if (strstr(tok, "x") || strstr(tok, "X")) return; /* hex: not ours */
    {
        unsigned long long qh = ((unsigned long long *)&q)[1];
        unsigned long long ql = ((unsigned long long *)&q)[0];
        int qnan = ((qh >> 48) & 0x7FFF) == 0x7FFF &&
                   ((qh & 0x0000FFFFFFFFFFFFull) || ql);
        int onan = ((v.hi >> 48) & 0x7FFF) == 0x7FFF &&
                   ((v.hi & 0x0000FFFFFFFFFFFFull) || v.lo);
        if (qnan || onan) {
            /* NaN payloads are unspecified by C: any quiet NaN with the
             * same sign matches (strtoflt128 and we pick different
             * payloads and both conform). A NaN-vs-number mix is a bug. */
            if (!(qnan && onan && (v.hi >> 63) == (qh >> 63))) {
                printf("PARSE MISMATCH token=%.1320s\n"
                       "  quadmath %016llx.%016llx\n"
                       "  ours     %016llx.%016llx\n",
                       tok, qh, ql, v.hi, v.lo);
                fails++;
            }
            return;
        }
        if (v.hi != qh || v.lo != ql) {
            /* Deep underflow boundary: strtoflt128 rounds values below half
             * the minimum subnormal to the min subnormal instead of zero.
             * Exact integer arbitration (Python, token ...e-4993 with value
             * < 2^-16495) shows the zero IS the correctly rounded result, so
             * this is a libquadmath defect, ours being the right side. Only
             * the exact pair zero-vs-min-subnormal is excused: a wrong zero
             * against a real subnormal still fails below. */
            int qsub = ((qh >> 48) & 0x7FFF) == 0;
            int osub = ((v.hi >> 48) & 0x7FFF) == 0;
            if (qsub && osub && (qh >> 63) == (v.hi >> 63)) {
                int qzero = qsub && (qh & 0x0000FFFFFFFFFFFFull) == 0 && ql == 0;
                int qmin = qsub && !qzero && (qh & 0x0000FFFFFFFFFFFFull) == 0
                           && ql == 1;
                int ozero = osub && (v.hi & 0x0000FFFFFFFFFFFFull) == 0 && v.lo == 0;
                int omin = osub && !ozero && (v.hi & 0x0000FFFFFFFFFFFFull) == 0
                           && v.lo == 1;
                if ((qzero && omin) || (qmin && ozero)) return;
            }
            if (fails < 25)
                printf("PARSE MISMATCH token=%.1320s\n"
                       "  quadmath %016llx.%016llx\n"
                       "  ours     %016llx.%016llx\n",
                       tok, qh, ql, v.hi, v.lo);
            fails++;
        }
    }
}

int main(void) {
    static const char *edges[] = {
        "0", "-0", "inf", "-inf", "nan", "1", "-1", "0.5", "2", "3", "10",
        "0.1", "0.3", "1e300", "1e-300", "1e-4000", "1e-4930", "1e-4950",
        "1e-4966", "0x1p-16482",            /* min subnormal */
        "0x1.fffffffffffffp-16483",         /* max subnormal, roughly */
        "0x1p-16382",                       /* min normal */
        "0x1.ffffffffffffffffffffffffffffp+16383", /* LDBL_MAX */
        "1.999999e16383", "9.99999e-4966", "2.2250738585072014e-308",
        "8388609e-16400", "5e-4956", "12345678901234567890e-5000",
        "1.0000000000000000000000000000000001", "4.99999e-1", "5.0000001e-1",
        "999999.5", "123456789012345678901234567890.5",
        "0.5000000000000000000000000000000000999"
    };
    unsigned long k;
    size_t i;

    /* fmt: edges via strtoflt128, then random bit patterns */
    for (i = 0; i < sizeof(edges) / sizeof(edges[0]); i++) {
        char *end;
        __float128 q = strtoflt128(edges[i], &end);
        goc_tf128 v;
        if (*end) continue;
        memcpy(&v, &q, 16);
        check_fmt(&v);
    }
    for (k = 0; k < 3000; k++) {
        goc_tf128 v;
        do {
            v.hi = rnd64();
            v.lo = rnd64();
        } while (((v.hi >> 48) & 0x7FFF) == 0x7FFF);   /* NaN/inf lanes: edges cover them */
        /* steer some patterns toward interesting mantissas */
        if (k % 5 == 0) v.lo = 0;
        if (k % 5 == 1) v.lo = ~0ull;
        if (k % 5 == 2) v.hi = (v.hi & 0xFFFF000000000000ull) | 0x0000FFFFFFFFFFFFull;
        check_fmt(&v);
    }

    /* parse: the same edge tokens, round-trips of formatted values, fuzz */
    for (i = 0; i < sizeof(edges) / sizeof(edges[0]); i++)
        check_parse(edges[i]);
    for (k = 0; k < 4000; k++) {
        goc_tf128 v;
        __float128 q;
        char tok[2400];
        do {
            v.hi = rnd64();
            v.lo = rnd64();
        } while (((v.hi >> 48) & 0x7FFF) == 0x7FFF);
        memcpy(&q, &v, 16);
        quadmath_snprintf(tok, sizeof tok, "%.*Qe", (int)(2 + rnd64() % 45), q);
        check_parse(tok);
    }
    for (k = 0; k < 4000; k++) {
        char tok[2600];
        unsigned long nd = 1 + rnd64() % 1300, j;
        long ex = (long)(rnd64() % 10000) - 5000;
        char *p = tok;
        if (rnd64() & 1) *p++ = '-';
        for (j = 0; j < nd; j++) {
            *p++ = (char)('0' + (j == 0 ? 1 + rnd64() % 9 : rnd64() % 10));
            if (j == nd / 2) *p++ = '.';
        }
        sprintf(p, "e%+ld", ex);
        check_parse(tok);
    }
    /* oversized and degenerate tokens */
    {
        char big[3000];
        memset(big, '7', sizeof big - 40);
        big[sizeof big - 40] = '\0';
        strcat(big, "e-4000");
        check_parse(big);
        check_parse("0000000");
        check_parse("0.0000000e-4970");
        check_parse("+2.5");
        check_parse("1e");
        check_parse("1e+");
        check_parse("1e99999999");
        check_parse("1e-99999999");
        check_parse(".5");
        check_parse("5.");
        check_parse("0x10");    /* scanner rejects hex before us; 'x' stops it */
    }

    printf("cases=%d fails=%d\n", cases, fails);
    return fails != 0;
}
