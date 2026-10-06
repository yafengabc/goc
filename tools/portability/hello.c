/* A program using only goclib's public API -- no libc header, no glibc.
 *
 * Built twice and compared: once with gocl (the ELF backend) and once by gcc in
 * the alpine container against the same goclib sources compiled as
 * libgoclib.a. Same output from both is the evidence that goclib is a library
 * rather than a compiler-specific blob. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>

int main(void) {
    char buf[128];
    const char *s = "goclib";
    int n = 42;

    fputs("hello from ", stdout);
    fputs(s, stdout);
    fputs("\n", stdout);

    printf("n=%d  n*2=%d  s=%s\n", n, n * 2, s);

    sprintf(buf, "[%s|%d|%5.2f]", s, n, 3.14159);
    fputs(buf, stdout);
    fputs("\n", stdout);

    if (strlen(s) != 6) { fputs("BAD strlen\n", stdout); return 1; }
    if (strcmp(s, "goclib") != 0) { fputs("BAD strcmp\n", stdout); return 1; }

    /* heap */
    {
        char *p = (char *)malloc(64);
        int i;
        if (!p) { fputs("BAD malloc\n", stdout); return 1; }
        for (i = 0; i < 10; i++) p[i] = (char)('0' + i);
        p[10] = 0;
        fputs("heap=", stdout);
        fputs(p, stdout);
        fputs("\n", stdout);
        free(p);
    }

    /* math */
    printf("sqrt(144)=%.1f  2^10=%.0f\n", sqrt(144.0), pow(2.0, 10.0));

    fputs("OK\n", stdout);
    return 0;
}
