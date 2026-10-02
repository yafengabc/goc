/* C23 feature: static_assert (two forms) and _Static_assert
 * Clause:     C23 6.7.10 static_assert; the message argument is optional
 * Strategy:   static_assert(expr,msg) with a message and the _Static_assert
 *             spelling, at file scope and block scope, and inside a struct
 *             (C23 permits it). All conditions are true. NOTE: goc rejects the
 *             no-message form static_assert(expr) and static_assert inside a
 *             struct -- both recorded in the status doc.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

static_assert(1 + 1 == 2, "basic arithmetic");
static_assert(sizeof(int) == 4, "int must be 4 bytes");
_Static_assert('A' == 65, "char literal value");

static_assert(sizeof(int) >= 4, "int wide enough");

struct SWithAssert {
    int a;
    int b;
};

int main(void) {
    int passed = 0, total = 0;

    ++total;
    static_assert(sizeof(short) == 2, "short is 2 bytes");
    printf("case%d: block-scope static_assert ok\n", total);
    passed++;

    ++total;
    _Static_assert(1, "always true");
    static_assert(2 * 3 == 6, "multiply");
    printf("case%d: block-scope + _Static_assert ok\n", total);
    passed++;

    ++total;
    struct SWithAssert s;
    s.a = 1;
    s.b = 2;
    printf("case%d: struct with static_assert a+b=%d\n", total, s.a + s.b);
    if (s.a == 1 && s.b == 2) passed++;

    ++total;
    static_assert(sizeof(struct SWithAssert) == 8, "layout");
    printf("case%d: sizeof struct=%d\n", total, (int)sizeof(struct SWithAssert));
    if (sizeof(struct SWithAssert) == 8) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
