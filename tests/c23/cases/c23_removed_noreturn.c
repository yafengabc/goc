/* C23 feature: _Noreturn / noreturn macro (C23 keeps but suggests [[noreturn]])
 * Clause:     C23 6.7.13 / attribute [[noreturn]]; _Noreturn still a keyword
 * Strategy:   probe _Noreturn declaration + definition syntax, the noreturn macro
 *             spelling, and <stdnoreturn.h> include. Neither function is ever called
 *             at runtime, so no non-returning behavior is exercised (no UB).
 *             gcc -std=c2x accepts both cleanly (no deprecation warning).
 *             goc stance (header skip note, bare noreturn spelling) recorded in status.
 * Status:     PASS (both accept; stdout identical)
 * EXPECT: PASS
 */
#include <stdio.h>
#include <stdnoreturn.h>

/* subcase 1: _Noreturn function declaration */
_Noreturn void decl_only(void);

/* subcase 2: _Noreturn function definition (never called) */
_Noreturn void defined_fn(void) {
    printf("");
}

/* subcase 3: noreturn macro spelling (gcc: macro from header; goc: native spelling) */
noreturn void macro_fn(void);

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: _Noreturn declaration parsed\n", total);
    ++passed;

    ++total;
    printf("case%d: _Noreturn definition parsed\n", total);
    ++passed;

    ++total;
    printf("case%d: noreturn macro spelling parsed\n", total);
    ++passed;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
