#include "goclib.h"

/* ------------------- compiler-rt builtins (soft float) -------------------
 *
 * A target without a floating-point unit has no instruction for `double`
 * arithmetic, so the compiler emits a *call* to one of these helpers instead
 * (the `__*df3` family), and a 64-bit division or a double<->64-bit conversion
 * becomes a call too. 32-bit ARM's soft-float ABI (`-arch armel`) is such a
 * target: the default `-arch arm` is armhf, where the same operations are VFP
 * instructions and none of this is referenced.
 *
 * The constraint that shapes the whole file: **these functions must not perform
 * floating-point arithmetic themselves.** A `+` on two doubles would compile to
 * a call to `__adddf3`, so writing the addition as `a + b` here is infinite
 * recursion. Everything below works on the IEEE-754 bit pattern through a
 * union; the only floating-point values that appear are ones written as bit
 * patterns or produced by integer arithmetic.
 *
 * ## The internal format
 *
 * Every operation carries its significand in a 64-bit word with three bits of
 * headroom below the value, so that rounding is a shift and a compare rather
 * than a reconstruction:
 *
 *     bit 55      the leading one (a normal or subnormal value, normalised)
 *     bits 54..3  the 52 fraction bits -- exactly what a `double` stores
 *     bit  2      guard    (round if set)
 *     bit  1      round    (together with the guard, decides a tie)
 *     bit  0      sticky   (something was lost below; never rounded from)
 *
 * A helper that produces a significand in this form can hand it to
 * soft_finish(), which rounds once and packs the result -- so "round correctly"
 * is written once rather than once per operation. Getting that wrong in each
 * place separately is how an implementation ends up 1 ulp off in six
 * operations and exactly right in none.
 *
 * ## Why the compares avoid 64-bit comparisons
 *
 * LLVM lowers a 64-bit integer compare on this target into a *floating-point*
 * compare, which is a call to one of the __*df2 functions in this file. Writing
 * `a < b` on an unsigned long long therefore re-enters this file and recurses
 * until the stack is exhausted. Every 64-bit compare below is split into its
 * two 32-bit halves (soft_cmp64), where `cmp` exists.
 */

typedef union {
    double d;
    unsigned long long u;
} soft_double;

#define DBL_SIGN_BIT 0x8000000000000000ull
#define DBL_EXP_MASK 0x7FF0000000000000ull
#define DBL_SIG_MASK 0x000FFFFFFFFFFFFFull
#define DBL_NAN_BITS 0x7FF8000000000000ull
#define DBL_INF_BITS 0x7FF0000000000000ull

/* The leading one, and the three bits below the fraction. */
#define SF_LEAD   0x0080000000000000ull /* the leading one: bit 55 */
#define SF_GUARD  0x0000000000000004ull
#define SF_ROUND  0x0000000000000002ull
#define SF_STICKY 0x0000000000000001ull
/* Strictly above the leading one. A set bit here means the value carries past
 * bit 55 and must be shifted down before it can be rounded.
 *
 * Strictly is the whole point: including bit 55 itself makes the shift loop
 * fire once on *every* value, so 1.0 normalises to 2.0 with the exponent
 * unchanged and the result comes out as 1.0. The mask is bits 56..63. */
#define SF_ABOVE_LEAD 0xFF00000000000000ull

/* --- helpers over the raw bits ------------------------------------------- */

static int soft_exp(unsigned long long u) {
    return (int)((u & DBL_EXP_MASK) >> 52);
}

static int nonzero64(unsigned long long u) {
    /* 32-bit halves: a 64-bit compare would become a call back into here. */
    if ((unsigned int)(u >> 32) != 0u) return 1;
    return (unsigned int)(u & 0xFFFFFFFFull) != 0u;
}

/* soft_cmp64 returns -1, 0 or 1 for a < b, a == b, a > b, on unsigned values,
 * using only 32-bit comparisons. See the file comment for why. */
static int soft_cmp64(unsigned long long a, unsigned long long b) {
    unsigned int ha = (unsigned int)(a >> 32), hb = (unsigned int)(b >> 32);
    unsigned int la, lb;
    if (ha != hb) return ha < hb ? -1 : 1;
    la = (unsigned int)(a & 0xFFFFFFFFull);
    lb = (unsigned int)(b & 0xFFFFFFFFull);
    if (la == lb) return 0;
    return la < lb ? -1 : 1;
}

