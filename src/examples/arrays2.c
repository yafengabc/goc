// Multi-dimensional arrays and a user-defined variadic function that actually
// receives a double.
//
// Multi-dimensional arrays used to be silently wrong on two counts: the
// declarator wrapped each dimension as it was read (so "int m[2][3]" became 3
// elements of int[2]), and the stride of a nested subscript reused the outer
// array's stride instead of the inner element type's. Together they made
// m[0][1] read m[1][0]'s bytes.
//
// The variadic half guards a different bug: a call to a user-defined "..."
// function was only treated as variadic when it was named printf/sprintf, so a
// double argument went in an XMM register while the callee's va_list save area
// only ever spills the integer registers -- va_arg then read 0.0.
#include <stdarg.h>
#include <stdio.h>

int my_printf(const char *fmt, ...) {
    va_list ap;
    int n = 0;
    char c;
    va_start(ap, fmt);
    while ((c = *fmt) != 0) {
        fmt = fmt + 1;
        if (c == '%') {
            c = *fmt;
            fmt = fmt + 1;
            if (c == 'd') {
                printf("%d", va_arg(ap, int));
                n = n + 1;
            } else if (c == 'f') {
                printf("%f", va_arg(ap, double));
                n = n + 1;
            }
        } else {
            putchar(c);
        }
    }
    va_end(ap);
    return n;
}

int gm[4][2]; // global 2-D array

int main() {
    int m[2][3];
    char g[3][4];
    double d[2][2];
    int i = 0;
    int j = 0;

    for (i = 0; i < 2; i = i + 1) {
        for (j = 0; j < 3; j = j + 1) {
            m[i][j] = i * 10 + j;
        }
    }
    printf("m: %d %d %d %d %d %d\n", m[0][0], m[0][1], m[0][2], m[1][0], m[1][1], m[1][2]);

    for (i = 0; i < 3; i = i + 1) {
        for (j = 0; j < 4; j = j + 1) {
            g[i][j] = 'a' + i * 4 + j;
        }
    }
    printf("g: %c %c %c %c\n", g[0][0], g[1][1], g[2][3], g[1][2]);

    for (i = 0; i < 2; i = i + 1) {
        for (j = 0; j < 2; j = j + 1) {
            d[i][j] = i * 2 + j + 0.5;
        }
    }
    printf("d: %f %f %f %f\n", d[0][0], d[0][1], d[1][0], d[1][1]);

    // A 2-D array passed to another function decays to a pointer to its first
    // row, so the callee subscripts it with the row stride intact.
    for (i = 0; i < 4; i = i + 1) {
        for (j = 0; j < 2; j = j + 1) {
            gm[i][j] = i * 100 + j;
        }
    }
    printf("gm: %d %d %d %d\n", gm[0][0], gm[1][1], gm[2][0], gm[3][1]);

    // User-defined variadic function: int and double arguments.
    printf("builtin: %f\n", 1.5);
    my_printf("mine: %d / ", 7);
    my_printf("%f\n", 2.5);
    my_printf("count: %d %f\n", 3, 0.25);
    printf("returned: %d\n", my_printf("%d %f\n", 4, 0.125));

    return 0;
}
