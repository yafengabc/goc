#include "goclib.h"

/* --------------- binary128: goc's long double, in software -----------------
 *
 * goc's `long double` is IEEE-754 binary128 on every target: 1 sign bit, 15
 * exponent bits (bias 16383) and a 112-bit stored fraction -- 113 bits of
 * significand. It is deliberately NOT x86's 80-bit x87 format and NOT whatever
 * a given FPU happens to offer. One format everywhere is what lets a long
 * double program produce the same bits under the goc and gocl back ends, and
 * the same bits on a machine with no floating-point unit at all.
 *
 * So the arithmetic has to live here, on the bit pattern, in integer code.
 * Nothing in this file performs a floating-point operation, for the same
 * reason softfloat.c does not: a `+` between two long doubles compiles to a
 * call straight back into this file.
 *
 * ## The internal significand
 *
 * A finite nonzero value is carried as a 128-bit significand S whose leading
 * one sits at bit 127, plus an unbiased exponent:
 *
 *     value = (-1)^sign * (S / 2^127) * 2^exp
 *
 * Bits 127..15 are the significand (bit 127 is the implicit one); bits 14..0
 * are the rounding bits -- bit 14 the guard, bit 13 the round bit, bits 12..0
 * the sticky residue. Leaving room below the significand is what turns "round
 * correctly" into a shift and a compare, and lets it be written once in
 * tf_pack() instead of once per operation. An operation that produces a
 * significand in this form hands it straight to tf_pack() and is done.
 *
 * ## Why every 64-bit comparison splits into 32-bit halves
 *
 * On a soft-float target LLVM lowers a 64-bit integer comparison into a call,
 * so `a < b` on an unsigned long long in this file would recurse (the same
 * trap softfloat.c documents). tf_gt64/tf_ne64 compare the halves.
 *
 * ## Why these are not called __addtf3
 *
 * That name belongs to the compiler's own convention: LLVM turns `fadd fp128`
 * into a call to __addtf3 with an fp128 in XMM0, which is not this ABI (two
 * integer halves). Taking the reserved name now would hand both back ends a
 * function with a signature they cannot call and no diagnostic. #47 (gocl) and
 * #48 (goc) add the thin long-double-typed wrappers once the front end can
 * express the type; that is a handful of one-line functions then, and it is
 * the point where the ABI can actually be checked.
 */

#define TF_SIGN     0x8000000000000000ull /* hi bit 63: the sign */
#define TF_EXP_MASK 0x7FFF000000000000ull /* hi bits 62..48 */
#define TF_FRAC_HI  0x0000FFFFFFFFFFFFull /* hi bits 47..0: fraction 111..64 */
#define TF_QUIET    0x0000800000000000ull /* hi bit 47: fraction bit 111 */
#define TF_IMPLICIT 0x0001000000000000ull /* hi bit 48: 2^112 in the 128-bit word */
#define TF_BIAS     16383
#define TF_EXP_MAX  0x7FFF

/* The defaults handed back for an invalid operation (inf-inf, 0*inf, 0/0):
 * a positive quiet NaN and an infinity, matching what the hardware produces. */
#define TF_NAN_HI 0x7FFF800000000000ull
#define TF_NAN_LO 0x0000000000000000ull
#define TF_INF_HI 0x7FFF000000000000ull

/* Not an enum: goc has no enum constants (an `enum { A = 1 }` leaves A
 * undefined at codegen), and goclib has to compile under goc. */
#define TF_ZERO   0
#define TF_FINITE 1
#define TF_INF    2
#define TF_NAN    3

/* --- 64-bit helpers (32-bit halves, see the header) ----------------------- */

static int tf_is_zero64(unsigned long long a) {
    return (unsigned int)(a >> 32) == 0u && (unsigned int)(a & 0xFFFFFFFFull) == 0u;
}

static int tf_ne64(unsigned long long a, unsigned long long b) {
    if ((unsigned int)(a >> 32) != (unsigned int)(b >> 32)) return 1;
    return (unsigned int)(a & 0xFFFFFFFFull) != (unsigned int)(b & 0xFFFFFFFFull);
}

static int tf_gt64(unsigned long long a, unsigned long long b) {
    unsigned int ah = (unsigned int)(a >> 32), bh = (unsigned int)(b >> 32);
    if (ah != bh) return ah > bh;
    return (unsigned int)(a & 0xFFFFFFFFull) > (unsigned int)(b & 0xFFFFFFFFull);
}

/* --- 128-bit helpers ------------------------------------------------------ */

