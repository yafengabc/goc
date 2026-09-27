// Exercises struct/union support end to end: definitions, locals, globals,
// whole-aggregate assignment, member read/write (int, char, double, nested),
// passing structs BY VALUE (hidden pointer; the callee gets a private copy),
// and returning structs BY VALUE through the hidden sret pointer -- including
// struct-returning calls nested inside argument lists of other calls.
//
// The pick() call uses six user arguments on top of the hidden pointer, which
// forces the stack-argument path on BOTH targets (Win64 user register
// capacity is 3 with sret, SysV is 5).

struct Point { int x; int y; };
struct Small { char a; char b; };
struct Rect { struct Point tl; struct Point br; };
struct Mix { int tag; double v; };
union Num { int i; char c; };

struct Point gpt; // global struct: zero-initialised in .data

struct Point make(int x, int y) {
    struct Point p;
    p.x = x;
    p.y = y;
    return p;
}

struct Point idp(struct Point p) {
    return p;
}

int sum(struct Point p) {
    return p.x + p.y;
}

// Bumping the parameter must not be visible to the caller: the struct was
// passed by value (the callee works on its own copy).
int bump(struct Point p) {
    p.x = p.x + 100;
    p.y = p.y + 200;
    return p.x + p.y;
}

// Sub-8-byte struct: exercises the byte-wise tail of copyBytes on both the
// sret write and the by-value parameter copy.
struct Small mksmall(int a, int b) {
    struct Small s;
    s.a = a;
    s.b = b;
    return s;
}

int smallsum(struct Small s) {
    return s.a + s.b;
}

// A struct whose members are themselves structs, assigned from nested
// struct-returning calls, then returned as a whole.
struct Rect mkrect(int x0, int y0, int x1, int y1) {
    struct Rect r;
    r.tl = make(x0, y0);
    r.br = make(x1, y1);
    return r;
}

// Six user arguments plus the hidden result pointer: pushes user arguments
// 4/5/6 onto the caller's stack-argument area on Win64, argument 6 on SysV.
int pick(struct Point p, int a, int b, int c, int d, int e) {
    return p.x + p.y + a + b + c + d + e;
}

struct Mix mkmix(int tag, double v) {
    struct Mix m;
    m.tag = tag;
    m.v = v;
    return m;
}

union Num mknum(int v) {
    union Num u;
    u.i = v;
    return u;
}

int main() {
    struct Point p;
    struct Point q;
    struct Small s;
    struct Rect r;
    struct Mix m;
    union Num u;
    struct Point pts[3];
    int i;

    printf("== by value ==\n");
    p = make(3, 4);
    printf("%d %d\n", p.x, p.y);
    printf("%d\n", sum(make(10, 20)));
    printf("%d\n", bump(make(1, 1)));
    bump(p);
    printf("%d %d\n", p.x, p.y); // caller's copy unchanged

    printf("== assign ==\n");
    q = p;
    q.x = 99;
    printf("%d %d %d %d\n", p.x, p.y, q.x, q.y);

    printf("== sret chain ==\n");
    q = idp(make(5, 6));
    printf("%d %d\n", q.x, q.y);
    printf("%d\n", sum(idp(make(7, 8))));

    printf("== small ==\n");
    s = mksmall(65, 66);
    printf("%d %d %d\n", s.a, s.b, smallsum(s));

    printf("== nested ==\n");
    r = mkrect(1, 2, 30, 40);
    printf("%d %d %d %d\n", r.tl.x, r.tl.y, r.br.x, r.br.y);
    printf("%d\n", sum(r.tl));

    printf("== stack args ==\n");
    printf("%d\n", pick(make(1, 2), 3, 4, 5, 6, 7));

    printf("== array ==\n");
    for (i = 0; i < 3; i++) {
        pts[i] = make(i, i * i);
    }
    printf("%d %d %d\n", pts[0].y, pts[1].y, pts[2].y);
    printf("%d\n", sum(pts[2]));

    printf("== double member ==\n");
    m = mkmix(7, 2.5);
    printf("%d %f\n", m.tag, m.v);

    printf("== union ==\n");
    u = mknum(300);
    printf("%d %d\n", u.i, u.c); // c aliases the low byte of i
    u.c = 7;
    printf("%d\n", u.i);

    printf("== global ==\n");
    printf("%d %d\n", gpt.x, gpt.y);
    gpt = make(11, 22);
    gpt.x = 33;
    printf("%d %d\n", gpt.x, gpt.y);

    return 0;
}
