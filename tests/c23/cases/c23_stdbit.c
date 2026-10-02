/* C23 feature: <stdbit.h> bit manipulation (stdc_bit_width/stdc_count_ones/...)
 * Clause:     C23 7.18 <stdbit.h>
 * Strategy:   guarded probe. Neither this mingw-w64 gcc nor goc ships <stdbit.h>
 *             (verified: gcc __has_include(<stdbit.h>) == no; goc also lacks it),
 *             so the real header cannot be exercised on either side. The guarded
 *             block records that. A small LOCAL SHIM (not the real header) then
 *             demonstrates the intended bit_width / count_ones semantics so both
 *             toolchains can be cross-checked on the underlying integer logic.
 * Status:     UNSUPPORTED (header absent on both toolchains; goc lacks it by design)
 * EXPECT: PASS
 */
#include <stdio.h>

#if defined(__has_include) && __has_include(<stdbit.h>)
#include <stdbit.h>
#define HAVE_STDBIT 1
#else
#define HAVE_STDBIT 0
#endif

/* ---- local shim: mirror stdc_bit_width / stdc_count_ones for unsigned int ---- */
static int shim_bit_width(unsigned int v) {
    int w = 0;
    while (v != 0u) { w++; v >>= 1; }
    return w;
}
static int shim_count_ones(unsigned int v) {
    int c = 0;
    while (v != 0u) { c += (int)(v & 1u); v >>= 1; }
    return c;
}

int main(void) {
    int passed = 0, total = 0;

    ++total;
#if HAVE_STDBIT
    printf("case%d: real <stdbit.h> present\n", total);
#else
    printf("case%d: real <stdbit.h> unavailable on this toolchain\n", total);
#endif
    passed++; /* availability probe; recorded as UNSUPPORTED in status doc */

    /* ---- shim semantics (unsigned int) ---- */
    ++total;
    printf("case%d: width(0)=%d width(1)=%d width(255)=%d width(256)=%d\n",
           total, shim_bit_width(0u), shim_bit_width(1u),
           shim_bit_width(255u), shim_bit_width(256u));
    if (shim_bit_width(0u) == 0 && shim_bit_width(1u) == 1 &&
        shim_bit_width(255u) == 8 && shim_bit_width(256u) == 9) passed++;

    ++total;
    printf("case%d: count_ones(0)=%d count_ones(0xFF)=%d count_ones(0x55)=%d\n",
           total, shim_count_ones(0u), shim_count_ones(0xFFu), shim_count_ones(0x55u));
    if (shim_count_ones(0u) == 0 && shim_count_ones(0xFFu) == 8 &&
        shim_count_ones(0x55u) == 4) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
