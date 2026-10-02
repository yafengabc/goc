/* Integer literal width is part of the constant's type, and dropping it is
 * silently wrong: `1 << 52` is zero because the shift happens in 32 bits,
 * while `1LL << 52` is 2^52. The u/U suffix matters just as much -- it makes
 * the comparison unsigned, so -1 is the largest value rather than the
 * smallest. */
#include <stdio.h>

int main() {
    long long a = 1LL << 52;
    long long b = 1LL << 40;
    printf("1LL<<52=%lld\n", a);
    printf("1LL<<40=%lld\n", b);
    printf("1LL<<31=%lld\n", 1LL << 31);
    printf("1<<31=%d\n", 1 << 31);

    /* A plain int constant shifts in 32 bits: 1<<32 is 0, not 2^32. The
     * unsigned suffix does not widen it either -- 1u is still 32 bits. */
    printf("1<<32=%d\n", 1 << 32);
    printf("1u<<31=%u\n", 1u << 31);
    printf("1ULL<<32=%llu\n", 1ULL << 32);

    /* Unsigned suffix flips the ordering of negative values. */
    printf("-1 < 1U: %d\n", (-1 < 1u) ? 1 : 0);
    printf("-1 < 1: %d\n", (-1 < 1) ? 1 : 0);

    /* Writing a negative int through a pointer must read back negative:
     * the store is 4 bytes wide, so the load has to sign-extend. */
    {
        int x = 0;
        int *p = &x;
        *p = -23;
        printf("via ptr=%d\n", x);
        printf("via ptr as double=%.0f\n", (double)x);
    }

    /* 64-bit literals keep every bit: 2^63 is the int64 minimum. */
    printf("min=%lld\n", -9223372036854775807LL - 1);
    printf("hex=%lld\n", 0x8000000000000000ULL >> 60);
    return 0;
}
