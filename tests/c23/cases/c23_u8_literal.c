/* C23 feature: u8 string / character literal VALUE semantics
 * Clause:     C23 6.4.4.4 (character constants), 6.4.5 (string literals)
 * Strategy:   sizeof (incl. NUL), byte content, adjacent literal concatenation,
 *             and u8'x' value. Type identity (char8_t vs unsigned char via
 *             _Generic) is owned by group A1, not tested here.
 *             NOTE: u8R"()" raw string and multi-byte u8'<non-ascii>' are
 *             REJECTED by gcc -std=c2x on this toolchain, so they are omitted;
 *             see group_D1.md.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

int main(void) {
    int passed = 0, total = 0;

    ++total;
    unsigned long s1 = (unsigned long)sizeof(u8"abc");   /* 3 chars + NUL */
    printf("case%d: sizeof(u8\"abc\")=%lu\n", total, s1);
    if (s1 == 4UL) passed++;

    ++total;
    unsigned long s2 = (unsigned long)sizeof(u8"");      /* NUL only */
    printf("case%d: sizeof(u8\"\")=%lu\n", total, s2);
    if (s2 == 1UL) passed++;

    ++total;
    const unsigned char *p = (const unsigned char *)u8"abc";
    printf("case%d: bytes=%d %d %d\n", total, (int)p[0], (int)p[1], (int)p[2]);
    if (p[0] == 'a' && p[1] == 'b' && p[2] == 'c') passed++;

    ++total;
    /* adjacent u8 + u8 string concatenation */
    unsigned long s4 = (unsigned long)sizeof(u8"ab" "cd");
    const unsigned char *q = (const unsigned char *)(u8"ab" "cd");
    printf("case%d: concat len=%lu bytes=%d%d%d%d\n", total, s4,
           (int)q[0], (int)q[1], (int)q[2], (int)q[3]);
    if (s4 == 5UL && q[0] == 'a' && q[1] == 'b' && q[2] == 'c' && q[3] == 'd') passed++;

    ++total;
    /* mixed u8-prefixed + unprefixed adjacent string concatenation */
    unsigned long s5 = (unsigned long)sizeof(u8"a" "b");
    printf("case%d: mixed concat len=%lu\n", total, s5);
    if (s5 == 3UL) passed++;

    ++total;
    int c1 = (int)u8'x';
    printf("case%d: u8'x'=%d\n", total, c1);
    if (c1 == 'x') passed++;

    ++total;
    int c2 = (int)u8'A';
    printf("case%d: u8'A'=%d\n", total, c2);
    if (c2 == 'A') passed++;

    ++total;
    /* trailing NUL present after a u8 string */
    const unsigned char *r = (const unsigned char *)u8"Z";
    printf("case%d: NUL=%d\n", total, (int)r[1]);
    if (r[1] == 0) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
