/* ============================================================
   c89_func.c - C89 functions: prototypes, by-value, recursion, function ptrs
   Standard   : ISO/IEC 9899:1990 (C89) 6.7.1 function definitions,
                6.7.5 function declarators, 6.1.2.2.1 function pointer
   Strategy   : 7 subcases: prototype definition and call with by-value
                parameter, recursion (factorial), return a selected pointer,
                function pointer call, function pointer passed as parameter,
                void function, main(argc,argv) form.
                NOTE: K&R old-style definition `int f(a,b) int a; int b; {...}`
                is NOT accepted by goc (parse error: "expected type specifier,
                got a"); gcc -std=c89 accepts it.  It is not embedded here so
                the suite stays goc-compilable; the gap is recorded in the
                report cross-findings.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int fact(int n);              /* forward declaration */
int add2(int a, int b);

int add2(int a, int b) {
    return a + b;
}

int fact(int n) {
    if (n <= 1) return 1;
    return n * fact(n - 1);    /* recursion */
}

int *choose(int *a, int *b, int which) {
    return which ? a : b;      /* return a pointer */
}

void greet(void) {
    printf("  hi\n");
}

int apply(int (*fp)(int, int), int x, int y) {
    return fp(x, y);           /* function pointer parameter */
}

int main(int argc, char **argv) {
    int a = 3;
    int b = 4;
    int (*fp)(int, int);
    int *r;
    (void)argc;
    (void)argv;

    /* case1: prototype call and by-value parameter */
    printf("case1: %d\n", add2(a, b));

    /* case2: recursion */
    printf("case2: %d\n", fact(5));

    /* case3: return a pointer selected by a condition */
    r = choose(&a, &b, 1);
    printf("case3: %d\n", *r);

    /* case4: function pointer call (function decays to pointer) */
    fp = add2;
    printf("case4: %d\n", fp(10, 20));

    /* case5: function pointer passed as a parameter */
    printf("case5: %d\n", apply(fp, 1, 2));

    /* case6: void function */
    greet();

    /* case7: main(argc,argv) form; both run with no args so argc == 1 */
    printf("case7: argc=%d\n", argc);

    return 0;
}