static int soft_is_nan(unsigned long long u) {
    /* Not the textbook `(u & EXP_MASK) == EXP_MASK && (u & SIG_MASK) != 0`,
     * and not because it is wrong -- because LLVM recognises exactly that
     * shape as `isnan`, and then folds
     *
     *     isnan(a) || isnan(b)      into      fcmp uno a, b
     *
     * On a target with no FPU an `fcmp uno` IS a call to __unorddf2, so the
     * predicate every soft-float helper here opens with turned into a call to
     * the function being written: __unorddf2 called __unorddf2, forever, until
     * the stack ran out. The link was clean and the fault was a store below
     * the stack from a helper nothing in the source named.
     *
     * The exponents are compared through an XOR and the mantissa is assembled
     * from the two 32-bit halves, so neither test is a 64-bit compare (which
     * would become a library call) and neither recombines into the isnan
     * idiom. `e ^ 0x7FF00000 < 0x100000` is true only for e == 0x7FF00000:
     * every other value of a masked exponent differs from it above bit 20. */
    unsigned int hi = (unsigned int)(u >> 32);
    unsigned int lo = (unsigned int)u;
    unsigned int e = hi & 0x7FF00000u;
    unsigned int m = (hi & 0x000FFFFFu) | lo;
    return (e ^ 0x7FF00000u) < 0x100000u && m != 0u;
}

static int soft_is_inf(unsigned long long u) {
    return soft_exp(u) == 0x7FF && !nonzero64(u & DBL_SIG_MASK);
}

static int soft_is_zero(unsigned long long u) {
    return !nonzero64(u & ~DBL_SIGN_BIT);
}

/* shift_right drops n low bits off a value in the internal format, setting the
 * sticky bit if anything nonzero falls off. n is never more than the width. */
static unsigned long long soft_shr(unsigned long long v, int n) {
    unsigned long long lost;
    if (n <= 0) return v;
    if (n >= 64) return nonzero64(v) ? SF_STICKY : 0ull;
    lost = v & ((1ull << n) - 1ull);
    v >>= n;
    if (nonzero64(lost)) v |= SF_STICKY;
    return v;
}

static unsigned long long soft_shl(unsigned long long v, int n) {
    if (n <= 0) return v;
    if (n >= 64) return 0ull;
    return v << n;
}

/* unpack turns a stored double into the internal format, together with the
 * biased exponent. A zero or subnormal comes back with the sticky bit set and
 * the caller decides what to do with it (only the callers that can produce a
 * subnormal need to care). */
static unsigned long long soft_unpack(unsigned long long u, int *exp_out) {
    int e = soft_exp(u);
    unsigned long long m = u & DBL_SIG_MASK;
    if (e == 0) {
        /* Subnormal (or zero): no implicit leading one, and the value is
         * smaller than 2^-1022, so it is expressed as a normal number with a
         * correspondingly low exponent. */
        *exp_out = 0;
        if (m == 0) return 0ull;
        /* Shift the leading one up to bit 55, counting the places. */
        {
            int sh = 0;
            unsigned long long v = m;
            while (!(v & SF_LEAD)) { v <<= 1; sh++; }
            *exp_out = 1 - sh;
            return v; /* bits below are all real; the shift lost nothing */
        }
    }
    *exp_out = e;
    return (m | 0x0010000000000000ull) << 3; /* fraction up to bit 55, 3 spare */
}

/* soft_finish rounds an internal-format significand to a double. `exp` is the
 * biased exponent the value belongs to; the rounding may carry it into the next
 * binade, and an exponent past the top becomes an infinity. */
