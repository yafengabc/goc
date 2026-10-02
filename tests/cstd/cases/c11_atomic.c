/* ============================================================
   c11_atomic.c - <stdatomic.h>, atomic_int, atomic_store/load
   Standard   : ISO/IEC 9899:2011 (C11) 7.17
   Strategy   : UNSUPPORTED in goc: stdatomic.h is skipped (note on stderr),
               atomic_int / atomic_store / atomic_load undefined.
               The bare `_Atomic int x;` syntax behaviour is a separate probe.
               gcc -std=c11 compiles and returns 0.
   Status     : UNSUPPORTED (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#include <stdatomic.h>

int main(void) {
    atomic_int a = 0;
    atomic_store(&a, 5);
    int v = atomic_load(&a);
    return v == 5 ? 0 : 1;
}