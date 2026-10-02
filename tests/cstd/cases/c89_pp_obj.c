/* ============================================================
   c89_pp_obj.c - object-like macros, undef, empty body, predefined macros
   Standard   : ISO/IEC 9899:1990 (C89) 6.8 macro replacement, 6.8.9 control-line directives
   Strategy   : case1 object macro expansion; case2 empty macro body; case3 undef+redefine;
                case4 identical redefinition (gcc accepts); case5 self reference is non recursive;
                case6 __FILE__/__LINE__; case7 definedness probes of __STDC__/__DATE__/__TIME__
                each case printf distinct, gcc -std=c89 diff
   Status     : PARTIAL: case1-6 match gcc; case7 goc has no __STDC__/__DATE__/__TIME__ (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

#define N 100

#define EMPTY_BODY
int varEMPTY_BODY = 5;            /* expands to "int var_ = 5;" */

#define M 1
#undef M
#define M 2

#define R 7
#define R 7                          /* identical redefinition, legal */

int selfref = 11;
#define selfref selfref              /* expansion of self stops, identifier resolves */

int main(void) {
    printf("case1: N+1=%d\n", N + 1);
    printf("case2: empty-body=%d\n", varEMPTY_BODY);
    printf("case3: after-undef-redef M=%d\n", M);
    printf("case4: identical-redef R=%d\n", R);
    printf("case5: non-recursive selfref=%d\n", selfref);
    printf("case6: file=[%s] line=%d\n", __FILE__, __LINE__);
#ifdef __STDC__
    printf("case7: __STDC__=defined\n");
#else
    printf("case7: __STDC__=NOT-defined\n");
#endif
#ifdef __DATE__
    printf("case7: __DATE__=defined\n");
#else
    printf("case7: __DATE__=NOT-defined\n");
#endif
#ifdef __TIME__
    printf("case7: __TIME__=defined\n");
#else
    printf("case7: __TIME__=NOT-defined\n");
#endif
    return 0;
}
