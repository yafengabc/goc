/* ============================================================
   c99_inline.c - inline functions (C99 6.7.4)
   Standard   : ISO/IEC 9899:1999 (C99) 6.7.4
   Strategy   : 2 subcases: static inline (portable), plain inline with
                extern redeclaration so gcc -std=c99 emits a definition.
                goc degrades inline to a normal function (same output).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
static inline int sqa(int x) { return x * x; }
inline int dbl(int x) { return x * 2; }
extern int dbl(int);
int main(void) {
    printf("case1: %d\n", sqa(5));
    printf("case2: %d\n", dbl(21));
    return 0;
}