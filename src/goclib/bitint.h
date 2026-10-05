/* =============================================================================
 * bitint.h -- runtime helpers for C23 _BitInt(N) values.
 *
 * A _BitInt(N) value is a little-endian array of ceil(N/64) 64-bit words.
 * The goc code generator calls these helpers directly; user code normally
 * only needs __goclib_bi_str to print. `n` is always the word count.
 * ========================================================================== */
#ifndef BITINT_H
#define BITINT_H

void __goclib_bi_zero(unsigned long long *r, long n);
void __goclib_bi_copy(unsigned long long *r, const unsigned long long *a, long n);
void __goclib_bi_from_i64(unsigned long long *r, long long v, long n, long dstSigned);
void __goclib_bi_from_i64_trunc(unsigned long long *r, long long v, long bits, long dstSigned);
void __goclib_bi_conv(unsigned long long *r, const unsigned long long *a,
                      long dstBits, long dstSigned, long srcBits, long srcSigned);
void __goclib_bi_from_i64_trunc(unsigned long long *r, long long v, long bits, long dstSigned);
void __goclib_bi_conv(unsigned long long *r, const unsigned long long *a,
                      long dstBits, long dstSigned, long srcBits, long srcSigned);
long long __goclib_bi_to_i64(const unsigned long long *a, long n);
long __goclib_bi_is_zero(const unsigned long long *a, long n);
void __goclib_bi_add(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n);
void __goclib_bi_sub(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n);
void __goclib_bi_neg(unsigned long long *r, const unsigned long long *a, long n);
void __goclib_bi_not(unsigned long long *r, const unsigned long long *a, long n);
void __goclib_bi_and(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n);
void __goclib_bi_or(unsigned long long *r, const unsigned long long *a,
                    const unsigned long long *b, long n);
void __goclib_bi_xor(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n);
void __goclib_bi_shl(unsigned long long *r, const unsigned long long *a,
                     unsigned long long sh, long n);
void __goclib_bi_shr_u(unsigned long long *r, const unsigned long long *a,
                       unsigned long long sh, long n);
void __goclib_bi_shr_s(unsigned long long *r, const unsigned long long *a,
                       unsigned long long sh, long n);
long __goclib_bi_cmp(const unsigned long long *a, const unsigned long long *b,
                     long n, long isSigned);
void __goclib_bi_mul(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n);
void __goclib_bi_div_u(unsigned long long *r, const unsigned long long *a,
                       const unsigned long long *b, long n);
void __goclib_bi_mod_u(unsigned long long *r, const unsigned long long *a,
                       const unsigned long long *b, long n);
void __goclib_bi_div_s(unsigned long long *r, const unsigned long long *a,
                       const unsigned long long *b, long n);
void __goclib_bi_mod_s(unsigned long long *r, const unsigned long long *a,
                       const unsigned long long *b, long n);
void __goclib_bi_widen_u(unsigned long long *r, const unsigned long long *a,
                         long nDst, long nSrc);
void __goclib_bi_widen_s(unsigned long long *r, const unsigned long long *a,
                         long nDst, long nSrc);
long __goclib_bi_str(char *buf, const unsigned long long *a, long n, long isSigned);

#endif /* BITINT_H */
