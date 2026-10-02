/* ============================================================
   c89_lit_dotfloat.c - leading-dot float literals: goc parse error
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.3.1 floating constants (.5 is valid)
   Strategy   : case1 .5 assigned and printed; case2 .5f float suffix;
                case3 .5e2 exponent; case4 .5L long double; baseline 0.5 for contrast.
                gcc accepts all; goc fails to parse the leading "." token.
   Status     : FAIL (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    double a = .5;
    float b = .5f;
    double c = .5e2;
    double d = .5L;
    double base = 0.5;
    printf("case1: dot5=%f\n", a);
    printf("case2: dot5f=%f\n", b);
    printf("case3: dot5e2=%f\n", c);
    printf("case4: dot5L=%f base=%f\n", d, base);
    return 0;
}
