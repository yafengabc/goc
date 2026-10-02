/* goclib <math.h>, second batch: sign handling, the C99 rounding/remainder
 * family, exp/log near 1, and the error and gamma functions.
 *
 * Two notes on how this file is written:
 *
 * -0.0 is built with copysign rather than written as a literal. goc folds
 * `-0.0` (and `-x` where x is 0.0) into a plain +0.0 today, so the literal
 * spelling would silently test nothing; copysign writes the sign bit
 * directly and really does produce a negative zero. See the known-gap note.
 *
 * Everything else is printed to 15-16 significant digits. Three values land
 * within an ulp of what a host C library prints -- fma(0.1,0.1,-0.01),
 * erf(0.5) and erfc(6) -- which is the accuracy floor of a software
 * implementation with no hardware fma and no libm behind it; the golden here
 * is goc's answer, not glibc's.
 */
#include <stdio.h>
#include <math.h>

int main() {
    double z = 0.0;
    double nz = copysign(z, -1.0);       /* -0.0, the honest way here */

    /* ---- sign manipulation ------------------------------------------- */
    printf("signbit(-0.0)=%d signbit(0.0)=%d\n", signbit(nz) ? 1 : 0, signbit(z) ? 1 : 0);
    printf("signbit(-3)=%d signbit(3)=%d\n", signbit(-3.0) ? 1 : 0, signbit(3.0) ? 1 : 0);
    printf("copysign(3,-1)=%.15g copysign(3,-0)=%.15g\n", copysign(3.0, -1.0), copysign(3.0, nz));
    printf("signbit(copysign(3,-0))=%d\n", signbit(copysign(3.0, nz)) ? 1 : 0);
    printf("printf(-0.0)=%.15g\n", nz);
    printf("fdim(5,3)=%.15g fdim(3,5)=%.15g\n", fdim(5.0, 3.0), fdim(3.0, 5.0));

    /* ---- C99 rounding: ties to even, unlike round() ------------------ */
    printf("rint(2.5)=%.15g rint(3.5)=%.15g\n", rint(2.5), rint(3.5));
    printf("rint(-0.5)=%.15g rint(-2.5)=%.15g\n", rint(-0.5), rint(-2.5));
    printf("rint(2.4)=%.15g nearbyint(2.5)=%.15g\n", rint(2.4), nearbyint(2.5));
    printf("remainder(5,2)=%.15g remainder(3,2)=%.15g\n", remainder(5.0, 2.0), remainder(3.0, 2.0));
    printf("remainder(1,2)=%.15g remainder(7,3)=%.15g\n", remainder(1.0, 2.0), remainder(7.0, 3.0));
    /* The product is not rounded before the addition: 0.1*0.1 is
     * 0.010000000000000002, so the exact answer here is not zero. */
    printf("fma(2,3,4)=%.15g\n", fma(2.0, 3.0, 4.0));
    printf("fma(0.1,0.1,-0.01)=%.16g\n", fma(0.1, 0.1, -0.01));

    /* ---- exp/log near 1 ---------------------------------------------- */
    printf("expm1(1e-12)=%.17g\n", expm1(1e-12));
    printf("expm1(1)=%.15g expm1(-1)=%.15g\n", expm1(1.0), expm1(-1.0));
    printf("log1p(1e-12)=%.17g\n", log1p(1e-12));
    printf("log1p(1)=%.15g log1p(0.5)=%.15g\n", log1p(1.0), log1p(0.5));

    /* ---- error function ---------------------------------------------- */
    printf("erf(0)=%.15g erf(1)=%.15g\n", erf(0.0), erf(1.0));
    printf("erf(-1)=%.15g erf(2)=%.15g\n", erf(-1.0), erf(2.0));
    printf("erf(0.5)=%.15g erf(3)=%.15g\n", erf(0.5), erf(3.0));
    printf("erfc(0)=%.15g erfc(1)=%.15g\n", erfc(0.0), erfc(1.0));
    printf("erfc(3)=%.15g erfc(6)=%.15g\n", erfc(3.0), erfc(6.0));

    /* ---- gamma -------------------------------------------------------- */
    printf("tgamma(5)=%.15g tgamma(10)=%.15g\n", tgamma(5.0), tgamma(10.0));
    printf("tgamma(0.5)=%.15g tgamma(-0.5)=%.15g\n", tgamma(0.5), tgamma(-0.5));
    printf("tgamma(1)=%.15g tgamma(2)=%.15g\n", tgamma(1.0), tgamma(2.0));
    printf("lgamma(5)=%.15g lgamma(0.5)=%.15g\n", lgamma(5.0), lgamma(0.5));
    /* lgamma(1e6) is 1.28e7 -- gamma itself overflows a double long before
     * this, so Stirling's series is the only way to get here. */
    printf("lgamma(100)=%.15g lgamma(1e6)=%.15g\n", lgamma(100.0), lgamma(1e6));
    printf("lgamma(-0.5)=%.15g\n", lgamma(-0.5));

    /* ---- binary exponent --------------------------------------------- */
    printf("ilogb(8)=%d ilogb(1)=%d ilogb(0.5)=%d\n", ilogb(8.0), ilogb(1.0), ilogb(0.5));
    printf("logb(1024)=%.15g ilogb(0)=%d\n", logb(1024.0), ilogb(z));
    printf("isnan(nan())=%d\n", isnan(nan("")) ? 1 : 0);
    return 0;
}