static double soft_finish(unsigned long long v, int exp, int neg) {
    soft_double r;
    unsigned long long sign = neg ? DBL_SIGN_BIT : 0ull;
    unsigned long long frac, guard, round, sticky;
    int sh = 0;

    if (v == 0ull) {
        r.u = sign;
        return r.d;
    }
    /* The internal format puts the leading one at bit 55, so an addition can
     * carry it to bit 56 and a multiplication can leave it anywhere in 55..58.
     * Renormalising here -- rather than trusting each caller to have done it --
     * is what makes "round correctly" a single piece of code: without it,
     * 1.0 + 1.0 rounded to 1.0 because the significand was read one binade
     * too low. */
    while (v & SF_ABOVE_LEAD) {
        /* A right shift moves the round bit into the sticky position and
         * pushes the old sticky bit out of the word; the old sticky bit must
         * be folded back in or the sum of pi + e would round up by 1 ulp
         * (the earlier unconditional v |= SF_STICKY turned a shifted-out 0
         * into a sticky 1). */
        unsigned long long lsb = v & 1ull;
        v >>= 1; exp++;
        if (lsb) v |= SF_STICKY;
    }
    while (!(v & SF_LEAD)) { v <<= 1; exp--; }
    /* A left shift pads the low bits with zeros and moves the old guard /
     * round / sticky bits up into the fraction, which is their correct data
     * position; no sticky needs to be manufactured. */

    guard = v & SF_GUARD;
    round = v & SF_ROUND;
    sticky = v & SF_STICKY;
    frac = v >> 3;
    if (guard && (round || sticky || (frac & 1ull))) {
        frac++;
        if (frac == 0x0020000000000000ull) { /* carried out of the fraction */
            frac >>= 1;
            exp++;
        }
    }
    if (exp >= 0x7FF) {
        r.u = sign | DBL_INF_BITS;
        return r.d;
    }
    if (exp < 0) {
        /* Subnormal result: the value is frac * 2^(exp-1023) with the
         * implicit leading one now sitting below 2^-1022, so the fraction is
         * shifted right by -exp places. The shifted-out bits form the
         * guard/round/sticky of a round-to-nearest-even step; carrying past
         * bit 52 re-enters the normal range at the smallest exponent.
         * Earlier versions flushed to zero here, which failed 1/1e308 (that
         * is 9.999...e-309, a subnormal, not 0). */
        int sh = 1 - exp;
        unsigned long long lost, guard, rest, sub;
        if (sh >= 64) { r.u = sign; return r.d; }
        lost = frac & ((1ull << sh) - 1ull);
        guard = (lost >> (sh - 1)) & 1ull;
        rest = lost & ((1ull << (sh - 1)) - 1ull);
        sub = frac >> sh;
        if (guard && (rest || (sub & 1ull))) sub++;
        if (sub > 0x001FFFFFFFFFFFFFull) {
            /* Rounded up into the smallest normal: exponent 1, fraction 0. */
            r.u = sign | (1ull << 52);
            return r.d;
        }
        r.u = sign | (sub & DBL_SIG_MASK);
        return r.d;
    }
    r.u = sign | ((unsigned long long)(exp & 0x7FF) << 52) | (frac & DBL_SIG_MASK);
    return r.d;
}

/* --- addition and subtraction -------------------------------------------- */

static double soft_add_sub(double a, double b, int subtract) {
    soft_double x, y, r;
    unsigned long long ua, ub, va, vb, sum;
    int ea, eb, exp, neg_a, neg_b, neg;

    x.d = a; y.d = b;
    ua = x.u; ub = y.u;
    if (soft_is_nan(ua) || soft_is_nan(ub)) {
        r.u = DBL_NAN_BITS;
        return r.d;
    }
    /* Subtraction is addition of the negated operand. Doing the flip on the
     * bit pattern -- rather than tracking a separate "subtract" flag through
     * every case below -- is what keeps the sign handling in one place. */
    if (subtract) ub ^= DBL_SIGN_BIT;

    neg_a = (int)(ua >> 63);
    neg_b = (int)(ub >> 63);

    /* Infinities, and the one case where they do not combine. */
    if (soft_is_inf(ua) || soft_is_inf(ub)) {
        if (soft_is_inf(ua) && soft_is_inf(ub)) {
            if (neg_a != neg_b) {
                r.u = DBL_NAN_BITS;  /* inf - inf */
                return r.d;
            }
            r.u = ((unsigned long long)neg_a << 63) | DBL_INF_BITS;
            return r.d;
        }
        neg = soft_is_inf(ua) ? neg_a : neg_b;
        r.u = ((unsigned long long)neg << 63) | DBL_INF_BITS;
        return r.d;
    }

    /* Zeros. The sign of a zero result is where IEEE is most surprising, so
     * the cases are spelled out: x + 0 is x, 0 - x is -x, and a sum of two
     * zeros is +0 unless both were the same sign and this is a subtraction
     * (-0 - -0 is -0, while -0 + -0 is +0). */
    if (soft_is_zero(ua) && soft_is_zero(ub)) {
        r.u = (subtract && neg_a == neg_b) ? DBL_SIGN_BIT : 0ull;
        return r.d;
    }
    if (soft_is_zero(ua)) { r.u = ub; return r.d; }
    if (soft_is_zero(ub)) { r.u = ua; return r.d; }

    va = soft_unpack(ua, &ea);
    vb = soft_unpack(ub, &eb);

    /* Align on the larger exponent. */
    if (ea > eb) {
        exp = ea;
        vb = soft_shr(vb, ea - eb);
    } else if (eb > ea) {
        exp = eb;
        va = soft_shr(va, eb - ea);
    } else {
        exp = ea;
    }

    /* With both values at the same scale the sign alone decides the operation.
     * Testing the exponents as well -- which an earlier version of this did --
     * is what made 1.5 - 0.5 come out as 1.5: the exponents differed, so the
     * code took the addition path and added a sticky-shifted 0.5 to 1.5. */
    if (neg_a != neg_b) {
        int c = soft_cmp64(va, vb);
        if (c == 0) {
            /* x + (-x) is +0 in round-to-nearest, whatever the signs were. */
            r.u = 0ull;
            return r.d;
        }
        if (c > 0) { sum = va - vb; neg = neg_a; }
        else       { sum = vb - va; neg = neg_b; }
    } else {
        sum = va + vb;
        neg = neg_a;
    }

    /* The sum can need 54 bits; the internal format has room for exactly that
     * (bit 56), and soft_finish carries out of the fraction into the exponent,
     * so nothing has to be shifted here. */
    return soft_finish(sum, exp, neg);
}

