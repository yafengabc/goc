/* C11 anonymous struct/union members: flattened field access, transparent
   positional initialisation, designated initialisation into the anonymous
   subtree, union overlap, and sizeof/layout -- all cross-checked against
   gcc -std=c11 on the same source. */
#include <stdio.h>

struct Inner { int a, b; };

struct S1 {
    int tag;
    struct { int x, y; };        /* anonymous struct */
    struct Inner;                /* anonymous tagged struct */
    char c;
};

union U1 {
    struct { short lo, hi; };    /* anonymous struct in union: overlap at 0 */
    int whole;
};

struct S2 {
    char k;
    union {                      /* anonymous union */
        int i;
        char bytes[4];
    };
    double d;
};

int main(void) {
    /* transparent positional init: tag, x, y, a, b, c */
    struct S1 s = {1, 2, 3, 10, 20, 'Z'};
    printf("tag=%d x=%d y=%d c=%c\n", s.tag, s.x, s.y, s.c);
    printf("inner a=%d b=%d\n", s.a, s.b);
    printf("sizeof S1=%d\n", (int)sizeof(struct S1));

    union U1 u;
    u.whole = 0x00030002;
    printf("lo=%d hi=%d\n", u.lo, u.hi);
    printf("sizeof U1=%d\n", (int)sizeof(union U1));

    struct S2 v = {'A'};
    v.i = 0x41424344;
    printf("k=%c bytes=%c%c%c%c\n", v.k, v.bytes[0], v.bytes[1], v.bytes[2], v.bytes[3]);
    v.d = 2.5;
    printf("d=%.2f sizeof S2=%d\n", v.d, (int)sizeof(struct S2));

    struct S2 *p = &v;
    printf("arrow i=%d\n", p->i);

    /* designated init reaches into anonymous members */
    struct S1 q = {.tag = 7, .b = 55, .c = '?'};
    printf("q=%d %d %d %d %d %c\n", q.tag, q.x, q.y, q.a, q.b, q.c);
    return 0;
}
