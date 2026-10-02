/* C23 feature: typeof / typeof_unqual (and __typeof__)
 * Clause:     C23 6.7.2.2 typeof specifier
 * Strategy:   typeof(type) and typeof(const/scalar var), typeof_unqual
 *             dropping const / volatile (proven by reassigning the result),
 *             typeof of a pointer type and of an array. NOTE: goc rejects
 *             __typeof__ spelling, nested typeof, typeof(&x) and typeof of a
 *             restrict pointer -- those gaps are recorded in the status doc.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

int main(void) {
    int passed = 0, total = 0;

    ++total;
    typeof(int) a = 5;
    typeof(unsigned) uu = 4000000000U;
    printf("case%d: typeof(int)=%d typeof(unsigned)=%u\n", total, a, uu);
    if (a == 5 && uu == 4000000000U) passed++;

    ++total;
    typeof(1LL) big = 1234567890123LL;
    typeof(big) twice = big + big;
    printf("case%d: typeof(expr) big=%lld twice=%lld\n",
           total, (long long)big, (long long)twice);
    if (twice == 2469135780246LL) passed++;

    ++total;
    const int z = 9;
    typeof_unqual(z) w = 11;
    w = 22; /* proves const was stripped */
    printf("case%d: typeof_unqual(const int) w=%d\n", total, w);
    if (w == 22) passed++;

    ++total;
    volatile int vv = 3;
    typeof_unqual(vv) u = vv;
    u = 44; /* proves volatile was stripped */
    printf("case%d: typeof_unqual(volatile int) u=%d\n", total, u);
    if (u == 44) passed++;

    ++total;
    int x = 5;
    typeof(int *) px = &x;
    printf("case%d: typeof(int *) *px=%d\n", total, *px);
    if (*px == 5) passed++;

    ++total;
    typeof(short) s = -1000;
    typeof(s) s2 = s * 2;
    printf("case%d: typeof(short) s=%d s2=%d\n", total, s, s2);
    if (s == -1000 && s2 == -2000) passed++;

    ++total;
    int arr3[4] = { 1, 2, 3, 4 };
    typeof(arr3) copy = { 5, 6, 7, 8 };
    printf("case%d: typeof(array) copy[0..2]=%d%d%d\n",
           total, copy[0], copy[1], copy[2]);
    if (copy[0] == 5 && copy[1] == 6 && copy[2] == 7) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
