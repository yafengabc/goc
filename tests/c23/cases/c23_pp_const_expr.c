/* C23 feature: #if controlling constant expressions
 * Clause:     C23 6.10.1 "Conditional inclusion"
 * Strategy:   exercise arithmetic, bitwise, logical, ternary, character constant,
 *             defined() and hex forms in #if, plus intmax/uintmax overflow
 *             wraparound semantics. Each result is expanded into an integer macro.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

#define SYM 1

#if (2+3)*4 == 20
#  define E_ARITH 1
#else
#  define E_ARITH 0
#endif

#if (0xF0 & 0x0F) == 0 && (0xF0 | 0x0F) == 0xFF
#  define E_BIT 1
#else
#  define E_BIT 0
#endif

#if (1 && 0) == 0 && (1 || 0) == 1
#  define E_LOGIC 1
#else
#  define E_LOGIC 0
#endif

#if (1 ? 5 : 9) == 5 && (0 ? 5 : 9) == 9
#  define E_TERN 1
#else
#  define E_TERN 0
#endif

#if 'A' == 65
#  define E_CHAR 1
#else
#  define E_CHAR 0
#endif

#if defined(SYM) && !defined(NO_SUCH_SYM)
#  define E_DEFINED 1
#else
#  define E_DEFINED 0
#endif

#if 0x10 == 16 && 0xff == 255
#  define E_HEX 1
#else
#  define E_HEX 0
#endif

/* intmax wraparound: INT64_MAX + 1 must wrap to INT64_MIN in a #if */
#if 9223372036854775807 + 1 == -9223372036854775808
#  define E_WRAP 1
#else
#  define E_WRAP 0
#endif

int main(void) {
    int passed = 0, total = 0;

    ++total; printf("case%d: arith=%d (want 1)\n", total, E_ARITH);
    if (E_ARITH) passed++;

    ++total; printf("case%d: bit=%d (want 1)\n", total, E_BIT);
    if (E_BIT) passed++;

    ++total; printf("case%d: logic=%d (want 1)\n", total, E_LOGIC);
    if (E_LOGIC) passed++;

    ++total; printf("case%d: ternary=%d (want 1)\n", total, E_TERN);
    if (E_TERN) passed++;

    ++total; printf("case%d: char=%d (want 1)\n", total, E_CHAR);
    if (E_CHAR) passed++;

    ++total; printf("case%d: defined=%d (want 1)\n", total, E_DEFINED);
    if (E_DEFINED) passed++;

    ++total; printf("case%d: hex=%d (want 1)\n", total, E_HEX);
    if (E_HEX) passed++;

    ++total; printf("case%d: intmax wraparound=%d (want 1)\n", total, E_WRAP);
    if (E_WRAP) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
