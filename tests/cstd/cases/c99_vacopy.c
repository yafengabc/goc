/* ============================================================
   c99_vacopy.c - va_copy for independent va_list (C99 7.15)
   Standard   : ISO/IEC 9899:1999 (C99) 7.15.2
   Strategy   : gcc side: va_copy then two independent passes.
                goc side: va_copy not in goclib (codegen error, real gap).
   Status     : FAIL (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
#include <stdarg.h>
static void two(int n, ...) {
    va_list a, b;
    va_start(a, n);
    va_copy(b, a);
    int s1 = 0, s2 = 0, i;
    for (i = 0; i < n; i++) s1 += va_arg(a, int);
    for (i = 0; i < n; i++) s2 += va_arg(b, int);
    printf("case1: %d %d\n", s1, s2);
    va_end(a);
    va_end(b);
}
int main(void) {
    two(3, 10, 20, 30);
    return 0;
}