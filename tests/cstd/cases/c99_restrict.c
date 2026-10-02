/* ============================================================
   c99_restrict.c - restrict-qualified pointers (C99 6.7.3.1)
   Standard   : ISO/IEC 9899:1999 (C99) 6.7.3.1
   Strategy   : 2 subcases: restrict pointer parameters in a loop,
                restrict local pointer declaration. Behavior only.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
static void add(int *restrict p, int *restrict q, int n) {
    while (n-- > 0) *p++ += *q++;
}
int main(void) {
    int a[4] = {1, 2, 3, 4};
    int b[4] = {10, 20, 30, 40};
    add(a, b, 4);
    printf("case1: %d %d %d %d\n", a[0], a[1], a[2], a[3]);
    int x = 5;
    int *restrict r = &x;
    *r = 42;
    printf("case2: %d\n", x);
    return 0;
}