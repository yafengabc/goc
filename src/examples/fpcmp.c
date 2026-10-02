/* Floating-point comparisons have to read the "unordered" result of ucomisd,
 * or NaN silently compares equal to itself and every isnan() test in the
 * library answers 0. printf's %g also has to survive values past 2^63 and
 * spell out the two non-finite results. */
#include <stdio.h>
#include <math.h>

int main() {
    double z = 0.0;
    double n = z / z;                   /* NaN */
    double i = 1.0 / z;                 /* +inf */

    /* The classic test: NaN is the only value that is not equal to itself. */
    printf("nan!=nan=%d\n", n != n ? 1 : 0);
    printf("nan==nan=%d\n", n == n ? 1 : 0);
    printf("nan<1=%d nan>1=%d\n", n < 1.0 ? 1 : 0, n > 1.0 ? 1 : 0);
    printf("nan<=nan=%d nan>=nan=%d\n", n <= n ? 1 : 0, n >= n ? 1 : 0);
    printf("inf!=inf=%d\n", i != i ? 1 : 0);
    printf("inf>0=%d -inf<0=%d\n", i > 0.0 ? 1 : 0, (-i) < 0.0 ? 1 : 0);
    printf("isnan=%d isinf=%d isfinite=%d\n",
           isnan(n) ? 1 : 0, isinf(i) ? 1 : 0, isfinite(1.0) ? 1 : 0);

    /* Ordered comparisons must be untouched by the NaN handling. */
    printf("1<2=%d 2<=2=%d 3>4=%d\n",
           1.0 < 2.0 ? 1 : 0, 2.0 <= 2.0 ? 1 : 0, 3.0 > 4.0 ? 1 : 0);
    printf("int 1<2=%d -1<1=%d\n", 1 < 2 ? 1 : 0, -1 < 1 ? 1 : 0);

    /* printf: the integer part cannot go through a (long) cast, which is
     * undefined past 2^63 and used to print nothing at all. */
    printf("1e19=%g\n", 1e19);
    printf("1e20=%g\n", 1e20);
    printf("1e19 f=%.1f\n", 1e19);
    printf("inf=%g nan=%g\n", i, n);
    printf("-1e19=%g\n", -1e19);
    return 0;
}
