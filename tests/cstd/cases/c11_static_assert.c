/* ============================================================
   c11_static_assert.c - _Static_assert keyword and static_assert macro
   Standard   : ISO/IEC 9899:2011 (C11) 6.7.10
   Strategy   : 4 positive subcases (file scope, after typedef, block scope,
               sizeof-based). assert.h supplies the static_assert macro.
               The failing case is a recorded probe, not here. gcc -std=c11 diff
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#include <stdio.h>
#include <assert.h>

_Static_assert(2 + 2 == 4, "arithmetic must hold");

typedef unsigned int uint32;
static_assert(sizeof(uint32) == 4, "uint32 must be 4 bytes");

int main(void) {
    _Static_assert('A' == 65, "ascii A must be 65");
    static_assert(sizeof(int *) == sizeof(void *), "pointers equal size");
    printf("case1: file-scope _Static_assert ok\n");
    printf("case2: typedef-adjacent static_assert ok\n");
    printf("case3: block-scope _Static_assert ok\n");
    printf("case4: sizeof-based static_assert ok\n");
    return 0;
}