/* The two names LLVM emits for the two operations. They are one function
 * because subtraction is addition of a negated operand, and the negation is
 * done inside; which name appears is decided by the source expression, not by
 * anything here. Both must exist: a build that defines only one leaves the
 * other as an undefined symbol the linker reports by name. */
double __adddf3(double a, double b) { return soft_add_sub(a, b, 0); }
double __subdf3(double a, double b) { return soft_add_sub(a, b, 1); }

/* --- multiplication -------------------------------------------------------- */

/* __muldf3: 53x53 is 106 bits, so the product is formed at full width and the
 * top 56 bits are kept -- which is exactly the internal format, with the
 * remaining 50 bits feeding the guard, round and sticky positions. */
double __muldf3(double a, double b) {
    soft_double x, y, r;
    unsigned long long ua, ub, ma, mb, plo, phi, frac;
    int ea, eb, exp, neg_a, neg_b, neg, sh;

    x.d = a; y.d = b;
    ua = x.u; ub = y.u;
    if (soft_is_nan(ua) || soft_is_nan(ub)) { r.u = DBL_NAN_BITS; return r.d; }
    neg_a = (int)(ua >> 63);
    neg_b = (int)(ub >> 63);
    neg = neg_a ^ neg_b;

    if (soft_is_inf(ua) || soft_is_inf(ub)) {
        /* inf * 0 is the indeterminate form. */
        if ((soft_is_inf(ua) && soft_is_zero(ub)) || (soft_is_inf(ub) && soft_is_zero(ua))) {
            r.u = DBL_NAN_BITS;
            return r.d;
        }
        r.u = ((unsigned long long)neg << 63) | DBL_INF_BITS;
        return r.d;
    }
    if (soft_is_zero(ua) || soft_is_zero(ub)) {
        r.u = (unsigned long long)neg << 63;
        return r.d;
    }

    ma = soft_unpack(ua, &ea);
    mb = soft_unpack(ub, &eb);
    /* Both operands are 1.f in the internal format, so the product is 1.f in
     * that same format and the exponents simply add. */
    exp = ea + eb - 1023;

    /* 56x56 -> 112 bits, as an explicit (phi:plo) pair.
     *
     * 32-bit halves rather than the 28-bit ones an earlier version used: each
     * partial product is then at most 2^64 and the assembly is four adds and
     * two carries, with no chain of accumulators whose ordering decides the
     * answer. That chain is what made the earlier version produce a product
     * right in magnitude but wrong in its low bits -- 1e278 * 1e278 came out
     * as 2.8e278 rather than overflowing to infinity. */
    {
        unsigned long long a0 = ma & 0xFFFFFFFFull, a1 = ma >> 32;
        unsigned long long b0 = mb & 0xFFFFFFFFull, b1 = mb >> 32;
        unsigned long long p00 = a0 * b0, p01 = a0 * b1;
        unsigned long long p10 = a1 * b0, p11 = a1 * b1;
        unsigned long long mid = (p01 & 0xFFFFFFFFull) + (p10 & 0xFFFFFFFFull);
        unsigned long long mid_hi, mid_lo, lo_hi;
        mid_lo = mid & 0xFFFFFFFFull;
        mid_hi = (p01 >> 32) + (p10 >> 32) + (mid >> 32);
        /* The low 64 bits are p00's two halves plus the low half of the
         * middle term. p00's own high half is *added* to mid_lo (it is the
         * carry into the same word position), not dropped: (p00>>32) and
         * mid_lo both occupy bits 63..32 of the result. The earlier code
         * wrote mid_lo alone into that slot and tested the low word for a
         * carry that cannot occur, so the low 64 bits of 0.1*0.2 read
         * 0x3851EB808F5C2900 instead of 0xDC28F5C28F5C2900 -- 0xA3D70A42
         * vanished and every product whose low-word halves summed carried
         * lost 8-10 bits of fraction (0.1*0.2 came out 0.019999999999999931
         * instead of 0.020000000000000004). */
        lo_hi = (p00 >> 32) + mid_lo;
        plo = (p00 & 0xFFFFFFFFull) | (lo_hi << 32);
        phi = p11 + mid_hi + (lo_hi >> 32);
    }

    /* The product's leading one is at bit 110 or 111 of the 112-bit result
     * (bits 55 of the two 56-bit inputs, plus one extra binade when the
     * 1.fa * 1.fb product reaches [2,4)). Keeping the top 57 bits -- (phi << 9)
     * | (plo >> 55) -- leaves the leading one at bit 55 or 56, which is
     * exactly the range soft_finish normalises from (bit 56 is its
     * SF_ABOVE_LEAD carry case, handled by shifting down and raising exp).
     * Taking only the top 56 bits -- (phi << 8) | (plo >> 56) -- stranded the
     * leading one at bit 54 for products below 2 (1.5 * 2.0 came out 1.5),
     * and the older (phi << 24) | (plo >> 40) overflowed the word entirely
     * (1.5 * 2.0 came out 3e-36, pi * e as 1302). */
    frac = (phi << 9) | (plo >> 55);
    /* guard = P bit 57, round = P bit 56, sticky starts at P bit 55 and
     * continues through plo's low 55 bits. */
    if ((plo & 0x007FFFFFFFFFFFFFULL) != 0ull) frac |= SF_STICKY;

    sh = 0;
    while (!(frac & SF_LEAD) && sh < 64) { frac <<= 1; exp--; sh++; }
    if (sh > 0) frac |= SF_STICKY;
    return soft_finish(frac, exp, neg);
}

