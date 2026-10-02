/* C23 feature: generic selection _Generic
 * Clause:     C23 6.5.1.1 (generic selection)
 * Strategy:   exact type matching (int vs long vs unsigned int do not cross
 *             match), controlling expression is unevaluated (x++ has no side
 *             effect), char/short are NOT promoted before matching, typedef
 *             name usable as an association type, default branch, nested
 *             _Generic, and use inside a function-like macro.
 * Status:     PENDING
 * EXPECT: PASS
 *
 * goc gap found during testing (NOT compiled into this file; recorded in
 * group_D2.md):
 *   - pointer-to-const vs pointer-to-non-const: goc treats 'int *' and
 *     'const int *' as the SAME type in a _Generic list, error:
 *       "type int* appears twice in the _Generic association list"
 *     (gcc distinguishes them).
 */
#include <stdio.h>

typedef long mylong;
#define KIND(v) _Generic((v), int: 1, long: 2, unsigned int: 3, default: 0)

int main(void) {
    int passed = 0, total = 0;

    ++total;
    {
        int r = _Generic(0, int: 1, long: 2, unsigned int: 3, default: 9);
        printf("case%d: int literal picks -> %d\n", total, r);
        if (r == 1) passed++;
    }

    ++total;
    {
        int r = _Generic(1L, int: 1, long: 2, unsigned int: 3, default: 9);
        printf("case%d: long literal picks -> %d\n", total, r);
        if (r == 2) passed++;
    }

    ++total;
    {
        int r = _Generic(1U, int: 1, long: 2, unsigned int: 3, default: 9);
        printf("case%d: unsigned literal picks -> %d\n", total, r);
        if (r == 3) passed++;
    }

    ++total;
    /* controlling expression is unevaluated: x++ must NOT run */
    {
        int x = 0;
        int r = _Generic(x++, int: 0, default: 9);
        printf("case%d: unevaluated x=%d (r=%d)\n", total, x, r);
        if (x == 0 && r == 0) passed++;
    }

    ++total;
    /* char is NOT integer-promoted before matching */
    {
        char c = 0;
        int r = _Generic(c, int: 1, char: 2, default: 9);
        printf("case%d: char picks -> %d\n", total, r);
        if (r == 2) passed++;
    }

    ++total;
    /* short is NOT integer-promoted before matching */
    {
        short s = 0;
        int r = _Generic(s, int: 1, short: 2, default: 9);
        printf("case%d: short picks -> %d\n", total, r);
        if (r == 2) passed++;
    }

    ++total;
    /* a typedef name works as an association type */
    {
        int r = _Generic(1L, mylong: 1, int: 2, double: 3, default: 9);
        printf("case%d: typedef association picks -> %d\n", total, r);
        if (r == 1) passed++;
    }

    ++total;
    /* default branch */
    {
        double f = 0.0;
        int r = _Generic(f, int: 1, long: 2, default: 9);
        printf("case%d: default picks -> %d\n", total, r);
        if (r == 9) passed++;
    }

    ++total;
    /* nested _Generic */
    {
        long lv = _Generic((char)0,
                           char: _Generic(1L, long: 100L, default: 0L),
                           default: 5L);
        printf("case%d: nested picks -> %ld\n", total, lv);
        if (lv == 100L) passed++;
    }

    ++total;
    /* inside a function-like macro, applied to three kinds */
    {
        int a = KIND(0);
        int b = KIND(1L);
        int c = KIND(1U);
        printf("case%d: macro kind -> %d %d %d\n", total, a, b, c);
        if (a == 1 && b == 2 && c == 3) passed++;
    }

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
