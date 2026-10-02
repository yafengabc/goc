/* ============================================================
   c11_noreturn.c - _Noreturn keyword and stdnoreturn.h noreturn macro
   Standard   : ISO/IEC 9899:2011 (C11) 6.7.4
   Strategy   : 3 subcases (keyword definition, macro definition, bare
               declaration). Bodies are infinite loops so they truly do not
               return; never called. gcc -std=c11 diff
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#include <stdio.h>
#include <stdnoreturn.h>

_Noreturn void loop_a(void) { for (;;) {} }
noreturn void loop_b(void) { for (;;) {} }
_Noreturn void end_now(void);

int main(void) {
    printf("case1: _Noreturn keyword definition ok\n");
    printf("case2: noreturn macro definition ok\n");
    printf("case3: _Noreturn declaration ok\n");
    return 0;
}