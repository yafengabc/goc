
#include <stdio.h>

int main() {
    const int K = 100;
    int x = K + 1;
    printf("const use: %d\n", x);

    const int *p = &x;          // pointer to const int
    printf("via const ptr: %d\n", *p);

    const _Bool T = 1;
    if (T) printf("const bool true\n");

    int arr[3] = {1, 2, 3};
    const int *q = arr;
    printf("read const ptr: %d %d\n", q[0], q[2]);

    return 0;
}
