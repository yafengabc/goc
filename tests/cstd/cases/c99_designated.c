/* ============================================================
   c99_designated.c - designated initializers (C99 6.7.8)
   Standard   : ISO/IEC 9899:1999 (C99) 6.7.8
   Strategy   : 4 subcases: [i]= array designated, .field= struct,
                nested designated in struct array, repeated designator
                (latter wins). Note: positional-after-designated mixing
                is rejected by goc (cross-findings).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
struct P { int x; int y; };
int main(void) {
    int a[6] = { [2] = 7, [0] = 5 };
    printf("case1: %d %d %d\n", a[0], a[1], a[2]);
    struct P s = { .y = 3, .x = 1 };
    printf("case2: %d %d\n", s.x, s.y);
    struct P arr[3] = { [1] = { .x = 9, .y = 8 } };
    printf("case3: %d %d\n", arr[1].x, arr[1].y);
    int b[5] = { [3] = 1, [3] = 2 };
    printf("case4: %d\n", b[3]);
    return 0;
}