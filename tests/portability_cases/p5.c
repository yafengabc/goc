#include <stdio.h>
/* Bypass the printf specialisation: call vfprintf directly. */
static int mywrite(FILE *s, const char *p, unsigned long n) {
    return (int)fwrite(p, 1, n, s);
}
int main(void) {
    FILE *o = stdout;
    int r = mywrite(o, "raw\n", 4);
    fprintf(stderr, "r=%d\n", r);
    return 0;
}
