/* C23 feature: attributes [[deprecated]] and [[deprecated("message")]]
 * Clause:     C23 6.7.13 (deprecated attribute)
 * Strategy:   deprecate functions (declaration/definition split, attribute on the
 *             declaration) and a variable. Using a deprecated entity must warn;
 *             the message form must carry its text. Warnings are recorded verbatim
 *             in the status document. The program always compiles and runs.
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>

/* attribute on the declaration; definition below carries no attribute */
[[deprecated]] int old_api(void);
[[deprecated("use new_api instead")]] int old_api2(void);

int old_api(void) { return 1; }
int old_api2(void) { return 2; }

[[deprecated]] int leg_var = 100;

int main(void) {
    int passed = 0, total = 0;

    /* case1: deprecated function, attribute on the declaration only */
    ++total;
    int a = old_api();
    printf("case1: old_api()=%d (deprecated warning expected)\n", a);
    if (a == 1) passed++;

    /* case2: deprecated function with message text */
    ++total;
    int b = old_api2();
    printf("case2: old_api2()=%d (deprecated warning + message expected)\n", b);
    if (b == 2) passed++;

    /* case3: deprecated file-scope variable */
    ++total;
    int c = leg_var;
    printf("case3: leg_var=%d (deprecated warning expected)\n", c);
    if (c == 100) passed++;

    /* case4: take address of a deprecated function (another use site) */
    ++total;
    int (*fp)(void) = &old_api;
    printf("case4: fp(&old_api) nonzero=%d\n", (int)(fp != 0));
    if (fp != 0) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
