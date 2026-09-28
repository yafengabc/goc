// Exercises printf %p (pointer formatting): "0x" followed by 16 hex digits.
// Fixed addresses are used so the output is deterministic -- real pointers
// vary under ASLR and are not golden-tested here.

#include <stdio.h>

int main() {
    printf("null=%p\n", (void *)0);
    printf("val=%p\n", (void *)0x1234);
    return 0;
}
