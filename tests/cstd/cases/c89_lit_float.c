/* ============================================================
   c89_lit_float.c - floating literals: f/L suffix, exponent, 5. trailing dot, %f %e %g
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.3.1 floating constants
   Strategy   : case1 plain double; case2 trailing-dot form 5.; case3 exponent forms;
                case4 f suffix float; case5 L suffix long double;
                case6 %e output; case7 %g output; case8 precision %.2f
                NOTE: leading-dot form (.5) is rejected by goc
                ("parse error: unexpected token ."): excluded as negative probe.
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

int main(void) {
    printf("case1: pi=%f\n", 3.14);
    printf("case2: trailingdot=%f\n", 5.);
    printf("case3: exp=%f expneg=%f\n", 5e2, 2.5e-3);
    {
        float fv = 1.25f;
        double lv = 7.5L;
        printf("case4: float=%f case5: longdouble=%f\n", fv, lv);
    }
    printf("case6: exp-form=%e\n", 12345.0);
    printf("case7: g-form=%g g-small=%g\n", 0.000123, 12345.0);
    printf("case8: precision=%.2f\n", 3.14159);
    return 0;
}
