#include <stdio.h>
void bsort(int a[], int n) {
    for (int i = 0; i < n-1; i++) {
        int bound = n - 1 - i;            /* 手动把内循环边界提到外循环 */
        for (int j = 0; j < bound; j++)
            if (a[j] > a[j+1]) { int t = a[j]; a[j] = a[j+1]; a[j+1] = t; }
    }
}
int main(void) {
    int a[10000];
    for (int i = 0; i < 10000; i++) a[i] = 10000 - i;
    bsort(a, 10000);
    printf("sorted=%d\n", a[0]);
    return 0;
}
