/* ============================================================
   c89_lit_octal.c - octal integer literals (0-prefixed): silent misparse in goc
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.3.1 integer constants (leading 0 = octal)
   Strategy   : case1 decimal baseline; case2 plain octal 010 and 0777;
                case3 octal with U and L suffixes; case4 octal arithmetic expression.
                DANGER: goc does not error, it silently reads 0-prefixed literals
                as DECIMAL: 010 prints 10 where gcc prints 8, 0777 prints 777
                where gcc prints 511.  Silent wrong output is worse than a hard
                compile error: never trust a 0-prefixed literal under goc.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    printf("case1: decimal-10=%d decimal-777=%d\n", 10, 777);
    printf("case2: octal-010=%d octal-0777=%d\n", 010, 0777);
    printf("case3: octal-u=%u octal-l=%ld\n", 010U, 010L);
    printf("case4: octal-arith=%d\n", 010 + 010);
    return 0;
}
