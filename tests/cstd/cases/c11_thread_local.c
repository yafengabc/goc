/* ============================================================
   c11_thread_local.c - _Thread_local / thread_local storage class
   Standard   : ISO/IEC 9899:2011 (C11) 6.11.5 (threads.h defines the macro)
   Strategy   : 5 subcases (global _Thread_local, global thread_local macro,
               static function-local TLS, cross-function read, address-of,
               mutation). thread_local is mapped to the keyword because this gcc
               lacks threads.h. gcc -std=c11 diff
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#include <stdio.h>

/* mirrors <threads.h>, which is unavailable on this host */
#define thread_local _Thread_local

_Thread_local int glb = 11;
thread_local static int st = 22;

static int read_st(void) { return st; }
static int read_glb(void) { return glb; }

int main(void) {
    static _Thread_local int sfunc = 44;   /* static function-local TLS */
    printf("case1: glb=%d st=%d\n", read_glb(), read_st());
    printf("case2: sfunc=%d\n", sfunc);
    int *p = &glb;
    printf("case3: addr-of *p=%d\n", *p);
    glb = 55;
    printf("case4: mutated=%d\n", read_glb());
    printf("case5: st_read=%d\n", read_st());
    return 0;
}