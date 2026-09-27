#include <stdio.h>
int fib(int n) {
    if (n < 2) { return n; }
    return fib(n - 1) + fib(n - 2);
}
int max3(int a, int b, int c) {
    int m = a;
    if (b > m) { m = b; }
    if (c > m) { m = c; }
    return m;
}
int main() {
    printf("fib(10) = %d\n", fib(10));
    printf("max3(7, 42, 13) = %d\n", max3(7, 42, 13));
    int x = (2 + 3) * (10 - 4) / 5 % 7;
    printf("expr = %d\n", x);
    int y = -8 / 3;
    int z = -8 % 3;
    printf("div=%d mod=%d\n", y, z);
    printf("nested: %d %d %d\n", fib(6), fib(7), fib(6) + fib(7) * 2);
    printf("cmp: %d %d %d\n", 3 < 5, 5 <= 5, 4 == 4 && 1 != 2);
    int i = 0;
    int acc = 0;
    while (i < 10) {
        if (i % 2 == 0) {
            acc = acc + i;
        }
        i = i + 1;
    }
    printf("even sum 0..9 = %d\n", acc);
    return 0;
}
