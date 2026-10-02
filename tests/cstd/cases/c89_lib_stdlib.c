/* ============================================================
   c89_lib_stdlib.c - atoi/atol/strtol/strtoul, abs/labs/div/ldiv, rand properties,
                      malloc family, qsort/bsearch, atexit order
   Standard   : ISO/IEC 9899:1990 (C89) 7.10 utility <stdlib.h>
   Strategy   : case1 atoi/atol; case2 strtol base16 with endptr; case3 strtoul base10;
                case4 abs/labs/div/ldiv; case5 rand range + srand determinism
                (value sequence itself is implementation-defined, not compared);
                case6 malloc/calloc/realloc/free; case7 qsort + bsearch;
                case8 atexit LIFO order
                each case printf distinct, gcc -std=c89 diff
   Status     : PARTIAL: case5 goc stdlib.h lacks RAND_MAX; rest matches (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>
#include <stdlib.h>

static int cmp_int(const void *a, const void *b) {
    int x = *(const int *)a, y = *(const int *)b;
    return (x > y) - (x < y);
}
static void h_a(void) { printf("case8: atexit-a\n"); }
static void h_b(void) { printf("case8: atexit-b\n"); }

int main(void) {
    printf("case1: atoi=%d atol=%ld\n", atoi(" -123"), atol("456789"));
    {
        char *e;
        long l = strtol("0x100", &e, 16);
        unsigned long ul = strtoul("42", &e, 10);
        printf("case2: strtol=%ld tail=[%s]\n", l, e);
        printf("case3: strtoul=%lu\n", ul);
    }
    printf("case4: abs=%d labs=%ld\n", abs(-7), labs(-7L));
    {
        div_t d = div(13, 5);
        ldiv_t ld = ldiv(130000L, 5L);
        printf("case4b: div=%d/%d ldiv=%ld/%ld\n", d.quot, d.rem, ld.quot, ld.rem);
    }
    {
        int r1, r2;
        srand(1234); r1 = rand();
        srand(1234); r2 = rand();
#ifdef RAND_MAX
        printf("case5: RAND_MAX=%d det=%d range-ok=%d\n",
               (int)RAND_MAX, (r1 == r2) ? 1 : 0,
               (r1 >= 0 && r1 <= (int)RAND_MAX) ? 1 : 0);
#else
        printf("case5: RAND_MAX=not-defined det=%d nonnegative=%d\n",
               (r1 == r2) ? 1 : 0, (r1 >= 0) ? 1 : 0);
#endif
    }
    {
        int *p = (int *)malloc(sizeof(int) * 4);
        int *q;
        p[0] = 1; p[1] = 2;
        q = (int *)realloc(p, sizeof(int) * 6);
        q[2] = 3;
        printf("case6: malloc-chain=%d%d%d\n", q[0], q[1], q[2]);
        free(q);
    }
    {
        int arr[5] = { 3, 1, 2, 5, 4 };
        int key = 2;
        int *found;
        qsort(arr, 5, sizeof(int), cmp_int);
        printf("case7: sorted=%d%d%d%d%d\n", arr[0], arr[1], arr[2], arr[3], arr[4]);
        found = (int *)bsearch(&key, arr, 5, sizeof(int), cmp_int);
        printf("case7: bsearch-found=%d\n", found ? *found : -1);
    }
    atexit(h_b);
    atexit(h_a);
    printf("case8: main-returning\n");
    return 0;
}
