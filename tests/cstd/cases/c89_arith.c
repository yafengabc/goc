/* ============================================================
   c89_arith.c - C89 arithmetic operators & usual arithmetic conversions
   Standard   : ISO/IEC 9899:1990 (C89) 6.3 expressions, 6.1.2.5 types
   Strategy   : 10 subcases: integer promotion, unsigned wraparound,
                signed/unsigned mixing, int<->long conversion,
                division/remainder sign (C89 impl-defined, gcc truncates
                toward zero - record), float/double promotion, int
                division truncation, compound assignment conversion,
                narrowing cast, unsigned char promotion.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    /* case1: integer promotion char -> int, signed arithmetic */
    char a = 100, b = 30;
    printf("case1: %d\n", a + b);

    /* case2: unsigned wraparound */
    unsigned u = 0U;
    printf("case2: %u %u\n", u - 1U, u + 5U);

    /* case3: mixed signed/unsigned: int converted to unsigned */
    int i = -1;
    unsigned v = 1U;
    printf("case3: %d %u\n", (i < v) ? 1 : 0, (unsigned)i);

    /* case4: usual conversion int -> long */
    long L = 100000L;
    printf("case4: %ld\n", i + L);

    /* case5: division/remainder of negatives (impl-defined; gcc truncates) */
    printf("case5: %d %d %d %d\n", -7 / 2, -7 % 2, 7 / -2, 7 % -2);

    /* case6: float/double promotion */
    float f = 1.5f;
    printf("case6: %f\n", f + 2.25);

    /* case7: int division truncation */
    printf("case7: %d %d\n", 7 / 2, 7 % 2);

    /* case8: compound assignment applies conversion */
    int x = 100;
    x += 50;
    x /= 7;
    printf("case8: %d\n", x);

    /* case9: long to int narrowing via cast (impl-defined, wraps) */
    long big = 3000000000L;
    printf("case9: %d\n", (int)big);

    /* case10: promotion of unsigned char in comparison */
    unsigned char c = 0xFF;
    printf("case10: %d\n", c > 0 ? 1 : 0);
    return 0;
}
