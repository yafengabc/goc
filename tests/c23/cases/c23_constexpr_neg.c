/* C23 feature: constexpr NEGATIVE -- non-constant initializer at file scope
 * Clause:     C23 6.7.11 constexpr requires a constant initializer
 * Strategy:   a file-scope constexpr object initialized from a non-constant
 *             value. gcc -std=c2x must reject it ("initializer element is not
 *             constant"); goc must reject it too.
 * Status:     PENDING
 * EXPECT: REJECT
 */
#include <stdio.h>

int glob;

constexpr int bad = glob;

int main(void) {
    printf("should not compile: %d\n", bad);
    return 0;
}
