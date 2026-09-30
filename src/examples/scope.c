#include <stdio.h>

// Block-scoped variable shadowing must work correctly:
//   * sibling blocks may reuse a name without their homes colliding;
//   * an inner declaration masks an outer one of the same name;
//   * a for-loop variable stays local to the loop;
//   * nested blocks see the correct enclosing variable.
int main(void) {
    double z = 0.0;
    long L = 0;
    {
        double out = 3.5;
        z = out;
    }
    {
        long out = 7;
        L = out;
    }
    int x = 100;
    {
        int x = 5;   // masks the outer x
        x = x + 1;   // 6, discarded with the block
    }
    // outer x must be untouched (still 100)
    int sum = 0;
    for (int i = 0; i < 5; i++) {
        sum += i;     // 0..4 -> 10
    }
    int i = 42;       // independent of the loop's i
    int v = 1;
    {
        int w = 10;
        {
            int w = 20;   // masks the parent block's w
            v = v + w;    // 1 + 20 = 21
        }
        v = v + w;        // 21 + 10 = 31
    }
    printf("z=%f L=%ld x=%d sum=%d i=%d v=%d\n", z, L, x, sum, i, v);
    if (z == 3.5 && L == 7 && x == 100 && sum == 10 && i == 42 && v == 31) {
        return 0;
    }
    return 1;
}
