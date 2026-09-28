/* constptr.c -- pointer-typed struct members must store at pointer width.
 *
 * Regression lock for the lvalueWidth bug: a `const char *` struct member
 * was stored with its ELEMENT width (1 byte) instead of the 8-byte pointer,
 * so `wc.lpszClassName = "str"` truncated the address and every Win32
 * struct-with-char*-field call (WNDCLASSEX...) passed garbage pointers.
 *
 * If the member were still truncated, name=/plain= would print garbage or
 * crash, and the 64-bit handle would come back wrong.
 */

#include <stdio.h>

struct S {
    const char *name;   /* the width that used to collapse to 1 byte */
    void *handle;       /* control group: plain void* member */
    char *plain;        /* control group: non-const char* member */
};

int main(void) {
    struct S s;
    s.name = "world";
    s.handle = (void *)0x12345678;
    s.plain = "plain";

    printf("name=%s plain=%s\n", s.name, s.plain);
    printf("handle=%lu\n", (unsigned long)s.handle);

    /* pointer arithmetic through a char* member still strides 1 */
    printf("ame=%s\n", s.name + 1);
    return 0;
}
