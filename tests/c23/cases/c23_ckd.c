/* C23 feature: <stdckdint.h> checked integer operations ckd_add/ckd_sub/ckd_mul
 * Clause:     C23 7.22.2 "Checked integer operations"
 * Strategy:   exercise ckd_add/sub/mul across int, unsigned int, long, long long,
 *             unsigned long long: no-overflow paths, overflow paths, wrapped result
 *             values, and a mixed int+long destination. The overflow flag is
 *             asserted against the C23-standard expectation. goc's implementation
 *             computes in (long long) and only detects overflow when sizeof(*r) < 8,
 *             so 64-bit destinations (long/long long/unsigned long long) cannot be
 *             detected -- the boundary cases below isolate exactly that gap.
 * Status:     PARTIAL
 * EXPECT: PASS
 */
#include <stdio.h>
#include <stdckdint.h>
#include <limits.h>

int main(void) {
    int passed = 0, total = 0;
    int ri;
    unsigned int ru;
    long long rll;
    unsigned long long rull;
    int of;

    /* ---- int (32-bit destination, W=4: goc can detect) ---- */
    ++total;
    of = ckd_add(&ri, 100, 200);
    printf("case%d: int add no-overflow flag=%d result=%d\n", total, of, ri);
    if (of == 0 && ri == 300) passed++;

    ++total;
    of = ckd_add(&ri, INT_MAX, 1);
    printf("case%d: int add overflow flag=%d result=%d\n", total, of, ri);
    if (of == 1) passed++;

    ++total;
    of = ckd_sub(&ri, INT_MIN, 1);
    printf("case%d: int sub overflow flag=%d result=%d\n", total, of, ri);
    if (of == 1) passed++;

    ++total;
    of = ckd_mul(&ri, 1000, 1000);
    printf("case%d: int mul no-overflow flag=%d result=%d\n", total, of, ri);
    if (of == 0 && ri == 1000000) passed++;

    ++total;
    of = ckd_mul(&ri, 46341, 46341);
    printf("case%d: int mul overflow flag=%d result=%d\n", total, of, ri);
    if (of == 1) passed++;

    /* ---- unsigned int (32-bit destination, W=4) ---- */
    ++total;
    of = ckd_add(&ru, UINT_MAX, 1u);
    printf("case%d: uint add overflow flag=%u result=%u\n", total, of, ru);
    if (of == 1 && ru == 0u) passed++;

    ++total;
    of = ckd_add(&ru, 4000000000u, 0u);
    printf("case%d: uint add no-overflow flag=%u result=%u\n", total, of, ru);
    if (of == 0 && ru == 4000000000u) passed++;

    /* ---- mixed int + long into an int destination (W=4) ---- */
    ++total;
    {
        long b = 1L;
        of = ckd_add(&ri, INT_MAX, b);
        printf("case%d: mixed int+long overflow flag=%d result=%d\n", total, of, ri);
        if (of == 1) passed++;
    }

    /* ---- long long (64-bit destination, W=8: goc CANNOT detect) ---- */
    ++total;
    of = ckd_add(&rll, LLONG_MAX, 1);
    printf("case%d: ll add overflow flag=%d result=%lld\n", total, of, rll);
    if (of == 1) passed++;

    ++total;
    of = ckd_add(&rll, 4611686018427387903LL, 4611686018427387903LL);
    printf("case%d: ll add no-overflow flag=%d result=%lld\n", total, of, rll);
    if (of == 0 && rll == 9223372036854775806LL) passed++;

    /* ---- unsigned long long (64-bit destination, W=8) ---- */
    ++total;
    rull = ~(unsigned long long)0;
    of = ckd_add(&rull, rull, 1ull);
    printf("case%d: ull add overflow flag=%d result=%llu\n", total, of, rull);
    if (of == 1 && rull == 0ull) passed++;

    /* ---- long long mul overflow (64-bit destination) ---- */
    ++total;
    of = ckd_mul(&rll, LLONG_MAX, 2);
    printf("case%d: ll mul overflow flag=%d result=%lld\n", total, of, rll);
    if (of == 1) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
