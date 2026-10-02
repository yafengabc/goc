/* ============================================================
   c99_mixdecl.c - declarations mixed with statements (C99 6.8)
   Standard   : ISO/IEC 9899:1999 (C99) 6.8, 6.8.5
   Strategy   : 4 subcases: statement then declaration, for(int i=),
                declaration in nested block, declaration after a block.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
int main(void) {
    printf("case1: before decl\n");
    int x = 10;
    for (int i = 0; i < 3; i++) {
        int j = i * 2;
        printf("case2: %d\n", j);
    }
    {
        int k = x + 1;
        printf("case3: %d\n", k);
    }
    int z = x * 2;
    printf("case4: %d\n", z);
    return 0;
}