/* --- division ------------------------------------------------------------- */

/* __divdf3: the quotient is built one bit at a time by restoring division, with
 * the divisor held fixed and one quotient bit produced per step. That keeps
 * everything in 64 bits, which is the point -- a 53-by-53 division has no
 * shortcut on a machine with no divide instruction. */
double __divdf3(double a, double b) {
    soft_double x, y, r;
    unsigned long long ua, ub, va, vb, q = 0, rem;
    int ea, eb, exp, neg_a, neg_b, neg, i, sticky_acc = 0;

    x.d = a; y.d = b;
    ua = x.u; ub = y.u;
    if (soft_is_nan(ua) || soft_is_nan(ub)) { r.u = DBL_NAN_BITS; return r.d; }
    neg_a = (int)(ua >> 63);
    neg_b = (int)(ub >> 63);
    neg = neg_a ^ neg_b;

    if (soft_is_inf(ua)) {
        if (soft_is_inf(ub)) { r.u = DBL_NAN_BITS; return r.d; } /* inf/inf */
        r.u = ((unsigned long long)neg << 63) | DBL_INF_BITS;
        return r.d;
    }
    if (soft_is_inf(ub)) { r.u = (unsigned long long)neg << 63; return r.d; } /* x/inf */
    /* The divisor is checked before the dividend: 0/0 is indeterminate (NaN)
     * while x/0 (x != 0) is +-inf by the sign of x. Checking the dividend
     * first made 0/0 fall into the x/0 path and come back 0. */
    if (soft_is_zero(ub)) {
        if (soft_is_zero(ua)) { r.u = DBL_NAN_BITS; return r.d; }
        r.u = ((unsigned long long)neg << 63) | DBL_INF_BITS;
        return r.d;
    }
    if (soft_is_zero(ua)) { r.u = (unsigned long long)neg << 63; return r.d; }

    va = soft_unpack(ua, &ea);
    vb = soft_unpack(ub, &eb);
    /* Both are in the internal format: leading one at bit 55, 3 spare bits.
     * Shifting both up by 3 puts the leading one at bit 58, so the quotient
     * loop below has three bits of headroom before it runs off the top. */
    va <<= 3;
    vb <<= 3;
    /* ea and eb are biased exponents, so the quotient's biased exponent is
     * ea - eb + 1023 -- not ea - eb, which is a plain difference of the
     * stored fields and lands every division a factor of 2^1023 low (1/3 came
     * out as a subnormal 2.4e-307). */
    exp = ea - eb + 1023;

    if (soft_cmp64(va, vb) < 0) {
        /* Quotient below 1: scale the dividend up and lower the exponent so
         * the loop still produces a normalised result. */
        va <<= 1;
        exp--;
    }

    rem = va;
    for (i = 0; i < 62; i++) {
        q <<= 1;
        if (rem >= vb) {
            rem -= vb;
            q |= 1ull;
        }
        /* The bit about to leave the top of rem is what the rounding needs. */
        if (i >= 50 && (rem & 0x8000000000000000ull)) sticky_acc = 1;
        rem <<= 1;
    }
    /* q now has its leading one at bit 61, 62 or 63 (the quotient of two
     * values whose leading ones sit at bit 58, taken over 62 loop steps).
     * Move it to bit 55 -- the internal format -- without touching exp: the
     * exponent of the quotient is carried by exp alone, and the leading-one
     * position is a representation detail soft_finish reads the fraction from.
     * The shift is *right* -- a quotient above the format has to be scaled
     * down, not up. The earlier loop shifted left and counted `exp--` per
     * step, which pushed a bit-62 quotient off the top of the word and
     * subtracted 6 from the exponent in the same breath; 1/3 came out as
     * 21.33 (0.333 * 2^6). */
    {
        int sh = 0;
        while ((q & 0xFF00000000000000ULL) && sh < 64) { q >>= 1; sh++; }
        while (!(q & SF_LEAD) && sh < 64) { q <<= 1; sh++; }
    }
    /* The bits the loop shifted past the top of rem are the guard/round/sticky
     * input; `sticky_acc` already holds "something was lost". */
    if (sticky_acc) q |= SF_STICKY;
    return soft_finish(q, exp, neg);
}

