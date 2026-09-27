// Exercises function pointers end to end: pointers to functions held in
// variables, passed as parameters, returned from functions, stored in arrays,
// globals and struct members, and called through every spelling C allows
// (fp(x), (*fp)(x), tab[i](x), obj.m(x), pick(0)(x)). Also covers typedef'd
// function-pointer types, taking a function's address with &, decay of a bare
// function designator, comparing two function addresses, a double-returning
// target (XMM result), a struct-returning target (hidden result pointer), a
// pointer to a variadic library function, and addressing a library function.

#include <stdio.h>
#include <string.h>

int add1(int x) { return x + 1; }
int mul2(int x) { return x * 2; }

int add(int a, int b) { return a + b; }
int sub(int a, int b) { return a - b; }

double half(double x) { return x / 2.0; }

struct pt { int x; int y; };
struct pt mkpt(int a, int b) {
    struct pt p;
    p.x = a;
    p.y = b;
    return p;
}

typedef int (*binop)(int, int);
typedef int (*vfn)(const char *, ...);

// Global function pointers: zero initialised here, pointed at a target later.
int (*g_cb)(int);
struct pt (*g_pt)(int, int);

// A callback parameter: the callee receives the target by pointer.
int apply(binop op, int a, int b) { return op(a, b); }

// A function returning a function pointer.
binop pick(int which) {
    if (which == 0) return add;
    return sub;
}

// Same thing written with a grouped declarator: a function taking an int and
// returning a pointer to int(int).
int (*get(int k))(int) {
    if (k == 0) return add1;
    return mul2;
}

// A typedef may name a plain function type; "transform *" is then a pointer
// to it.
typedef int transform(int);

struct ops {
    binop f;
    int   k;
};

int main() {
    printf("== variable ==\n");
    int (*fp)(int) = add1;
    printf("direct: %d\n", fp(10));
    printf("star:   %d\n", (*fp)(10));
    fp = &mul2;
    printf("addr:   %d\n", fp(21));

    printf("== typedef + callback ==\n");
    binop op = add;
    printf("op:     %d\n", op(3, 4));
    printf("apply:  %d\n", apply(sub, 9, 4));

    printf("== table of pointers ==\n");
    int (*tab[2])(int);
    tab[0] = add1;
    tab[1] = mul2;
    int i = 0;
    while (i < 2) {
        printf("tab[%d]: %d\n", i, tab[i](5));
        i = i + 1;
    }

    printf("== struct member ==\n");
    struct ops o;
    o.f = add;
    o.k = 100;
    printf("member: %d\n", o.f(o.k, 1));

    printf("== returned pointer ==\n");
    printf("pick0:  %d\n", pick(0)(20, 5));
    printf("pick1:  %d\n", pick(1)(20, 5));

    printf("== getter ==\n");
    printf("get0:   %d\n", get(0)(5));
    printf("get1:   %d\n", (*get(1))(5));
    transform *tp = add1;
    printf("alias:  %d\n", tp(6));

    printf("== compare ==\n");
    int (*again)(int) = add1;
    printf("same:   %d\n", again == add1);
    printf("diff:   %d\n", again == mul2);

    printf("== double target ==\n");
    double (*dfp)(double) = half;
    printf("half:   %f\n", dfp(9.0));

    printf("== struct return ==\n");
    struct pt (*mp)(int, int) = mkpt;
    struct pt p = mp(1, 2);
    printf("p:      %d %d\n", p.x, p.y);
    printf("field:  %d\n", mp(5, 6).y);

    printf("== variadic pointer ==\n");
    vfn pf = printf;
    (*pf)("via variadic pointer: %d %s\n", 7, "yes");

    printf("== global pointers ==\n");
    g_cb = add1;
    printf("g_cb:   %d\n", g_cb(4));
    g_pt = mkpt;
    struct pt gp = g_pt(9, 10);
    printf("g_pt:   %d %d\n", gp.x, gp.y);

    printf("== library address ==\n");
    unsigned long (*slen)(const char *) = strlen;
    printf("strlen: %d\n", slen("abcd"));

    return 0;
}
