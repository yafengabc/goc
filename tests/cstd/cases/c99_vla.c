/* ============================================================
   c99_vla.c - variable-length arrays (C99 6.7.5.2)
   Standard   : ISO/IEC 9899:1999 (C99) 6.7.5.2
   Strategy   : gcc side: int a[n], sizeof VLA, 2-D VLA. goc side:
                parse error at the first [n] (not implemented).
   Status     : UNSUPPORTED (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
int main(void) {
    int n = 4;
    int a[n];
    int i;
    for (i = 0; i < n; i++) a[i] = i * i;
    printf("case1: %d %d\n", a[0], a[3]);
    printf("case2: %d\n", (int)sizeof(a));
    int m = 3;
    int b[m][m];
    b[0][0] = 1;
    printf("case3: %d\n", b[0][0]);
    return 0;
}