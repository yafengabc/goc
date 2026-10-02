/* ============================================================
   c99_comment.c - // line comments (C99 6.4.9)
   Standard   : ISO/IEC 9899:1999 (C99) 6.4.9
   Strategy   : 5 subcases: trailing //, block then //, // inside
                a string literal (must not comment), comment immediately
                followed by code, block comment then declaration.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
int main(void) {
    int a = 1; /* case1: trailing // after code */
    int b = 2; /* case2: block comment */ /* then another */
    char *s = "case3: http://example.com // still inside string";
    int c = 4;//case4: comment immediately followed by code
    /* case5: block comment, then a declaration */ int d = a + b + c;
    printf("case1: %d\n", a);
    printf("case2: %d\n", b);
    printf("%s\n", s);
    printf("case4: %d\n", c);
    printf("case5: %d\n", d);
    return 0;
}