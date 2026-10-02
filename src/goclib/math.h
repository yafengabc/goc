#ifndef GOC_MATH_H
#define GOC_MATH_H

/* isinf() below is defined against DBL_MAX, so the range header comes with
 * this one rather than being left to the includer. */
#include <float.h>

/* goc math.h -- the subset of <math.h> goclib implements.
 *
 * Every function here is a software implementation in plain C: there is no
 * libm and no hardware transcendentals, so exp/log/sin/... are series and
 * Newton iterations rather than single instructions. They are accurate to
 * roughly one ULP over the normal argument range -- math.c documents where
 * each one stops being exact (the argument reduction in sin/cos is the
 * weakest link).
 *
 * All of these take and return double. The float-suffixed spellings
 * (fabsf, floorf, powf, ...) are not provided: goc evaluates float as an
 * effective double anyway (see the float note in README).
 *
 * Anything outside this list -- the long double family, the Bessel
 * functions, the complex header -- does not exist. Calling one fails at
 * codegen with "unknown function", which is a better outcome than a
 * silently wrong result.
 */

/* ---- rounding and remainder -------------------------------------------- */

/* Absolute value. */
double fabs(double x);
/* Largest integral value not greater than x. */
double floor(double x);
/* Smallest integral value not less than x. */
double ceil(double x);
/* Toward zero: the integer part, keeping x's sign. */
double trunc(double x);
/* Nearest integer, halves away from zero (C99 round, not rint). */
double round(double x);
/* Remainder of x/y, with the sign of x. Returns 0 when y is 0. */
double fmod(double x, double y);
/* Split into integral and fractional parts, both carrying x's sign; the
 * integral part is stored through ip. */
double modf(double x, double *ip);

/* ---- powers and logarithms --------------------------------------------- */

/* Square root. Returns 0 for a negative argument, and NaN unchanged. */
double sqrt(double x);
/* Cube root: defined for negatives, where sqrt is not. */
double cbrt(double x);
/* x^y. An integral exponent goes through repeated squaring instead of
 * exp/log, so pow(x,2) and pow(x,3) stay exact. A negative base with a
 * non-integral exponent is a domain error and yields NaN. */
double pow(double x, double y);
double exp(double x);
double exp2(double x);
/* Natural log: log(0) is -infinity, log of a negative is NaN. */
double log(double x);
double log2(double x);
double log10(double x);

/* ---- trigonometric ----------------------------------------------------- */

/* Radians. Argument reduction is one-step, so accuracy degrades for
 * arguments beyond roughly 2^60. */
double sin(double x);
double cos(double x);
double tan(double x);
/* NaN outside [-1,1]; +/-pi/2 at the endpoints. */
double asin(double x);
double acos(double x);
double atan(double x);
/* atan(y/x) with the quadrant fixed, so it also works when x is 0. */
double atan2(double y, double x);

/* ---- hyperbolic -------------------------------------------------------- */

double sinh(double x);
double cosh(double x);
double tanh(double x);

/* ---- exponent/mantissa and combining ----------------------------------- */

/* Decompose into m in [0.5,1) and an exponent with x = m * 2^e. Zero,
 * infinity and NaN come back unchanged with *e set to 0. */
double frexp(double x, int *e);
/* x * 2^e -- exact, and the only way to reach a value that plain
 * multiplication would overflow or underflow on the way to. Overflow
 * gives +/-infinity. */
double ldexp(double x, int e);
/* sqrt(x*x + y*y) without the intermediate overflow the naive form hits. */
double hypot(double x, double y);
/* min/max, ignoring NaN: fmin(1, NaN) is 1. */
double fmin(double x, double y);
double fmax(double x, double y);

/* ---- sign manipulation -------------------------------------------------- */

/* The sign bit, including the two cases `x < 0.0` cannot see: -0.0 and a
 * negative NaN. */
int signbit(double x);
/* x carrying the sign of y -- how -0.0 and the sign of a result are moved
 * around without disturbing the magnitude. */
double copysign(double x, double y);
/* Positive difference: x - y when that is positive, +0 otherwise. */
double fdim(double x, double y);

/* ---- C99 rounding, remainder and fused multiply-add --------------------- */

/* Nearest integer, ties to even -- the IEEE default, and the difference
 * from round(): rint(-0.5) is -0.0, rint(2.5) is 2. */
