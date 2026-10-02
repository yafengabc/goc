/* C23 feature: compound literals (T){...}
 * Clause:     C23 6.5.2.5 (compound literals)
 * Strategy:   block-scope scalar/aggregate values, &(T){...} take-address,
 *             array-to-pointer decay, per-iteration re-initialisation inside a
 *             loop (gcc reuses one stack slot but re-zeros it each iteration;
 *             we assert the value resets), struct/union, designated init,
 *             by-value function argument, nested aggregate.
 * Status:     PENDING
 * EXPECT: PASS
 *
 * goc gaps found during testing (NOT compiled into this file; recorded in
 * group_D2.md):
 *   - file-scope compound literal  -> type error: "compound literal requires
 *       block scope (file-scope static literals are not supported)"
 *   - const-qualified literal      -> type error: "compound literal is
 *       const-qualified"
 */
#include <stdio.h>

struct Pt { int x; int y; };
union U { int i; double d; };
struct Outer { struct Pt pt; int z; };

int take_int(int v) { return v + 100; }

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int a = (int){5};
    printf("case%d: block scalar -> %d\n", total, a);
    if (a == 5) passed++;

    ++total;
    {
        int *p = &(int){7};
        *p = 9;
        printf("case%d: mutable through & -> %d\n", total, *p);
        if (*p == 9) passed++;
    }

    ++total;
    {
        const int *q = &(int){5};
        printf("case%d: &(int){5} deref -> %d\n", total, *q);
        if (*q == 5) passed++;
    }

    ++total;
    int *ap = (int[]){1, 2, 3};
    printf("case%d: array decay -> %d %d %d\n", total, ap[0], ap[1], ap[2]);
    if (ap[0] == 1 && ap[1] == 2 && ap[2] == 3) passed++;

    ++total;
    /* gcc reuses one stack slot across loop iterations but re-initialises the
     * unnamed object every iteration: scribble then verify it resets. */
    int reinit = 1;
    for (int i = 0; i < 3; i++) {
        int *q = &(int){7};
        if (*q != 7) reinit = 0;
        *q = 999;
    }
    printf("case%d: loop re-init each iter -> %d\n", total, reinit);
    if (reinit == 1) passed++;

    ++total;
    struct Pt s = (struct Pt){1, 2};
    printf("case%d: struct literal -> %d %d\n", total, s.x, s.y);
    if (s.x == 1 && s.y == 2) passed++;

    ++total;
    union U uu = (union U){42};
    printf("case%d: union literal -> %d\n", total, uu.i);
    if (uu.i == 42) passed++;

    ++total;
    struct Pt t = (struct Pt){.y = 5, .x = 1};
    printf("case%d: designated literal -> %d %d\n", total, t.x, t.y);
    if (t.x == 1 && t.y == 5) passed++;

    ++total;
    int r = take_int((int){13});
    printf("case%d: by-value arg -> %d\n", total, r);
    if (r == 113) passed++;

    ++total;
    struct Outer o = (struct Outer){ {1, 2}, 3 };
    printf("case%d: nested aggregate -> %d %d %d\n", total, o.pt.x, o.pt.y, o.z);
    if (o.pt.x == 1 && o.pt.y == 2 && o.z == 3) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
