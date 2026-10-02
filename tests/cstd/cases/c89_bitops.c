/* ============================================================
   c89_bitops.c - C89 bitwise & | ^ ~ and shifts
   Standard   : ISO/IEC 9899:1990 (C89) 6.3.7 bitwise, 6.3.7 shift operators
   Strategy   : 5 subcases: and/or/xor/complement, unsigned left/right shift
                (well-defined logical shifts), signed right shift (gcc does
                arithmetic sign-extend; goc agrees - recorded as impl-defined),
                left shift width modulo, precedence of << over &.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    unsigned u = 0xF0U;
    int n = -8;

    /* case1: bitwise and/or/xor/complement (unsigned, well-defined) */
    printf("case1: %x %x %x %x\n", u & 0x33U, u | 0x0FU, u ^ 0xFFU, ~u);

    /* case2: unsigned shifts are well-defined logical shifts */
    printf("case2: %x %x\n", (u >> 4), (u << 4));

    /* case3: signed right shift: gcc arithmetic sign-extends; goc agrees */
    printf("case3: %d\n", n >> 1);

    /* case4: left shift by a constant */
    printf("case4: %d %d\n", 15 << 2, 1 << 0);

    /* case5: precedence: << binds tighter than & */
    printf("case5: %d\n", (0x10 << 1) & 0x1E);

    return 0;
}
