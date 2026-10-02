/* ============================================================
   c89_pp_func.c - function-like macros, # stringize, ## paste, prescan, nesting
   Standard   : ISO/IEC 9899:1990 (C89) 6.8.3 replacement, # and ## operators
   Strategy   : case1 # stringize (plain and of expression); case2 ## token paste
                (identifier and numeric); case3 empty-argument paste; case4 argument prescan;
                case4b # suppresses prescan; case5 one-hop forwarding nesting
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

#define STR(x) #x
#define GLUE(a, b) a##b
#define GLUE2(a, b) a ## b
#define DOUBLE(x) ((x) + (x))
#define OUTER(x) INNER(x)
#define INNER(x) ((x) + 1)
#define A 10
#define B 20

int main(void) {
    int GLUE(left, right) = 3;
    int GLUE(, tail) = 8;

    printf("case1: str=[%s]\n", STR(hello world));
    printf("case1b: str-expr=[%s]\n", STR(1 + 1));
    printf("case2: glued-id=%d\n", leftright);
    printf("case2b: glued-num=%d\n", GLUE2(1, 2));
    printf("case3: empty-arg-paste=%d\n", tail);
    printf("case4: prescan=%d\n", DOUBLE(A + B));
    printf("case4b: stringize-no-prescan=[%s]\n", STR(A));
    printf("case5: nested-forward=%d\n", OUTER(4));
    return 0;
}
