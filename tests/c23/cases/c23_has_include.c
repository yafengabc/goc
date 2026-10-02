/* C23 feature: __has_include
 * Clause:     C23 6.10.2 "Source file inclusion" (feature-test macro)
 * Strategy:   probe __has_include only for headers where the two compilers agree
 *             on availability (goc ships stdio/stdlib/string/math; neither ships
 *             threads.h), plus a guaranteed-missing header, the quoted form, and a
 *             boolean-logic combination. __has_include may only appear inside a
 *             preprocessing directive, so each result is expanded into an integer
 *             macro. NOTE: measured on this build goc reports 1 for the headers it
 *             actually has and 0 for the ones it lacks (wchar/complex/stdatomic/
 *             iso646), so it tracks goc's real header inventory -- see status doc.
 * Status:     PENDING
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

#if __has_include(<threads.h>)
#  define HI_THREADS 1
#else
#  define HI_THREADS 0
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
    printf("case%d: __has_include(<threads.h>) = %d (want 0)\n", total, HI_THREADS);
    if (HI_THREADS == 0) passed++;

    ++total;
    printf("case%d: __has_include(\"stdio.h\") = %d (want 1)\n", total, HI_QSTDIO);
    if (HI_QSTDIO == 1) passed++;

    ++total;
    printf("case%d: combo(stdio && !missing) = %d (want 1)\n", total, HI_COMBO);
    if (HI_COMBO == 1) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
