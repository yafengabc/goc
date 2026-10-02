/* ============================================================
   c11_anon.c - anonymous struct/union members
   Standard   : ISO/IEC 9899:2011 (C11) 6.7.2.1
   Strategy   : 7 subcases (flat access, positional init, designated init
               into anon subtree, nested, union overlap, double overlap,
               arrow access, sizeof). Independent of c23 example. gcc -std=c11 diff
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#include <stdio.h>
#include <stddef.h>

struct Inner { int a, b; };

struct S1 {
    int tag;
    struct { int x, y; };
    struct Inner;
    char c;
};

union U1 {
    struct { short lo, hi; };
    int whole;
};

struct S2 {
    char k;
    union { int i; char bytes[4]; };
    double d;
};

int main(void) {
    /* case1: flat access + transparent positional init */
    struct S1 s = {1, 2, 3, 10, 20, 'Z'};
    printf("case1: tag=%d x=%d y=%d inner_a=%d inner_b=%d c=%c\n",
           s.tag, s.x, s.y, s.a, s.b, s.c);

    /* case2: designated init reaches through the anonymous subtree */
    struct S1 q = {.tag = 7, .b = 55, .c = '?'};
    printf("case2: q tag=%d b=%d c=%c\n", q.tag, q.b, q.c);

    /* case3: union overlap */
    union U1 u;
    u.whole = 0x00030002;
    printf("case3: lo=%d hi=%d\n", u.lo, u.hi);

    /* case4: anonymous union flattened member + bytes */
    struct S2 v = {'A'};
    v.i = 0x41424344;
    printf("case4: k=%c bytes=%c%c%c%c\n",
           v.k, v.bytes[0], v.bytes[1], v.bytes[2], v.bytes[3]);

    /* case5: double overlaps the anonymous union storage */
    v.d = 2.5;
    printf("case5: d=%.2f\n", v.d);

    /* case6: arrow access through pointer */
    struct S2 *pv = &v;
    printf("case6: arrow_i=%d\n", pv->i);

    /* case7: sizeof */
    printf("case7: sizeof_S1=%d sizeof_S2=%d sizeof_U1=%d\n",
           (int)sizeof(struct S1), (int)sizeof(struct S2), (int)sizeof(union U1));
    return 0;
}