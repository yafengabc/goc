#include <stdio.h>
#include <string.h>
int main(void) {
    char b[64];
    strcpy(b, "x");
    sprintf(b, "n=%d", 42);
    write(1, b, 4);
    write(1, "\n", 1);
    return 0;
}
