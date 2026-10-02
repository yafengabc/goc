/* C23 feature: implicit function declaration removed in C23
 * Clause:     C23 removed "implicit declaration of function" (C89 rule)
 * Strategy:   call an undeclared function g(42) from main.
 *             gcc -std=c2x rejects with [-Wimplicit-function-declaration] as error.
 *             If goc accepts it that is a C89-compat deviation (recorded).
 * Status:     PASS (both reject; goc rejects at codegen, not with "implicit decl")
 * EXPECT: REJECT
 */
int main(void) {
    return g(42) == 42 ? 0 : 1;
}
