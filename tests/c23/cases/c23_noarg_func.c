/* C23 feature: empty prototype f() == f(void) (no longer old-style K&R declaration)
 * Clause:     C23 6.11.6 / 6.7.6.3; goc roadmap "no-arg f()==f(void)"
 * Strategy:   int f(); declaration is compatible with int f(void) definition; direct calls;
 *             function-pointer identity between () and (void) forms; mixing g(void) and g().
 *             No arguments are ever passed (that is the E-group c23_noarg_args.c scope).
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

int f();                 /* empty prototype: C23 == int f(void) */
int f(void) { return 42; }

int g(void);             /* prototyped form first */
int g() { return 7; }    /* empty-prototype redefinition must be compatible */

int main(void) {
    int passed = 0, total = 0;

    /* case1: call f() declared as int f() */
    ++total;
    int r1 = f();
    printf("case1: f() = %d\n", r1);
    if (r1 == 42) passed++;

    /* case2: call g() declared as int g(void) */
    ++total;
    int r2 = g();
    printf("case2: g() = %d\n", r2);
    if (r2 == 7) passed++;

    /* case3: function pointer to f with (void) type assigns fine */
    ++total;
    int (*pfv)(void) = f;
    int r3 = pfv();
    printf("case3: (*pfv)(void)() = %d\n", r3);
    if (r3 == 42) passed++;

    /* case4: function pointer to g with empty () type assigns fine */
    ++total;
    int (*pfn)() = g;
    int r4 = pfn();
    printf("case4: (*pfn)() = %d\n", r4);
    if (r4 == 7) passed++;

    /* case5: round-trip: f through a () pointer, then through a (void) pointer */
    ++total;
    int (*pempty)() = f;
    int (*pvoid)(void) = pempty;
    int r5 = pvoid();
    printf("case5: f via ()->(void) pointer = %d\n", r5);
    if (r5 == 42) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
