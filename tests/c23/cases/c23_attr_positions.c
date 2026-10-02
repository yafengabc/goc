/* C23 feature: attribute placement positions
 * Clause:     C23 6.7.13 (permissible positions for an attribute)
 * Strategy:   cover the positions goc accepts: attribute at the start of a
 *             function declarator, on a function definition, on a variable, two
 *             stacked attributes, and an attribute placed BEFORE the storage-class
 *             specifier. Positions goc REJECTS (attribute on a parameter, on a
 *             struct/enum/typedef type, and an attribute AFTER a storage class)
 *             are documented from direct probes in the status file and are not in
 *             this compiling file.
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>

/* case1: attribute at the start of the function declarator */
[[nodiscard]] int f_start(void) { return 1; }

/* case2: attribute on the definition (the prior declaration carries none) */
int f_def_impl(void);
[[nodiscard]] int f_def_impl(void) { return 2; }

/* case3: attribute on a variable */
[[maybe_unused]] int gv_var = 3;

/* case4: two attributes stacked on one function */
[[deprecated]] [[nodiscard]] int f_multi(void) { return 4; }

/* case5: attribute BEFORE the storage-class specifier */
[[maybe_unused]] static int gv_static = 5;

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int a = f_start();
    printf("case1: f_start=%d\n", a);
    if (a == 1) passed++;

    ++total;
    int b = f_def_impl();
    printf("case2: f_def_impl=%d\n", b);
    if (b == 2) passed++;

    ++total;
    printf("case3: gv_var=%d\n", gv_var);
    if (gv_var == 3) passed++;

    ++total;
    int d = f_multi();
    printf("case4: f_multi=%d\n", d);
    if (d == 4) passed++;

    ++total;
    printf("case5: gv_static=%d\n", gv_static);
    if (gv_static == 5) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
