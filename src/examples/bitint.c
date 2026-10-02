#include <stdio.h>
#include <stdlib.h>
#include <bitint.h>

/* C23 _BitInt(N): arbitrary-width integers. goc stores a _BitInt(N) as
 * ceil(N/64) little-endian 64-bit words and lowers every operation to the
 * goclib big-integer helpers (schoolbook + Karatsuba multiplication, Knuth D
 * division, decimal printing). This example exercises the core paths and
 * cross-checks a few wide values; the wide lines were verified against
 * Python's arbitrary-precision integers. */
typedef unsigned _BitInt(128) U128;
typedef unsigned _BitInt(512) U512;
typedef unsigned _BitInt(640) U640;
typedef unsigned _BitInt(768) U768;
typedef unsigned _BitInt(1024) U1024;
typedef signed _BitInt(640) S640;

static char *gbs(unsigned long long *v, long n) {
    char *b = (char *)malloc(4096);
    long L = __goclib_bi_str(b, v, n, 0);
    b[L] = 0;
    return b;
}

int main(void) {
    /* 128-bit: (2^127 - 1) * 3 wraps mod 2^128 */
    U128 m = (U128)0 - 1;              /* 2^128 - 1 */
    m = m / 2;                         /* 2^127 - 1 */
    m = m * 3;
    char *s = gbs((unsigned long long *)&m, 2);
    printf("m128=%s\n", s);
    free((void *)s);

    /* 640-bit negative term (wrapped), sign-extended into a 768-bit multiply:
     * (2^640 - 2793657715) * 8 mod 2^768 -- the modular value of
     * -2793657715*8 mod 2^768. */
    U640 t = 2793657715ULL;
    t = 0 - t;
    U768 w = (U768)(S640)t;            /* sign-extend, not zero-extend */
    U768 p = w * 8;
    s = gbs((unsigned long long *)&p, 12);
    printf("neg8=%s\n", s);
    free((void *)s);

    /* 1024-bit: (2^1024 - 1) / 3 and the remainder (both = 6148914691236517205) */
    U1024 all = (U1024)0 - 1;
    U1024 q = all / 3;
    U1024 r = all % 3;
    s = gbs((unsigned long long *)&q, 16);
    printf("div3q=%s\n", s);
    free((void *)s);
    s = gbs((unsigned long long *)&r, 16);
    printf("div3r=%s\n", s);
    free((void *)s);

    /* shift: 1 << 1000 has 301 decimal digits (10^300 <= 2^1000 < 10^301);
     * shifting back right recovers 1. */
    U1024 sh = 1;
    sh = sh << 1000;
    sh = sh >> 999;
    s = gbs((unsigned long long *)&sh, 16);
    printf("sh2=%s\n", s);
    free((void *)s);

    /* 512-bit square truncation: (2^256 - 1)^2 mod 2^512 = 2^512 - 2^257 + 1 */
    U512 x = 1;
    x = x << 256;
    x = x - 1;
    U512 sq = x * x;
    s = gbs((unsigned long long *)&sq, 8);
    printf("sq512=%s\n", s);
    free((void *)s);
    return 0;
}
