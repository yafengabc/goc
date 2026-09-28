// embstr.c -- char* strings embedded in global/static aggregates.
//
// A global "struct { char *p; } s = {"hi"}" cannot store the pointer in
// .data: goa has no data relocations for dq, so the address must be written
// at startup. The entry stub binds every pointer slot a string literal
// initialises -- top-level globals, static locals, and char* members nested
// at any depth inside a braced initialiser -- with
// `lea rax,[rip+<obj>]; lea rdx,[rip+<str>]; mov [rax+<off>],rdx`.
// This is the regression lock for that path.

#include <stdio.h>

struct S { char *p; int n; };
struct S s1 = {"hello", 42};
struct S sarr[2] = {{"one", 1}, {"two", 2}};

struct Nested { struct { char *a[2]; } in; char *tail; };
struct Nested nn = {{{"x0", "x1"}, }, "tail"};

struct D { int n; char *p; };
struct D dd = {.n = 9, .p = "designated"};

union U { char *p; int x; };
union U u = {"union-str"};

struct Mix { char buf[8]; char *p; };
struct Mix mx = {"hi", "bufptr"};

struct Big { char pad[300]; char *p; };
struct Big big = {.p = "bigptr"};

char *arr3[3] = {"aa", "bb", "cc"};

int static_test(void) {
    static char *msg = "static-str";
    static struct S ss = {"static-s", 5};
    static char *sarr[2] = {"sa", "sb"};
    printf("%s %s %d\n", msg, ss.p, ss.n);
    printf("%s %s\n", sarr[0], sarr[1]);
    return 0;
}

int main() {
    printf("%s %d\n", s1.p, s1.n);
    printf("%s %s\n", sarr[0].p, sarr[1].p);
    printf("%s %s %s\n", nn.in.a[0], nn.in.a[1], nn.tail);
    printf("%d %s\n", dd.n, dd.p);
    printf("%s\n", u.p);
    printf("%s %s\n", mx.buf, mx.p);
    printf("%s\n", big.p);
    printf("%s %s %s\n", arr3[0], arr3[1], arr3[2]);
    static_test();
    return 0;
}
