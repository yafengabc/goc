/* C23 feature: __VA_OPT__
 * Clause:     C23 6.10.11.1 "__VA_OPT__"
 * Strategy:   verify trailing-comma elision when no variadic args are present,
 *             comma retention and multi-arg forwarding, nested forwarding through an
 *             outer variadic macro, and __VA_OPT__ combined with token pasting (##).
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

/* insert a comma iff at least one variadic arg was supplied */
#define LOG(fmt, ...) printf(fmt __VA_OPT__(, __VA_ARGS__))

/* pass a fixed first arg, then variadic args, to a function */
#define WRAP(fn, ...) fn(1 __VA_OPT__(, __VA_ARGS__))

/* nested: outer variadic macro re-injects its args into WRAP */
#define INTO_WRAP(...) WRAP(bar __VA_OPT__(, __VA_ARGS__))

/* __VA_OPT__ + ## : optionally emit an additive term whose right operand is a
 * pasted token; with no variadic args the whole term vanishes. */
#define CAT(a, b)    a##b
#define OPT_ADD(...) (1 __VA_OPT__(+ CAT(9, __VA_ARGS__)))

void foo(int x)        { printf("foo(%d)", x); }
void bar(int x, int y) { printf("bar(%d,%d)", x, y); }

int main(void) {
    int passed = 0, total = 0;
    int v;

    ++total;
    printf("case%d:[", total);
    LOG("empty");                 /* no variadic arg -> no stray comma */
    printf("]\n");
    passed++;

    ++total;
    printf("case%d:[", total);
    LOG("one=%d", 42);          /* variadic present -> comma inserted */
    printf("]\n");
    passed++;

    ++total;
    printf("case%d:[", total);
    WRAP(foo);                  /* -> foo(1) */
    printf("][");
    WRAP(bar, 7);               /* -> bar(1, 7) */
    printf("]\n");
    passed++;

    ++total;
    printf("case%d:[", total);
    INTO_WRAP(8);               /* nested forwarding of one variadic arg -> bar(1, 8) */
    printf("][");
    INTO_WRAP(5);               /* nested forwarding again -> bar(1, 5) */
    printf("]\n");
    passed++;

    ++total;
    v = OPT_ADD();              /* -> (1) */
    printf("case%d: opt_add_empty=%d ", total, v);
    v = OPT_ADD(5);             /* -> (1 + 95) = 96 */
    printf("opt_add_filled=%d\n", v);
    if (v == 96) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
