/* ============================================================
   c89_control.c - C89 flow control: if/else, switch, loops, break/continue, goto
   Standard   : ISO/IEC 9899:1990 (C89) 6.6.4 selection, 6.6.5 iteration,
                6.6.6 goto/continue/break
   Strategy   : 7 subcases: if/else, switch fall-through with default last,
                while sum, do-while runs at least once, for with break/continue,
                goto backward over a label, goto forward over a skipped block,
                void function early return.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

void vf(int x) {
    if (x > 0) {
        printf("  vf pos %d\n", x);
        return;          /* early return from a void function */
    }
    printf("  vf nonpos\n");
}

int main(void) {
    int i;
    int s;

    /* case1: if/else */
    i = 3;
    if (i > 0) printf("case1: pos\n"); else printf("case1: nonpos\n");

    /* case2: switch with fall-through and default last */
    s = 2;
    switch (s) {
        case 1: printf("case2: one\n");
        case 2: printf("case2: two\n");   /* falls through to case 3 */
        case 3: printf("case2: three\n"); break;
        default: printf("case2: def\n"); break;
    }

    /* case3: while loop summing 1..3 */
    s = 0;
    i = 1;
    while (i <= 3) {
        s = s + i;
        i = i + 1;
    }
    printf("case3: %d\n", s);

    /* case4: do-while runs the body at least once */
    i = 0;
    do { i = i + 1; } while (i < 3);
    printf("case4: %d\n", i);

    /* case5: for loop with continue and break */
    s = 0;
    for (i = 1; i <= 5; i = i + 1) {
        if (i == 2) continue;
        if (i == 4) break;
        s = s + i;
    }
    printf("case5: %d\n", s);   /* 1 + 3 */

    /* case6: goto backward over a label */
    i = 0;
back:
    printf("case6: %d ", i);
    i = i + 1;
    if (i < 3) goto back;
    printf("\n");

    /* case7: goto forward skips a block; then void function return */
    goto skip;
    printf("case7: skipped\n");
skip:
    printf("case7: jumped\n");
    vf(5);
    vf(-1);

    return 0;
}