static int tf_cmp128(unsigned long long ah, unsigned long long al,
                     unsigned long long bh, unsigned long long bl) {
    if (tf_ne64(ah, bh)) return tf_gt64(ah, bh) ? 1 : -1;
    if (tf_ne64(al, bl)) return tf_gt64(al, bl) ? 1 : -1;
    return 0;
}

/* Returns 1 when the sum carries out of bit 127. */
static int tf_add128(unsigned long long *rh, unsigned long long *rl,
                     unsigned long long ah, unsigned long long al,
                     unsigned long long bh, unsigned long long bl) {
    unsigned long long l = al + bl;
    int carry = tf_gt64(al, l) ? 1 : 0;
    unsigned long long h = ah + bh;
    int out = tf_gt64(ah, h) ? 1 : 0;
    if (carry) {
        unsigned long long h2 = h + 1ull;
        if (tf_gt64(h, h2)) out = 1;
        h = h2;
    }
    *rh = h;
    *rl = l;
    return out;
}

/* ah:al >= bh:bl is required; the result is exact and non-negative. */
static void tf_sub128(unsigned long long *rh, unsigned long long *rl,
                      unsigned long long ah, unsigned long long al,
                      unsigned long long bh, unsigned long long bl) {
    unsigned long long l = al - bl;
    int borrow = tf_gt64(bl, al) ? 1 : 0;
    *rh = ah - bh - (unsigned long long)borrow;
    *rl = l;
}

static void tf_shl(unsigned long long *hi, unsigned long long *lo, int n) {
    unsigned long long h = *hi, l = *lo;
    if (n <= 0) return;
    if (n >= 128) { *hi = 0; *lo = 0; return; }
    if (n >= 64) { *hi = l << (n - 64); *lo = 0; return; }
    *hi = (h << n) | (l >> (64 - n));
    *lo = l << n;
}

/* Right shift with a sticky: anything shifted out is folded into bit 0, which
 * sits far below the guard and so is seen by the rounding in tf_pack(). */
static void tf_shr(unsigned long long *hi, unsigned long long *lo, int n) {
    unsigned long long h = *hi, l = *lo, lost;
    if (n <= 0) return;
    if (n >= 128) {
        *hi = 0;
        *lo = tf_is_zero64(h | l) ? 0ull : 1ull;
        return;
    }
    if (n >= 64) {
        if (n == 64) {
            lost = l;
        } else {
            lost = l | (h & ((1ull << (n - 64)) - 1ull));
        }
        *hi = 0;
        *lo = h >> (n - 64);
    } else {
        lost = l & ((1ull << n) - 1ull);
        *hi = h >> n;
        *lo = (l >> n) | (h << (64 - n));
    }
    if (!tf_is_zero64(lost)) *lo |= 1ull;
}

/* Shift left until the leading one reaches bit 127, keeping exp in step. */
static void tf_normalize(unsigned long long *hi, unsigned long long *lo, int *exp) {
    while (((*hi >> 63) & 1ull) == 0 && !tf_is_zero64(*hi | *lo)) {
        tf_shl(hi, lo, 1);
        (*exp)--;
    }
}

/* 64x64 -> 128, built from 32x32 -> 64 pieces. */
static void tf_mul64(unsigned long long a, unsigned long long b,
                     unsigned long long *hi, unsigned long long *lo) {
    unsigned long long a0 = a & 0xFFFFFFFFull, a1 = a >> 32;
    unsigned long long b0 = b & 0xFFFFFFFFull, b1 = b >> 32;
    unsigned long long p00 = a0 * b0;
    unsigned long long p01 = a0 * b1;
    unsigned long long p10 = a1 * b0;
    unsigned long long p11 = a1 * b1;
    unsigned long long mid = (p00 >> 32) + (p01 & 0xFFFFFFFFull) + (p10 & 0xFFFFFFFFull);
    *lo = (p00 & 0xFFFFFFFFull) | (mid << 32);
    *hi = p11 + (p01 >> 32) + (p10 >> 32) + (mid >> 32);
}

/* --- unpack / pack -------------------------------------------------------- */

typedef struct {
    unsigned long long shi, slo; /* the significand, leading one at bit 127 */
    int exp;                     /* value = (S / 2^127) * 2^exp */
    int sign;
    int cls;
} tf_val;

static int tf_is_nan_bits(unsigned long long hi, unsigned long long lo) {
    if (((hi >> 48) & 0x7FFFull) != 0x7FFFull) return 0;
    return !tf_is_zero64((hi & TF_FRAC_HI) | lo);
}

