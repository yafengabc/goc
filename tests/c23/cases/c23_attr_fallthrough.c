/* C23 feature: attribute [[fallthrough]]
 * Clause:     C23 6.7.13 (fallthrough attribute)
 * Strategy:   (1) a case that falls through with NO attribute -> gcc
 *             -Wimplicit-fallthrough warning; (2) a case that falls through WITH
 *             [[fallthrough]]; -> clean; (3) [[fallthrough]] placed on the last
 *             case -> record gcc's diagnostic. gcc still compiles all three. The
 *             outside-switch usage is a separate REJECT file (gcc hard-errors).
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>

/* (1) implicit fallthrough, no attribute: gcc warns */
int f_no_attr(int x) {
    int r = 0;
    switch (x) {
        case 1:
            r = 10;
        case 2:
            r += 20;
            break;
        default:
            r = 99;
    }
    return r;
}

/* (2) explicit [[fallthrough]]: gcc clean */
int f_with_attr(int x) {
    int r = 0;
    switch (x) {
        case 1:
            r = 10;
            [[fallthrough]];
        case 2:
            r += 20;
            break;
        default:
            r = 99;
    }
    return r;
}

/* (3) [[fallthrough]] as the last statement (no following label): record gcc diag */
int f_last(int x) {
    int r = 0;
    switch (x) {
        case 1:
            r = 10;
            break;
        case 2:
            r = 20;
            [[fallthrough]];
    }
    return r;
}

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int a = f_no_attr(1);   /* 10 then fall to case2 -> 30 */
    printf("case1: f_no_attr(1)=%d (implicit fallthrough, gcc warns)\n", a);
    if (a == 30) passed++;

    ++total;
    int b = f_with_attr(1); /* 10 then [[fallthrough]] to case2 -> 30 */
    printf("case2: f_with_attr(1)=%d (explicit fallthrough, gcc clean)\n", b);
    if (b == 30) passed++;

    ++total;
    int c = f_last(2);      /* case2, r=20 */
    printf("case3: f_last(2)=%d (fallthrough on last case; record gcc diag)\n", c);
    if (c == 20) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
