/* ============================================================
   c89_lit_esc.c - octal \ooo and hex \xhh escapes: goc rejects at preprocess time
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.3.4 escape sequences, 6.1.3.5 string literals
   Strategy   : case1 octal char escape '\101' = 'A'; case2 hex char escape '\x41' = 'A';
                case3 boundary '\0' NUL and '\377' and '\x7f'; case4 octal escape
                inside a string literal.  gcc decodes all; goc aborts the whole file
                with a preprocess error.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

int main(void) {
    printf("case1: octal-escape=%d\n", '\101');
    printf("case2: hex-escape=%d\n", '\x41');
    printf("case3: nul=%d oct377=%d hex7f=%d\n", '\0', '\377', '\x7f');
    {
        char s[] = "a\101b";
        printf("case4: string-escape chars=%d,%d\n", s[1], (int)sizeof(s));
    }
    return 0;
}