static void tf_unpack(unsigned long long hi, unsigned long long lo, tf_val *v) {
    unsigned long long e = (hi >> 48) & 0x7FFFull;
    unsigned long long fh = hi & TF_FRAC_HI;
    v->sign = (int)((hi >> 63) & 1ull);
    v->shi = 0;
    v->slo = 0;
    v->exp = 0;
    if (e == 0x7FFFull) {
        v->cls = tf_is_zero64(fh | lo) ? TF_INF : TF_NAN;
        return;
    }
    /* Subnormals are the same shape with the implicit one absent and the
     * exponent read as 1: one unpack path for both, no special case later.
     *
     * The 113-bit significand spans both words -- 2^112 is bit 48 of shi, not
     * something that fits in the low word -- and the 15-bit shift lifts it so
     * that bit lands at 127. */
    v->shi = (e != 0 ? TF_IMPLICIT : 0ull) | fh;
    v->slo = lo;
    v->exp = (int)(e != 0 ? e : 1ull) - TF_BIAS;
    tf_shl(&v->shi, &v->slo, 15);
    if (tf_is_zero64(v->shi | v->slo)) {
        v->cls = TF_ZERO;
        return;
    }
    v->cls = TF_FINITE;
    tf_normalize(&v->shi, &v->slo, &v->exp);
}

/* Round to nearest, ties to even, and pack. S must be normalized (leading one
 * at bit 127) unless it came out of the subnormal path below. */
static void tf_pack(goc_tf128 *r, int sign, int exp,
                    unsigned long long shi, unsigned long long slo) {
    unsigned long long ef;
    if (tf_is_zero64(shi | slo)) {
        r->hi = sign ? TF_SIGN : 0ull;
        r->lo = 0ull;
        return;
    }
    if (exp > TF_BIAS) { /* overflow: round-to-nearest gives infinity */
        r->hi = (sign ? TF_SIGN : 0ull) | TF_INF_HI;
        r->lo = 0ull;
        return;
    }
    if (exp < 1 - TF_BIAS) { /* denormalize; the shift keeps a sticky */
        tf_shr(&shi, &slo, (1 - TF_BIAS) - exp);
        exp = 1 - TF_BIAS;
        if (tf_is_zero64(shi | slo)) {
            r->hi = sign ? TF_SIGN : 0ull;
            r->lo = 0ull;
            return;
        }
    }
    {
        int g = (int)((slo >> 14) & 1ull);          /* the guard */
        int rs = !tf_is_zero64(slo & 0x3FFFull);    /* round bit or sticky */
        int lsb = (int)((slo >> 15) & 1ull);        /* significand LSB, for ties */
        if (g && (rs || lsb)) {
            unsigned long long nl = slo + 0x8000ull; /* add one at the LSB */
            int carry = tf_gt64(slo, nl) ? 1 : 0;
            unsigned long long nh = shi + (unsigned long long)carry;
            if (tf_gt64(shi, nh)) {
                /* Every significand bit was one: 2^113 rounds up to 2^112
                 * with the exponent one higher. */
                shi = 0x8000000000000000ull;
                slo = 0ull;
                exp++;
            } else {
                shi = nh;
                slo = nl & ~0x7FFFull;
            }
        } else {
            slo &= ~0x7FFFull;
        }
    }
    /* The check has to happen *after* rounding, not only before it: a value
     * shifted so far down that nothing but the sticky survived is non-zero on
     * the way in and zero on the way out, and testing earlier packs it as the
     * smallest subnormal instead of the zero it rounded to. */
    if (tf_is_zero64(shi | slo)) {
        r->hi = sign ? TF_SIGN : 0ull;
        r->lo = 0ull;
        return;
    }
    /* A significand that never reached bit 127 is a subnormal, and a subnormal
     * is spelled with an exponent field of 0 -- not with the exponent the value
     * would have had. exp was pinned at 1-BIAS above precisely so that the
     * exception falls out: a value that rounds up to the smallest normal does
     * reach bit 127 and takes field 1. */
    ef = ((shi >> 63) & 1ull) ? (unsigned long long)(exp + TF_BIAS) : 0ull;
    r->hi = (sign ? TF_SIGN : 0ull) | (ef << 48) | ((shi >> 15) & TF_FRAC_HI);
    r->lo = ((shi & 0x7FFFull) << 49) | (slo >> 15);
}

/* --- add / sub ------------------------------------------------------------ */

