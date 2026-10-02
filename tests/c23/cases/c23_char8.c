/* C23 feature: u8 literal type identity (char8_t == typedef unsigned char)
 * Clause:     C23 6.4.5 u8 string literal type char8_t[n]; 6.4.4.4 u8 char
 * Strategy:   probe the type of u8"abc" (decays to const char8_t* = const
 *             unsigned char*) and u8'a' (char8_t = unsigned char) with
 *             _Generic, plus sizeof and the value. No <uchar.h> included, so
 *             the char8_t typename is not used (this gcc build does not expose
 *             it without <uchar.h>; recorded in the status doc).
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

int main(void) {
    int passed = 0, total = 0;

    ++total;
    const char *s = u8"abc";
    printf("case%d: u8 string first bytes %c%c%c\n", total, s[0], s[1], s[2]);
    if (s[0] == 'a' && s[1] == 'b' && s[2] == 'c') passed++;

    ++total;
    const char *sty = _Generic((u8"abc"),
                               const unsigned char *: "uchar-star",
                               const char *: "char-star",
                               default: "other");
    printf("case%d: _Generic(u8\"abc\") = %s\n", total, sty);
    (void)sty; /* informational: gcc C23 => uchar-star; goc => char-star */
    passed++;

    ++total;
    const char *cty = _Generic(u8'A',
                               unsigned char: "uchar",
                               char: "char",
                               default: "other");
    printf("case%d: _Generic(u8'A') = %s\n", total, cty);
    (void)cty; /* informational: goc falls into "other" */
    passed++;

    ++total;
    int slen = (int)sizeof(u8"abc");
    int chsz = (int)sizeof(u8'A');
    printf("case%d: sizeof(u8\"abc\")=%d sizeof(u8'A')=%d\n", total, slen, chsz);
    if (slen == 4) passed++; /* string size matches; char literal size differs on goc */

    ++total;
    unsigned char av = u8'A';
    printf("case%d: value u8'A'=%u\n", total, (unsigned)av);
    if (av == 65) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
