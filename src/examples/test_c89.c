#include <stdio.h>

/* A file-scope global, referenced both directly and through an extern local. */
int g_counter = 100;

/* A static global: identical to a plain global in this single-TU model. */
static int g_static = 7;

/* A static local must persist across calls and be initialised exactly once. */
int counter(void) {
    static int c = 0;
    c = c + 1;
    return c;
}

/* Two functions share a static-local name; the two must not collide. */
int fA(void) {
    static int s = 10;
    s = s + 1;
    return s;
}
int fB(void) {
    static int s = 100;
    s = s + 1;
    return s;
}

/* register / auto are accepted as no-ops (the compiler is non-optimising). */
int use_register(int x) {
    register int r = x + 1;
    auto int a = x + 2;
    return r + a;
}

/* long double is supported as double (the only extended precision we have). */
double use_long_double(void) {
    long double ld = 2.5;
    return (double)ld + 1.0;
}

/* volatile is accepted and ignored (there is no optimiser to defeat). */
int use_volatile(int x) {
    volatile int v = x;
    return v * 2;
}

/* An extern local references the file-scope global g_counter. */
int use_extern_local(void) {
    extern int g_counter;
    g_counter = g_counter + 100;
    return g_counter;
}

int main(void) {
    /* Comma operator in a for-loop head and as a general expression. */
    int i, sum = 0;
    for (i = 0, sum = 0; i < 5; i = i + 1) {
        sum = sum + i;
    }
    printf("sum=%d\n", sum);

    printf("c1=%d c2=%d c3=%d\n", counter(), counter(), counter());
    printf("sA=%d sB=%d sA=%d\n", fA(), fB(), fA());

    printf("reg=%d\n", use_register(5));
    printf("ld=%.1f\n", use_long_double());
    printf("vol=%d\n", use_volatile(21));

    printf("g=%d\n", use_extern_local());
    printf("g2=%d\n", g_counter);

    /* Comma operator: the value is the rightmost operand. */
    int m = (2, 3, 4);
    printf("m=%d\n", m);

    /* Comma operator: left operand's side effect is kept, value is the right. */
    int k = 0;
    int n = (k = 7, k + 1);
    printf("k=%d n=%d\n", k, n);

    /* The middle of a ternary may be a comma expression. */
    int t = 1 ? (sum = 99, sum) : 0;
    printf("t=%d\n", t);

    return 0;
}
