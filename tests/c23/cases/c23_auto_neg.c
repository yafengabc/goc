/* C23 feature: auto NEGATIVE -- auto without an initializer
 * Clause:     C23 6.7.9 auto requires an initializer to deduce the type
 * Strategy:   `auto x;` has no initializer, so the type cannot be deduced.
 *             gcc -std=c2x must reject it; goc must reject it too.
 * Status:     PENDING
 * EXPECT: REJECT
 */
#include <stdio.h>

int main(void) {
    auto x;
    printf("should not compile: %d\n", x);
    return 0;
}