static goc_tf128 tf_addsub(goc_tf128 a, goc_tf128 b, int subtract) {
    tf_val va, vb, *pa, *pb, *pt;
    goc_tf128 r;
    int sign, exp, cmp;
    unsigned long long shi, slo;

    tf_unpack(a.hi, a.lo, &va);
    tf_unpack(b.hi, b.lo, &vb);
    if (subtract) vb.sign ^= 1; /* subtracting b is adding b negated, and that
                                 * is the right thing for a zero too: 0 - 0
                                 * is +0, and -0 negated is +0. */
    if (va.cls == TF_NAN) { r.hi = a.hi | TF_QUIET; r.lo = a.lo; return r; }
    if (vb.cls == TF_NAN) { r.hi = b.hi | TF_QUIET; r.lo = b.lo; return r; }
    if (va.cls == TF_INF) {
        if (vb.cls == TF_INF && va.sign != vb.sign) { /* inf + -inf */
            r.hi = TF_NAN_HI; r.lo = TF_NAN_LO; return r;
        }
        r.hi = (va.sign ? TF_SIGN : 0ull) | TF_INF_HI; r.lo = 0ull; return r;
    }
    if (vb.cls == TF_INF) {
        r.hi = (vb.sign ? TF_SIGN : 0ull) | TF_INF_HI; r.lo = 0ull; return r;
    }
    if (va.cls == TF_ZERO && vb.cls == TF_ZERO) {
        /* Opposite-signed zeros sum to +0 under round-to-nearest. */
        int s = (va.sign == vb.sign) ? va.sign : 0;
        r.hi = s ? TF_SIGN : 0ull; r.lo = 0ull; return r;
    }
    if (va.cls == TF_ZERO) { r.hi = b.hi ^ (subtract ? TF_SIGN : 0ull); r.lo = b.lo; return r; }
    if (vb.cls == TF_ZERO) { r.hi = a.hi; r.lo = a.lo; return r; }

    pa = &va;
    pb = &vb;
    if (pa->exp < pb->exp) { pt = pa; pa = pb; pb = pt; }
    exp = pa->exp;
    shi = pb->shi;
    slo = pb->slo;
    tf_shr(&shi, &slo, exp - pb->exp); /* folds the lost bits into a sticky */
    if (pa->sign == pb->sign) {
        int carry = tf_add128(&shi, &slo, pa->shi, pa->slo, shi, slo);
        sign = pa->sign;
        if (carry) {
            unsigned long long lost = slo & 1ull;
            tf_shr(&shi, &slo, 1);
            /* The bit that carried out of the 128-bit sum is the result's
             * leading one; tf_shr cannot put it back because it never saw it.
             * Dropping it used to be invisible -- the implicit bit is masked
             * out when packing -- but it leaves the significand looking
             * subnormal, which decides the exponent field. */
            shi |= 0x8000000000000000ull;
            if (lost) slo |= 1ull;
            exp++;
        }
    } else {
        cmp = tf_cmp128(pa->shi, pa->slo, shi, slo);
        if (cmp == 0) { /* exact cancellation is +0, whatever the signs */
            r.hi = 0ull; r.lo = 0ull; return r;
        }
        if (cmp > 0) {
            tf_sub128(&shi, &slo, pa->shi, pa->slo, shi, slo);
            sign = pa->sign;
        } else {
            tf_sub128(&shi, &slo, shi, slo, pa->shi, pa->slo);
            sign = pb->sign;
        }
        tf_normalize(&shi, &slo, &exp);
    }
    tf_pack(&r, sign, exp, shi, slo);
    return r;
}

goc_tf128 goc_tf_add(goc_tf128 a, goc_tf128 b) { return tf_addsub(a, b, 0); }
goc_tf128 goc_tf_sub(goc_tf128 a, goc_tf128 b) { return tf_addsub(a, b, 1); }

/* --- mul ------------------------------------------------------------------ */

