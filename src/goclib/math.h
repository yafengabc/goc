#ifndef GOC_MATH_H
#define GOC_MATH_H

/* isinf() below is defined against DBL_MAX, so the range header comes with
 * this one rather than being left to the includer. */
#include <float.h>

/* goc math.h -- the subset of <math.h> goclib implements.
 *
 * Only the five functions below exist. There is no libm and no hardware
 * transcendentals here, so the usual family -- pow, exp, log, sin, cos,
 * tan and friends -- is absent on purpose: calling one fails at codegen
 * with "unknown function", which is a better outcome than a silently
 * wrong result. sqrt uses Newton's method; floor/ceil go through the
 * double->long conversion, so neither is a single instruction.
 *
 * All of these take and return double. The float-suffixed spellings
 * (fabsf, floorf, ...) are not provided: goc evaluates float as an
 * effective double anyway (see the float note in README).
 */

/* Absolute value. */
double fabs(double x);
/* Largest integral value not greater than x. */
double floor(double x);
/* Smallest integral value not less than x. */
double ceil(double x);
/* Remainder of x/y, with the sign of x. Returns 0 when y is 0. */
double fmod(double x, double y);
/* Square root. Returns 0 for a negative argument, and NaN unchanged. */
double sqrt(double x);

/* ---- classification ---------------------------------------------------- */
#define NAN      (0.0 / 0.0)
#define INFINITY (1.0 / 0.0)
/* The only x that is not equal to itself. */
#define isnan(x) ((x) != (x))
/* A finite double is never greater than DBL_MAX, so this is true exactly
 * for +/-infinity; for NaN every comparison is false, which is also right.
 */
#define isinf(x) (fabs(x) > DBL_MAX)

#endif /* GOC_MATH_H */
