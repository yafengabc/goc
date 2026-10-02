/* C23 MVP item #10 (attributes [[...]]). The deprecated / nodiscard attributes
 * emit a warning on stderr (not captured by the golden) but otherwise compile
 * and run normally. Any other attribute is silently accepted.
 * Golden: src/expected/c23_attr.txt */

#include <stdio.h>

[[nodiscard]] int twice(int x) { return x * 2; }

[[deprecated]] int legacy(void) { return 1; }

int main(void) {
    int r = twice(21);
    printf("twice=%d\n", r);
    printf("legacy=%d\n", legacy());
    return 0;
}
