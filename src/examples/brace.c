// Braced initialisers for arrays, structs and unions: positional and
// designated (".x ="), length inference ("int a[] = {...}"), partial
// initialisation (the rest is zero), nesting, string literals inside braces,
// and the global/.data form. Also exercises struct-member-array addressing
// and decay (mx.name as an element, an argument and a %s), which this work
// fixed alongside the initialiser itself.
#include <stdio.h>

struct point { int x; int y; };
struct line { struct point a; struct point b; };
struct mixed { char name[8]; int n; double d; };
union u { int i; char c[4]; };
struct tag { char name[6]; struct point p; };

int ia[5] = {1, 2, 3};
int m22[2][2] = {{1, 2}, {3, 4}};
struct point gp = {7, 8};
struct line gl = {{1, 2}, {3, 4}};
struct mixed gm = {"ab", 5, 2.5};
struct point path[3] = {{1, 1}, {2, 4}};
struct tag gt = {"box", {5, 6}};

int main() {
    int a[4] = {10, 20};
    int full[] = {1, 2, 3};
    struct point p = {3, 4};
    struct line l = {{5, 6}, {7, 8}};
    struct mixed mx = {"hi", 9, 1.25};
    struct point z = {};
    struct point pd = {.y = 9, .x = 8};
    union u uu = {0x41424344};
    struct point lp[2] = {{7, 7}, {8, 8}};
    char rows[2][6] = {"ab", "cd"};
    struct tag lt = {.p = {.x = 3, .y = 4}};

    printf("== local ==\n");
    printf("a: %d %d %d %d\n", a[0], a[1], a[2], a[3]);
    printf("full: %d sizeof=%d\n", full[2], (int)sizeof(full));
    printf("p: %d %d\n", p.x, p.y);
    printf("l: %d %d %d %d\n", l.a.x, l.a.y, l.b.x, l.b.y);
    printf("mx: %s %d %.2f\n", mx.name, mx.n, mx.d);
    printf("z: %d %d\n", z.x, z.y);
    printf("pd: %d %d\n", pd.x, pd.y);
    printf("uu: %d %d\n", (int)uu.c[0], (int)uu.c[3]);
    printf("lp0: %d %d lp1: %d %d\n", lp[0].x, lp[0].y, lp[1].x, lp[1].y);
    printf("rows: %s %s\n", rows[0], rows[1]);
    printf("lt: %d %d\n", lt.p.x, lt.p.y);
    printf("== global ==\n");
    printf("ia: %d %d %d %d\n", ia[0], ia[1], ia[2], ia[3]);
    printf("m22: %d %d %d %d\n", m22[0][0], m22[0][1], m22[1][0], m22[1][1]);
    printf("gp: %d %d\n", gp.x, gp.y);
    printf("gl: %d %d %d %d\n", gl.a.x, gl.a.y, gl.b.x, gl.b.y);
    printf("gm: %s %d %.2f\n", gm.name, gm.n, gm.d);
    printf("path: %d %d %d %d\n", path[0].x, path[0].y, path[2].x, path[2].y);
    printf("gt: %s %d %d\n", gt.name, gt.p.x, gt.p.y);
    printf("sizeof rows=%d\n", (int)sizeof(rows));
    return 0;
}
