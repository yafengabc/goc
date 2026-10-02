/* C23 feature: __has_c_attribute
 * Clause:     C23 6.10.10 "__has_c_attribute"
 * Strategy:   exercise the STANDARD portable guard from the testing contract,
 *             `#if defined(__has_c_attribute) && __has_c_attribute(attr)`, over seven
 *             standard attributes (deprecated, nodiscard, noreturn, maybe_unused,
 *             fallthrough, likely, unlikely). Each guarded result is expanded into an
 *             integer macro. The `passed` counter is graded against the CONFORMING
 *             (gcc) guarded values; goc is expected to report the guard inactive (it
 *             does not expose __has_c_attribute to defined()), so the guarded probes
 *             fall to 0, producing a DIFF. See status doc for the unguarded nuance.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

#if defined(__has_c_attribute)
#  define GUARD_ACTIVE 1
#else
#  define GUARD_ACTIVE 0
#endif

#if defined(__has_c_attribute) && __has_c_attribute(deprecated)
#  define G_DEP 1
#else
#  define G_DEP 0
#endif
#if defined(__has_c_attribute) && __has_c_attribute(nodiscard)
#  define G_ND 1
#else
#  define G_ND 0
#endif
#if defined(__has_c_attribute) && __has_c_attribute(noreturn)
#  define G_NR 1
#else
#  define G_NR 0
#endif
#if defined(__has_c_attribute) && __has_c_attribute(maybe_unused)
#  define G_MU 1
#else
#  define G_MU 0
#endif
#if defined(__has_c_attribute) && __has_c_attribute(fallthrough)
#  define G_FT 1
#else
#  define G_FT 0
#endif
#if defined(__has_c_attribute) && __has_c_attribute(likely)
#  define G_LK 1
#else
#  define G_LK 0
#endif
#if defined(__has_c_attribute) && __has_c_attribute(unlikely)
#  define G_UL 1
#else
#  define G_UL 0
#endif

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: guard_active=%d deprecated=%d (want 1)\n", total, GUARD_ACTIVE, G_DEP);
    if (G_DEP == 1) passed++;

    ++total;
    printf("case%d: nodiscard=%d (want 1)\n", total, G_ND);
    if (G_ND == 1) passed++;

    ++total;
    printf("case%d: noreturn=%d (want 1)\n", total, G_NR);
    if (G_NR == 1) passed++;

    ++total;
    printf("case%d: maybe_unused=%d (want 1)\n", total, G_MU);
    if (G_MU == 1) passed++;

    ++total;
    printf("case%d: fallthrough=%d (want 1)\n", total, G_FT);
    if (G_FT == 1) passed++;

    ++total;
    printf("case%d: likely=%d (want 0)\n", total, G_LK);
    if (G_LK == 0) passed++;

    ++total;
    printf("case%d: unlikely=%d (want 0)\n", total, G_UL);
    if (G_UL == 0) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
