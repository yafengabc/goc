
#include <stdio.h>

struct G { _Bool f; int n; } g = { 9, 42 };

_Bool is_pos(int x) {
    return x > 0;
}

int main() {
    _Bool b;
    b = 0;
    printf("b0=%d\n", b);
    b = 5;      // any non-zero -> 1
    printf("b5=%d\n", b);
    b = -3;
    printf("bneg=%d\n", b);
    b = 100;
    printf("b100=%d\n", b);

    // comparisons yield _Bool (0 or 1)
    printf("cmp: %d %d %d\n", (3 > 2), (2 > 3), (3 == 3));

    // _Bool promotes to int in arithmetic (0/1)
    _Bool t = 1, f = 0;
    printf("arith: %d %d %d\n", t + t, t + f, f - t);

    // function returning _Bool
    printf("fn: %d %d\n", is_pos(7), is_pos(-1));

    // _Bool used in conditions
    if (b) printf("b is true\n");
    if (!b) printf("b is false\n");

    // _Bool in a struct
    struct S { _Bool flag; int n; };
    struct S s;
    s.flag = 9;   // -> 1
    s.n = 42;
    printf("struct: %d %d\n", s.flag, s.n);

    // int -> _Bool truncation
    int big = 256;
    _Bool bb = big;
    printf("bb=%d\n", bb);

    // _Bool struct member: assign, ++, -- all normalise to 0/1
    struct B { _Bool f; int n; } bv;
    bv.f = 7;
    printf("bm_assign=%d\n", bv.f);
    bv.f++;
    printf("bm_inc=%d\n", bv.f);
    bv.f = 5;
    bv.f--;
    printf("bm_dec=%d\n", bv.f);

    // global _Bool brace init normalises too
    printf("global: %d %d\n", g.f, g.n);

    return 0;
}
