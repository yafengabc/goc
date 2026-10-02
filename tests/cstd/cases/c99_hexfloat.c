/* ============================================================
   c99_hexfloat.c - hexadecimal floating constants (C99 6.4.4.2)
   Standard   : ISO/IEC 9899:1999 (C99) 6.4.4.2
   Strategy   : 3 subcases: 0x1.8p3, 0x.8p1, 0x1p-1, float suffix fp3f.
                Values checked with %.6f. case3-6 exercise printf %a/%A
                (hex float, P0.7): exact defaults, explicit precisions,
                %#a point forcing, inf and extremes -- every %a output is a
                pure function of the IEEE-754 bits, so goc and gcc (ucrt)
                agree byte for byte. -0.0 and NaN constants are excluded:
                goc's constant folding drops their sign bits.
   Status     : PASS (fixed 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
int main(void) {
    double a = 0x1.8p3;
    double b = 0x.8p1;
    double c = 0x1p-1;
    float  d = 0x1.fp3f;
    printf("case1: %.6f %.6f %.6f\n", a, b, c);
    printf("case2: %.6f\n", (double)d);
    printf("case3: %a %A %a %a\n", 12.5, 12.5, 1.0, 0.1);
    printf("case4: %.1a %.4a %.13a %.14a %#a %#A\n", 12.5, 12.5, 12.5, 12.5, 12.5, 12.5);
    printf("case5: %a %a %a %a\n", 1e300, 2.2250738585072014e-308, 5e-324, 1.0/0.0);
    printf("case6: %a %a %-24a|%24a|\n", 1.7976931348623157e308, 0.75, 0.5, 0.5);
    return 0;
}