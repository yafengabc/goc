#include <stdio.h>
#include <stdarg.h>
static int twice(const char *fmt, ...) {
    va_list ap, m;
    va_start(ap, fmt);
    va_copy(m, ap);
    int a = 0, b = 0;
    a = va_arg(ap, int);
    b = va_arg(m, int);
    va_end(m);
    va_end(ap);
    return a * 100 + b;
}
int main(void) {
    printf("t=%d\n", twice("i", 7));
    printf("s=%d %s %x\n", 42, "abc", 255);
    return 0;
}
