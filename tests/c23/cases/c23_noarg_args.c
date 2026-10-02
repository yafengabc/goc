/* C23 feature: calling an unprototyped f() with extra arguments must be rejected
 * Clause:     C23 6.5.2.2 (function call argument checking for f() declarator)
 * Strategy:   declare int f(); (empty-paren declarator) then call f(1,2) with two
 *             arguments. gcc -std=c2x rejects with "too many arguments to function".
 *             If goc accepts this it is a C89-compat deviation (recorded, not a
 *             goc compile bug). A2's c23_noarg_func.c covers the normal-call side.
 * Status:     PASS (both reject; goc type error "expected 0 arguments, got 2")
 * EXPECT: REJECT
 */
int f();

int main(void) {
    return f(1, 2);
}
