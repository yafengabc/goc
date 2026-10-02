/* C23 MVP item #5 (typeof / typeof_unqual). typeof(type) and typeof(var) build
 * a type from a type name or from an existing variable's type at parse time;
 * typeof_unqual drops the const qualifier. long long is used so the literal
 * width is the same on the LLP64 (Windows) and LP64 (Linux) targets.
 * Golden: src/expected/c23_typeof.txt */

#include <stdio.h>

int main(void) {
    typeof(1LL) a = 1234567890123LL;
    typeof(a) b = 2;
    int x = 5;
    typeof(x) y = 7;
    const int z = 9;
    typeof_unqual(z) w = 11;   /* w has a non-const int type */
    printf("a=%lld\n", a);
    printf("b=%lld\n", b);
    printf("y=%d\n", y);
    printf("w=%d\n", w);
    return 0;
}