goc_tf128 goc_tf_mul(goc_tf128 a, goc_tf128 b) {
    tf_val va, vb;
    goc_tf128 r;
    unsigned long long w3, w2, w1, w0, mh, ml, t;
    unsigned long long shi, slo;
    int exp;

    tf_unpack(a.hi, a.lo, &va);
    tf_unpack(b.hi, b.lo, &vb);
    if (va.cls == TF_NAN) { r.hi = a.hi | TF_QUIET; r.lo = a.lo; return r; }
    if (vb.cls == TF_NAN) { r.hi = b.hi | TF_QUIET; r.lo = b.lo; return r; }
    if ((va.cls == TF_INF && vb.cls == TF_ZERO) ||
        (va.cls == TF_ZERO && vb.cls == TF_INF)) {
        r.hi = TF_NAN_HI; r.lo = TF_NAN_LO; return r;
    }
    if (va.cls == TF_INF || vb.cls == TF_INF) {
        r.hi = ((va.sign ^ vb.sign) ? TF_SIGN : 0ull) | TF_INF_HI; r.lo = 0ull; return r;
    }
    if (va.cls == TF_ZERO || vb.cls == TF_ZERO) {
        r.hi = ((va.sign ^ vb.sign) ? TF_SIGN : 0ull); r.lo = 0ull; return r;
    }

    /* 128 x 128 -> 256, but only the top 128 bits can survive: the product is
     * in [2^254, 2^256) and the result carries 113 significand bits plus 15
     * rounding bits, so everything below the top 128 is a sticky. */
    tf_mul64(va.shi, vb.shi, &mh, &ml);
    w3 = mh; w2 = ml; w1 = 0; w0 = 0;
    tf_mul64(va.shi, vb.slo, &mh, &ml);
    t = w2; w2 = w2 + mh; if (tf_gt64(t, w2)) w3 = w3 + 1ull;
    t = w1; w1 = w1 + ml; if (tf_gt64(t, w1)) {
        t = w2; w2 = w2 + 1ull; if (tf_gt64(t, w2)) w3 = w3 + 1ull;
    }
    tf_mul64(va.slo, vb.shi, &mh, &ml);
    t = w2; w2 = w2 + mh; if (tf_gt64(t, w2)) w3 = w3 + 1ull;
    t = w1; w1 = w1 + ml; if (tf_gt64(t, w1)) {
        t = w2; w2 = w2 + 1ull; if (tf_gt64(t, w2)) w3 = w3 + 1ull;
    }
    tf_mul64(va.slo, vb.slo, &mh, &ml);
    t = w1; w1 = w1 + mh; if (tf_gt64(t, w1)) {
        t = w2; w2 = w2 + 1ull; if (tf_gt64(t, w2)) w3 = w3 + 1ull;
    }
    w0 = ml;

    if (w3 >> 63) {
        /* the product's leading one is at bit 255: shift right 128 */
        shi = w3; slo = w2;
        exp = va.exp + vb.exp + 1;
        if (!tf_is_zero64(w1 | w0)) slo |= 1ull;
    } else {
        /* bit 254: shift right 127, which is a 1-bit shift of the top 128
         * with the top bit of w1 brought in at the bottom */
        shi = (w3 << 1) | (w2 >> 63);
        slo = (w2 << 1) | (w1 >> 63);
        exp = va.exp + vb.exp;
        if (!tf_is_zero64((w1 & 0x7FFFFFFFFFFFFFFFull) | w0)) slo |= 1ull;
    }
    tf_pack(&r, va.sign ^ vb.sign, exp, shi, slo);
    return r;
}

/* --- div ------------------------------------------------------------------ */

/* w[0] holds bits 255..192, w[3] bits 63..0. */
static int tf_bit256(unsigned long long *w, int b) {
    int wi = 3 - (b >> 6);
    return (int)((w[wi] >> (b & 63)) & 1ull);
}

