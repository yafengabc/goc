// A global char* initialised by a string literal must point at the constant
// at startup. goc cannot store the address in .data (goa has no data
// relocations for dq), so the entry stub writes it with
// `lea rax,[rip+G_msg]; lea rdx,[rip+LCn]; mov [rax+0],rdx`. This is the
// regression lock for that path.

#include <stdio.h>

char *msg = "hello from global string";

int main() {
    printf("%s\n", msg);
    return 0;
}
