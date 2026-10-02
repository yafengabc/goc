/* C23 <stdckdint.h> -- checked integer operations for goc.
 *
 * goc models integers at their C widths (char 8-bit, short 16-bit, int 32-bit,
 * long/long long 64-bit), so overflow detection keys off sizeof() of the result
 * type. Signedness is derived at runtime from a read of *r, because goc does
 * not yet support typeof() in cast position (typeof(x))y.
 *
 *   bool overflowed = ckd_add(&r, a, b);   // r = a + b; overflow iff result
 *                                             does not fit the type of r
 *
 * The macros return a _Bool. Operands are evaluated twice (no statement-
 * expressions in goc yet), so pass plain variables/expressions without side
 * effects. 64-bit (long/long long) overflow cannot be detected without 128-bit
 * math and is reported as non-overflowing.
 */
#ifndef STDCKDINT_H
#define STDCKDINT_H

/* Signedness of the destination type. v - v - 1 is -1 for signed types and the
 * type's maximum value for unsigned types; the latter is > 0. The read of *r is
 * safe because the caller assigns *r before the overflow test is evaluated. */
#define __ckd_signed(r) (((*(r) - *(r) - 1) > 0) ? 0 : 1)

/* Signed overflow: S (a 64-bit sum/diff/product) fits a W-byte signed type iff
 * truncating it to W bits and sign-extending back equals S. goc's >> on signed
 * is arithmetic (sar), so the high shift then arithmetic shift sign-extends. */
#define __ckd_oflow_signed(S, W)                                        \
    ((W) >= 8 ? 0                                                       \
     : ((S) != ((long long)(S) << (64 - 8 * (W)) >> (64 - 8 * (W)))))

/* Unsigned overflow: bits above the W-byte field must be clear. */
#define __ckd_oflow_unsigned(S, W)                                      \
    ((W) >= 8 ? 0                                                       \
     : (((unsigned long long)(S) >> (8 * (W))) != 0))

#define __ckd_oflow(S, W, r)                                           \
    (__ckd_signed(r) ? __ckd_oflow_signed(S, W) : __ckd_oflow_unsigned(S, W))

#define ckd_add(r, a, b)                                               \
    ((*(r) = (a) + (b)),                                               \
     __ckd_oflow((long long)(a) + (long long)(b), sizeof(*(r)), r))

#define ckd_sub(r, a, b)                                               \
    ((*(r) = (a) - (b)),                                               \
     __ckd_oflow((long long)(a) - (long long)(b), sizeof(*(r)), r))

#define ckd_mul(r, a, b)                                               \
    ((*(r) = (a) * (b)),                                               \
     __ckd_oflow((long long)(a) * (long long)(b), sizeof(*(r)), r))

#endif /* STDCKDINT_H */
