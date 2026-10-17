/* C23 feature: generic selection _Generic
 * Clause:     C23 6.5.1.1 (generic selection)
 * Strategy:   exact type matching (int vs long vs unsigned int do not cross
 *             match), controlling expression is unevaluated (x++ has no side
 *             effect), char/short are NOT promoted before matching, typedef
 *             name usable as an association type, default branch, nested
 *             _Generic, and use inside a function-like macro. Types that
 *             share a representation but stay distinct in C are also kept
 *             apart: char vs signed char, long vs long long, double vs long
 *             double, and pointer-to-const vs pointer-to-non-const.
 * Status:     PENDING
 * EXPECT: PASS
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

    ++total;
    /* pointer-to-const vs pointer-to-non-const are distinct types: the
     * const belongs to the pointee, so it survives lvalue conversion */
    {
        int *ip = 0;
        const int *cip = 0;
        int r1 = _Generic(ip,  int*: 1, const int*: 2, default: 9);
        int r2 = _Generic(cip, int*: 1, const int*: 2, default: 9);
        printf("case%d: ptr const -> %d %d\n", total, r1, r2);
        if (r1 == 1 && r2 == 2) passed++;
    }

    ++total;
    /* char, signed char and unsigned char are three distinct types even
     * though char's representation matches one of the others */
    {
        char pc = 0;
        signed char sc = 0;
        unsigned char uc = 0;
        int r1 = _Generic(pc, char: 1, signed char: 2, default: 9);
        int r2 = _Generic(sc, char: 1, signed char: 2, default: 9);
        int r3 = _Generic(uc, char: 1, signed char: 2, default: 9);
        printf("case%d: char kinds -> %d %d %d\n", total, r1, r2, r3);
        if (r1 == 1 && r2 == 2 && r3 == 9) passed++;
    }

    ++total;
    /* long and long long are distinct types even when both are 64 bits */
    {
        long a = 0;
        long long b = 0;
        int r1 = _Generic(a, long: 1, long long: 2, default: 9);
        int r2 = _Generic(b, long: 1, long long: 2, default: 9);
        printf("case%d: long kinds -> %d %d\n", total, r1, r2);
        if (r1 == 1 && r2 == 2) passed++;
    }

    ++total;
    /* double and long double are distinct types */
    {
        double d = 0;
        long double ld = 0;
        int r1 = _Generic(d,  double: 1, long double: 2, default: 9);
        int r2 = _Generic(ld, double: 1, long double: 2, default: 9);
        printf("case%d: double kinds -> %d %d\n", total, r1, r2);
        if (r1 == 1 && r2 == 2) passed++;
    }

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
