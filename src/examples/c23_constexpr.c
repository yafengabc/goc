/* C23 MVP item #9 (constexpr, light version). goc folds constexpr object
 * constants and constexpr functions invoked with constant arguments; the values
 * print the same on both targets.
 * Golden: src/expected/c23_constexpr.txt */

#include <stdio.h>

constexpr int N = 10;
constexpr int sq(int v) { return v * v; }
constexpr double pi = 3.14159;

int main(void) {
    printf("N=%d\n", N);
    printf("sq5=%d\n", sq(5));
    printf("pi=%f\n", pi);
    return 0;
}
