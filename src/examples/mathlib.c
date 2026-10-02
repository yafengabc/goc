/* goclib <math.h> beyond sqrt: the transcendental family is software
 * (series + Newton) rather than a hardware instruction, so this exercises
 * the accuracy-sensitive paths -- argument reduction near quadrant edges,
 * the exp/log round trip, the exponent-splitting inside frexp/ldexp, and
 * the domain edges (asin/acos at +-1, log at 0, pow with a negative base).
 *
 * Every expected value here is printed to 15 significant digits, which is
 * one ULP of headroom for the implementations; a regression in a series
 * term shows up as a digit change rather than as a crash. Three values
 * (pow(2,0.5), sqrt(2), sin(-2.5)) land within 2 ULP of what glibc prints
 * for the same expression, so their last digit differs from a host C
 * library's -- that gap is the accuracy floor of a software implementation
 * and is the expected output here, not a bug. */
#include <stdio.h>
#include <math.h>

int main() {
    int e;
    double ip;
    double m;

    /* exp/log round trip, plus the bases that have exact answers. */
    printf("exp(1)=%.15g\n", exp(1.0));
    printf("exp(-2.5)=%.15g\n", exp(-2.5));
    printf("exp2(10)=%.15g\n", exp2(10.0));
    printf("log(exp(3))=%.15g\n", log(exp(3.0)));
    printf("log2(1024)=%.15g\n", log2(1024.0));
    printf("log10(1e-7)=%.15g\n", log10(1e-7));

    /* pow: integral exponents take the exact repeated-squaring path. The
     * 1e18 ceiling this used to carry is gone -- %g now renders a double
     * whose integer part overflows a 64-bit long in exponential form. */
    printf("pow(2,50)=%.15g\n", pow(2.0, 50.0));
    printf("pow(3,20)=%.15g\n", pow(3.0, 20.0));
    printf("pow(2,0.5)=%.15g\n", pow(2.0, 0.5));
    printf("pow(1.5,2.5)=%.15g\n", pow(1.5, 2.5));
    printf("pow(-2,3)=%.15g\n", pow(-2.0, 3.0));
    /* A negative base with a fractional exponent is a domain error, and a
     * NaN is not printable here -- assert the classification instead. */
    printf("pow(-2,0.5) nan=%d\n", isnan(pow(-2.0, 0.5)) ? 1 : 0);
    printf("pow(0,0)=%.15g pow(x,0)=%.15g\n", pow(0.0, 0.0), pow(5.0, 0.0));

    /* Trig: exact angles, the quadrant boundaries, and a reduction case. */
    printf("sin(0)=%.15g cos(0)=%.15g\n", sin(0.0), cos(0.0));
    printf("sin(pi/6)=%.15g cos(pi/3)=%.15g\n", sin(M_PI / 6.0), cos(M_PI / 3.0));
    printf("tan(pi/4)=%.15g\n", tan(M_PI / 4.0));
    printf("sin(1)=%.15g cos(1)=%.15g\n", sin(1.0), cos(1.0));
    printf("sin(10)=%.15g cos(10)=%.15g\n", sin(10.0), cos(10.0));
    printf("sin(-2.5)=%.15g cos(-2.5)=%.15g\n", sin(-2.5), cos(-2.5));

    /* Inverse trig and the domain edges. */
    printf("atan(1)*4=%.15g\n", atan(1.0) * 4.0);
    printf("atan2(-1,-1)=%.15g\n", atan2(-1.0, -1.0));
    printf("atan2(1,0)=%.15g\n", atan2(1.0, 0.0));
    printf("asin(0.5)=%.15g acos(0.5)=%.15g\n", asin(0.5), acos(0.5));
    printf("asin(1)=%.15g asin(-1)=%.15g\n", asin(1.0), asin(-1.0));
    printf("asin(2) nan=%d\n", isnan(asin(2.0)) ? 1 : 0);
    printf("sin(asin(0.7))=%.15g\n", sin(asin(0.7)));

    /* Hyperbolics. */
    printf("sinh(1)=%.15g cosh(1)=%.15g\n", sinh(1.0), cosh(1.0));
    printf("tanh(1)=%.15g tanh(50)=%.15g\n", tanh(1.0), tanh(50.0));

    /* Roots and the exponent/mantissa primitives. */
    printf("sqrt(2)=%.15g cbrt(27)=%.15g\n", sqrt(2.0), cbrt(27.0));
    printf("cbrt(-8)=%.15g cbrt(1e-9)=%.15g\n", cbrt(-8.0), cbrt(1e-9));
    printf("hypot(3,4)=%.15g\n", hypot(3.0, 4.0));
    printf("hypot(3e8,4e8)=%.15g\n", hypot(3e8, 4e8));
    m = frexp(1024.0, &e);
    printf("frexp(1024)=%.15g e=%d\n", m, e);
    printf("frexp(1e-7) e=%d\n", (frexp(1e-7, &e), e));
    printf("ldexp(0.5,11)=%.15g\n", ldexp(0.5, 11));
    /* Smallest subnormal: printf has no digits for it yet, so assert the
     * value rather than the rendering. */
    printf("ldexp(1,-1074)>0=%d\n", ldexp(1.0, -1074) > 0.0 ? 1 : 0);
    printf("ldexp(1,60)=%.15g\n", ldexp(1.0, 60));

    /* Rounding, splitting, min/max. */
    /* Two statements on purpose: modf writes through &ip, and the order in
     * which the arguments of a call are evaluated is unspecified, so folding
     * the call into the printf would let `ip` be read before modf fills it
     * (a real compiler printed 0 here). */
    m = modf(-3.75, &ip);
    printf("modf(-3.75)=%.15g ip=%.15g\n", m, ip);
    printf("trunc(2.9)=%.15g trunc(-2.9)=%.15g\n", trunc(2.9), trunc(-2.9));
    printf("round(2.5)=%.15g round(-2.5)=%.15g\n", round(2.5), round(-2.5));
    printf("floor(-2.1)=%.15g ceil(-2.1)=%.15g\n", floor(-2.1), ceil(-2.1));
    printf("fmod(-7,3)=%.15g\n", fmod(-7.0, 3.0));
    printf("fmin(3,nan)==3=%d fmax(3,nan)==3=%d\n",
           fmin(3.0, NAN) == 3.0 ? 1 : 0, fmax(3.0, NAN) == 3.0 ? 1 : 0);

    /* Classification: the macros are the only NaN/inf test here. */
    printf("isnan(nan)=%d isinf(inf)=%d isfinite(1)=%d\n",
           isnan(NAN) ? 1 : 0, isinf(INFINITY) ? 1 : 0, isfinite(1.0) ? 1 : 0);
    printf("log(0) -inf=%d log(-1) nan=%d\n",
           (log(0.0) < 0.0 && isinf(log(0.0))) ? 1 : 0, isnan(log(-1.0)) ? 1 : 0);
    return 0;
}
