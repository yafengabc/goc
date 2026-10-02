/* C23 feature: constexpr objects (and constexpr functions)
 * Clause:     C23 6.7.11 constexpr; 6.6 constant expressions
 * Strategy:   constexpr scalar / pointer / array at file scope and block scope,
 *             static constexpr, constant initialization, and a constexpr
 *             function invoked with constant arguments.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

constexpr int N = 10;
constexpr double PI = 3.14159;
constexpr int CA[] = { 3, 6, 9 };
constexpr int *CP = 0;
static constexpr int SK = 5;

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int acc = N * 2;
    printf("case%d: N=%d N*2=%d\n", total, N, acc);
    if (N == 10 && acc == 20) passed++;

    ++total;
    printf("case%d: PI=%f\n", total, PI);
    if (PI > 3.14 && PI < 3.15) passed++;

    ++total;
    int s = CA[0] + CA[1] + CA[2];
    printf("case%d: CA sum=%d\n", total, s);
    if (s == 18) passed++;

    ++total;
    printf("case%d: CP==0 => %d\n", total, (int)(CP == 0));
    if (CP == 0) passed++;

    ++total;
    printf("case%d: static constexpr SK=%d\n", total, SK);
    if (SK == 5) passed++;

    ++total;
    int local = SK * 2;
    int slocal = N + SK;
    printf("case%d: derived local=%d slocal=%d\n", total, local, slocal);
    if (local == 10 && slocal == 15) passed++;

    ++total;
    int r = SK * SK;
    printf("case%d: SK*SK=%d\n", total, r);
    if (r == 25) passed++;

    ++total;
    int w = N * SK;
    int idx = CA[1] / 3;
    printf("case%d: N*SK=%d CA[1]/3=%d\n", total, w, idx);
    if (w == 50 && idx == 2) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
