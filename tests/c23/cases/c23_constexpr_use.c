/* C23 feature: constexpr objects used at compile time
 * Clause:     C23 6.7.12 (constexpr)
 * Strategy:   a constexpr int used as: local array dimension, switch case
 *             label, _Static_assert condition, bitfield width, file-scope
 *             (global) array size, and in a folded arithmetic expression.
 *             gcc -std=c2x folds constexpr objects into the constant-expression
 *             grammar; goc only folds constexpr into runtime values and rejects
 *             every grammar position (see group_D2.md for the verbatim errors).
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

constexpr int N = 5;
constexpr int CK = 3;
constexpr int BW = 4;
constexpr int A = 2;
constexpr int B = 3;

_Static_assert(N == 5, "constexpr in static_assert");

struct BFw { int a : BW; };

int garr[N] = {1, 2, 3, 4, 5};

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int arr[N] = {1, 2, 3, 4, 5};
    int s = 0;
    for (int i = 0; i < N; i++) s += arr[i];
    printf("case%d: constexpr array dim sum -> %d\n", total, s);
    if (s == 15) passed++;

    ++total;
    int m = 0;
    switch (CK) {
        case CK: m = 1; break;
        default: m = 2; break;
    }
    printf("case%d: constexpr case label -> %d\n", total, m);
    if (m == 1) passed++;

    ++total;
    struct BFw w; w.a = 5;
    printf("case%d: constexpr bitfield width -> %d\n", total, w.a);
    if (w.a == 5) passed++;

    ++total;
    int gs = garr[0] + garr[N - 1];
    printf("case%d: file-scope constexpr array -> %d\n", total, gs);
    if (gs == 1 + 5) passed++;

    ++total;
    int wide[A * B];
    int wn = (int)(sizeof wide / sizeof wide[0]);
    printf("case%d: constexpr arithmetic dim -> %d\n", total, wn);
    if (wn == A * B) passed++;

    ++total;
    constexpr int D = A + B * 2;
    printf("case%d: constexpr folded -> %d\n", total, D);
    if (D == A + B * 2 && D == 8) passed++;

    ++total;
    int an = (int)(sizeof arr / sizeof arr[0]);
    printf("case%d: sizeof constexpr array -> %d\n", total, an);
    if (an == N) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
