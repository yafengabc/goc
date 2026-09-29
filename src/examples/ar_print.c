/* Array content printing, Python style: the print builtin lowers a bare
 * array identifier to a thin goclib printer (one per element type) with the
 * compile-time length, instead of the %p fallback. Covers every scalar
 * element type that has a printer: short/int/long/float/double/_Bool, plus
 * the unsigned* reuse of the same-width signed printer. Struct arrays are
 * not built in -- the user defines their own T_array_print (the same T_f
 * naming convention as UFCS methods) and print() dispatches to it, so any
 * nameable element type gets a printable array form. */
struct Point { int x; int y; };

/* User-defined array printer: print(pts) below dispatches here because the
 * element type is struct Point. The compiler passes the array and its
 * compile-time length, exactly like the builtin int_array_print. */
int Point_array_print(struct Point *a, long n) {
    long i;
    printf("[");
    for (i = 0; i < n; i++) {
        if (i > 0) printf(", ");
        printf("{%d,%d}", a[i].x, a[i].y);
    }
    printf("]\n");
    return 0;
}

int main() {
    short s[3] = {10, -20, 30};
    int a[5] = {1, 2, 3, 4, 5};
    long l[2] = {1000000000, -7};
    float f[3] = {1.5, 2.5, 3.5};
    double d[4] = {1.25, -2.5, 0.0, 3.75};
    unsigned int u[2] = {7, 42};
    _Bool b[2] = {1, 0};
    struct Point pts[2] = {{1, 2}, {3, 4}};
    char msg[] = "string wins";
    print(s);
    print(a);
    print(l);
    print(f);
    print(d);
    print(u);
    print(b);
    print(pts);
    print(msg);
    return 0;
}
