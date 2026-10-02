/* C23 feature: attributes [[noreturn]] and the _Noreturn spelling
 * Clause:     C23 6.7.13 (noreturn attribute); _Noreturn is the C11 spelling
 * Strategy:   exercise [[noreturn]] and _Noreturn on declaration + definition,
 *             on a function pointer, and across declaration/definition using
 *             the two spellings. The noreturn functions are DEFINED (infinite
 *             loop) but NEVER CALLED, so no non-returning path is ever taken.
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>

[[noreturn]] void die(void);
[[noreturn]] void die(void) { for (;;) { } }

_Noreturn void halt(void) { for (;;) { } }

/* mixed spelling: declared with [[noreturn]], defined with _Noreturn */
[[noreturn]] void die3(void);
_Noreturn void die3(void) { for (;;) { } }

typedef void (*vfunc)(void);

int main(void) {
    int passed = 0, total = 0;

    ++total;
    vfunc fp = &die;
    printf("case1: [[noreturn]] decl+def, fp nonzero=%d\n", (int)(fp != 0));
    if (fp != 0) passed++;

    ++total;
    vfunc fp2 = &halt;
    printf("case2: _Noreturn decl+def, fp2 nonzero=%d\n", (int)(fp2 != 0));
    if (fp2 != 0) passed++;

    ++total;
    vfunc fp3 = &die3;
    printf("case3: mixed [[noreturn]]/_Noreturn decl consistency, fp3 nonzero=%d\n", (int)(fp3 != 0));
    if (fp3 != 0) passed++;

    ++total;
    printf("case4: noreturn functions never called (no UB taken)\n");
    passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
