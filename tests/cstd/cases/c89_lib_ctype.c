/* ============================================================
   c89_lib_ctype.c - 12 classification predicates + tolower/toupper, EOF and edge chars
   Standard   : ISO/IEC 9899:1990 (C89) 7.4 character classification <ctype.h>
   Strategy   : case1 classification bitmask for '0','A','a',' ','\t','\n';
                case2 tolower/toupper mapping; case3 tolower(EOF) behavior
                mask bit layout: 1 isalnum 2 isalpha 4 iscntrl 8 isdigit
                16 isgraph 32 islower 64 isprint 128 ispunct 256 isspace
                512 isupper 1024 isxdigit
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>
#include <ctype.h>

static int mask_of(int c) {
    int m = 0;
    if (isalnum(c)) m |= 1;
    if (isalpha(c)) m |= 2;
    if (iscntrl(c)) m |= 4;
    if (isdigit(c)) m |= 8;
    if (isgraph(c)) m |= 16;
    if (islower(c)) m |= 32;
    if (isprint(c)) m |= 64;
    if (ispunct(c)) m |= 128;
    if (isspace(c)) m |= 256;
    if (isupper(c)) m |= 512;
    if (isxdigit(c)) m |= 1024;
    return m;
}

int main(void) {
    printf("case1: '0'=%d 'A'=%d 'a'=%d sp=%d tab=%d nl=%d\n",
           mask_of('0'), mask_of('A'), mask_of('a'),
           mask_of(' '), mask_of('\t'), mask_of('\n'));
    printf("case2: lower(A)=%c upper(a)=%c\n",
           (char)tolower('A'), (char)toupper('a'));
    printf("case3: tolower(EOF)=%d toupper(EOF)=%d\n",
           tolower(EOF), toupper(EOF));
    return 0;
}
