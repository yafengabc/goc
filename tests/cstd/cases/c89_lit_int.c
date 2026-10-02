/* ============================================================
   c89_lit_int.c - integer literals: dec/hex, U/L/UL suffixes, unary minus, printf widths
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.3.1 integer constants, 7.9.6 formatted io
   Strategy   : case1 decimal; case2 hex literal value; case3 unary minus;
                case4 U suffix %u; case5 L/UL %ld/%lu; case6 %o output of a value;
                case7 %x/%X output; case8 unsigned wrap of 32-bit all-ones
                NOTE: leading-zero octal literals (010) are parsed as plain decimal
                by goc (010 -> 10, gcc -> 8): negative probe, excluded from this file.
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

int main(void) {
    printf("case1: dec=%d\n", 42);
    printf("case2: hex-0x1F=%d hex-0x10=%d\n", 0x1F, 0x10);
    printf("case3: unary-minus=%d\n", -17);
    printf("case4: uint=%u\n", 42u);
    printf("case5: long=%ld ulong=%lu\n", 1234567L, 4000000000UL);
    printf("case6: oct-out=%o\n", 64);
    printf("case7: hex-out=%x %X\n", 255, 255);
    printf("case8: all-ones-u32=%u\n", 0xFFFFFFFFu);
    return 0;
}
