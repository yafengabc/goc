/* ============================================================
   c99_math.c - C99 math macros and functions (C99 7.12)
   Standard   : ISO/IEC 9899:1999 (C99) 7.12
   Strategy   : 7 subcases using %.6f / boolean normalization: rounding
                family, abs/sqrt/pow/hypot, fmod, isnan/isinf/isfinite,
                signbit, fpclassify via FP_* compare, nan().
                HUGE_VAL absent from goc math.h (cross-findings).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
#include <math.h>
int main(void) {
    double x = 3.7;
    printf("case1: %.6f %.6f %.6f %.6f\n", round(x), trunc(x), floor(x), ceil(x));
    printf("case2: %.6f %.6f %.6f %.6f\n", fabs(-2.5), sqrt(16.0), pow(2.0, 3.0), hypot(3.0, 4.0));
    printf("case3: %.6f\n", fmod(10.0, 3.0));
    printf("case4: nan=%d inf=%d fin=%d\n", isnan(NAN) ? 1 : 0, isinf(INFINITY) ? 1 : 0, isfinite(3.5) ? 1 : 0);
    printf("case5: sign=%d\n", signbit(-2.0) ? 1 : 0);
    printf("case6: nan=%d zero=%d norm=%d\n",
        (fpclassify(NAN) == FP_NAN) ? 1 : 0,
        (fpclassify(0.0) == FP_ZERO) ? 1 : 0,
        (fpclassify(3.5) == FP_NORMAL) ? 1 : 0);
    printf("case7: %.6f\n", nan(""));
    return 0;
}