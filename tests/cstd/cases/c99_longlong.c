/* ============================================================
   c99_longlong.c - long long / unsigned long long (C99 6.2.5, 6.4.4.1)
   Standard   : ISO/IEC 9899:1999 (C99) 6.2.5, 7.18.2
   Strategy   : 5 subcases: LL literal, ULL literal + %llx, signed
                wrap past LLONG_MAX, unsigned wrap to max, LLONG_MIN/MAX.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
#include <stdio.h>
#include <limits.h>
int main(void) {
    long long a = 1234567890123LL;
    unsigned long long b = 1234567890123ULL;
    printf("case1: %lld\n", a);
    printf("case2: %llu %llx\n", b, b);
    long long ov = 9223372036854775807LL; ov = ov + 1;
    printf("case3: %lld\n", ov);
    unsigned long long u = 0ULL; u = u - 1;
    printf("case4: %llu\n", u);
    printf("case5: %lld %lld\n", LLONG_MAX, LLONG_MIN);
    return 0;
}