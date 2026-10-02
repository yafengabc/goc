/* ============================================================
   c99_complex.c - complex.h / double _Complex (C99 6.2.5, 7.3)
   Standard   : ISO/IEC 9899:1999 (C99) 6.2.5, 7.3
   Strategy   : gcc side: creal/cimag/I. goc side: complex.h skipped,
                _Complex keyword not parsed (not implemented).
   Status     : UNSUPPORTED (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
#include <math.h>
#include <complex.h>
int main(void) {
    double _Complex z = 2.0 + 3.0 * I;
    double r = creal(z);
    double im = cimag(z);
    printf("case1: %.6f %.6f\n", r, im);
    return 0;
}