/* C23 MVP item #8 (_Alignas / _Alignof). Both forms of _Alignas are exercised:
 * an integer constant and a type. _Alignof reports the alignment of a type or
 * object. Alignments of the fundamental types are ABI-stable across the Windows
 * and Linux targets, so the golden is identical on both.
 * Golden: src/expected/c23_align.txt */

#include <stdio.h>
#include <stddef.h>

int main(void) {
    printf("ai=%d\n", (int)_Alignof(int));
    printf("ad=%d\n", (int)_Alignof(double));
    printf("ac=%d\n", (int)_Alignof(char));
    _Alignas(16) int x;
    printf("ax=%d\n", (int)_Alignof(x));
    _Alignas(double) char c;
    printf("ac2=%d\n", (int)_Alignof(c));
    return 0;
}
