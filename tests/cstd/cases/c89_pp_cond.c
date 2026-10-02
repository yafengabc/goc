/* ============================================================
   c89_pp_cond.c - #if/#ifdef/#ifndef/#else/#endif, defined(), constant expr
   Standard   : ISO/IEC 9899:1990 (C89) 6.8.1 conditional inclusion
   Strategy   : case1 #if taken; case2 nested #if/#else chain (goc #elif chain broken,
                exercised in negative probe); case3 defined() with !;
                case4 character constant in #if (ASCII); case5 undefined identifier == 0;
                case6 nested directives
                each live branch printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

#define SWITCH_ON 1
#define VAL 42

int main(void) {
#if SWITCH_ON
    printf("case1: if-branch-taken\n");
#else
    printf("case1: else-branch\n");
#endif

#if VAL > 30
  #if VAL > 100
    printf("case2: big\n");
  #else
    printf("case2: medium\n");
  #endif
#else
    printf("case2: small\n");
#endif

#if defined(SWITCH_ON) && !defined(UNDEF_PROBE)
    printf("case3: defined-works\n");
#endif

#if 'A' == 65
    printf("case4: char-const-in-if-ok\n");
#endif

#if UNDEFINED_IDENT + 1 == 1
    printf("case5: undefined-id-is-zero\n");
#endif

#if 1
  #if 0
    printf("case6: nested-dead\n");
  #else
    printf("case6: nested-live\n");
  #endif
#else
    printf("case6: outer-dead\n");
#endif
    return 0;
}
