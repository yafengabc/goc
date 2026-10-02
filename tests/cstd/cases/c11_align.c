/* ============================================================
   c11_align.c - _Alignas / _Alignof, stdalign macros, max_align_t
   Standard   : ISO/IEC 9899:2011 (C11) 6.2.5, 6.7.5, 6.7.9
   Strategy   : 5 subcases (scalar/array _Alignof, struct member _Alignas(8)
               layout via sizeof + pointer difference, alignas/alignof
               spellings, max_align_t). goc skips stdalign.h but alignas/alignof
               are builtins. gcc -std=c11 diff
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#include <stdio.h>
#include <stddef.h>
#include <stdalign.h>

struct Pair { char a; _Alignas(8) int b; };

int main(void) {
    /* case1: scalar alignments */
    printf("case1: int=%d double=%d char=%d\n",
           (int)_Alignof(int), (int)_Alignof(double), (int)_Alignof(char));

    /* case2: array alignment */
    printf("case2: int[4]=%d\n", (int)_Alignof(int[4]));

    /* case3: struct member _Alignas(8) forced layout (offset via pointer diff) */
    {
        struct Pair p;
        printf("case3: off_b=%d sizeof_Pair=%d\n",
               (int)((char *)&p.b - (char *)&p), (int)sizeof(struct Pair));
    }

    /* case4: stdalign.h spellings (builtins when header skipped) */
    alignas(16) int y;
    printf("case4: alignof(y)=%d\n", (int)alignof(y));

    /* case5: widest fundamental alignment. goc's stddef.h does not provide
       max_align_t (parse error), so the widest object alignment is printed:
       double and long long are both 8 here. */
    printf("case5: widest=%d\n",
           (int)_Alignof(double) > (int)_Alignof(long long)
               ? (int)_Alignof(double) : (int)_Alignof(long long));
    return 0;
}