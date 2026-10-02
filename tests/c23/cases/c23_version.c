/* C23 feature: predefined macros __STDC_VERSION__ / __STDC__ / __STDC_HOSTED__
 * Clause:     C23 6.11 "Predefined macro names"; __STDC_VERSION__ shall be 202311L
 * Strategy:   print the macro values; verify __STDC_VERSION__ == 202311L and the
 *             presence/values of __STDC__ and __STDC_HOSTED__.
 * Status:     PASS (verified 2026-10-02; P1.7 fixed - __STDC_VERSION__=202311,
 *             __STDC__=1, __STDC_HOSTED__=1, cross-checked with gcc -std=c2x)
 * EXPECT: PASS
 */
#include <stdio.h>

int main(void) {
    int passed = 0, total = 0;

    ++total;
    long v = __STDC_VERSION__;
    printf("case%d: __STDC_VERSION__ = %ld\n", total, v);
    if (v == 202311L) passed++;

    ++total;
    printf("case%d: __STDC__ = %d\n", total, (int)__STDC__);
    if (__STDC__ == 1) passed++;

    ++total;
#ifdef __STDC_HOSTED__
    printf("case%d: __STDC_HOSTED__ = %d\n", total, (int)__STDC_HOSTED__);
    if (__STDC_HOSTED__ == 1) passed++;
#else
    printf("case%d: __STDC_HOSTED__ undefined\n", total);
#endif

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
