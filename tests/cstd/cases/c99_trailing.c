/* ============================================================
   c99_trailing.c - trailing comma in enumerator / initializer lists
   Standard   : ISO/IEC 9899:1999 (C99) 6.7.2.2, 6.7.8
   Strategy   : 3 subcases: enum trailing comma, array initializer
                trailing comma, printf output.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
enum E { A = 1, B = 2, };
int main(void) {
    int a[3] = {1, 2, 3,};
    printf("case1: %d %d\n", A, B);
    printf("case2: %d\n", a[2]);
    printf("case3: ok\n");
    return 0;
}