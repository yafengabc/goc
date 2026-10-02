/* printf's exponential forms. %e is always scientific; %g picks a shape --
 * fixed while the decimal exponent stays inside [-4, precision) and
 * exponential outside it -- and counts the precision in SIGNIFICANT digits,
 * not fractional ones. Both are C's rule, not a goc invention: the golden
 * byte-matches what a host C library prints for this file.
 *
 * The interesting cases are the extremes. 1e300 has an integer part that does
 * not fit in a 64-bit long (goc used to print all 301 of its digits), 1e-300
 * has no room in fixed point at all (it used to print 0), and 5e-324 is the
 * smallest subnormal, whose exponent is below anything a 10^k table reaches.
 */
#include <stdio.h>
#include <math.h>

int main() {
    double z = 0.0;

    /* %e: six fractional digits by default, two exponent digits minimum and
     * three only once the exponent needs them. */
    printf("%e\n", 1.0);
    printf("%e\n", 1234.5678);
    printf("%e\n", 0.000123);
    printf("%e\n", -1.5e-7);
    printf("%.3e\n", 1234.5678);
    printf("%.0e\n", 1234.5678);
    printf("%E\n", 1234.5678);

    /* %g: the shape flips exactly at the precision boundary. */
    printf("%g\n", 0.0001);
    printf("%g\n", 0.00001);
    printf("%g\n", 123456.0);
    printf("%g\n", 1234567.0);
    printf("%g\n", 1234.5);
    printf("%.10g\n", 3.14159265358979);
    printf("%G\n", 1e-10);

    /* The values that used to defeat goc. */
    printf("%g\n", 1e300);
    printf("%g\n", -1e-300);
    printf("%e\n", 1e300);
    printf("%e\n", 5e-324);
    printf("%g\n", z);
    printf("%g\n", 1.0 / z);
    printf("isnan=%d\n", isnan(z / z) ? 1 : 0);

    /* Width and the left-justify flag still apply to the exponential forms. */
    printf("[%12.3e]\n", 1234.5678);
    printf("[%-12.3e]\n", 1234.5678);
    return 0;
}
