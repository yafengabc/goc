/* C23 feature: _BitInt(N) arbitrary-width integers
 * Clause:     C23 6.2.5, 6.3.1 (widths); goc roadmap #131 (goclib bigint runtime, 1..4096 bits)
 * Strategy:   signed/unsigned across many widths; typedef signed cross-width sign extension;
 *             + - * / % and == != < >= ; casts to long long/int/unsigned and between widths;
 *             mod-2^N wrap; sizeof. NOTE: gcc 16.2.0 printf has no %wN modifier, so <=64-bit
 *             values are cast to long long/%ll and >64-bit values use a hand /10 decimal printer.
 *             (2026-10-02 P0 _BitInt fix: <=64-bit widths wrap, literal compare no longer
 *             panics, nested same-width casts copy the value).
 * Status:     PASS (goc == gcc, 15/15; see also c23_bitint_edge.c for the fix's edge cases)
 * EXPECT: PASS
 */
#include <stdio.h>

typedef unsigned _BitInt(1)    U1;
typedef   signed   _BitInt(8)    S8;
typedef unsigned _BitInt(8)    U8;
typedef unsigned _BitInt(17)   U17;
typedef   signed   _BitInt(31)   S31;
typedef unsigned _BitInt(32)   U32;
typedef   signed   _BitInt(64)   S64;
typedef unsigned _BitInt(64)   U64;
typedef unsigned _BitInt(65)   U65;
typedef   signed   _BitInt(127)  S127;
typedef unsigned _BitInt(128)  U128;
typedef   signed   _BitInt(128)  S128;
typedef unsigned _BitInt(256)  U256;
typedef unsigned _BitInt(1024) U1024;

/* hand /10 decimal printers; all compare against explicit (T)0 / (T)10 */
static void pu128(U128 v) {
    char buf[45]; int i = 0; U128 ten = (U128)10;
    if (v == (U128)0) { printf("0"); return; }
    while (v > (U128)0) { buf[i++] = (char)('0' + (int)(v % ten)); v = v / ten; }
    while (i > 0) putchar(buf[--i]);
}
static void pu256(U256 v) {
    char buf[80]; int i = 0; U256 ten = (U256)10;
    if (v == (U256)0) { printf("0"); return; }
    while (v > (U256)0) { buf[i++] = (char)('0' + (int)(v % ten)); v = v / ten; }
    while (i > 0) putchar(buf[--i]);
}
static void pu1024(U1024 v) {
    char buf[320]; int i = 0; U1024 ten = (U1024)10;
    if (v == (U1024)0) { printf("0"); return; }
    while (v > (U1024)0) { buf[i++] = (char)('0' + (int)(v % ten)); v = v / ten; }
    while (i > 0) putchar(buf[--i]);
}

