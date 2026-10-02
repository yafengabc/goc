/* ============================================================
   c17_smoke.c - C17 regression smoke (C17 is a defect report, no new feature)
   Standard   : ISO/IEC 9899:2017 (C17)
   Strategy   : one quick group each of _Generic, _Static_assert, _Alignas,
               anonymous member, thread_local. thread_local is mapped to the
               keyword because this gcc lacks threads.h. gcc -std=c17 diff.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c17)
   ============================================================ */
#include <stdio.h>
#include <stddef.h>

#define thread_local _Thread_local

_Static_assert(1 == 1, "c17 smoke");
thread_local int tls = 7;

struct Smoke {
    int tag;
    struct { int x; };
    double d;
};

int main(void) {
    printf("case1: generic=%d\n", _Generic(1, int:1, default:0));
    printf("case2: static_assert ok\n");
    _Alignas(16) int a;
    printf("case3: align=%d\n", (int)_Alignof(a));
    struct Smoke v = {1, 2, 3.0};
    printf("case4: anon tag=%d x=%d d=%.1f\n", v.tag, v.x, v.d);
    printf("case5: tls=%d\n", tls);
    return 0;
}