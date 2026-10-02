/* C23 feature: bit-fields
 * Clause:     C23 6.7.2.1 (structure and union specifiers, bit-fields)
 * Strategy:   widths 1..3, signed vs unsigned vs plain-int (plain-int sign
 *             is implementation-defined: gcc defaults to signed, recorded by
 *             comparing 7 in a 3-bit field), zero-width forcing new allocation,
 *             unnamed bit-field, sizeof layout, and volatile bit-field.
 * Status:     PENDING
 * EXPECT: PASS
 *
 * goc gap found during testing (NOT compiled into this file; recorded in
 * group_D2.md):
 *   - _BitInt(N) bit-field -> goc parse error:
 *       "bit-field base type must be an integer type, got _BitInt(7)"
 *     gcc -std=c2x accepts it (e.g. _BitInt(7) x:5 reads -1, unsigned
 *     _BitInt(7) y:4 reads 15, struct size 2).
 */
#include <stdio.h>

struct Widths {
    unsigned int a : 1;
    unsigned int b : 2;
    unsigned int c : 3;
};
struct Sign {
    signed int s : 3;
    unsigned int u : 3;
};
struct Plain {
    int f : 3;
};
struct ZeroW {
    unsigned int a : 1;
    unsigned int : 0;
    unsigned int b : 1;
};
struct Unnamed {
    unsigned int a : 2;
    unsigned int : 3;
    unsigned int b : 2;
};
struct Vol {
    volatile int v : 3;
};

int main(void) {
    int passed = 0, total = 0;

    ++total;
    struct Widths w; w.a = 1; w.b = 3; w.c = 7;
    printf("case%d: widths 1/2/3 -> %u %u %u\n", total, w.a, w.b, w.c);
    if (w.a == 1 && w.b == 3 && w.c == 7) passed++;

    ++total;
    struct Sign sg; sg.s = -1; sg.u = 7;
    printf("case%d: signed vs unsigned -> %d %u\n", total, sg.s, sg.u);
    if (sg.s == -1 && sg.u == 7) passed++;

    ++total;
    /* plain int bit-field: gcc defaults to signed; assigning 7 to a 3-bit
     * signed field yields -1. goc's own sign-ness is recorded by the comparison. */
    struct Plain pb; pb.f = 7;
    printf("case%d: plain int bf(7) -> %d\n", total, pb.f);
    if (pb.f == -1) passed++;

    ++total;
    struct ZeroW zw; zw.a = 1; zw.b = 1;
    printf("case%d: zero-width layout -> %u %u sizeof=%d\n", total, zw.a, zw.b, (int)sizeof zw);
    if (zw.a == 1 && zw.b == 1) passed++;

    ++total;
    struct Unnamed un; un.a = 3; un.b = 3;
    printf("case%d: unnamed field -> %u %u sizeof=%d\n", total, un.a, un.b, (int)sizeof un);
    if (un.a == 3 && un.b == 3) passed++;

    ++total;
    struct Vol vv; vv.v = -4;
    printf("case%d: volatile bf -> %d\n", total, vv.v);
    if (vv.v == -4) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
