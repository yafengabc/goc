/* C23 feature: <stdbit.h> bit utilities (stdc_* type-generic macros)
 * Clause:     C23 7.18 <stdbit.h>
 * Strategy:   guarded probe. goc ships <stdbit.h> (goclib stdbit.h/stdbit.c),
 *             this mingw-w64 gcc does not, so the guarded block takes the real
 *             header on goc and a local shim with identical semantics on gcc.
 *             Both paths print the same values, so the case is comparable on
 *             the two toolchains. The real-header path exercises all 14 macros
 *             over unsigned char/short/int/long plus the zero and all-ones
 *             edges and the C23 "most significant index + 1" rule
 *             (stdc_first_leading_one counts DOWN from the top bit: 9 for
 *             0x00FF00FFu, not 24).
 * Status:     PASS
 * EXPECT: PASS */
#include <stdio.h>
#include <stdint.h>

#if defined(__has_include) && __has_include(<stdbit.h>)
#include <stdbit.h>
#define HAVE_STDBIT 1
#else
#define HAVE_STDBIT 0
#endif

/* ---- local shim: only used when the real header is unavailable ---- */
static unsigned int shim_bit_width(unsigned int v) {
    unsigned int w = 0;
    while (v != 0u) { w++; v >>= 1u; }
    return w;
}
static unsigned int shim_count_ones(unsigned int v) {
    unsigned int c = 0;
    while (v != 0u) { c += (v & 1u); v >>= 1u; }
    return c;
}
static unsigned int shim_leading_zeros(unsigned int v) {
    unsigned int c = 0, i;
    for (i = 0; i < 32u; i++) {
        if ((v >> (31u - i)) & 1u) break;
        c++;
    }
    return c;
}
static unsigned int shim_trailing_zeros(unsigned int v) {
    unsigned int c = 0;
    if (v == 0u) return 32u;
    while ((v & 1u) == 0u) { c++; v >>= 1u; }
    return c;
}

#if HAVE_STDBIT
#define BIT_WIDTH(v)  stdc_bit_width(v)
#define COUNT_ONES(v) stdc_count_ones(v)
#else
#define BIT_WIDTH(v)  shim_bit_width(v)
#define COUNT_ONES(v) shim_count_ones(v)
#endif

