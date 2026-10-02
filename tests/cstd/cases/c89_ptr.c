/* ============================================================
   c89_ptr.c - C89 pointers: & * [] equivalence, arithmetic, decay, void*
   Standard   : ISO/IEC 9899:1990 (C89) 6.3.5 unary & *, 6.3.6 additive,
                6.3.8 relational, array-to-pointer conversion
   Strategy   : 6 subcases: &a[1]/a+1 equivalence, pointer+int scaling,
                pointer difference in elements, pointer comparison, array
                sizeof not decaying, string literal subscript and size,
                NULL pointer and void* round-trip.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    int a[5] = { 10, 20, 30, 40, 50 };
    int *p;
    int *q;
    char *s;
    void *v;
    int x;

    /* case1: &a[1] == a+1, *(a+1) == a[1] */
    p = &a[1];
    printf("case1: %d %d\n", *p, *(a + 1));

    /* case2: pointer + int scales by element size; p[3] == *(p+3) */
    p = a;
    printf("case2: %d %d\n", *(p + 2), p[3]);

    /* case3: pointer difference counts elements */
    q = &a[4];
    printf("case3: %d\n", (int)(q - p));

    /* case4: pointer comparison */
    printf("case4: %d %d\n", p < q, p == q);

    /* case5: sizeof on a local array stays the array size (no decay) */
    printf("case5: %d\n", (int)sizeof(a));

    /* case6: string literal subscript and sizeof (includes NUL) */
    s = "hello";
    printf("case6: %c %c %d\n", s[0], s[4], (int)sizeof("hello"));

    /* case7: NULL pointer and void* round-trip */
    v = 0;
    printf("case7: %d\n", (v == 0) ? 1 : 0);
    x = 123;
    v = &x;
    printf("case7b: %d\n", *(int *)v);

    return 0;
}
