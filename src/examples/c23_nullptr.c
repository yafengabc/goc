/* C23 MVP item #6 (nullptr / nullptr_t). nullptr_t is a typedef for void* in
 * goc's model, and nullptr is the null pointer constant. Sizes and comparisons
 * are identical on both targets.
 * Golden: src/expected/c23_nullptr.txt */

#include <stdio.h>
#include <stddef.h>

int main(void) {
    nullptr_t p = nullptr;
    printf("null==0=%d\n", p == 0);
    printf("sizeof=%d\n", (int)sizeof(nullptr_t));
    int *q = nullptr;
    printf("qnull=%d\n", q == nullptr);
    int n = 42;
    int *r = &n;            /* a non-null pointer */
    printf("rnonnull=%d\n", r != nullptr);
    return 0;
}
