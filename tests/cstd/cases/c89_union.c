/* ============================================================
   c89_union.c - C89 unions: overlap, first-member init, struct of union
   Standard   : ISO/IEC 9899:1990 (C89) 6.5.2.3 structure/union specifiers
   Strategy   : 5 subcases: union member overlap, initialize first member,
                one member aliases another (int overlaps unsigned), whole-
                union assignment, union nested in struct, sizeof.  Members
                are same-width (int/unsigned) so sizeof is identical on goc
                and gcc; a long member would differ (LP64 vs LLP64).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

union U {
    int i;
    unsigned u;
};

struct W {
    int tag;
    union U u;
};

int main(void) {
    union U u;
    struct W w;

    /* case1: initialize first member; sizeof(union) = member size */
    u.i = 0x01020304;
    printf("case1: %d %d\n", u.i, (int)sizeof(union U));

    /* case2: write through unsigned member, observe aliased int value */
    u.u = 0xFFFFFFFFU;
    printf("case2: %d\n", u.i);

    /* case3: whole-union assignment */
    {
        union U u2;
        u2 = u;
        printf("case3: %d\n", u2.i);
    }

    /* case4: union nested inside a struct */
    w.tag = 1;
    w.u.i = 42;
    printf("case4: %d %d\n", w.tag, w.u.i);

    /* case5: sizeof of struct containing a union */
    printf("case5: %d\n", (int)sizeof(struct W));

    return 0;
}