goc_tf128 goc_tf_div(goc_tf128 a, goc_tf128 b) {
    tf_val va, vb;
    goc_tf128 r;
    unsigned long long w[4];
    unsigned long long qh, ql, r1, r0, nl;
    int i, shift, exp, rc, borrow, qbit;

    tf_unpack(a.hi, a.lo, &va);
    tf_unpack(b.hi, b.lo, &vb);
    if (va.cls == TF_NAN) { r.hi = a.hi | TF_QUIET; r.lo = a.lo; return r; }
    if (vb.cls == TF_NAN) { r.hi = b.hi | TF_QUIET; r.lo = b.lo; return r; }
    if (va.cls == TF_INF && vb.cls == TF_INF) { r.hi = TF_NAN_HI; r.lo = TF_NAN_LO; return r; }
    if (va.cls == TF_INF) {
        r.hi = ((va.sign ^ vb.sign) ? TF_SIGN : 0ull) | TF_INF_HI; r.lo = 0ull; return r;
    }
    if (vb.cls == TF_INF) {
        r.hi = ((va.sign ^ vb.sign) ? TF_SIGN : 0ull); r.lo = 0ull; return r;
    }
    if (vb.cls == TF_ZERO) {
        if (va.cls == TF_ZERO) { r.hi = TF_NAN_HI; r.lo = TF_NAN_LO; return r; }
        r.hi = ((va.sign ^ vb.sign) ? TF_SIGN : 0ull) | TF_INF_HI; r.lo = 0ull; return r;
    }
    if (va.cls == TF_ZERO) {
        r.hi = ((va.sign ^ vb.sign) ? TF_SIGN : 0ull); r.lo = 0ull; return r;
    }

    /* Both significands are in [2^127, 2^128), so the quotient is in (0.5, 2).
     * Scaling the dividend by 2^127 or 2^128 -- chosen so the quotient lands
     * in [2^127, 2^128) -- makes the long division produce exactly the 128
     * bits the result needs and no more. */
    if (tf_cmp128(va.shi, va.slo, vb.shi, vb.slo) >= 0) {
        shift = 127;
        exp = va.exp - vb.exp;
    } else {
        shift = 128;
        exp = va.exp - vb.exp - 1;
    }
    if (shift == 127) {
        w[0] = va.shi >> 1;
        w[1] = (va.shi << 63) | (va.slo >> 1);
        w[2] = va.slo << 63;
        w[3] = 0;
    } else {
        w[0] = va.shi;
        w[1] = va.slo;
        w[2] = 0;
        w[3] = 0;
    }

    /* Binary long division. The remainder stays below the divisor, so after
     * the shift it can hold one bit above 128 -- rc.
     *
     * It runs down to bit 0 of the dividend, which is 128+shift iterations,
     * not 128: the dividend's bits below `shift` are all zero, but the
     * quotient's bits below the top 128 are NOT -- they come out of the
     * remainder, which is where 1/3 gets its 0101 pattern. Stopping at bit
     * `shift` would produce the leading one and 127 zeros. */
    qh = 0; ql = 0; rc = 0; r1 = 0; r0 = 0;
    for (i = 0; i <= 127 + shift; i++) {
        qbit = tf_bit256(w, 127 + shift - i);
        rc = (int)(r1 >> 63);
        r1 = (r1 << 1) | (r0 >> 63);
        r0 = (r0 << 1) | (unsigned long long)qbit;
        if (rc || tf_cmp128(r1, r0, vb.shi, vb.slo) >= 0) {
            /* With rc set the divisor is subtracted modulo 2^128, which is
             * the same thing: R - D = 2^128 + (M - D) with M < D. */
            nl = r0 - vb.slo;
            borrow = tf_gt64(vb.slo, r0) ? 1 : 0;
            r1 = r1 - vb.shi - (unsigned long long)borrow;
            r0 = nl;
            rc = 0;
            qbit = 1;
        } else {
            qbit = 0;
        }
        qh = (qh << 1) | (ql >> 63);
        ql = (ql << 1) | (unsigned long long)qbit;
    }
    if (!tf_is_zero64(r1 | r0)) ql |= 1ull;
    tf_pack(&r, va.sign ^ vb.sign, exp, qh, ql);
    return r;
}

/* --- compare / negate ----------------------------------------------------- */

int goc_tf_cmp(goc_tf128 a, goc_tf128 b) {
    int sa, sb, c;
    unsigned long long ah, bh;
    if (tf_is_nan_bits(a.hi, a.lo) || tf_is_nan_bits(b.hi, b.lo)) return 2;
    sa = (int)((a.hi >> 63) & 1ull);
    sb = (int)((b.hi >> 63) & 1ull);
    ah = a.hi & 0x7FFFFFFFFFFFFFFFull;
    bh = b.hi & 0x7FFFFFFFFFFFFFFFull;
    if (tf_is_zero64(ah | a.lo) && tf_is_zero64(bh | b.lo)) return 0; /* +0 == -0 */
    if (sa != sb) return sa ? -1 : 1;
    /* With the sign stripped the remaining 127 bits order the magnitudes. */
    c = tf_cmp128(ah, a.lo, bh, b.lo);
    if (sa) c = -c;
    return c;
}

goc_tf128 goc_tf_neg(goc_tf128 a) {
    goc_tf128 r;
    r.hi = a.hi ^ TF_SIGN;
    r.lo = a.lo;
    return r;
}

/* --- widen / narrow ------------------------------------------------------- */

goc_tf128 goc_tf_from_double(unsigned long long bits) {
    goc_tf128 r;
    unsigned long long frac = bits & 0x000FFFFFFFFFFFFFull;
    unsigned long long e = (bits >> 52) & 0x7FFull;
    int sign = (int)((bits >> 63) & 1ull);
    unsigned long long shi, slo;
    int exp;

    if (e == 0x7FFull) {
        if (tf_is_zero64(frac)) {
            r.hi = (sign ? TF_SIGN : 0ull) | TF_INF_HI;
        } else {
            r.hi = TF_NAN_HI; /* the payload does not survive the widening */
        }
        r.lo = 0ull;
        return r;
    }
    /* 53 significand bits, placed so the leading one lands at bit 127. */
    shi = 0;
    slo = (e != 0 ? (1ull << 52) : 0ull) | frac;
    exp = (int)(e != 0 ? e : 1ull) - 1023;
    tf_shl(&shi, &slo, 75);
    if (tf_is_zero64(shi | slo)) {
        r.hi = sign ? TF_SIGN : 0ull;
        r.lo = 0ull;
        return r;
    }
    tf_normalize(&shi, &slo, &exp);
    tf_pack(&r, sign, exp, shi, slo);
    return r;
}

