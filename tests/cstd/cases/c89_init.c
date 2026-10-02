/* ============================================================
   c89_init.c - C89 initializers: scalar/array/struct/union, partial zero-fill
   Standard   : ISO/IEC 9899:1990 (C89) 6.5.7 initialization
   Strategy   : 6 subcases: scalar and array init, partial array rest zero-
                filled, nested struct init, string literal init of a char
                array, static constant-expression init, excess initializer
                (gcc warns but runs; goc accepts - recorded).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int g_static = 5 + 3;           /* static init must be a constant expression */
int g_partial[4] = { 1, 2 };   /* remaining elements zero */

struct Pt { int x; int y; };
struct Box { struct Pt p; int w; };

int main(void) {
    int sc = 10;
    int arr[3] = { 10, 20, 30 };
    int partial[4] = { 1, 2 };
    struct Pt sp = { 3, 4 };
    struct Box box = { { 5, 6 }, 7 };
    char str[8] = "abc";
    int excess[2] = { 1, 2, 3 };   /* excess element: gcc warns, goc accepts */

    /* case1: scalar and array initialization */
    printf("case1: %d %d %d %d\n", sc, arr[0], arr[1], arr[2]);

    /* case2: partial array, remaining elements zero-filled */
    printf("case2: %d %d %d %d\n", partial[0], partial[1], partial[2], partial[3]);

    /* case3: nested struct initialization */
    printf("case3: %d %d %d\n", box.p.x, box.p.y, box.w);

    /* case4: string literal initializes a char array (NUL-terminated) */
    printf("case4: %d %d %d %d %d\n", str[0], str[1], str[2], str[3], (int)sizeof(str));

    /* case5: static global constant-expression init and partial global */
    printf("case5: %d %d\n", g_static, g_partial[2]);

    /* case6: excess initializer: extra element dropped; first two kept */
    printf("case6: %d %d\n", excess[0], excess[1]);

    return 0;
}
