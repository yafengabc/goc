#include <stdarg.h>
static int putnum(unsigned long v) {
    char t[24]; int n = 0;
    if (!v) t[n++] = '0';
    while (v) { t[n++] = (char)('0' + v % 10); v /= 10; }
    while (n) write(1, &t[--n], 1);
    write(1, "\n", 1);
    return 0;
}
/* take receives the va_list as a PARAMETER and reads one int from it. */
static int take(const char *fmt, va_list ap) {
    int v = 0;
    const char *p = fmt;
    while (*p) { if (*p == 'i') v = va_arg(ap, int); p++; }
    return v;
}
static int outer(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int a = take(fmt, ap);      /* consumes 7 */
    int b = take(fmt, ap);      /* nothing left -> 0 */
    va_end(ap);
    return a * 100 + b;         /* 700 */
}
int main(void) {
    putnum((unsigned long)outer("i", 7));
    return 0;
}
