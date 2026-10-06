#include <stdio.h>
#include <stdarg.h>
/* One emit only, no argument. */
static int emit(FILE *s, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = vfprintf(s, fmt, ap);
    va_end(ap);
    return n;
}
int main(void) {
    emit(stdout, "only\n");
    return 0;
}
