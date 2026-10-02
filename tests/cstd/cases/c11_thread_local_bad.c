/* ============================================================
   c11_thread_local_bad.c - block-scope non-static _Thread_local (rejection class)
   Standard   : ISO/IEC 9899:2011 (C11) 6.7.1
   Strategy   : gcc -std=c11 rejects ("function-scope ... implicitly auto and
                declared '_Thread_local'"); goc must reject cleanly instead of
                panicking (P0.2). Two spellings (_Thread_local keyword and the
                thread_local macro, mirroring threads.h) plus an address-of, all
                at block scope without static/extern.
   Status     : PASS (rejection class) (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#define thread_local _Thread_local

int main(void) {
    _Thread_local int a = 5;     /* bad: block scope, no static/extern */
    thread_local int b = 6;      /* bad: thread_local macro spelling */
    _Thread_local int *p = &a;   /* bad: address-of variant */
    return a + b + *p;
}