goc_tf128 goc_tf_from_float(unsigned int bits) {
    goc_tf128 r;
    unsigned long long frac = (unsigned long long)(bits & 0x007FFFFFu);
    unsigned long long e = (unsigned long long)((bits >> 23) & 0xFFu);
    int sign = (int)((bits >> 31) & 1u);
    unsigned long long shi, slo;
    int exp;

    if (e == 0xFFull) {
        if (tf_is_zero64(frac)) {
            r.hi = (sign ? TF_SIGN : 0ull) | TF_INF_HI;
        } else {
            r.hi = TF_NAN_HI;
        }
        r.lo = 0ull;
        return r;
    }
    shi = 0;
    slo = (e != 0 ? (1ull << 23) : 0ull) | frac;
    exp = (int)(e != 0 ? e : 1ull) - 127;
    tf_shl(&shi, &slo, 104);
    if (tf_is_zero64(shi | slo)) {
        r.hi = sign ? TF_SIGN : 0ull;
        r.lo = 0ull;
        return r;
    }
    tf_normalize(&shi, &slo, &exp);
    tf_pack(&r, sign, exp, shi, slo);
    return r;
}

/* --- to double / float ---------------------------------------------------- */

unsigned long long goc_tf_to_double(goc_tf128 a) {
    tf_val v;
    unsigned long long m, frac, sh;
    int sign, efield, g, rs, lsb, exp;

    tf_unpack(a.hi, a.lo, &v);
    sign = v.sign;
    if (v.cls == TF_NAN) return 0x7FF8000000000000ull; /* the payload is lost */
    if (v.cls == TF_INF) return ((unsigned long long)sign << 63) | 0x7FF0000000000000ull;
    if (v.cls == TF_ZERO) return (unsigned long long)sign << 63;
    exp = v.exp;
    sh = v.shi;
    if (exp < -1022) {
        tf_shr(&sh, &v.slo, -1022 - exp);
        exp = -1022;
        if (tf_is_zero64(sh | v.slo)) return (unsigned long long)sign << 63;
    }
    /* The 53-bit significand is S >> 75: bits 127..75, all in shi. */
    m = sh >> 11;
    g = (int)((sh >> 10) & 1ull);
    rs = !tf_is_zero64((sh & 0x3FFull) | v.slo);
    lsb = (int)(m & 1ull);
    if (g && (rs || lsb)) m++;
    efield = exp + 1023;
    if (m >= (1ull << 53)) { m >>= 1; efield++; }
    if (efield >= 0x7FF) return ((unsigned long long)sign << 63) | 0x7FF0000000000000ull;
    if (m >= (1ull << 52)) {
        frac = m - (1ull << 52);
        return ((unsigned long long)sign << 63) | ((unsigned long long)efield << 52) | frac;
    }
    /* subnormal (or a zero that rounding did not rescue): exponent field 0 */
    return ((unsigned long long)sign << 63) | m;
}

unsigned int goc_tf_to_float(goc_tf128 a) {
    tf_val v;
    unsigned long long m, sh;
    unsigned int f;
    int sign, efield, g, rs, lsb, exp;

    tf_unpack(a.hi, a.lo, &v);
    sign = v.sign;
    if (v.cls == TF_NAN) return 0x7FC00000u;
    if (v.cls == TF_INF) return ((unsigned int)sign << 31) | 0x7F800000u;
    if (v.cls == TF_ZERO) return (unsigned int)sign << 31;
    exp = v.exp;
    sh = v.shi;
    if (exp < -126) {
        tf_shr(&sh, &v.slo, -126 - exp);
        exp = -126;
        if (tf_is_zero64(sh | v.slo)) return (unsigned int)sign << 31;
    }
    m = sh >> 40; /* the 24-bit significand: S bits 127..104 */
    g = (int)((sh >> 39) & 1ull);
    rs = !tf_is_zero64((sh & 0x7FFFFFFFFFull) | v.slo);
    lsb = (int)(m & 1ull);
    if (g && (rs || lsb)) m++;
    efield = exp + 127;
    if (m >= (1ull << 24)) { m >>= 1; efield++; }
    if (efield >= 0xFF) return ((unsigned int)sign << 31) | 0x7F800000u;
    if (m >= (1ull << 23)) {
        f = (unsigned int)(m - (1ull << 23));
        return ((unsigned int)sign << 31) | ((unsigned int)efield << 23) | f;
    }
    return ((unsigned int)sign << 31) | (unsigned int)m;
}

