/* C23 feature: <stdatomic.h> fetch family, atomic_exchange, compare-exchange
 * Clause:     C11 7.17.7 (fetch and exchange), 7.17.8 (compare-exchange)
 * Strategy:   every atomic_fetch_* operation reports the value the object held
 *             BEFORE the update, which is the whole point of the family and
 *             the part no C expression can express. Each case checks both the
 *             returned old value and the new contents of the object, across
 *             widths (char / short / int / unsigned / long long), and the
 *             compare-exchange form checks the failure path updating
 *             *expected. Cross-checked against gcc -std=c2x.
 * Status:     PASS
 * EXPECT: PASS */
#include <stdio.h>
#include <stdatomic.h>

atomic_int gcounter;

int main(void) {
    int passed = 0, total = 0;
    int old;

    /* case: fetch_add -- returns the previous value, object takes the sum */
    ++total;
    atomic_int a = 10;
    old = atomic_fetch_add(&a, 5);
    printf("case%d: old=%d now=%d\n", total, old, atomic_load(&a));
    if (old == 10 && atomic_load(&a) == 15) passed++;

    /* case: fetch_sub on the object the previous case left behind */
    ++total;
    old = atomic_fetch_sub(&a, 3);
    printf("case%d: old=%d now=%d\n", total, old, atomic_load(&a));
    if (old == 15 && atomic_load(&a) == 12) passed++;

    /* case: fetch_and / fetch_or / fetch_xor need a CAS loop */
    ++total;
    atomic_uint u = 0xF0u;
    unsigned o1 = atomic_fetch_and(&u, 0x3Cu);
    unsigned o2 = atomic_fetch_or(&u, 0x05u);
    unsigned o3 = atomic_fetch_xor(&u, 0xFFu);
    printf("case%d: %u %u %u now=%u\n", total, o1, o2, o3, atomic_load(&u));
    if (o1 == 0xF0u && o2 == 0x30u && o3 == 0x35u && atomic_load(&u) == 0xCAu) passed++;

    /* case: narrow widths -- the locked access must stay 1 and 2 bytes */
    ++total;
    atomic_char c = 100;
    char co = atomic_fetch_add(&c, 27);
    atomic_short s = 300;
    short so = atomic_fetch_sub(&s, 1);
    printf("case%d: co=%d c=%d so=%d s=%d\n", total, (int)co, (int)atomic_load(&c),
           (int)so, (int)atomic_load(&s));
    if (co == 100 && atomic_load(&c) == 127 && so == 300 && atomic_load(&s) == 299) passed++;

    /* case: 8-byte widths */
    ++total;
    atomic_llong ll = 4000000000LL;
    long long lold = atomic_fetch_add(&ll, 1000000000LL);
    atomic_ullong ull = 18446744073709551615ULL;
    unsigned long long uold = atomic_fetch_xor(&ull, 0xFFu);
    printf("case%d: lold=%lld ll=%lld uold=%llu ull=%llu\n", total, lold, atomic_load(&ll),
           uold, atomic_load(&ull));
    if (lold == 4000000000LL && atomic_load(&ll) == 5000000000LL &&
        uold == 18446744073709551615ULL && atomic_load(&ull) == 18446744073709551360ULL) passed++;

    /* case: atomic_exchange -- unconditional swap, old value returned */
    ++total;
    atomic_int e = 42;
    old = atomic_exchange(&e, 99);
    printf("case%d: old=%d now=%d\n", total, old, atomic_load(&e));
    if (old == 42 && atomic_load(&e) == 99) passed++;

    /* case: compare-exchange, success then failure. On failure the observed
     * value has to land in *expected. */
    ++total;
    atomic_int t = 7;
    int expect = 7;
    int ok1 = atomic_compare_exchange_strong(&t, &expect, 8);
    int ok2 = atomic_compare_exchange_strong(&t, &expect, 9);
    printf("case%d: ok1=%d ok2=%d expect=%d now=%d\n", total, ok1, ok2, expect, atomic_load(&t));
    if (ok1 != 0 && ok2 == 0 && expect == 8 && atomic_load(&t) == 8) passed++;

    /* case: the _explicit spellings and a file-scope object */
    ++total;
    atomic_int g = 0;
    (void)atomic_fetch_add_explicit(&g, 4, memory_order_relaxed);
    (void)atomic_exchange_explicit(&g, 77, memory_order_seq_cst);
    int weak = atomic_compare_exchange_weak_explicit(&gcounter, &g, 1,
                                                     memory_order_seq_cst,
                                                     memory_order_relaxed);
    (void)atomic_fetch_add_explicit(&gcounter, 6, memory_order_relaxed);
    printf("case%d: g=%d weak=%d gcounter=%d\n", total, atomic_load(&g), weak,
           atomic_load(&gcounter));
    if (atomic_load(&g) == 0 && weak == 0 && atomic_load(&gcounter) == 6) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
