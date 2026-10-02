#include <stdio.h>
int fib(int n) { return n < 2 ? n : fib(n-1) + fib(n-2); }
int main(void) {
    long s = 0;
    for (int i = 0; i < 1000000; i++) s += i * 3 + 1;
    printf("fib=%d sum=%ld\n", fib(30), s);
    return 0;
}