/* --- to / from integers --------------------------------------------------- */

/* S >> n into 64 bits. Returns 1 when the result does not fit. */
static int tf_shr64(unsigned long long shi, unsigned long long slo, int n,
                    unsigned long long *out) {
    if (n <= 0) return 1; /* S >= 2^127 */
    if (n >= 128) { *out = 0ull; return 0; }
    if (n >= 64) {
        *out = (n == 64) ? shi : (shi >> (n - 64));
        return 0;
    }
    if (!tf_is_zero64(shi >> n)) return 1;
    *out = (shi << (64 - n)) | (slo >> n);
    return 0;
}

long long goc_tf_to_ll(goc_tf128 a) {
    tf_val v;
    unsigned long long mag;
    tf_unpack(a.hi, a.lo, &v);
    if (v.cls == TF_NAN) return 0;
    /* Out-of-range and NaN are undefined in C; saturating is the choice here
     * and it is the same choice in both directions, which is what matters --
     * a program that relies on it is relying on nothing the standard promises. */
    if (v.cls == TF_INF) return v.sign ? (long long)0x8000000000000000ull
                                      : (long long)0x7FFFFFFFFFFFFFFFull;
    if (v.cls == TF_ZERO) return 0;
    if (v.exp < 0) return 0; /* |x| < 1 truncates toward zero */
    if (tf_shr64(v.shi, v.slo, 127 - v.exp, &mag)) {
        return v.sign ? (long long)0x8000000000000000ull
                      : (long long)0x7FFFFFFFFFFFFFFFull;
    }
    if (v.sign) {
        if (tf_gt64(mag, 0x8000000000000000ull)) return (long long)0x8000000000000000ull;
        return (long long)(0ull - mag);
    }
    if (tf_gt64(mag, 0x7FFFFFFFFFFFFFFFull)) return (long long)0x7FFFFFFFFFFFFFFFull;
    return (long long)mag;
}

unsigned long long goc_tf_to_ull(goc_tf128 a) {
    tf_val v;
    unsigned long long mag;
    tf_unpack(a.hi, a.lo, &v);
    if (v.cls == TF_NAN) return 0ull;
    if (v.cls == TF_ZERO) return 0ull;
    if (v.sign) return 0ull; /* negative to unsigned: saturate at 0 */
    if (v.cls == TF_INF) return 0xFFFFFFFFFFFFFFFFull;
    if (v.exp < 0) return 0ull;
    if (tf_shr64(v.shi, v.slo, 127 - v.exp, &mag)) return 0xFFFFFFFFFFFFFFFFull;
    return mag;
}

int goc_tf_to_int(goc_tf128 a) {
    long long v = goc_tf_to_ll(a);
    if (v > 2147483647LL) return 2147483647;
    if (v < -2147483648LL) return -2147483648;
    return (int)v;
}

unsigned int goc_tf_to_uint(goc_tf128 a) {
    unsigned long long v = goc_tf_to_ull(a);
    if (tf_gt64(v, 0xFFFFFFFFull)) return 0xFFFFFFFFu;
    return (unsigned int)v;
}

goc_tf128 goc_tf_from_ll(long long v) {
    unsigned long long mag;
    int sign = 0;
    goc_tf128 r;
    unsigned long long shi, slo;
    int exp;
    if (v < 0) {
        sign = 1;
        /* -LLONG_MIN overflows; 0 - v as unsigned is the same bits. */
        mag = 0ull - (unsigned long long)v;
    } else {
        mag = (unsigned long long)v;
    }
    if (tf_is_zero64(mag)) { r.hi = 0ull; r.lo = 0ull; return r; }
    shi = 0;
    slo = mag;
    exp = 63;
    tf_shl(&shi, &slo, 64);
    tf_normalize(&shi, &slo, &exp);
    tf_pack(&r, sign, exp, shi, slo);
    return r;
}

goc_tf128 goc_tf_from_ull(unsigned long long v) {
    goc_tf128 r;
    unsigned long long shi, slo;
    int exp;
    if (tf_is_zero64(v)) { r.hi = 0ull; r.lo = 0ull; return r; }
    shi = 0;
    slo = v;
    exp = 63;
    tf_shl(&shi, &slo, 64);
    tf_normalize(&shi, &slo, &exp);
    tf_pack(&r, 0, exp, shi, slo);
    return r;
}
