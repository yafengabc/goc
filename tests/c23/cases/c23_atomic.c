/* C23 feature: _Atomic / <stdatomic.h> (C11; goc roadmap #129)
 * Clause:     C23 6.7.2.4 _Atomic; 7.17 <stdatomic.h>
 * Strategy:   exercise the _Atomic type specifier (both the "_Atomic T" and
 *             "_Atomic(T)" spellings), ++/-- and the compound assignments on
 *             atomic scalars of several widths, a struct member, a global and
 *             a pointer target, plus the <stdatomic.h> typedefs and the
 *             atomic_store / atomic_load macros. Values are cross-checked
 *             against gcc -std=c2x.
 * Status:     PASS
 * EXPECT: PASS */
#include <stdio.h>
#include <stdatomic.h>

_Atomic int gcounter;

int main(void) {
    int passed = 0, total = 0;
    int pre, post;

    /* case: the two _Atomic spellings and pre/post increment */
    ++total;
    _Atomic int x = 5;
    _Atomic(long) y = 0;
    pre = ++x;
    post = x++;
    y++;
    printf("case%d: x=%d pre=%d post=%d y=%ld\n", total, x, pre, post, (long)y);
    if (x == 7 && pre == 6 && post == 6 && y == 1) passed++;

    /* case: an atomic reached through a pointer, and a struct member */
    ++total;
    struct H { _Atomic int a; int b; };
    struct H h;
    h.a = 0;
    h.b = 100;
    _Atomic int *p = &x;
    (*p)++;
    h.a++;
    h.a += 3;
    printf("case%d: x=%d a=%d b=%d\n", total, x, h.a, h.b);
    if (x == 8 && h.a == 4 && h.b == 100) passed++;

    /* case: narrow and wide atomic widths */
    ++total;
    _Atomic unsigned char c = 250;
    c++;
    _Atomic long L = 100;
    L--;
    --L;
    printf("case%d: c=%d L=%ld\n", total, (int)c, (long)L);
    if (c == 251 && L == 98) passed++;

    /* case: the compound operators without a single atomic instruction
     *       (goc lowers these to a LOCK CMPXCHG retry loop) */
    ++total;
    _Atomic int a = 0xFF;
    a &= 0x0F;
    a |= 0x30;
    a ^= 0x01;
    _Atomic int b = 6;
    b *= 7;
    b /= 2;
    b %= 5;
    b <<= 3;
    b >>= 1;
    printf("case%d: a=%d b=%d\n", total, a, b);
    if (a == 62 && b == 4) passed++;

    /* case: a global counter */
    ++total;
    gcounter = 0;
    for (int i = 0; i < 100; i++) gcounter++;
    printf("case%d: gcounter=%d\n", total, gcounter);
    if (gcounter == 100) passed++;

    /* case: <stdatomic.h> typedefs and the load/store macros */
    ++total;
    atomic_int ai = 0;
    atomic_store(&ai, 5);
    ai++;
    atomic_ulong ul = 0;
    atomic_store(&ul, 4000000000UL);
    ul += 1;
    printf("case%d: ai=%d ul=%lu\n", total, (int)atomic_load(&ai), (unsigned long)atomic_load(&ul));
    if ((int)atomic_load(&ai) == 6 && (unsigned long)atomic_load(&ul) == 4000000001UL) passed++;

    /* case: sizeof is the underlying type's */
    ++total;
    printf("case%d: sizes %d %d %d\n", total,
           (int)sizeof(atomic_int), (int)sizeof(atomic_uchar), (int)sizeof(atomic_long));
    /* goc is LP64 (long is 8 bytes); gcc on Windows is LLP64 (4). Compare
     * only the widths that agree on both. */
    if ((int)sizeof(atomic_int) == 4 && (int)sizeof(atomic_uchar) == 1) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
