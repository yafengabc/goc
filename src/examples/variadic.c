// Definitive test for goc's variadic support: a goc-compiled printf-style
// function (my_printf) using va_list/va_start/va_arg/va_end, that actually
// prints the real argument values (decimal ints, strings, chars, longs) so we
// can confirm both the ordering AND the data are correct. All vararg types are
// int-class (char*, int, long) so the caller's typed-arg placement lines up
// with the callee's positional save-area read.
#include <stdarg.h>

int putchar(int c);

// Print a signed decimal integer via recursion (no itoa in the toy libc).
int putint(int v) {
    if (v < 0) {
        putchar('-');
        v = 0 - v;
    }
    if (v >= 10) {
        putint(v / 10);
    }
    putchar('0' + (v % 10));
    return 0;
}

int my_printf(const char* fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int i = 0;
    while (fmt[i] != 0) {
        if (fmt[i] == '%') {
            i = i + 1;
            if (fmt[i] == 'd') {
                int v = va_arg(ap, int);
                putint(v);
            } else if (fmt[i] == 's') {
                const char* s = va_arg(ap, const char*);
                int j = 0;
                while (s[j] != 0) {
                    putchar(s[j]);
                    j = j + 1;
                }
            } else if (fmt[i] == 'c') {
                int c = va_arg(ap, int);
                putchar(c);
            } else if (fmt[i] == 'l') {
                long v = va_arg(ap, long);
                putint((int)v);
            } else {
                putchar(fmt[i]);
            }
        } else {
            putchar(fmt[i]);
        }
        i = i + 1;
    }
    va_end(ap);
    return 0;
}

int main() {
    my_printf("hello %s count=%d char=%c big=%ld neg=%d\n",
              "world", 42, 65, 123456789L, -7);
    my_printf("nums: %d %d %d\n", 100, 200, 300);
    return 0;
}
