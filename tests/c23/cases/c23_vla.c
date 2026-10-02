/* C23 feature: variable-length arrays (VLA) and variably-modified types
 * Clause:     C23 6.7.6.2 (array declarators), 6.10.8.3 (__STDC_NO_VLA__)
 * Strategy:   runtime-sized array, sizeof(VLA) evaluated at runtime (side
 *             effect), multi-dimensional VLA, VLA function parameter, and the
 *             array-parameter 'static' minimum-size qualifier. The whole body
 *             sits under '#else' of the __STDC_NO_VLA__ guard: a conforming
 *             implementation that does not define the macro must support VLA.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

#ifdef __STDC_NO_VLA__
int main(void) {
    printf("VLA unsupported by this implementation\n");
    printf("SUMMARY: 0/1\n");
    return 0;
}
#else

int vsum(int n, int a[n]) {
    int s = 0;
    for (int i = 0; i < n; i++) s += a[i];
    return s;
}

int statlen(int a[static 5]) { return a[0]; }

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int n = 5;
    int arr[n];
    for (int i = 0; i < n; i++) arr[i] = i * i;
    int s = 0;
    for (int i = 0; i < n; i++) s += arr[i];
    printf("case%d: runtime-sized array sum -> %d\n", total, s);
    if (s == 0 + 1 + 4 + 9 + 16) passed++;

    ++total;
    /* sizeof(VLA) IS evaluated at runtime: the side effect must fire */
    int k = 3;
    int sz = (int)sizeof(int[k++]);
    printf("case%d: sizeof VLA runtime eval -> sz=%d k=%d\n", total, sz, k);
    if (sz == 3 * (int)sizeof(int) && k == 4) passed++;

    ++total;
    int rows = 2, cols = 3;
    int mat[rows][cols];
    mat[1][2] = 7;
    printf("case%d: multi-dim VLA -> %d\n", total, mat[1][2]);
    if (mat[1][2] == 7) passed++;

    ++total;
    int data[5] = {1, 2, 3, 4, 5};
    int r = vsum(5, data);
    printf("case%d: VLA function param -> %d\n", total, r);
    if (r == 15) passed++;

    ++total;
    int r2 = statlen(data);
    printf("case%d: array param static 5 -> %d\n", total, r2);
    if (r2 == 1) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
#endif
