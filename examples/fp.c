// Exercises double floating point end to end: literals, arithmetic, mixed
// int/double promotion, all six comparisons, if/while conditions, %f
// formatting (including negatives), and user functions that take or return
// doubles.

double circle(double r) {
    return 3.14159 * r * r;
}

int main() {
    printf("== literals ==\n");
    printf("%f\n", 1.5);
    printf("%f\n", -2.25);
    printf("%f\n", 0.0);

    printf("== arithmetic ==\n");
    double a = 1.5;
    double b = 2.25;
    printf("a=%f b=%f\n", a, b);
    printf("sum=%f\n", a + b);
    printf("diff=%f\n", a - b);
    printf("prod=%f\n", a * b);
    printf("div=%f\n", a / b);
    printf("neg=%f\n", -a);

    printf("== mixed ==\n");
    double c = 3;
    c = c + 1;
    printf("c=%f\n", c);
    int i = 2;
    printf("mixed=%f\n", a * i);
    printf("promote=%f\n", a + 2);

    printf("== comparison ==\n");
    printf("lt=%d\n", a < b);
    printf("gt=%d\n", a > b);
    printf("le=%d\n", a <= b);
    printf("ge=%d\n", a >= b);
    printf("eq=%d\n", a == b);
    printf("ne=%d\n", a != b);
    if (a < b) { printf("a<b\n"); }
    if (a > b) { printf("a>b\n"); } else { printf("a<=b\n"); }

    printf("== while ==\n");
    double d = 0.5;
    int n = 0;
    while (d < 10.0) {
        d = d * 2.0;
        n = n + 1;
    }
    printf("doubled %d times, now %f\n", n, d);

    printf("== function ==\n");
    printf("circle(2)=%f\n", circle(2));
    printf("circle(1.5)=%f\n", circle(1.5));

    printf("== chained ==\n");
    double x = 1.0;
    x = x + 2.0 * x;
    printf("x=%f\n", x);

    return 0;
}
