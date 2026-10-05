/*
 * cstd_lib_math2.c -- goclib <math.h> coverage, round 2.
 *
 * scalbn / scalbln are the exact "multiply by 2^n" primitives (C99 7.12.6.13
 * / 7.12.6.14); every other scaling routine (ldexp, the frexp/modf halves)
 * reduces to them. They differ only in the exponent's width: scalbn takes an
 * int, scalbln a long.
 *
 *   case1  scalbn(1.5, 4) and scalbln(1.5, 4L) both equal 24
 *   case2  negative exponent divides, and scaling zero stays zero
 *   case3  a fractional scale and an underflow-to-zero, both exactly printable
 *
 * Cross-checked against mingw gcc; the results are identical because the
 * exponent is applied by an exact power-of-two shift, with no rounding.
 * (Large exponents such as scalbn(1.0, 1023) yield a correct bit pattern --
 * verified identical to gcc's 0x7FE0000000000000 -- but goclib's printf
 * still rounds the ~300-digit decimal expansion of 2^1023 poorly; that
 * printf floating-point printing gap is tracked separately and is not a
 * scalbn defect.)
 */
#include <stdio.h>
#include <math.h>

int main(void) {
    printf("case1: scalbn=%.0f scalbln=%.0f\n",
           scalbn(1.5, 4), scalbln(1.5, 4L));

    printf("case2: scalbn-neg=%.6f scalbn0=%.1f\n",
           scalbn(1.0, -10), scalbn(0.0, 5));

    /* A tidy fractional scale and an underflow-to-zero, both exactly
     * printable. Avoids the independent printf-large-double printing gap. */
    printf("case3: scalbn-half=%.2f scalbn-underflow=%.0f\n",
           scalbn(1.5, -1), scalbn(1.0, -1075));
    return 0;
}
