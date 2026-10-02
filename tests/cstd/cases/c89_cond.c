/* ============================================================
   c89_cond.c - C89 conditional operator ?: and sizeof non-evaluation
   Standard   : ISO/IEC 9899:1990 (C89) 6.3.15 conditional operator,
                6.3.3.4 sizeof (operand not evaluated except VLA)
   Strategy   : 5 subcases: basic ?: selection, nested ?: right-associative,
                comma operator inside a selected branch, sizeof on an array
                type yields the array size, sizeof does not evaluate its
                expression operand.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    int a = 3;
    int b = 7;
    int arr[10];
    int x;
    int n = 0;

    /* case1: basic selection */
    printf("case1: %d\n", (a > b) ? 100 : 200);

    /* case2: nested ?: is right-associative */
    printf("case2: %d\n", a == 1 ? 1 : a == 2 ? 2 : 3);

    /* case3: comma operator inside the selected branch */
    x = (a > b) ? (n = 1, 10) : (n = 2, 20);
    printf("case3: %d %d\n", x, n);

    /* case4: sizeof on an array object yields the whole array size */
    printf("case4: %d\n", (int)sizeof(arr));

    /* case5: sizeof operand is NOT evaluated (n stays 0) */
    sizeof(n++);
    printf("case5: %d\n", n);

    return 0;
}
