/* A conversion's length modifier is part of its contract with the caller, not
 * decoration: `%d`, `%ld` and `%lld` name arguments of three different widths,
 * and a formatter that reads one width for all of them is right only for
 * whichever one it happened to pick.
 *
 * The two failure modes are mirror images and both have actually happened
 * here:
 *
 *   reading `long` for every conversion -- harmless-looking on x86-64, where
 *   an int still arrives in an eight-byte slot -- reads eight bytes out of a
 *   four-byte slot on a 32-bit target, so printf("%d", 0) printed 4294967296.
 *
 *   reading `int` for every conversion -- correct for %d -- keeps only the low
 *   half of a %lld argument, so printf("%lld", 1LL << 52) printed 0.
 *
 * So this case pins all of them at once, including the signedness split
 * (%u/%lu/%llu must not print a negative number) and the two promoted widths
 * (%hd/%hhd are passed as int and must be read as int). */
#include <stdio.h>

int main(void) {
    printf("d=%d ld=%ld lld=%lld\n", -42, -43L, -44LL);
    printf("u=%u lu=%lu llu=%llu\n", 42u, 43uL, 44uLL);
    printf("x=%x lx=%lx llx=%llx\n", 0x12ab, 0x12abUL, 0x12abULL);
    printf("X=%X lX=%lX llX=%llX\n", 0x12ab, 0x12abUL, 0x12abULL);
    printf("o=%o lo=%lo llo=%llo\n", 777, 777UL, 777ULL);
    printf("hd=%hd hhd=%hhd hu=%hu\n", (short)-7, (signed char)-8, (unsigned short)9);
    /* %zd/%zu name size_t and ptrdiff_t, so the arguments are declared with
     * exactly those types rather than as `long`: the two hosts size `long`
     * differently (MinGW keeps it 32-bit, goc makes it 64-bit), and a cast
     * that happened to match one would be undefined on the other. */
    ptrdiff_t negoff = -9;
    size_t len = 9;
    printf("zd=%zd zu=%zu\n", negoff, len);
    printf("jd=%jd ju=%ju\n", -10LL, 10ULL);
    printf("min=%lld max=%llu\n", -9223372036854775807LL - 1, 18446744073709551615ULL);
    printf("shift=%lld shiftu=%llu\n", 1LL << 52, 1ULL << 63);
    printf("mixed=%d/%ld/%lld/%s\n", 1, 2L, 3LL, "four");
    return 0;
}
