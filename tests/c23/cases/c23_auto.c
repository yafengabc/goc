/* C23 feature: auto type deduction
 * Clause:     C23 6.7.9 auto
 * Strategy:   auto deduces int / char / long long / pointer / array decay /
 *             function pointer, combined with const and static, used in a
 *             for-init, and at file scope.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

auto g_auto = 10;

static int add2(int a, int b) {
    return a + b;
}

int main(void) {
    int passed = 0, total = 0;

    ++total;
    auto x = 1;
    auto c = 'a';
    auto ll = 1234567890123LL;
    printf("case%d: x=%d c=%c ll=%lld\n", total, x, c, (long long)ll);
    if (x == 1 && c == 'a' && ll == 1234567890123LL) passed++;

    ++total;
    auto p = &x;
    printf("case%d: *p=%d\n", total, *p);
    if (*p == 1) passed++;

    ++total;
    int arr[4] = { 10, 20, 30, 40 };
    auto ap = arr; /* decays to int pointer */
    printf("case%d: ap[1]=%d\n", total, ap[1]);
    if (ap[1] == 20) passed++;

    ++total;
    auto fp = &add2;
    printf("case%d: fp(3,4)=%d\n", total, fp(3, 4));
    if (fp(3, 4) == 7) passed++;

    ++total;
    const auto ca = 9;
    static auto sa = 11;
    printf("case%d: ca=%d sa=%d\n", total, ca, sa);
    if (ca == 9 && sa == 11) passed++;

    ++total;
    int acc = 0;
    for (auto i = 0; i < 3; i++) acc += i;
    printf("case%d: for-init auto acc=%d\n", total, acc);
    if (acc == 3) passed++;

    ++total;
    printf("case%d: file-scope g_auto=%d\n", total, g_auto);
    if (g_auto == 10) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
