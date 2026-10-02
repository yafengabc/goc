/* C23 feature: constexpr pointers and arrays
 * Clause:     C23 6.7.12 (constexpr), 6.6 (constant expressions)
 * Strategy:   constexpr null pointer, constexpr array object (sizeof and
 *             element access), and use of the constexpr values.
 * Status:     PENDING
 * EXPECT: PASS
 *
 * Items intentionally NOT compiled into this gcc-passing file (recorded in
 * group_D2.md):
 *   - constexpr int *p = &obj;   -> gcc 16.2 REJECTS ("'constexpr' pointer
 *       initializer is not null"); goc ACCEPTS the syntax but dereferencing
 *       *p segfaults (0xC0000005), so it is unusable either way.
 *   - constexpr const char *s = "lit"; -> gcc 16.2 REJECTS (same reason).
 *   - _Static_assert(&a != &b, ...) address comparison -> gcc ACCEPTS but goc
 *       REJECTS ("expected ';' after global declaration").
 */
#include <stdio.h>

constexpr int *np = (int *)0;
constexpr int ca[3] = {10, 20, 30};

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: constexpr null ptr -> %d\n", total, (int)(long long)np);
    if (np == (int *)0) passed++;

    ++total;
    printf("case%d: constexpr array elements -> %d %d %d\n", total, ca[0], ca[1], ca[2]);
    if (ca[0] == 10 && ca[1] == 20 && ca[2] == 30) passed++;

    ++total;
    int cnt = (int)(sizeof ca / sizeof ca[0]);
    printf("case%d: constexpr array sizeof count -> %d\n", total, cnt);
    if (cnt == 3) passed++;

    ++total;
    int s = ca[0] + ca[1] + ca[2];
    printf("case%d: constexpr array sum -> %d\n", total, s);
    if (s == 60) passed++;

    ++total;
    int isnull = (np == 0);
    printf("case%d: null ptr comparison -> %d\n", total, isnull);
    if (isnull == 1) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
