#include <stdio.h>
int main(void) {
    char b[32];
    sprintf(b, "%d", 42);      /* just one integer, nothing else */
    write(1, b, 2);
    write(1, "\n", 1);
    return 0;
}
