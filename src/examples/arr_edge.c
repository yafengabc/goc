// Array print/method edge cases:
//   - float/double array precision: %g prints six SIGNIFICANT digits (C's
//     rule, not six fractional ones) and strips trailing zeros, so
//     0.9999999 -> 1, 1e-7 -> 1e-07, and 1e10 switches to the exponential
//     form because its exponent is past the precision.
//   - a user T_array_print printer and T_array_* UFCS methods coexist on
//     the same struct array: print(pts) reaches the printer, pts.sum()
//     the method.
//
// The built-in array printers end their line with '\n' themselves; a
// user-defined T_array_print is a plain function call, so it must print
// its own newline too (see Point_array_print below).

struct Point { int x; int y; };

int Point_array_print(struct Point *a, long n) {
    long i;
    printf("[");
    for (i = 0; i < n; i++) {
        if (i > 0) printf(", ");
        printf("(%d, %d)", a[i].x, a[i].y);
    }
    printf("]\n");
    return 0;
}

long Point_array_sum(struct Point *a, long n) {
    long i; long s = 0;
    for (i = 0; i < n; i++) s = s + a[i].x + a[i].y;
    return s;
}

int main(void) {
    double d[6] = {1.5, 2.25, 0.1, 0.2, 1234.5, 0.9999999};
    print(d);
    double tiny[3] = {1e-6, 1e-7, 1e10};
    print(tiny);
    float f[3] = {1.5, 0.1, 1234.5};
    print(f);
    struct Point pts[2] = {{1, 2}, {3, 4}};
    print(pts);
    print(pts.sum());
    return 0;
}
