#include <stdio.h>
/* No format specifiers at all: exercises vfmt's literal-copy path only. */
int main(void) {
    char b[32];
    sprintf(b, "plain text");
    write(1, b, 10);
    write(1, "\n", 1);
    return 0;
}
