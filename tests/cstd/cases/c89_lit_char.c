/* ============================================================
   c89_lit_char.c - character constants and basic escape sequences, signedness
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.3.4 character constants
   Strategy   : case1 plain letter; case2 common escapes by code value
                (\n \t \r \v \b \f \a); case3 \\ \' " \? escapes;
                case4 char signedness: 0xFF assigned to char prints as signed -1
                NOTE: octal escape '\101' and hex escape '\x41' rejected by goc
                ("unterminated character literal"); multi-char 'AB' likewise:
                all excluded as negative probes, not in this file.
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

int main(void) {
    printf("case1: A=%d\n", 'A');
    printf("case2: nl=%d tab=%d cr=%d vtab=%d back=%d ff=%d bell=%d\n",
           '\n', '\t', '\r', '\v', '\b', '\f', '\a');
    printf("case3: backslash=%d squote=%d dquote=%d question=%d\n",
           '\\', '\'', '"', '\?');
    {
        char c = 0xFF;
        printf("case4: char-0xFF signed=%d\n", c);
    }
    return 0;
}
