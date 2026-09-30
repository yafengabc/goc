#ifndef GOC_FLOAT_H
#define GOC_FLOAT_H

/* goc float.h -- range and precision of the floating types.
 *
 * goc has exactly two floating types: float (IEEE-754 binary32) and double
 * (binary64), and there is no long double -- it is folded into double. Every
 * macro below is a plain constant, so it works in a case label, an array
 * length and an #if alike.
 *
 * Caveat carried over from printf: goc's %g has no exponent form, so a value
 * as small as DBL_EPSILON prints as "0". The constant itself is exact and
 * compares correctly -- it is only the rendering that is lossy.
 */

/* ---- float (binary32) -------------------------------------------------- */
#define FLT_MANT_DIG 24
#define FLT_DIG      6
#define FLT_MIN      1.1754943508222875e-38F
#define FLT_MAX      3.4028234663852886e+38F
#define FLT_EPSILON  1.1920928955078125e-07F

/* ---- double (binary64); long double is an alias ------------------------ */
#define DBL_MANT_DIG 53
#define DBL_DIG      15
#define DBL_MIN      2.2250738585072014e-308
#define DBL_MAX      1.7976931348623157e+308
#define DBL_EPSILON  2.2204460492503131e-16

#define LDBL_MANT_DIG DBL_MANT_DIG
#define LDBL_DIG      DBL_DIG
#define LDBL_MIN      DBL_MIN
#define LDBL_MAX      DBL_MAX
#define LDBL_EPSILON  DBL_EPSILON

#endif /* GOC_FLOAT_H */
