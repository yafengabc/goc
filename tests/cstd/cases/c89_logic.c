/* ============================================================
   c89_logic.c - C89 relational/equality/logical operators & short-circuit
   Standard   : ISO/IEC 9899:1990 (C89) 6.3.3 relational, 6.3.3.3 equality,
                6.3.13 logical &&, 6.3.14 logical ||, 6.3.3.3 unary !
   Strategy   : 5 subcases: relational yields 0/1, equality, && short-circuit
                (right side not evaluated), || short-circuit, ! truth negation
                and ! vs == precedence.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int side;
int touch(void) { side = side + 1; return side; }

int main(void) {
    int a = 5;
    int b = 5;
    int c = 0;

    /* case1: relational operators yield 0/1 */
    printf("case1: %d %d %d %d\n", a < b, a <= b, a > b, a >= b);

    /* case2: equality operators */
    printf("case2: %d %d\n", a == b, a != b);

    /* case3: && short-circuits: right side not evaluated when left is false */
    side = 0;
    if (c && touch()) { printf("case3: yes\n"); } else { printf("case3: no\n"); }
    printf("case3: side=%d\n", side);

    /* case4: || short-circuits: right side not evaluated when left is true */
    side = 0;
    if (a || touch()) { printf("case4: yes\n"); } else { printf("case4: no\n"); }
    printf("case4: side=%d\n", side);

    /* case5: ! on scalars; ! binds tighter than == */
    printf("case5: %d %d %d\n", !c, !a, (!a == 0) ? 1 : 0);

    return 0;
}
