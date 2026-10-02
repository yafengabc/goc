/* ============================================================
   c17_stdver.c - record __STDC_VERSION__ and friends (C17)
   Standard   : ISO/IEC 9899:2017 (C17) 6.10.8.1
   Strategy   : record-only file. gcc -std=c17 sets __STDC_VERSION__=201710L;
               goc (single-mode C23, P1.7) sets __STDC_VERSION__=202311L and
               __STDC__/__STDC_HOSTED__=1, so case1 differs by design
               (record, not match); cases 2-5 now agree.
   Status     : PARTIAL (OUTPUT_DIFF by design: version value) (verified 2026-10-02, goc vs gcc -std=c17)
   ============================================================ */
#include <stdio.h>

int main(void) {
#ifdef __STDC_VERSION__
    printf("case1: STDC_VERSION=%ld\n", (long)__STDC_VERSION__);
#else
    printf("case1: STDC_VERSION=undefined\n");
#endif
#ifdef __STDC__
    printf("case2: STDC=%d\n", __STDC__);
#else
    printf("case2: STDC=undefined\n");
#endif
#ifdef __STDC_HOSTED__
    printf("case3: HOSTED=%d\n", __STDC_HOSTED__);
#else
    printf("case3: HOSTED=undefined\n");
#endif
#ifdef __STDC_NO_ATOMICS__
    printf("case4: NO_ATOMICS=defined\n");
#else
    printf("case4: NO_ATOMICS=not-defined\n");
#endif
#ifdef __STDC_NO_THREADS__
    printf("case5: NO_THREADS=defined\n");
#else
    printf("case5: NO_THREADS=not-defined\n");
#endif
    return 0;
}