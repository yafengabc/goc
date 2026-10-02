/* ============================================================
   c11_generic.c - _Generic type-generic selection
   Standard   : ISO/IEC 9899:2011 (C11) 6.5.1.1
   Strategy   : 6 subcases (exact match, unevaluated ctrl expr, default,
               char-literal vs lvalue promotion trap, typedef+const, nested)
               distinguishable printf; gcc -std=c11 diff
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#include <stdio.h>

typedef const int cint;

int main(void) {
    /* case1: exact type match */
    printf("case1: exact=%d\n", _Generic(1, int:1, float:2, double:3, default:0));

    /* case2: controlling expression is unevaluated (x=5 must not run) */
    int x = 0;
    int g = _Generic((x = 5), int:1, default:0);
    printf("case2: g=%d x=%d\n", g, x);

    /* case3: default branch */
    printf("case3: default=%d\n", _Generic(3.14, int:1, default:99));

    /* case4: promotion trap. char lvalue keeps type char;
       the character literal 'a' is already int in C. */
    char ch = 'x';
    printf("case4: lvalue_char=%d lit_int=%d\n",
           _Generic(ch, int:100, char:200, default:0),
           _Generic('a', int:100, char:200, default:0));

    /* case5: typedef + const; top-level qualifier ignored on matching */
    printf("case5: qual=%d\n", _Generic((cint)0, int:1, long:2, default:0));

    /* case6: nested _Generic */
    printf("case6: nested=%d\n",
           _Generic(1, int:_Generic(2, int:10, default:0), default:-1));
    return 0;
}