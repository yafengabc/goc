/* ============================================================
   c99_hexfloat.c - hexadecimal floating constants (C99 6.4.4.2)
   Standard   : ISO/IEC 9899:1999 (C99) 6.4.4.2
   Strategy   : 3 subcases: 0x1.8p3, 0x.8p1, 0x1p-1, float suffix fp3f.
                Values checked with %.6f (goc %a unsupported, .tmp probe).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
int main(void) {
    double a = 0x1.8p3;
    double b = 0x.8p1;
    double c = 0x1p-1;
    float  d = 0x1.fp3f;
    printf("case1: %.6f %.6f %.6f\n", a, b, c);
    printf("case2: %.6f\n", (double)d);
    return 0;
}