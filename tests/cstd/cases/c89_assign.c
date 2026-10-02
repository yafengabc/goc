/* ============================================================
   c89_assign.c - C89 assignment & compound assignment, casts, comma operator
   Standard   : ISO/IEC 9899:1990 (C89) 6.3.16 assignment operators
   Strategy   : 6 subcases: arithmetic compound assignments, compound assign
                applies type conversion (char wrap), chained assignment right-
                associative, cast truncation long->int, comma operator value,
                bitwise compound assignments.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    int a;
    int b;
    char c;
    long big;

    /* case1: arithmetic compound assignments */
    a = 10;
    a += 5;
    a -= 3;
    a *= 2;
    a /= 4;
    a %= 3;
    printf("case1: %d\n", a);

    /* case2: compound assignment applies its type conversion (char wraps) */
    c = 100;
    c += 200;
    printf("case2: %d\n", c);

    /* case3: chained assignment is right-associative */
    b = a = 7;
    printf("case3: %d %d\n", a, b);

    /* case4: cast truncation of a long to int */
    big = 3000000000L;
    printf("case4: %d\n", (int)big);

    /* case5: comma operator evaluates left, yields right */
    a = (1, 2, 3);
    printf("case5: %d\n", a);

    /* case6: bitwise compound assignments */
    a = 0xF0;
    a &= 0x33;
    a |= 0x0F;
    a ^= 0xFF;
    printf("case6: %x\n", a);

    return 0;
}
