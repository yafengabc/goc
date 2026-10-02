/* C23 feature: __has_include
 * Clause:     C23 6.10.2 "Source file inclusion" (feature-test macro)
 * Strategy:   probe __has_include only for headers where the two compilers agree
 *             on availability, plus a guaranteed-missing header, the quoted form,
 *             and a boolean-logic combination. Batch H gave goc stdbool/stdnoreturn/
 *             uchar/threads headers that this mingw gcc lacks, so those are NOT
 *             probed (goc's inventory is now strictly richer; see C23_STATUS).
 *             <stdbit.h> is absent on both and stands in as the "missing" probe.
 *             __has_include may only appear inside a preprocessing directive, so
 *             each result is expanded into an integer macro.
 * Status:     PASS (batch H, 2026-10-02: goc vs gcc -std=c2x 6/6 identical)
 * EXPECT: PASS
 */
#include <stdio.h>

#if __has_include(<stdio.h>)
#  define HI_STDIO 1
#else
#  define HI_STDIO 0
#endif

#if __has_include(<string.h>)
#  define HI_STRING 1
#else
#  define HI_STRING 0
#endif

#if __has_include("no_such_header_zzz.h")
#  define HI_MISSING 1
#else
#  define HI_MISSING 0
#endif

#if __has_include(<stdbit.h>)
#  define HI_STDBIT 1
#else
#  define HI_STDBIT 0
#endif

#if __has_include("stdio.h")
#  define HI_QSTDIO 1
#else
#  define HI_QSTDIO 0
#endif

#if __has_include(<stdio.h>) && !__has_include("no_such_header_zzz.h")
#  define HI_COMBO 1
#else
#  define HI_COMBO 0
#endif

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: __has_include(<stdio.h>) = %d (want 1)\n", total, HI_STDIO);
    if (HI_STDIO == 1) passed++;

    ++total;
    printf("case%d: __has_include(<string.h>) = %d (want 1)\n", total, HI_STRING);
    if (HI_STRING == 1) passed++;

    ++total;
    printf("case%d: __has_include(\"missing.h\") = %d (want 0)\n", total, HI_MISSING);
    if (HI_MISSING == 0) passed++;

    ++total;
    printf("case%d: __has_include(<stdbit.h>) = %d (want 0)\n", total, HI_STDBIT);
    if (HI_STDBIT == 0) passed++;

    ++total;
    printf("case%d: __has_include(\"stdio.h\") = %d (want 1)\n", total, HI_QSTDIO);
    if (HI_QSTDIO == 1) passed++;

    ++total;
    printf("case%d: combo(stdio && !missing) = %d (want 1)\n", total, HI_COMBO);
    if (HI_COMBO == 1) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
