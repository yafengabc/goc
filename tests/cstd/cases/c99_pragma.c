/* ============================================================
   c99_pragma.c - _Pragma operator (C99 6.10.9)
   Standard   : ISO/IEC 9899:1999 (C99) 6.10.9
   Strategy   : gcc emits a compile-time message and compiles; goc
                rejects the _Pragma token (not implemented).
   Status     : UNSUPPORTED (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
_Pragma("message(\"pragma probe: gcc sees this at compile time\")")
int main(void) {
    printf("case1: pragma probe ran\n");
    return 0;
}