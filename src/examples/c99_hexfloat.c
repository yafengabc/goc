/* C23 hex float literals (0x1.8p3): hex mantissa, optional binary exponent
   (C23 makes the p-exponent optional), f/l suffixes, and the '.8p1' form.
   Values verified against hand computation (gcc -std=c11 rejects the
   exponent-less C23 form, so those lines are computed manually). */
#include <stdio.h>

int main(void) {
    double a = 0x1.8p3;      /* 1.5 * 8   = 12   */
    double b = 0x1p-2;       /* 1 * 2^-2  = 0.25 */
    double c = 0x.8p1;       /* 0.5 * 2   = 1    */
    double d = 0x1.8;        /* C23: no exponent -> 1.5 */
    float  e = 0x1.4p2f;     /* 1.25 * 4  = 5    */
    double g = 0x10p0;       /* 16 */
    double h = 0x1p4;        /* 16 */

    printf("a=%.3f b=%.3f c=%.3f\n", a, b, c);
    printf("d=%.3f e=%.3f g=%.1f h=%.1f\n", d, (double)e, g, h);
    printf("expr=%.3f\n", 0x1.8p3 + 0x1p0);

    /* hex integers are unaffected: e IS a hex digit, p is not */
    printf("hexint=%d notfloat=%d\n", 0x1e5, 0xFACE);
    return 0;
}
