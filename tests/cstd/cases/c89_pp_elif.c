/* ============================================================
   c89_pp_elif.c - #elif chains: goc skips even the true #elif branch
   Standard   : ISO/IEC 9899:1990 (C89) 6.8.1 conditional inclusion (#elif)
   Strategy   : case1 #if 0 / #elif 1 / #else; case2 multi #elif with first true
                (#if X==1 / #elif X==5 / #elif X==9 / #else); case3 #elif defined(X);
                case4 #if 1 with no #elif (sanity that plain if/else still works).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89; P0.6 fixed - a
                #if/#elif condition now expands macros even while the enclosing
                branch is inactive)
   ============================================================ */
#include <stdio.h>

#define X 5

int main(void) {
#if 0
    printf("case1: if-dead\n");
#elif 1
    printf("case1: elif-taken\n");
#else
    printf("case1: else\n");
#endif

#if X == 1
    printf("case2: branch-1\n");
#elif X == 5
    printf("case2: branch-5\n");
#elif X == 9
    printf("case2: branch-9\n");
#else
    printf("case2: else\n");
#endif

#if defined(UNDEF)
    printf("case3: undef-branch\n");
#elif defined(X)
    printf("case3: defined-X\n");
#else
    printf("case3: else\n");
#endif

#if 1
    printf("case4: plain-if-ok\n");
#else
    printf("case4: plain-else\n");
#endif
    return 0;
}
