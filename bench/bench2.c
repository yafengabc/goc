#include <stdio.h>
int fib(int n) { return n < 2 ? n : fib(n-1) + fib(n-2); }
void bsort(int a[], int n) {
    for (int i = 0; i < n-1; i++)
        for (int j = 0; j < n-1-i; j++)
            if (a[j] > a[j+1]) { int t = a[j]; a[j] = a[j+1]; a[j+1] = t; }
}
int main(void) {
    int a[10000];
    for (int i = 0; i < 10000; i++) a[i] = 10000 - i;
    bsort(a, 10000);
    printf("fib=%d sorted=%d\n", fib(35), a[0]);
    return 0;
}
