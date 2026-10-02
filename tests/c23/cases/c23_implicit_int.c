/* C23 feature: implicit int removed in C23
 * Clause:     C23 removed "return type defaults to int" (implicit-int)
 * Strategy:   define foo(void) with no return type specifier, then call it.
 *             gcc -std=c2x rejects with [-Wimplicit-int] as error. If goc accepts it
 *             that is a C89-compat deviation (recorded).
 * Status:     PASS (both reject; goc does NOT retain C89 implicit-int)
 * EXPECT: REJECT
 */
foo(void) {
    return 1;
}

int main(void) {
    return foo() == 1 ? 0 : 1;
}