/* --- comparisons ---------------------------------------------------------- */

/* Each returns 0 or 1, which is what the `fcmp`/`cset` pair LLVM lowers a C
 * comparison into on a soft-float target. The shared rules: a NaN compares
 * false against everything including itself, and +0 == -0 even though their
 * bit patterns differ. */

/* The comparison family is NOT a set of boolean predicates, and writing it as
 * one is the single most expensive mistake available here.
 *
 * libgcc -- and therefore every back end that names these when it lowers an
 * `fcmp` -- defines them by the SIGN or the ZERO-NESS of what they return:
 *
 *   __eqdf2(a,b)  0 if a == b,          nonzero otherwise
 *   __nedf2(a,b)  nonzero if a != b,    0 otherwise
 *   __ltdf2(a,b)  <  0 if a < b,        >= 0 otherwise
 *   __ledf2(a,b)  <= 0 if a <= b,       >  0 otherwise
 *   __gtdf2(a,b)  >  0 if a > b,        <= 0 otherwise
 *   __gedf2(a,b)  >= 0 if a >= b,       <  0 otherwise
 *
 * The generated code tests exactly that, so it is visible in the assembly: a
 * `>=` comes out as `call __gedf2; bgez`. A predicate that returns 1 when the
 * relation holds and 0 when it does not satisfies only the tests written as
 * `bnez`. Under `bgez` its 0 becomes "true", and `v >= 1e9` was true for
 * v = 1.5 -- which sent printf's integer formatter into `v / 1e9` forever and
 * took the whole stack out.
 *
 * Unordered (either operand NaN): every ordered comparison is false, so each
 * function returns the value that makes ITS test fail -- 0 for lt/gt (so
 * `< 0` and `> 0` are both false), +1 for le (so `<= 0` is false) and -1 for
 * ge (so `>= 0` is false). That is why le and ge cannot both be derived from
 * the three-way result alone; they have to name the unordered case themselves.
 *
 * __nedf2 is the one exception, and it is worth naming because it is the
 * opposite of what libgcc documents. libgcc says it returns 0 when the values
 * DIFFER; the generated code tests it with `bnez` / `snez`, which only reads
 * correctly if it returns nonzero when they differ -- a boolean predicate, not
 * a signed one. Following the published convention there inverted `!=` for
 * every soft-float program while leaving `==`, `<`, `<=`, `>` and `>=` all
 * correct, which is a confusing combination to debug from the outside.
 */