double rint(double x);
/* Same value as rint: goc always runs in round-to-nearest, so there is no
 * rounding mode for this to differ on. */
double nearbyint(double x);
/* IEEE remainder x - n*y with n the nearest integer (ties to even). Unlike
 * fmod it may be negative, and |result| <= |y|/2. */
double remainder(double x, double y);
/* x*y+z with a single rounding: the product is not rounded before the
 * addition. Dekker's split exposes the exact product as two doubles; well
 * outside its safe range this degrades to a plain x*y+z rather than to a
 * wrong answer. */
double fma(double x, double y, double z);

/* ---- exponentials and logarithms near 1 --------------------------------- */

/* exp(x)-1 and log(1+x), computed without the cancellation that makes the
 * naive spelling lose every significant digit as x approaches 0 --
 * exp(1e-12)-1 is 1.0000000827e-12 on a machine that subtracts first. */
double expm1(double x);
/* NaN below -1, -infinity at -1. */
double log1p(double x);

/* ---- error and gamma ---------------------------------------------------- */

/* The error function and its complement: the integral of the Gaussian, and
 * the tail. erf is the series near 0, erfc a continued fraction past 1.5. */
double erf(double x);
double erfc(double x);
/* The gamma function. Overflows past about 171.6; NaN at the non-positive
 * integers, where it has poles. */
double tgamma(double x);
/* log|gamma(x)| -- the form that survives where gamma itself would
 * overflow (Stirling's series past 8). */
double lgamma(double x);

/* ---- binary exponent ---------------------------------------------------- */

/* floor(log2|x|). Zero and non-finite arguments give the C99 sentinels
 * FP_ILOGB0 / FP_ILOGBNAN. */
int ilogb(double x);
double logb(double x);
/* INT_MIN / INT_MAX, spelled so that the 2147483648 in the middle is never
 * a long: goc reads every integer literal by its bit pattern. */
#define FP_ILOGB0   (-2147483647 - 1)
#define FP_ILOGBNAN 2147483647

/* A quiet NaN. The tag ("nan(chars)") is accepted and ignored: goc prints
 * every NaN as "nan", so there is nothing to carry. */
double nan(const char *tagp);

/* ---- C99: neighbour, scaled exponent, quotient remainder ---------------- */
/* The next double after x in the direction of y. The standard spells y as a
 * long double; goc has no long double, so nexttoward takes a plain double. */
double nextafter(double x, double y);
double nexttoward(double x, double y);
/* x * 2^n exactly, with an int (scalbn) or long (scalbln) exponent. */
double scalbn(double x, int n);
double scalbln(double x, long n);
/* IEEE remainder (like remainder) plus the low 7 bits -- mod 128, with the
 * sign of x/y -- of the integer quotient, stored through quo. */
double remquo(double x, double y, int *quo);

/* ---- C99 classification ------------------------------------------------ */
#define FP_NAN       0
#define FP_INFINITE  1
#define FP_NORMAL    2
#define FP_SUBNORMAL 3
#define FP_ZERO      4
int fpclassify(double x);

/* ---- classification and constants -------------------------------------- */
#define NAN      (0.0 / 0.0)
#define INFINITY (1.0 / 0.0)
/* The only x that is not equal to itself. */
#define isnan(x) ((x) != (x))
/* A finite double is never greater than DBL_MAX, so this is true exactly
 * for +/-infinity; for NaN every comparison is false, which is also right.
 */
#define isinf(x) (fabs(x) > DBL_MAX)
/* Complement of the two above; false for NaN. */
#define isfinite(x) (!isnan(x) && !isinf(x))

/* Positive infinity as a double expression (7.12.4.1). goc does not fold
 * 1.0/0.0 at compile time; the runtime division yields +inf per IEEE 754
 * semantics, and %g prints it as "inf", matching the compared gcc. */
#define HUGE_VAL  (1.0 / 0.0)
#define HUGE_VALF (1.0f / 0.0f)
#define HUGE_VALL (1.0L / 0.0L)

#define M_PI     3.14159265358979323846
#define M_PI_2   1.57079632679489661923
#define M_PI_4   0.78539816339744830962
#define M_E      2.71828182845904523536
#define M_LN2    0.69314718055994530942
#define M_LN10   2.30258509299404568402
#define M_LOG2E  1.44269504088896340736
#define M_SQRT2  1.41421356237309504880

#endif /* GOC_MATH_H */
