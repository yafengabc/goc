/* ============================================================
   c99_bool.c - _Bool and stdbool.h bool/true/false (C99 6.2.5, 7.16)
   Standard   : ISO/IEC 9899:1999 (C99) 6.2.5, 7.16
   Strategy   : 4 subcases: scalar _Bool conversion 0/non0, bool/true/
                false, int to _Bool, sizeof(_Bool).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
#include <stdbool.h>
int main(void) {
    _Bool b0 = 0, b1 = 42, b2 = -1;
    printf("case1: %d %d %d\n", (int)b0, (int)b1, (int)b2);
    bool t = true, f = false;
    printf("case2: %d %d\n", (int)t, (int)f);
    int x = 0, y = -9;
    _Bool bx = x, by = y;
    printf("case3: %d %d\n", (int)bx, (int)by);
    printf("case4: %d\n", (int)sizeof(_Bool));
    return 0;
}