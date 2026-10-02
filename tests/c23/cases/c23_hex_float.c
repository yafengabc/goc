/* C23 feature: hexadecimal floating literals (0x1.8p3)
 * Clause:     C99 6.4.4.2 (binary exponent p); C23 makes the exponent optional
 * Strategy:   values verified against hand computation; float suffixes f/l; value
 *             comparisons against double literals.
 *             NOTE: goc printf does NOT implement the %a conversion (it prints
 *             'a' literally), so %a round-trip is not used here; see group_D1.md.
 *             NOTE: the C23 exponent-less form '0x1.8' is REJECTED by this very
 *             gcc (-std=c2x: "hexadecimal floating constants require an
 *             exponent"), so it is intentionally omitted; goc's stance is recorded
 *             separately in group_D1.md.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

static int near(double got, double want) {
    return (got > want - 1e-9 && got < want + 1e-9);
}

int main(void) {
    int passed = 0, total = 0;

    ++total;
    double a = 0x1.8p3;      /* 1.5 * 8 = 12 */
    printf("case%d: 0x1.8p3=%.3f ok=%d\n", total, a, near(a, 12.0));
    if (near(a, 12.0)) passed++;

    ++total;
    double b = 0x1p-2;       /* 1 * 2^-2 = 0.25 */
    printf("case%d: 0x1p-2=%.3f ok=%d\n", total, b, near(b, 0.25));
    if (near(b, 0.25)) passed++;

    ++total;
    double c = 0x.8p1;       /* 0.5 * 2 = 1.0 */
    printf("case%d: 0x.8p1=%.3f ok=%d\n", total, c, near(c, 1.0));
    if (near(c, 1.0)) passed++;

    ++total;
    float e = 0x1.4p2f;      /* 1.25 * 4 = 5, float suffix */
    printf("case%d: 0x1.4p2f=%.3f ok=%d\n", total, (double)e, near((double)e, 5.0));
    if (near((double)e, 5.0)) passed++;

    ++total;
    double g = 0x10p0;       /* 16 * 2^0 = 16 */
    printf("case%d: 0x10p0=%.3f ok=%d\n", total, g, near(g, 16.0));
    if (near(g, 16.0)) passed++;

    ++total;
    double h = 0x1p4;        /* 1 * 16 = 16 */
    printf("case%d: 0x1p4=%.3f ok=%d\n", total, h, near(h, 16.0));
    if (near(h, 16.0)) passed++;

    ++total;
    double s = 0x1.8p3 + 0x1p0;   /* arithmetic: 12 + 1 = 13 */
    printf("case%d: 0x1.8p3+0x1p0=%.3f ok=%d\n", total, s, near(s, 13.0));
    if (near(s, 13.0)) passed++;

    ++total;
    long double q = 0x1.8p3L;     /* L suffix (long double) */
    printf("case%d: 0x1.8p3L=%.3f ok=%d\n", total, (double)q, near((double)q, 12.0));
    if (near((double)q, 12.0)) passed++;

    ++total;
    int eq = (0x1.8p3 == 12.0) ? 1 : 0;
    printf("case%d: 0x1.8p3==12.0 -> %d\n", total, eq);
    if (eq == 1) passed++;

    ++total;
    /* hex integers are unaffected: e is a hex digit, p is not */
    printf("case%d: 0x1e5=%d 0xFACE=%d\n", total, 0x1e5, 0xFACE);
    if (0x1e5 == 485 && 0xFACE == 64206) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
