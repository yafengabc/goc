/* ============================================================
   c89_struct.c - C89 structures: nesting, self-ref pointer, by-value pass
   Standard   : ISO/IEC 9899:1990 (C89) 6.5.2.3 structure/union specifiers
   Strategy   : 6 subcases: basic field access, by-value struct assignment,
                by-value struct parameter and return, self-referential pointer
                (linked node), nested struct inside struct, sizeof/layout.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

typedef struct { int x; int y; } Point;

struct Node {
    int val;
    struct Node *next;   /* self-referential pointer (incomplete type ok) */
};

struct Pair {
    Point p;
    int tag;
};

Point move(Point q, int dx, int dy) {
    q.x = q.x + dx;
    q.y = q.y + dy;
    return q;
}

int main(void) {
    Point a;
    Point b;
    struct Node n1;
    struct Node n2;
    struct Pair pr;

    /* case1: basic struct field access and assignment */
    a.x = 3;
    a.y = 4;
    printf("case1: %d %d\n", a.x, a.y);

    /* case2: by-value struct assignment (memberwise copy) */
    b = a;
    b.x = b.x + 10;
    printf("case2: %d %d | %d %d\n", a.x, a.y, b.x, b.y);

    /* case3: by-value struct parameter and return */
    b = move(a, 100, 200);
    printf("case3: %d %d\n", b.x, b.y);

    /* case4: self-referential pointer / two-node link */
    n1.val = 1;
    n2.val = 2;
    n1.next = &n2;
    n2.next = 0;
    printf("case4: %d %d\n", n1.val, n1.next->val);

    /* case5: nested struct inside struct */
    pr.p = a;
    pr.tag = 7;
    printf("case5: %d %d %d\n", pr.p.x, pr.p.y, pr.tag);

    /* case6: sizeof of structs (layout, record) */
    printf("case6: %d %d %d\n",
        (int)sizeof(Point), (int)sizeof(struct Node), (int)sizeof(struct Pair));

    return 0;
}
