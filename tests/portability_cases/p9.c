#include <stdio.h>
#include <stdarg.h>
/* Wrap vfprintf the way a caller outside printf would: build the va_list
 * here and hand it over. Nothing here is a printf specialisation. */
static int emit(FILE *s, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = vfprintf(s, fmt, ap);
    va_end(ap);
    return n;
}
int main(void) {
    
    emit(stdout, "n=%d\n", 42);
    return 0;
}
