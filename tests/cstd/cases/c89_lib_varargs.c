/* ============================================================
   c89_lib_varargs.c - va_list/va_start/va_arg/va_end with int, double and mixed args;
                      stddef.h NULL/size_t/ptrdiff_t, struct offset by pointer math
   Standard   : ISO/IEC 9899:1990 (C89) 7.8 variable args, 6.1.5 common definitions
   Strategy   : case1 va over ints; case2 va over doubles; case3 mixed int+double list;
                case4 stddef: NULL size, ptrdiff_t arithmetic, struct member offset
                computed by pointer difference (offsetof macro itself chokes goc's
                parser: negative probe, documented)
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>
#include <stdarg.h>
#include <stddef.h>

static void sum_ints(int n, ...) {
    va_list ap;
    int i, s = 0;
    va_start(ap, n);
    for (i = 0; i < n; i++) s += va_arg(ap, int);
    va_end(ap);
    printf("case1: int-sum=%d\n", s);
}
static void avg_dbl(int n, ...) {
    va_list ap;
    int i;
    double s = 0.0;
    va_start(ap, n);
    for (i = 0; i < n; i++) s += va_arg(ap, double);
    va_end(ap);
    printf("case2: double-avg=%.2f\n", s / n);
}
static void mixed(int n, ...) {
    va_list ap;
    int i;
    va_start(ap, n);
    printf("case3: mixed");
    for (i = 0; i < n; i++) {
        printf(" %d", va_arg(ap, int));
        printf(":%.1f", va_arg(ap, double));
    }
    printf("\n");
    va_end(ap);
}

struct pod { char a; double b; };

int main(void) {
    sum_ints(3, 10, 20, 30);
    avg_dbl(2, 1.0, 3.0);
    mixed(2, 1, 1.5, 2, 2.5);
    {
        int arr[4] = { 0, 1, 2, 3 };
        struct pod p;
        ptrdiff_t diff = &arr[3] - &arr[0];
        int off_b = (int)((char *)&p.b - (char *)&p.a);
        printf("case4: offset-b=%d ptrdiff=%d size-of-null=%d\n",
               off_b, (int)diff, (int)sizeof(NULL));
    }
    return 0;
}
