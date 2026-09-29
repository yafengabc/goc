/* Array methods: arr.f(args) forwards to T_array_f(arr, len, args) -- the
 * forwarding rule is type_array_function. An array of element type T spells
 * its methods T_array_f, the same T_array_ naming convention as the array
 * printers (T_array_print). The compiler prepends the array identifier
 * (which decays to T*) and its compile-time length: a C array carries no
 * runtime length, so only the compiler knows the count.
 *
 * Nothing here is built in -- every T_array_f is a plain user function the
 * checker resolves at compile time. Unsigned arrays keep their exact u
 * spelling (uint_array_add), because an array method reads and writes its
 * elements and uint is genuinely different from int (unlike print's shared
 * same-width printer, which only reads). */
struct Point { int x; int y; };

int int_array_add(int *a, long n, int x) {
    long i;
    for (i = 0; i < n; i++) a[i] = a[i] + x;
    return 0;
}

int uint_array_add(unsigned int *a, long n, int x) {
    long i;
    for (i = 0; i < n; i++) a[i] = a[i] + x;
    return 0;
}

double double_array_avg(double *a, long n) {
    long i;
    double s = 0.0;
    for (i = 0; i < n; i++) s = s + a[i];
    return s / (double)n;
}

long Point_array_sum(struct Point *a, long n) {
    long i;
    long s = 0;
    for (i = 0; i < n; i++) s = s + a[i].x + a[i].y;
    return s;
}

int char_array_upper(char *a, long n) {
    long i;
    /* n counts every element, and a string literal's array includes the
     * terminating NUL -- stop at it so the string stays terminated. */
    for (i = 0; i < n && a[i] != 0; i++) a[i] = a[i] - 32;
    return 0;
}

int main() {
    int a[3] = {1, 2, 3};
    unsigned int u[2] = {100, 200};
    double d[3] = {1.0, 2.0, 6.0};
    struct Point pts[2] = {{1, 2}, {3, 4}};
    char s[] = "abc";

    a.add(10);
    print(a);
    u.add(1000);
    print(u);
    print(d.avg());
    print(pts.sum());
    s.upper();
    print(s);
    return 0;
}