static int soft_cmp3(double a, double b) {
    soft_double x, y;
    int sa, sb;
    unsigned int ea, eb, ma, mb;
    x.d = a; y.d = b;
    if (soft_is_nan(x.u) || soft_is_nan(y.u)) return 0;  /* unordered */
    sa = (int)(x.u >> 63);
    sb = (int)(y.u >> 63);
    if (sa != sb) return sa ? -1 : 1;   /* negative beats non-negative */
    /* Same sign: compare the magnitudes, which for equal signs orders the same
     * way as the values -- but reversed, because both are negative. Split into
     * 32-bit halves so no 64-bit compare (and so no call back into here). */
    ea = (unsigned int)((x.u & DBL_EXP_MASK) >> 52);
    eb = (unsigned int)((y.u & DBL_EXP_MASK) >> 52);
    if (ea != eb) return ((ea > eb) == (sa == 0)) ? 1 : -1;
    ma = (unsigned int)((x.u & DBL_SIG_MASK) >> 32);
    mb = (unsigned int)((y.u & DBL_SIG_MASK) >> 32);
    if (ma != mb) return ((ma > mb) == (sa == 0)) ? 1 : -1;
    ma = (unsigned int)(x.u & 0xFFFFFFFFull);
    mb = (unsigned int)(y.u & 0xFFFFFFFFull);
    if (ma != mb) return ((ma > mb) == (sa == 0)) ? 1 : -1;
    return 0;  /* equal, and +0 == -0 falls out of the magnitudes being 0 */
}

static int soft_either_nan(double a, double b) {
    soft_double x, y;
    x.d = a; y.d = b;
    return soft_is_nan(x.u) || soft_is_nan(y.u);
}

int __eqdf2(double a, double b) {
    if (soft_either_nan(a, b)) return 1;          /* not equal */
    return soft_cmp3(a, b) == 0 ? 0 : 1;
}

int __nedf2(double a, double b) {
    if (soft_either_nan(a, b)) return 0;          /* ordered != is false */
    return soft_cmp3(a, b) == 0 ? 0 : 1;
}

int __ltdf2(double a, double b) { return soft_cmp3(a, b); }

int __ledf2(double a, double b) {
    if (soft_either_nan(a, b)) return 1;          /* > 0: a <= b does not hold */
    return soft_cmp3(a, b) <= 0 ? -1 : 1;
}

int __gedf2(double a, double b) {
    if (soft_either_nan(a, b)) return -1;         /* < 0: a >= b does not hold */
    return soft_cmp3(a, b) >= 0 ? 1 : -1;
}

int __gtdf2(double a, double b) { return soft_cmp3(a, b); }

int __unordereddf2(double a, double b) {
    soft_double x, y;
    x.d = a; y.d = b;
    return soft_is_nan(x.u) || soft_is_nan(y.u);
}

/* libgcc's spelling of the same predicate. Both are defined because which one
 * a target's generated code names depends on the instruction it lowered to. */
int __unorddf2(double a, double b) { return __unordereddf2(a, b); }

/* --- double <-> 64-bit integer conversions -------------------------------- */

/* A double outside the 64-bit range has no integer value, so these saturate --
 * the only answer a C conversion is entitled to produce, and what the hardware
 * conversion instructions give. */

long long __fixdfdi(double a) {
    soft_double x;
    unsigned long long v;
    int exp, sh, neg = 0;
    x.d = a;
    if (soft_is_nan(x.u)) return 0;
    if (soft_is_inf(x.u)) {
        if (x.u >> 63) return (-9223372036854775807LL - 1);
        return 9223372036854775807LL;
    }
    if (x.u >> 63) { neg = 1; x.u ^= DBL_SIGN_BIT; }
    exp = soft_exp(x.u);
    v = (x.u & DBL_SIG_MASK) | 0x0010000000000000ull; /* 53-bit significand */
    if (v == 0x0010000000000000ull && exp == 0) return 0;  /* zero or subnormal */
    if (exp == 0) {
        /* Subnormal: below 1, so it truncates to zero. */
        return 0;
    }
    exp -= 1023;
    if (exp > 63) {
        if (neg) return (-9223372036854775807LL - 1);
        return 9223372036854775807LL;
    }
    /* The significand keeps its leading one at bit 52, which is exactly the
     * value's bit (52 + exp); the integer part is therefore the significand
     * shifted right by (52 - exp) when exp < 52, unchanged when exp == 52,
     * and shifted left by (exp - 52) when exp > 52. The earlier code shifted
     * *left* for every positive exp -- v <<= exp -- which overflowed the word
     * for any value above 1.0: 3.7 became 0xD999999999999A00 (the fraction
     * bits reinterpreted as an integer) instead of 3. */
    if (exp < 0) {
        /* Below 1: the fraction contributes nothing to the integer. */
        return 0;
    }
    if (exp < 52) {
        v >>= (52 - exp);
    } else if (exp > 52) {
        if (exp == 63) {
            /* Shifting a non-1.0 significand by 11 sets bit 63, which a
             * plain v <<= 11 would report as a negative number -- 1e19 came
             * back as -8446744073709551616. 2^63 exactly (v == 0x8000...)
             * is the one in-range overflow; anything above saturates. */
            v <<= 11;
            if (v > 0x8000000000000000ull) {
                if (neg) return (-9223372036854775807LL - 1);
                return 9223372036854775807LL;
            }
        } else {
            v <<= (exp - 52);
        }
    }
    if (neg) return -(long long)v;
    return (long long)v;
}

