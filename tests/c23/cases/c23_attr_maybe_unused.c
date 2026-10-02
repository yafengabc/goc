/* C23 feature: attribute [[maybe_unused]] (variable and static-function)
 * Clause:     C23 6.7.13 (maybe_unused attribute)
 * Strategy:   apply [[maybe_unused]] to a local variable and a static function,
 *             then compare against the bare (unattributed) baseline under gcc
 *             -Wall -Wextra, which warns -Wunused-variable / -Wunused-function.
 *             Attribute-on-parameter is intentionally NOT here: goc rejects it
 *             (see c23_attr_positions.c findings). Whether goc emits any unused
 *             diagnostic at all is recorded verbatim.
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>

[[maybe_unused]] static int mu_func(void) { return 1; }
static int bare_func(void) { return 2; }

int main(void) {
    int passed = 0, total = 0;

    /* case1: maybe_unused local variable, never read -> no warning expected */
    ++total;
    [[maybe_unused]] int mu = 99;
    printf("case1: maybe_unused local var (no warning expected)\n");
    passed++;

    /* case2: bare unused local variable -> gcc warns; goc behavior recorded */
    ++total;
    int bare = 42;
    printf("case2: bare unused local var (gcc -Wunused-variable; record goc)\n");
    passed++;

    /* case3: static maybe_unused function, never called -> no warning expected */
    ++total;
    printf("case3: static maybe_unused func unused (no warning expected)\n");
    passed++;

    /* case4: static bare function never called -> gcc -Wunused-function; record goc */
    ++total;
    printf("case4: static bare func unused (gcc -Wunused-function; record goc)\n");
    passed++;

    /* case5: maybe_unused local variable that IS used (control) */
    ++total;
    [[maybe_unused]] int used = 7;
    used = used + 1;
    printf("case5: used maybe_unused var=%d\n", used);
    if (used == 8) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
