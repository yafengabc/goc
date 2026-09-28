/* fmtlu.c -- printf unsigned/long format coverage.
 *
 * Locks the vfmt length-modifier and unsigned-specifier behaviour:
 *   %u        unsigned int
 *   %lu/%llu  length modifiers are skipped, the 8-byte va slot is read as
 *             unsigned (div, not idiv) so values with the high bit set
 *             print correctly
 *   %ld       long, still signed
 *   %x        mixing hex with unsigned in one call (va cursor alignment)
 */

#include <stdio.h>

int main(void) {
    unsigned long big = 0x77777777;   /* 2004318071 */
    long neg = -7;
    unsigned int ui = 4000000000u;    /* high bit set: must print unsigned */
    unsigned long long ull = 0x7777777777777777;

    printf("lu=%lu u=%u\n", big, 42);
    printf("ld=%ld d=%d\n", neg, 42);
    printf("ui=%u\n", ui);
    printf("llu=%llu\n", ull);
    printf("x=%x lu=%lu end\n", 0x2a, 123456);
    return 0;
}
