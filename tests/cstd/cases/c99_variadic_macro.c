/* ============================================================
   c99_variadic_macro.c - __VA_ARGS__ variadic macros (C99 6.10.3)
   Standard   : ISO/IEC 9899:1999 (C99) 6.10.3
   Strategy   : 4 subcases: __VA_ARGS__ with 2 args, with 3 args,
                forwarded into printf, macro-to-macro forwarding.
                Note: GNU ##__VA_ARGS__ empty-tail extension is rejected
                by goc (see .tmp probe, cross-findings).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
static int add2(int a, int b) { return a + b; }
static int add3(int a, int b, int c) { return a + b + c; }
#define ADD(...) add2(__VA_ARGS__)
#define ADD3(...) add3(__VA_ARGS__)
#define SHOW(fmt, ...) printf(fmt, __VA_ARGS__)
#define FWD(...) SHOW2(__VA_ARGS__)
static void SHOW2(const char *s) { printf("fwd:%s\n", s); }
int main(void) {
    printf("case1: %d\n", ADD(1, 2));
    printf("case2: %d\n", ADD3(1, 2, 3));
    SHOW("case3: %d %d\n", 7, 8);
    FWD("case4: hello");
    return 0;
}