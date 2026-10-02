/* ============================================================
   c99_funcname.c - __func__ predefined identifier (C99 6.4.2.2)
   Standard   : ISO/IEC 9899:1999 (C99) 6.4.2.2
   Strategy   : 3 functions each print __func__. gcc defines it; goc
                reports undeclared identifier (real gap).
   Status     : FAIL (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
void fa(void) { printf("fa: %s\n", __func__); }
void fb(void) { printf("fb: %s\n", __func__); }
int main(void) {
    printf("main: %s\n", __func__);
    fa();
    fb();
    return 0;
}