/* C23 feature: static_assert NEGATIVE -- a false condition
 * Clause:     C23 6.7.10 static_assert must evaluate to a non-constant / false
 * Strategy:   static_assert(0, ...) has a false condition, so it must abort
 *             compilation. gcc -std=c2x must reject it; goc must reject it too.
 * Status:     PENDING
 * EXPECT: REJECT
 */
#include <stdio.h>

static_assert(0, "this condition is always false");

int main(void) {
    printf("should not compile\n");
    return 0;
}
