#include <stdarg.h>

/* The canonical measure-then-write shape, using va_copy the way C99
 * intends: the copy is walked once, the original stays intact. */
static void measure_and_use(const char *fmt, ...) {
    const char *p;
    va_list ap;
    va_start(ap, fmt);
    va_list measure;
    va_copy(measure, ap);

    int total = 0;
    for (p = fmt; *p; p++)
        if (*p == 'i') total += va_arg(measure, int);   /* copy consumed */

    int again = 0;
    for (p = fmt; *p; p++)
        if (*p == 'i') again += va_arg(ap, int);        /* original intact */

    va_end(measure);
    va_end(ap);
    putnum((unsigned long)(total * 1000 + again));
}

static void putnum(unsigned long v) {
    char t[24];
    int n = 0;
    if (!v) t[n++] = '0';
    while (v) { t[n++] = (char)('0' + v % 10); v /= 10; }
    while (n) write(1, &t[--n], 1);
    write(1, "\n", 1);
}

int main(void) {
    measure_and_use("iij", 3, 10, 20);   /* want 13013 */
    return 0;
}
