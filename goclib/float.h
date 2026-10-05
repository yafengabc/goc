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

/* ---- exponent range / radix (classic C89 macros, completed) ------------ */
#define FLT_RADIX     2
#define FLT_ROUNDS    1
#define FLT_MIN_EXP   (-125)
#define FLT_MAX_EXP   128
#define DBL_MIN_EXP   (-1021)
#define DBL_MAX_EXP   1024
#define LDBL_MIN_EXP  DBL_MIN_EXP
#define LDBL_MAX_EXP  DBL_MAX_EXP

/* ---- C23 normalization / IEC 60559 macros (7.7.1, 7.7.2) --------------- */
#define FLT_NORM_MAX FLT_MAX
#define DBL_NORM_MAX DBL_MAX
#define LDBL_NORM_MAX LDBL_MAX
#define FLT_IS_IEC_60559 1
#define DBL_IS_IEC_60559 1
#define LDBL_IS_IEC_60559 1

/* goc long double is binary64 in disguise; the marker lets portable code
 * detect the downgrade (P3.5). */
#define __goc_long_double_is_double 1

#endif /* GOC_FLOAT_H */