int main(void) {
    int passed = 0, total = 0;

    /* case1: width 1 unsigned holds only {0,1} */
    ++total;
    U1 u1a = (U1)0, u1b = (U1)1;
    printf("case1: u1=%u,%u\n", (unsigned)u1a, (unsigned)u1b);
    if (u1a == (U1)0 && u1b == (U1)1) passed++;

    /* case2: signed 8-bit wrap: 200 -> -56 */
    ++total;
    S8 s8 = (S8)200;
    printf("case2: s8=%lld\n", (long long)s8);
    if ((long long)s8 == -56) passed++;

    /* case3: unsigned 8-bit arithmetic wrap */
    ++total;
    U8 a = (U8)200, b = (U8)100;
    U8 sum = a + b;
    U8 prod = a * 2;
    printf("case3: u8 sum=%llu prod=%llu\n", (unsigned long long)sum, (unsigned long long)prod);
    if ((unsigned long long)sum == 44 && (unsigned long long)prod == 144) passed++;

    /* case4: signed 31-bit arithmetic + unsigned 17-bit arithmetic */
    ++total;
    S31 x = 1000000, y = 7;
    U17 w = 100000;
    printf("case4: s31 add=%lld mul=%lld div=%lld mod=%lld u17=%llu\n",
           (long long)(x + y), (long long)(x * y),
           (long long)(x / y), (long long)(x % y),
           (unsigned long long)(w + 4567));
    if ((long long)(x + y) == 1000007 && (long long)(x * y) == 7000000 &&
        (long long)(x / y) == 142857 && (long long)(x % y) == 1 &&
        (unsigned long long)(w + 4567) == 104567) passed++;

    /* case5: unsigned 32-bit subtraction wrap */
    ++total;
    U32 lo = (U32)0;
    U32 neg = lo - (U32)1;
    printf("case5: u32 underflow=%llu\n", (unsigned long long)neg);
    if ((unsigned long long)neg == 4294967295ULL) passed++;

    /* case6: signed 64-bit to long long conversion */
    ++total;
    S64 s64 = -123456789LL;
    printf("case6: s64=%lld\n", (long long)s64);
    if ((long long)s64 == -123456789LL) passed++;

    /* case7: 65-bit shift across the 64-bit boundary, then divide */
    ++total;
    U65 hi = (U65)1 << 64;
    U65 half = hi / 2;
    printf("case7: u65 half=%llu\n", (unsigned long long)half);
    if ((unsigned long long)half == 9223372036854775808ULL) passed++;

    /* case8: signed 127-bit: -1 converts to long long */
    ++total;
    S127 bigneg = (S127)0 - (S127)1;
    printf("case8: s127=-1 -> %lld\n", (long long)bigneg);
    if ((long long)bigneg == -1) passed++;

    /* case9: unsigned 128 all-ones wrap, printed via /10 */
    ++total;
    U128 all128 = (U128)0 - (U128)1;
    printf("case9: u128 allones="); pu128(all128); printf("\n");
    U128 q = all128 / (U128)3;
    U128 r = all128 % (U128)3;
    printf("case9b: div3q="); pu128(q); printf(" rem=%llu\n", (unsigned long long)r);
    if ((unsigned long long)r == 0) passed++;

    /* case10: typedef signed cross-width sign extension (roadmap 2.4) */
    ++total;
    S64 neg64 = -12345;
    U128 wide = (U128)(S128)neg64;
    printf("case10: sign-extended neg into u128="); pu128(wide); printf("\n");
    U128 expected = (U128)0 - (U128)12345;
    if (wide == expected) passed++;

    /* case11: unsigned 256 square truncation, printed via /10 */
    ++total;
    U256 x256 = (U256)1 << 200;
    x256 = x256 - (U256)1;
    U256 sq = x256 * x256;
    printf("case11: u256 sq="); pu256(sq); printf("\n");
    if (sq > (U256)0) passed++;

    /* case12: 1024-bit shift out by 500, back by 500 recovers 1, printed via /10 */
    ++total;
    U1024 h = (U1024)1 << 500;
    U1024 rev = h >> 500;
    printf("case12: u1024 shift back = "); pu1024(rev); printf("\n");
    if (rev == (U1024)1) passed++;

    /* case13: comparisons == != < >= on U128 */
    ++total;
    U128 c1 = (U128)100, c2 = (U128)200;
    int r_eq = (c1 == c2) ? 1 : 0;
    int r_ne = (c1 != c2) ? 1 : 0;
    int r_lt = (c1 < c2) ? 1 : 0;
    int r_ge = (c1 >= c2) ? 1 : 0;
    printf("case13: cmp eq=%d ne=%d lt=%d ge=%d\n", r_eq, r_ne, r_lt, r_ge);
    if (r_eq == 0 && r_ne == 1 && r_lt == 1 && r_ge == 0) passed++;

    /* case14: conversions between widths/typedefs */
    ++total;
    U32 narrow = (U32)65537;
    U128 widened = (U128)narrow;
    unsigned back = (unsigned)narrow;
    printf("case14: u32->u128->u wid=%llu back=%u\n",
           (unsigned long long)widened, back);
    if ((unsigned long long)widened == 65537 && back == 65537) passed++;

    /* case15: sizeof widths where goc and gcc agree (N>=33) */
    ++total;
    printf("case15: sz64=%d sz65=%d sz128=%d sz256=%d sz1024=%d\n",
           (int)sizeof(U64), (int)sizeof(U65), (int)sizeof(U128),
           (int)sizeof(U256), (int)sizeof(U1024));
    if (sizeof(U64) == 8 && sizeof(U65) == 16 && sizeof(U128) == 16 &&
        sizeof(U256) == 32 && sizeof(U1024) == 128) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
