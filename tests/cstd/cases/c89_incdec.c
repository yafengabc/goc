/* ============================================================
   c89_incdec.c - C89 increment/decrement prefix/postfix & sequence points
   Standard   : ISO/IEC 9899:1990 (C89) 6.3.2.4 increment/decrement,
                6.6 statements (sequence points)
   Strategy   : 5 subcases: postfix returns old value, prefix returns new
                value, pointer postfix scales by element, pointer prefix,
                well-separated decrement uses (avoids unspecified arg order).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    int i;
    int j;
    int a[5] = { 10, 20, 30, 40, 50 };
    int *p;

    /* case1: postfix yields the old value */
    i = 5;
    j = i++;
    printf("case1: %d %d\n", j, i);

    /* case2: prefix yields the new value */
    j = ++i;
    printf("case2: %d %d\n", j, i);

    /* case3: pointer postfix: read then advance */
    p = a;
    j = *p++;
    printf("case3: %d %d\n", j, *p);

    /* case4: pointer prefix: advance then read */
    j = *++p;
    printf("case4: %d\n", j);

    /* case5: separate decrements so the sequence point is explicit */
    i = 10;
    i--;
    printf("case5: %d\n", i);
    printf("case5b: %d\n", --i);

    return 0;
}
