#ifndef GOC_FLOAT_H
#define GOC_FLOAT_H

/* goc float.h -- range and precision of the floating types.
 *
 * goc has three floating types: float (IEEE-754 binary32), double (binary64)
 * and long double (binary128). The integer-valued macros (MANT_DIG, DIG,
 * *_EXP, DECIMAL_DIG) are plain constants, so they work in a case label, an
 * array length and an #if alike.
 *
 * The binary128 values under goc are spelled as HEX floating constants, not
 * decimal ones: LDBL_MAX's exact decimal expansion runs to 4933 digits, and
 * any shortened decimal is only correct if it rounds back to the same bit
 * pattern -- a risk with no upside when 0x1.ffffffffffffffffffffffffffffp+
 * 16383 says the same thing exactly (112 fraction bits = 28 hex digits).
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

/* ---- double (binary64) -------------------------------------------------- */
#define DBL_MANT_DIG 53
#define DBL_DIG      15
#define DBL_MIN      2.2250738585072014e-308
#define DBL_MAX      1.7976931348623157e+308
#define DBL_EPSILON  2.2204460492503131e-16

/* ---- long double (binary128) -------------------------------------------- *
 *
 * goc's long double is IEEE binary128 on every target -- 15 exponent bits and
 * a 113-bit significand -- so its range is far wider than double's and its
 * subnormals reach down to 2^-16494. A host compiler's `long double' is
 * whatever that host says it is (x87 80-bit on x86-64 Linux, plain double
 * under MSVC), and goclib has no way to interrogate it from inside a header
 * compiled with -nostdinc, so the binary128 values are goc-only and every
 * other compiler keeps the conservative double aliases below. */
#ifdef __goc__
#define LDBL_MANT_DIG    113
#define LDBL_DIG         33
#define LDBL_DECIMAL_DIG 36
#define LDBL_MIN_EXP     (-16381)
#define LDBL_MAX_EXP     16384
#define LDBL_MIN_10_EXP  (-4931)
#define LDBL_MAX_10_EXP  4932
/* (2 - 2^-112) * 2^16383: 112 fraction bits is 28 hex 'f's. */
#define LDBL_MAX         0x1.ffffffffffffffffffffffffffffp+16383L
/* Smallest normal. C's MIN_EXP is one more than the exponent here, exactly
 * as DBL_MIN_EXP (-1021) relates to DBL_MIN (2^-1022). */
#define LDBL_MIN         0x1p-16382L
#define LDBL_EPSILON     0x1p-112L
/* Smallest subnormal: 112 steps below LDBL_MIN, i.e. 2^-16494. */
#define LDBL_TRUE_MIN    0x1p-16494L
#define LDBL_NORM_MAX    LDBL_MAX
#define LDBL_HAS_SUBNORM 1
#else
/* Host compiler: long double is not binary128 there, so the honest answer is
 * the double aliases -- every one of them is true of the type as it is, and
 * none claims a precision the host does not have. */
#define LDBL_MANT_DIG DBL_MANT_DIG
#define LDBL_DIG      DBL_DIG
#define LDBL_MIN      DBL_MIN
#define LDBL_MAX      DBL_MAX
#define LDBL_EPSILON  DBL_EPSILON
#endif

/* ---- exponent range / radix (classic C89 macros, completed) ------------ */
#define FLT_RADIX     2
#define FLT_ROUNDS    1
#define FLT_MIN_EXP   (-125)
#define FLT_MAX_EXP   128
#define DBL_MIN_EXP   (-1021)
#define DBL_MAX_EXP   1024
#ifndef __goc__   /* the binary128 pair is defined with the LDBL_* block */
#define LDBL_MIN_EXP  DBL_MIN_EXP
#define LDBL_MAX_EXP  DBL_MAX_EXP
#endif

/* ---- decimal exponent range and round-trip width ------------------------ */
#define FLT_MIN_10_EXP   (-37)
#define FLT_MAX_10_EXP   38
#define DBL_MIN_10_EXP   (-307)
#define DBL_MAX_10_EXP   308
#ifndef __goc__
#define LDBL_MIN_10_EXP  DBL_MIN_10_EXP
#define LDBL_MAX_10_EXP  DBL_MAX_10_EXP
#endif
/* DECIMAL_DIG: the number of decimal digits that survive a round trip
 * (text -> type -> text). ceil(1 + MANT_DIG * log10(2)), so 9 / 17 / 36. */
#define FLT_DECIMAL_DIG  9
#define DBL_DECIMAL_DIG  17
#ifndef __goc__
#define LDBL_DECIMAL_DIG DBL_DECIMAL_DIG
#endif

/* ---- C23 normalization / IEC 60559 macros (7.7.1, 7.7.2) --------------- */
#define FLT_NORM_MAX FLT_MAX
#define DBL_NORM_MAX DBL_MAX
#ifndef __goc__
#define LDBL_NORM_MAX LDBL_MAX
#endif
/* Subnormals: every goc floating type has them, flushed to zero nowhere. */
#define FLT_HAS_SUBNORM  1
#define DBL_HAS_SUBNORM  1
#ifndef __goc__
#define LDBL_HAS_SUBNORM 1
#endif
/* Smallest positive subnormal, which is *not* *_MIN: that one is the smallest
 * NORMAL. binary32/binary64 reach 2^-149 / 2^-1074. */
#define FLT_TRUE_MIN  0x1p-149F
#define DBL_TRUE_MIN  0x1p-1074
#ifndef __goc__
#define LDBL_TRUE_MIN DBL_TRUE_MIN
#endif
#define FLT_IS_IEC_60559 1
#define DBL_IS_IEC_60559 1
#define LDBL_IS_IEC_60559 1

#ifndef __goc__
/* A host compiler's long double is not binary128; the marker lets portable
 * code detect the downgrade (P3.5). Under goc the marker is absent, which is
 * what tells that same code the real binary128 constants are in play. */
#define __goc_long_double_is_double 1
#endif

#endif /* GOC_FLOAT_H */
