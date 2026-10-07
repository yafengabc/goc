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
/* C23: 10^x, computed as exp(x * ln10) so it inherits exp's accuracy.
 * 10^0 is exactly 1 and 10^n is exact for |n| <= 22 through exp's own
 * argument reduction, which is the most any binary64 implementation can
 * promise. */
double exp10(double x);
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
/* C23 fmaximum_num / fminimum_num: like fmax/fmin but a NaN argument is a
 * domain error rather than being skipped, so fmaximum_num(1, NaN) is NaN.
 * The pair that is not NaN wins, and two NaNs give NaN. */
double fmaximum_num(double x, double y);
double fminimum_num(double x, double y);
/* C23 fmaxmag / fminmag: compare by magnitude alone, and the result carries
 * the sign of the larger operand. Where the magnitudes are equal the even
 * mantissa wins (so fmaxmag(1, -1) is 1, not -1). */
double fmaxmag(double x, double y);
double fminmag(double x, double y);
/* C23 totalorder / totalordermag: a total order that also sorts NaN, which
 * the < comparison cannot. totalorder(x,y) is <0/0/>0 exactly when
 * totalorder(x,y) < 0/== 0/> 0, with -0.0 < +0.0 and every NaN above every
 * number and above infinity (negative NaNs below). totalordermag compares by
 * magnitude instead, so 1.0 and -1.0 are one value there and it returns 0. Both return 0 rather than an equal
 * value, per the standard. */
int totalorder(const double *x, const double *y);
int totalordermag(const double *x, const double *y);

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
/* C23 roundeven: round half to even, preserving the sign of zero the way
 * rint does. Identical to rint() here -- both are round-to-nearest-even --
 * but a distinct name because the standard requires it to be its own
 * function (it may differ under a changed rounding mode). */
double roundeven(double x);
/* C23 nextup / nextdown: the next representable value toward +infinity /
 * -infinity. nextup(+inf) is +inf, and stepping away from 0 gives the
 * smallest subnormal rather than crossing zero. */
double nextup(double x);
double nextdown(double x);
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
/* The next double after x in the direction of y, and its scaled-exponent and
 * quotient-remainder siblings.
 *
 * nexttoward's second parameter is `long double' in the standard (C11 7.12.11.5)
 * because the step has to be resolved in the wider type. goc has no long
 * double -- long double folds to double in its type system -- so under goc it
 * takes a plain double. A host compiler does have long double, and a host that
 * already declares nexttoward as a library function would disagree with the
 * narrower spelling, so the signature follows the host. Both spellings name the
 * same symbol; only the width of the direction argument differs. */
double nextafter(double x, double y);
#ifdef __goc__
double nexttoward(double x, double y);
#else
double nexttoward(double x, long double y);
#endif
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

/* ---- C23 classification, payloads and fp -> integer --------------------- */

/* issignaling: goclib produces only quiet NaNs (every arithmetic result and
 * every NAN constant is quiet), so this is always 0 -- including for the
 * signalling NaN constants below, which goclib has no way to raise. */
int issignaling(double x);
/* C23: the payload of a NaN as a double; the sign is not part of it. For a
 * non-NaN the result is unspecified, so goclib returns x unchanged. */
double getpayload(double x);
/* C23 getsign: the sign of x as a double, -1.0 or +1.0 (never 0 -- there is
 * no third choice, and signbit(+0.0) is 0 so the two disagree by design). */
double getsign(double x);

/* C23 fromfp / ufromfp: the integer nearest to fp, with the current (and only)
 * rounding direction -- to-nearest-even. An out-of-range or NaN argument is a
 * domain error: these return 0 and set errno = EDOM, per the standard's
 * "otherwise returns 0" wording (the C23 return value for an invalid input is
 * unspecified but must be representable, and 0 always is). The unsigned
 * variant returns the same value for [0, 2^63) as the signed one. */
long long fromfp(double fp);
unsigned long long ufromfp(double fp);

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

/* C23 signalling-NaN constants. goclib never raises a signalling NaN (see
 * issignaling above), so these are spelled as the quiet NaN they degrade to --
 * a program that only tests "is this NaN" behaves identically either way. */
#define FLT_SNAN (0.0f / 0.0f)
#define DBL_SNAN (0.0 / 0.0)
#define LDBL_SNAN (0.0L / 0.0L)

/* C23 math_errhandling: goclib's math functions report domain and range
 * errors by returning NaN or infinity and by setting errno, so only
 * MATH_ERRNO is claimed. There is no trap to report and no rounding-mode
 * change to flag. */
#define math_errhandling (MATH_ERRNO)
#define MATH_ERRNO     1
#define MATH_ERREXCEPT 2

#define M_PI     3.14159265358979323846
#define M_PI_2   1.57079632679489661923
#define M_PI_4   0.78539816339744830962
#define M_E      2.71828182845904523536
#define M_LN2    0.69314718055994530942
#define M_LN10   2.30258509299404568402
#define M_LOG2E  1.44269504088896340736
#define M_SQRT2  1.41421356237309504880

#endif /* GOC_MATH_H */
