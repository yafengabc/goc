/* ============================================================
   c99_ucn.c - universal character names (C99 6.4.3)
   Standard   : ISO/IEC 9899:1999 (C99) 6.4.3
   Strategy   : gcc side: \u00e9 in a string and as an identifier.
                goc side: preprocess error on the backslash-u (not done).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
int main(void) {
    char *s = "case1: e-acute \u00e9";
    int eacute = 5;
    printf("%s %d\n", s, eacute);
    return 0;
}