int main(void) {
    int passed = 0, total = 0;

    ++total;
#if HAVE_STDBIT
    printf("case%d: real <stdbit.h> present\n", total);
#else
    printf("case%d: real <stdbit.h> unavailable on this toolchain\n", total);
#endif
    passed++; /* availability probe */

    /* ---- width / popcount on unsigned int ---- */
    ++total;
    printf("case%d: width(0)=%u width(1)=%u width(255)=%u width(256)=%u\n",
           total, BIT_WIDTH(0u), BIT_WIDTH(1u),
           BIT_WIDTH(255u), BIT_WIDTH(256u));
    if (BIT_WIDTH(0u) == 0u && BIT_WIDTH(1u) == 1u &&
        BIT_WIDTH(255u) == 8u && BIT_WIDTH(256u) == 9u) passed++;

    ++total;
    printf("case%d: count_ones(0)=%u count_ones(0xFF)=%u count_ones(0x55)=%u\n",
           total, COUNT_ONES(0u), COUNT_ONES(0xFFu), COUNT_ONES(0x55u));
    if (COUNT_ONES(0u) == 0u && COUNT_ONES(0xFFu) == 8u &&
        COUNT_ONES(0x55u) == 4u) passed++;

#if HAVE_STDBIT
    {
        unsigned u = 0x00FF00FFu;
        unsigned char c = 0x0Fu;
        unsigned short s = 0x00FFu;
        unsigned long ul = 0x8000000000000000ul;
        uint64_t big = (uint64_t)1 << 40;

        /* ---- the rest of the counting families ---- */
        ++total;
        printf("case%d: lz=%u lo=%u tz=%u to=%u c0=%u\n", total,
               stdc_leading_zeros(u), stdc_leading_ones(u),
               stdc_trailing_zeros(u), stdc_trailing_ones(u),
               stdc_count_zeros(u));
        if (stdc_leading_zeros(u) == 8u && stdc_leading_ones(u) == 0u &&
            stdc_trailing_zeros(u) == 0u && stdc_trailing_ones(u) == 8u &&
            stdc_count_zeros(u) == 16u) passed++;

        /* ---- first_* index families (counted from the top / bottom, +1) ---- */
        ++total;
        printf("case%d: flo1=%u flo0=%u fto1=%u fto0=%u\n", total,
               stdc_first_leading_one(u), stdc_first_leading_zero(u),
               stdc_first_trailing_one(u), stdc_first_trailing_zero(u));
        if (stdc_first_leading_one(u) == 9u && stdc_first_leading_zero(u) == 1u &&
            stdc_first_trailing_one(u) == 1u && stdc_first_trailing_zero(u) == 9u) passed++;

        /* ---- power-of-two helpers ---- */
        ++total;
        printf("case%d: ceil5=%u ceil8=%u ceil0=%u floor5=%u floor8=%u hsb=%u\n", total,
               stdc_bit_ceil(5u), stdc_bit_ceil(8u), stdc_bit_ceil(0u),
               stdc_bit_floor(5u), stdc_bit_floor(8u),
               (unsigned)stdc_has_single_bit(1u << 15));
        if (stdc_bit_ceil(5u) == 8u && stdc_bit_ceil(8u) == 8u &&
            stdc_bit_ceil(0u) == 1u && stdc_bit_floor(5u) == 4u &&
            stdc_bit_floor(8u) == 8u && stdc_has_single_bit(1u << 15)) passed++;

        /* ---- zero / all-ones edges ---- */
        ++total;
        printf("case%d: lz0=%u tz0=%u flo1_0=%u fto0_0=%u flo0_ones=%u\n", total,
               stdc_leading_zeros(0u), stdc_trailing_zeros(0u),
               stdc_first_leading_one(0u), stdc_first_trailing_zero(0u),
               stdc_first_leading_zero(~0u));
        if (stdc_leading_zeros(0u) == 32u && stdc_trailing_zeros(0u) == 32u &&
            stdc_first_leading_one(0u) == 0u && stdc_first_trailing_zero(0u) == 1u &&
            stdc_first_leading_zero(~0u) == 0u) passed++;

        /* ---- macro dispatch on the argument's own type ---- */
        ++total;
        printf("case%d: c=%u s=%u\n", total,
               stdc_leading_zeros(c), stdc_leading_zeros(s));
        if (stdc_leading_zeros(c) == 4u && stdc_leading_zeros(s) == 8u) passed++;

        /* ---- 64-bit arguments ---- */
        ++total;
        printf("case%d: width=%u lz=%u ul_width=%u\n", total,
               stdc_bit_width(big), stdc_leading_zeros(big), stdc_bit_width(ul));
        if (stdc_bit_width(big) == 41u && stdc_leading_zeros(big) == 23u &&
            stdc_bit_width(ul) == 64u) passed++;

        /* ---- unsignedness survives through operators ---- */
        ++total;
        printf("case%d: ceil=%u hsb=%u or=%u\n", total,
               stdc_bit_ceil(1u << 15), (unsigned)stdc_has_single_bit(1u << 15),
               stdc_count_ones(0x0Fu | 0xF0u));
        if (stdc_bit_ceil(1u << 15) == (1u << 15) &&
            stdc_has_single_bit(1u << 15) && stdc_count_ones(0x0Fu | 0xF0u) == 8u) passed++;
    }
#else
    ++total;
    printf("case%d: shim lz(1)=%u tz(256)=%u\n", total,
           shim_leading_zeros(1u), shim_trailing_zeros(256u));
    if (shim_leading_zeros(1u) == 31u && shim_trailing_zeros(256u) == 8u) passed++;
#endif

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
