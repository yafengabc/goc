/* C23 feature: empty-argument-list macros  #define F()  and variadic  #define F(...)
 * Clause:     C23 6.10.3 "Macro replacement" (empty argument list; C23 clarified
 *             that F() with zero params is distinct from F(...))
 * Strategy:   verify a zero-parameter macro whose body expands to nothing, a
 *             zero-parameter macro with a body, a one-parameter macro for contrast,
 *             and a variadic macro invoked empty vs with args.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

#define EMP()              /* zero params, empty body: expands to nothing */
#define CONST42() 42       /* zero params, non-empty body */
#define ID(x)      (x)     /* one parameter */
#define VARI(...)          /* variadic: zero or more args, empty body */

int main(void) {
    int passed = 0, total = 0;
    int ok;

    int a = 1 EMP();       /* EMP() -> nothing: a = 1 */
    ++total;
    printf("case%d: 1 EMP() -> %d\n", total, a);
    ok = (a == 1);
    if (ok) passed++;

    int b = CONST42();     /* -> 42 */
    ++total;
    printf("case%d: CONST42() -> %d\n", total, b);
    ok = (b == 42);
    if (ok) passed++;

    int c = ID(7);         /* contrast: one-param macro */
    ++total;
    printf("case%d: ID(7) -> %d\n", total, c);
    ok = (c == 7);
    if (ok) passed++;

    int d = 1 VARI();      /* variadic invoked empty -> nothing: d = 1 */
    ++total;
    printf("case%d: 1 VARI() -> %d\n", total, d);
    ok = (d == 1);
    if (ok) passed++;

    int e = 2 VARI(9, 8);  /* variadic invoked with args, still empty body */
    ++total;
    printf("case%d: 2 VARI(9,8) -> %d\n", total, e);
    ok = (e == 2);
    if (ok) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
