/* ============================================================
   c89_lit_str.c - string literals: adjacent concat, escapes, empty, sizeof, indexing
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.3.5 string literals
   Strategy   : case1 adjacent concatenation; case2 escapes inside string;
                case3 empty string; case4 sizeof("abc") includes NUL;
                case5 s[1] indexing; case6 char[] vs char* sizeof difference
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

int main(void) {
    printf("case1: concat=[%s]\n", "a" "b" "c");
    printf("case2: escape=[%s]\n", "tab\there");
    printf("case3: empty=[%s] sizeof=%d\n", "", (int)sizeof(""));
    printf("case4: sizeof-abc=%d\n", (int)sizeof("abc"));
    {
        char s[] = "abc";
        const char *p = "abc";
        printf("case5: s[1]=%c\n", s[1]);
        printf("case6: sizeof-array=%d sizeof-pointer=%d\n",
               (int)sizeof(s), (int)sizeof(p));
    }
    return 0;
}