long long __fixdfsi(double a) { return __fixdfdi(a); }

unsigned long long __fixunsdfdi(double a) {
    soft_double x;
    unsigned long long v;
    int exp, sh;
    x.d = a;
    if (soft_is_nan(x.u)) return 0;
    if (soft_is_inf(x.u)) {
        if (x.u >> 63) return 0;  /* -inf converts to 0 when unsigned */
        return ~0ull;
    }
    if (x.u >> 63) return 0;      /* every negative value converts to 0 */
    exp = soft_exp(x.u);
    v = (x.u & DBL_SIG_MASK) | 0x0010000000000000ull;
    if (exp == 0) return 0;
    exp -= 1023;
    if (exp < 0) return 0;        /* below 1 truncates to 0 */
    if (exp > 63) return ~0ull;
    /* Same whole-part extraction as __fixdfdi: the significand's leading one
     * sits at bit 52, which is the value's bit (52 + exp). */
    if (exp < 52) {
        v >>= (52 - exp);
    } else if (exp > 52) {
        /* No saturation check for exp == 63 here: a significand 1.f in [1,2)
         * times 2^63 is in [2^63, 2^64), which is the full unsigned range --
         * the bit-63 shift-out is the value's own top bit, not an overflow.
         * 1e19 (native: 10000000000000000000) was wrongly saturated to ~0 by
         * the check the signed conversion needed. exp > 63 is already handled
         * above. */
        v <<= (exp - 52);
    }
    return v;
}

/* float <- integer: normalise the magnitude into the internal format and let
 * soft_finish round it. */
static double soft_from_uint(unsigned long long m, int neg) {
    int exp = 0, sh = 0, top;
    unsigned long long v;
    if (!nonzero64(m)) return soft_finish(0, 0, neg);
    /* The exponent comes from where the leading one *already* sits in m: a
     * 64-bit source's top bit is 2^63, so a value that needs no shifting has an
     * exponent of 63, not 0. Starting the count from 0 (as an earlier version
     * did) made every conversion off by a factor of 2^63 -- which is why
     * __floatsidf(100) came out as 5.7e-12. */
    top = 63;
    while (top > 0 && !((m >> top) & 1ull)) top--;
    exp = top;
    /* Move the leading one to bit 55 and open the three guard/round/sticky bits
     * below it, so the result is already in the internal format. A source whose
     * leading one is above bit 55 (any value >= 2^56) has to shift *right*;
     * writing that as m << (55 - top) was a negative shift, which is undefined
     * behaviour -- gcc's x86 shifter masks the count mod 64 and m << 56 zeroed
     * the word, so __floatunsdfdi(1e19) and __floatdidf(LLONG_MAX) came out 0. */
    if (top > 55) {
        v = m >> (top - 55);
        sh = top - 55;
    } else if (top < 55) {
        v = m << (55 - top);
        sh = 55 - top;
    } else {
        v = m;
        sh = 0;
    }
    /* Anything shifted off the bottom of the source matters only as sticky. */
    if (sh > 0 && (m & ((1ull << sh) - 1ull))) v |= SF_STICKY;
    return soft_finish(v, exp + 1023, neg);
}

double __floatsidf(unsigned int a) { return soft_from_uint((unsigned long long)a, 0); }

double __floatdidf(long long a) {
    /* 0 - (unsigned)a is the magnitude even for LLONG_MIN, where -a would
     * overflow. */
    if (a < 0) return soft_from_uint(0ull - (unsigned long long)a, 1);
    return soft_from_uint((unsigned long long)a, 0);
}

double __floatunsidf(unsigned int a) { return soft_from_uint((unsigned long long)a, 0); }

double __floatunsdfdi(unsigned long long a) { return soft_from_uint(a, 0); }

/* The 64-bit integer divide/multiply helpers (__udivdi3, __divdi3, __moddi3,
 * __umoddi3, __muldi3) live in intops.c, not here. */
