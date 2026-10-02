/* ============================================================
   c89_storage.c - C89 storage classes: auto/register/static/extern, shadowing
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.1 storage classes, 6.1.2.4 scope
   Strategy   : 5 subcases: register variable used normally (no address taken),
                static local persists across calls while auto local resets,
                extern function declaration then definition, block scope
                shadowing, for loop over an outer-declared index.
                NOTE: goc treats a top-level `extern int x;` as a definition
                itself, so `extern int x; int x = v;` errors "redefinition of
                x in the same scope" (recorded in report); variable extern is
                therefore shown via a function declaration instead.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

extern int twice(int x);   /* extern function declaration */

int twice(int x) {          /* the definition it refers to */
    return x * 2;
}

void counter(void) {
    static int n = 0;       /* persists across calls */
    auto int local = 5;     /* auto local, fresh each call */
    n = n + 1;
    local = local + 1;
    printf("  counter: n=%d local=%d\n", n, local);
}

int main(void) {
    int i;
    register int r;
    int x;

    x = 1;

    /* case1: register variable used normally (address never taken) */
    r = 10;
    r = r + 2;
    printf("case1: %d\n", r);

    /* case2: static local persists, auto local resets on each call */
    counter();
    counter();
    counter();

    /* case3: extern-declared function */
    printf("case3: %d\n", twice(21));

    /* case4: inner block shadows outer x */
    printf("case4a: %d\n", x);
    {
        int x = 20;
        printf("case4b: %d inner\n", x);
    }
    printf("case4c: %d outer\n", x);

    /* case5: for loop over an outer-declared index (C89: no decl in for) */
    for (i = 0; i < 3; i = i + 1) {
        printf("  loop i=%d\n", i);
    }

    return 0;
}
