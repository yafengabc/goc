/* C23 feature: binary integer literals 0b / 0B
 * Clause:     C23 6.4.4.1 (binary constants are new in C23)
 * Strategy:   verify values against decimal/hex, integer suffixes, mixed-base
 *             arithmetic, digit separators, and use inside a preprocessor #if.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int v1 = 0b1010;
    printf("case%d: 0b1010=%d\n", total, v1);
    if (v1 == 10) passed++;

    ++total;
    int v2 = 0B1111;
    printf("case%d: 0B1111=%d\n", total, v2);
    if (v2 == 15) passed++;

    ++total;
    int v3 = 0b0;
    printf("case%d: 0b0=%d\n", total, v3);
    if (v3 == 0) passed++;

    ++total;
    int v4 = 0b0101;   /* leading zero digits after the b are allowed */
    printf("case%d: 0b0101=%d\n", total, v4);
    if (v4 == 5) passed++;

    ++total;
    int v5 = 0b11111111;
    printf("case%d: 0b11111111=%d (hex 0xFF=%d)\n", total, v5, 0xFF);
    if (v5 == 0xFF) passed++;

    ++total;
    int v6 = 0b1010 + 0b0110;   /* mixed arithmetic */
    printf("case%d: 0b1010+0b0110=%d\n", total, v6);
    if (v6 == 16) passed++;

    ++total;
    unsigned long v7 = 0b11111111111111111111111111111111UL;
    printf("case%d: 32ones=%lu\n", total, v7);
    if (v7 == 4294967295UL) passed++;

    ++total;
    unsigned int v8 = 0b1010u;
    printf("case%d: 0b1010u=%u\n", total, v8);
    if (v8 == 10u) passed++;

    ++total;
    unsigned long v9 = 0b1010UL;
    printf("case%d: 0b1010UL=%lu\n", total, v9);
    if (v9 == 10UL) passed++;

    ++total;
    int v10 = 0b1010'1010;   /* binary literal with digit separator */
    printf("case%d: 0b1010'1010=%d\n", total, v10);
    if (v10 == 170) passed++;

    ++total;
#if 0b1100 == 12
    printf("case%d: #if 0b1100==12 ok\n", total);
    passed++;
#else
    printf("case%d: #if 0b1100 MISMATCH\n", total);
#endif

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
