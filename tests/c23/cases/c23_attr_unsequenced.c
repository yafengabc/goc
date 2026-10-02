/* C23 feature: function attributes [[unsequenced]] and [[reproducible]]
 * Clause:     C23 6.7.13 (unsequenced / reproducible behavior, C23 function attrs)
 * Strategy:   declare and call functions carrying [[unsequenced]] / [[reproducible]],
 *             and stack them with [[nodiscard]]. These are compile-time optimization
 *             hints with no observable run-time effect, so the file only verifies
 *             that both compilers accept the syntax and the functions still compute
 *             normally. Note gcc 16 prefers the attribute after the parameter list
 *             and warns on the prefix placement used here; that warning is recorded.
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>

[[unsequenced]] int uadd(int a, int b) { return a + b; }
[[reproducible]] int radd(int a, int b) { return a + b; }

/* stacked with [[nodiscard]] */
[[unsequenced]] [[nodiscard]] int mixed(int a) { return a * a; }

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int u = uadd(2, 3);
    printf("case1: uadd(2,3)=%d\n", u);
    if (u == 5) passed++;

    ++total;
    int r = radd(4, 5);
    printf("case2: radd(4,5)=%d\n", r);
    if (r == 9) passed++;

    ++total;
    int m = mixed(6);
    printf("case3: mixed(6)=%d (stacked unsequenced+nodiscard)\n", m);
    if (m == 36) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
