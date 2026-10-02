/* ============================================================
   c99_compound.c - C99 compound literals (T){...}
   Standard   : ISO/IEC 9899:1999 (C99) 6.5.2.5
   Strategy   : 8 subcases: block scope value use, address-taking,
                array decay, distinct objects in one block (two
                literals must not alias), designated-initializer
                combination, literal as function argument, const
                literal, sizeof of array literal.
                NOTE: file-scope compound literals are rejected by
                goc (see c99_compound_file.c, UNSUPPORTED).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99; P0.5: case8
                sizeof((int[]){1,2,3}) = 12, was 0)
   ============================================================ */
#include <stdio.h>

struct P { int x, y; };

static int sum2(struct P a, struct P b) {
    return a.x + a.y + b.x + b.y;
}

int main(void) {
    /* case1: block-scope compound literal, value use */
    struct P s = (struct P){3, 4};
    printf("case1: %d %d\n", s.x, s.y);

    /* case2: address-taking of a scalar literal */
    int *q = &(int){99};
    printf("case2: %d\n", *q);

    /* case3: array decay to pointer */
    int *r = (int[]){1, 2, 3, 4, 5};
    printf("case3: %d %d\n", r[0], r[4]);

    /* case4: two literals in one block are distinct objects */
    {
        int *u = (int[]){42};
        int *v = (int[]){42};
        printf("case4a: %d\n", u == v ? 0 : 1); /* 1 = distinct */
        *u = 1;
        *v = 2;
        printf("case4b: %d %d\n", *u, *v);      /* independent storage */
    }

    /* case5: combined with designated initializers (C99) */
    int *d = (int[]){ [2] = 7, [0] = 5 };
    printf("case5: %d %d %d\n", d[0], d[1], d[2]);

    /* case6: literal passed directly as function argument */
    printf("case6: %d\n", sum2((struct P){1, 2}, (struct P){3, 4}));

    /* case7: const compound literal */
    {
        const int ci = (const int){42};
        printf("case7: %d\n", ci);
    }

    /* case8: sizeof an array compound literal */
    printf("case8: %d\n", (int)sizeof((int[]){1, 2, 3}));

    return 0;
}
