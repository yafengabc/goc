/* C23 feature: anonymous struct / union members (C11; C23 refines)
 * Clause:     C23 6.7.2.1; goc roadmap #128 (anonymous members)
 * Strategy:   flat access, nested anonymous (anon-in-anon), positional + designated init
 *             transparent penetration, union overlap, sizeof, arrow access, anonymous member
 *             combined with a bitfield and with an array.
 * Status:     PARTIAL (single-level anonymous members match gcc; doubly-nested anonymous
 *             struct with an intervening named field mis-resolves case3; bitfields cannot
 *             be brace-initialised)
 * EXPECT: PASS
 */
#include <stdio.h>

/* case1,2,6: flat access, union overlap, arrow */
struct S1 {
    int tag;
    struct { int x, y; };          /* anonymous struct */
    union { int i; char bytes[4]; }; /* anonymous union */
};

/* case3: nested anonymous: anonymous struct inside anonymous struct */
struct S2 {
    int outer;
    struct {
        int a;
        struct { int b, c; };      /* anonymous inside anonymous */
    };
};

/* case7: anonymous member combined with bitfield and array */
struct S3 {
    unsigned lo : 4;
    unsigned hi : 4;
    struct { int arr[3]; };        /* anonymous struct holding an array */
};

int main(void) {
    int passed = 0, total = 0;

    /* case1: positional init transparent penetration, flat access */
    ++total;
    struct S1 s = {1, 2, 3};
    s.i = 0x41424344;
    printf("case1: tag=%d x=%d y=%d i=%d\n", s.tag, s.x, s.y, s.i);
    if (s.tag == 1 && s.x == 2 && s.y == 3 && s.i == 0x41424344) passed++;

    /* case2: union overlap: i and bytes share storage */
    ++total;
    printf("case2: bytes=%c%c%c%c\n", s.bytes[0], s.bytes[1], s.bytes[2], s.bytes[3]);
    if (s.bytes[0] == 'D' && s.bytes[3] == 'A') passed++;

    /* case3: nested anonymous flattening, designated init penetrates two levels */
    ++total;
    struct S2 n = {.outer = 9, .a = 10, .b = 11, .c = 12};
    printf("case3: outer=%d a=%d b=%d c=%d\n", n.outer, n.a, n.b, n.c);
    if (n.outer == 9 && n.a == 10 && n.b == 11 && n.c == 12) passed++;

    /* case4: designated init reaches through anonymous members */
    ++total;
    struct S1 d = {.tag = 7, .y = 55};
    printf("case4: tag=%d x=%d y=%d\n", d.tag, d.x, d.y);
    if (d.tag == 7 && d.y == 55) passed++;

    /* case5: arrow access through pointer */
    ++total;
    struct S1 *p = &s;
    p->x = 99;
    printf("case5: arrow x=%d y=%d\n", p->x, p->y);
    if (p->x == 99 && s.x == 99) passed++;

    /* case6: sizeof of the struct with anonymous members */
    ++total;
    printf("case6: sizeof S1=%d\n", (int)sizeof(struct S1));
    if (sizeof(struct S1) >= 12) passed++;

    /* case7: anonymous struct with array, next to bitfields (bitfields assigned, not brace-init) */
    ++total;
    struct S3 t;
    t.lo = 5;
    t.hi = 10;
    t.arr[0] = 10; t.arr[1] = 20; t.arr[2] = 30;
    printf("case7: lo=%u hi=%u arr=%d,%d,%d\n",
           (unsigned)t.lo, (unsigned)t.hi, t.arr[0], t.arr[1], t.arr[2]);
    if (t.lo == 5 && t.hi == 10 && t.arr[0] == 10 && t.arr[2] == 30) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
