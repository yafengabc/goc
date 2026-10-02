/* =============================================================================
 * bitint.c -- runtime helpers for C23 _BitInt(N) values.
 *
 * A _BitInt(N) value is stored as a little-endian array of 64-bit words:
 * word 0 holds bits 0..63, word 1 bits 64..127, and so on. The number of
 * words is ceil(N/64) and every storage slot is 8-byte aligned, so each word
 * can be loaded/stored directly.
 *
 * All helpers take the word count as their trailing `n` argument; the
 * compiler emits the calls and fixes n per operation. Signedness only
 * matters for division/modulo, right shift, comparison and decimal
 * conversion -- two's-complement addition/subtraction/multiplication and
 * the bitwise operations are width-agnostic.
 *
 * Implementation limit shared with the compiler front end: N <= 524288, i.e.
 * at most 8192 words (BI_WMAX). That is 64 KiB per value -- enough for a
 * 100,000-decimal-digit pi (~332,200 bits).
 *
 * Performance notes (this file is the hot path of every _BitInt benchmark):
 *   - multiplication is schoolbook over 32-bit limbs with a 64-bit
 *     accumulator (this C subset has no 128-bit type), skipping zero limbs
 *     and with the inner loop pre-bounded instead of break-tested;
 *   - division is Knuth algorithm D over normalised 32-bit limbs: O(m*n)
 *     limb operations instead of the O(bits) shift-subtract long division.
 *     That is the difference between "usable" and "unusable" at 10^5 digits;
 *   - decimal conversion divides by 10^9 per pass (nine digits at a time)
 *     over the value viewed as 32-bit limbs, so printing 100,000 digits
 *     costs ~11k passes rather than 100k full-width divisions.
 *
 * Scratch memory is ALLOCATED ON DEMAND and cached (see bi_scratch below):
 * a program that only uses 128-bit bitints never pays for the megabytes a
 * million-digit value needs, and there is no fixed width ceiling in the
 * runtime -- only the value's own storage (a _BitInt(N) is ceil(N/64)*8
 * bytes, fixed at compile time by C23) and available memory limit it.
 * ========================================================================== */

/* No fixed word ceiling any more -- only the declared width of the values
 * involved. BI_WMAX is kept as a sanity bound for the front end's own
 * (separate) limit, which is 4,194,304 bits = 65536 words per value. */
#define BI_WMAX 65536

/* ---- scratch pool (malloc'd on demand, cached across calls) ----------------
 *
 * Slot ownership (no nested helper may reuse another's slot):
 *   0,1,2 : multiplication limbs -- t, al, bl  / division -- u, v, ql
 *   3,4,5,6 : divmod_s magnitudes -- aa, bb, tq, tr  (then it calls udivmod,
 *            which uses 0,1,2)
 *   7     : a single word scratch (div_u / mod_u remainder, div_s / mod_s
 *            remainder) and bi_str's decimal text
 * A failed allocation makes the helper write zeros and return instead of
 * crashing: out of memory is not a reason to fault a correctly written
 * program. */
#define BI_SLOTS 8
static char *bi_scr[BI_SLOTS];
static long bi_scrcap[BI_SLOTS];

static char *bi_scratch(long slot, long nbytes) {
    if (nbytes < 8) nbytes = 8;
    if (nbytes > bi_scrcap[slot]) {
        if (bi_scr[slot] != 0) free((void *)bi_scr[slot]);
        bi_scr[slot] = (char *)malloc(nbytes);
        bi_scrcap[slot] = nbytes;
        if (bi_scr[slot] == 0) {
            bi_scrcap[slot] = 0;
            return 0;
        }
    }
    return bi_scr[slot];
}

/* ---- basics ---------------------------------------------------------------- */

void __goclib_bi_zero(unsigned long long *r, long n) {
    for (long i = 0; i < n; i++) r[i] = 0;
}

void __goclib_bi_copy(unsigned long long *r, const unsigned long long *a, long n) {
    for (long i = 0; i < n; i++) r[i] = a[i];
}

/* Convert a 64-bit integer: signed targets sign-extend, unsigned targets
 * take the bit pattern (word0 = pattern, the rest zero). */
void __goclib_bi_from_i64(unsigned long long *r, long long v, long n, long dstSigned) {
    r[0] = (unsigned long long)v;
    if (dstSigned && v < 0) {
        for (long i = 1; i < n; i++) r[i] = ~0ULL;
    } else {
        for (long i = 1; i < n; i++) r[i] = 0;
    }
}

/* Truncating read of the low 64 bits: the two's-complement low word IS the
 * int64 value pattern regardless of the bitint's signedness. */
long long __goclib_bi_to_i64(const unsigned long long *a, long n) {
    return (long long)a[0];
}

long __goclib_bi_is_zero(const unsigned long long *a, long n) {
    for (long i = 0; i < n; i++)
        if (a[i] != 0) return 0;
    return 1;
}

/* ---- add / sub / neg / bitwise --------------------------------------------- */

void __goclib_bi_add(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n) {
    unsigned long long carry = 0;
    for (long i = 0; i < n; i++) {
        unsigned long long s = a[i] + b[i];
        unsigned long long c1 = s < a[i];
        s += carry;
        c1 += s < carry;
        r[i] = s;
        carry = c1;
    }
}

void __goclib_bi_sub(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n) {
    unsigned long long borrow = 0;
    for (long i = 0; i < n; i++) {
        unsigned long long d = a[i] - b[i];
        unsigned long long b1 = a[i] < b[i];
        unsigned long long d2 = d - borrow;
        b1 += d < borrow;
        r[i] = d2;
        borrow = b1;
    }
}

void __goclib_bi_neg(unsigned long long *r, const unsigned long long *a, long n) {
    unsigned long long carry = 1;
    for (long i = 0; i < n; i++) {
        unsigned long long s = (unsigned long long)~a[i] + carry;
        carry = s < carry;
        r[i] = s;
    }
}

void __goclib_bi_not(unsigned long long *r, const unsigned long long *a, long n) {
    for (long i = 0; i < n; i++) r[i] = ~a[i];
}

void __goclib_bi_and(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n) {
    for (long i = 0; i < n; i++) r[i] = a[i] & b[i];
}

void __goclib_bi_or(unsigned long long *r, const unsigned long long *a,
                    const unsigned long long *b, long n) {
    for (long i = 0; i < n; i++) r[i] = a[i] | b[i];
}

void __goclib_bi_xor(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n) {
    for (long i = 0; i < n; i++) r[i] = a[i] ^ b[i];
}

/* ---- shifts ------------------------------------------------------------------ */

void __goclib_bi_shl(unsigned long long *r, const unsigned long long *a,
                     unsigned long long sh, long n) {
    unsigned long long wo = sh / 64;
    unsigned long long bi = sh % 64;
    if (wo >= (unsigned long long)n) {
        __goclib_bi_zero(r, n);
        return;
    }
    for (long i = n - 1; i >= 0; i--) {
        unsigned long long v = 0;
        long src = i - (long)wo;
        if (src >= 0) {
            v = a[src] << bi;
            if (bi != 0 && src - 1 >= 0) {
                v |= a[src - 1] >> (64 - bi);
            }
        }
        r[i] = v;
    }
}

/* arithmetic right shift fills with the sign bit, logical with zeros. */
static void bi_shr_common(unsigned long long *r, const unsigned long long *a,
                          unsigned long long sh, long n, long arithmetic) {
    unsigned long long wo = sh / 64;
    unsigned long long bi = sh % 64;
    unsigned long long fill = 0;
    if (arithmetic && (a[n - 1] >> 63) & 1) fill = ~0ULL;
    if (wo >= (unsigned long long)n) {
        for (long i = 0; i < n; i++) r[i] = fill;
        return;
    }
    for (long i = 0; i < n; i++) {
        unsigned long long v = fill;
        long src = i + (long)wo;
        if (src < n) {
            v = a[src] >> bi;
            if (bi != 0) {
                unsigned long long hi = fill;
                if (src + 1 < n) hi = a[src + 1];
                v |= hi << (64 - bi);
            }
        }
        r[i] = v;
    }
}

void __goclib_bi_shr_u(unsigned long long *r, const unsigned long long *a,
                       unsigned long long sh, long n) {
    bi_shr_common(r, a, sh, n, 0);
}

void __goclib_bi_shr_s(unsigned long long *r, const unsigned long long *a,
                       unsigned long long sh, long n) {
    bi_shr_common(r, a, sh, n, 1);
}

long __goclib_bi_cmp(const unsigned long long *a, const unsigned long long *b,
                     long n, long isSigned) {
    if (isSigned) {
        int aneg = (a[n - 1] >> 63) & 1;
        int bneg = (b[n - 1] >> 63) & 1;
        if (aneg != bneg) return bneg ? 1 : -1;
    }
    for (long i = n - 1; i >= 0; i--) {
        if (a[i] < b[i]) return -1;
        if (a[i] > b[i]) return 1;
    }
    return 0;
}

/* ---- multiplication ---------------------------------------------------------
 *
 * Two engines, selected by limb count:
 *   - schoolbook over 32-bit limbs with a 64-bit accumulator, used below
 *     KARA_TH limbs (and as the out-of-memory fallback);
 *   - Karatsuba above it, which turns the O(m^2) inner loop into
 *     O(m^1.585): three half-size products instead of four. On a
 *     1,000,000-digit value (103,812 limbs) that is the difference between
 *     "several minutes" and "tens of seconds".
 *
 * Operands are padded up to a power of two limbs so every recursive split is
 * balanced (a0/a1 and b0/b1 always the same size). Each recursion level
 * mallocs its own temporaries and frees them before returning, so the peak is
 * ~14*m limbs along one path rather than the whole tree.
 * -------------------------------------------------------------------------- */

#define KARA_TH 48

/* r[0..2m-1] = a[0..m-1] * b[0..m-1] (full product, any m >= 1).
 *
 * Karatsuba splits at lo = ceil(m/2): the low half has `lo` limbs and the
 * high half `hi = m - lo` (equal, or one limb shorter). Only two of the
 * three recursive products are strictly half-size -- the third is
 * (a0+a1)*(b0+b1) on lo+1 limbs -- so every recursive width is < m and the
 * recursion terminates for any m, powers of two not required. */
static void bi_mul_full(unsigned int *r, const unsigned int *a,
                        const unsigned int *b, long m) {
    if (m <= KARA_TH) {
        for (long i = 0; i < 2 * m; i++) r[i] = 0;
        for (long i = 0; i < m; i++) {
            unsigned int ai = a[i];
            if (ai == 0) continue;         /* skip zero limbs */
            unsigned long long carry = 0;
            for (long j = 0; j < m; j++) {
                unsigned long long cur = (unsigned long long)r[i + j] +
                                         (unsigned long long)ai * b[j] + carry;
                r[i + j] = (unsigned int)cur;
                carry = cur >> 32;
            }
            /* Row carry: this is the FULL product, so the carry out of the
             * row lands in limb i+m (still inside the 2m-limb result). The
             * truncating engine in __goclib_bi_mul drops it on purpose --
             * copying that shortcut here silently corrupted every product. */
            r[i + m] = (unsigned int)carry;
        }
        return;
    }
    long lo = (m + 1) / 2;                 /* low half (>= high half) */
    long hi = m - lo;                      /* high half */
    unsigned int *z0 = (unsigned int *)malloc((2 * lo) * 4);
    unsigned int *z2 = (unsigned int *)malloc((2 * hi) * 4);
    unsigned int *sa = (unsigned int *)malloc((lo + 1) * 4);
    unsigned int *sb = (unsigned int *)malloc((lo + 1) * 4);
    unsigned int *z1 = (unsigned int *)malloc((2 * lo + 2) * 4);
    if (z0 == 0 || z2 == 0 || sa == 0 || sb == 0 || z1 == 0) {
        /* Out of memory: the recursive path is an optimisation, not the
         * definition -- fall back to the (correct) schoolbook product. */
        if (z0) free((void *)z0);
        if (z2) free((void *)z2);
        if (sa) free((void *)sa);
        if (sb) free((void *)sb);
        if (z1) free((void *)z1);
        for (long i = 0; i < 2 * m; i++) r[i] = 0;
        for (long i = 0; i < m; i++) {
            unsigned long long carry = 0;
            for (long j = 0; j < m; j++) {
                unsigned long long cur = (unsigned long long)r[i + j] +
                                         (unsigned long long)a[i] * b[j] + carry;
                r[i + j] = (unsigned int)cur;
                carry = cur >> 32;
            }
            r[i + m] = (unsigned int)carry;
        }
        return;
    }

    bi_mul_full(z0, a, b, lo);             /* z0 = a0*b0       */
    bi_mul_full(z2, a + lo, b + lo, hi);   /* z2 = a1*b1       */

    /* sa = a0 + a1, sb = b0 + b1 (the short half is zero-padded) */
    unsigned long long cy = 0;
    for (long i = 0; i < lo; i++) {
        unsigned long long v = (unsigned long long)a[i];
        if (i < hi) v = v + (unsigned long long)a[lo + i];
        v = v + cy;
        sa[i] = (unsigned int)v;
        cy = v >> 32;
    }
    sa[lo] = (unsigned int)cy;
    cy = 0;
    for (long i = 0; i < lo; i++) {
        unsigned long long v = (unsigned long long)b[i];
        if (i < hi) v = v + (unsigned long long)b[lo + i];
        v = v + cy;
        sb[i] = (unsigned int)v;
        cy = v >> 32;
    }
    sb[lo] = (unsigned int)cy;

    bi_mul_full(z1, sa, sb, lo + 1);       /* z1 = (a0+a1)*(b0+b1) */

    /* z1 = z1 - z0 - z2 (unsigned borrow over the whole z1 width) */
    unsigned long long borrow = 0;
    for (long i = 0; i < 2 * lo + 2; i++) {
        unsigned long long sub = borrow;
        if (i < 2 * lo) sub = sub + (unsigned long long)z0[i];
        unsigned long long d = (unsigned long long)z1[i] - sub;
        borrow = (d > (unsigned long long)z1[i]) ? 1 : 0;
        z1[i] = (unsigned int)d;
    }
    borrow = 0;
    for (long i = 0; i < 2 * lo + 2; i++) {
        unsigned long long sub = borrow;
        if (i < 2 * hi) sub = sub + (unsigned long long)z2[i];
        unsigned long long d = (unsigned long long)z1[i] - sub;
        borrow = (d > (unsigned long long)z1[i]) ? 1 : 0;
        z1[i] = (unsigned int)d;
    }

    /* r = z0 + (z1 << lo) + (z2 << 2*lo) */
    for (long i = 0; i < 2 * m; i++) r[i] = 0;
    unsigned long long carry = 0;
    for (long i = 0; i < 2 * lo; i++) {
        unsigned long long s = (unsigned long long)z0[i] + carry;
        r[i] = (unsigned int)s;
        carry = s >> 32;
    }
    carry = 0;
    for (long i = 0; i < 2 * lo + 2; i++) {
        long k = i + lo;
        if (k >= 2 * m) break;
        unsigned long long s = (unsigned long long)r[k] + (unsigned long long)z1[i] + carry;
        r[k] = (unsigned int)s;
        carry = s >> 32;
    }
    carry = 0;
    for (long i = 0; i < 2 * hi; i++) {
        long k = i + 2 * lo;
        if (k >= 2 * m) break;
        unsigned long long s = (unsigned long long)r[k] + (unsigned long long)z2[i] + carry;
        r[k] = (unsigned int)s;
        carry = s >> 32;
    }

    free((void *)z0);
    free((void *)z2);
    free((void *)sa);
    free((void *)sb);
    free((void *)z1);
}

void __goclib_bi_mul(unsigned long long *r, const unsigned long long *a,
                     const unsigned long long *b, long n) {
    long m = 2 * n;                    /* limb counts */
    /* Narrow widths keep the schoolbook engine: splitting a 128-bit value
     * costs more in calls and scratch than it saves in multiplications. */
    if (m <= KARA_TH) {
        unsigned int *t = (unsigned int *)bi_scratch(0, m * 4);
        unsigned int *al = (unsigned int *)bi_scratch(1, m * 4);
        unsigned int *bl = (unsigned int *)bi_scratch(2, m * 4);
        if (t == 0 || al == 0 || bl == 0) {
            __goclib_bi_zero(r, n);
            return;
        }
        for (long i = 0; i < n; i++) {
            al[2 * i] = (unsigned int)a[i];
            al[2 * i + 1] = (unsigned int)(a[i] >> 32);
            bl[2 * i] = (unsigned int)b[i];
            bl[2 * i + 1] = (unsigned int)(b[i] >> 32);
        }
        for (long i = 0; i < m; i++) t[i] = 0;
        for (long i = 0; i < m; i++) {
            unsigned int ai = al[i];
            if (ai == 0) continue;         /* skip zero limbs */
            unsigned long long carry = 0;
            long lim = m - i;              /* limbs above m-1 are truncated away */
            for (long j = 0; j < lim; j++) {
                unsigned long long cur = (unsigned long long)t[i + j] +
                                         (unsigned long long)ai * bl[j] + carry;
                t[i + j] = (unsigned int)cur;
                carry = cur >> 32;
            }
        }
        for (long i = 0; i < n; i++) {
            r[i] = (unsigned long long)t[2 * i] | ((unsigned long long)t[2 * i + 1] << 32);
        }
        return;
    }
    unsigned int *pa = (unsigned int *)malloc(m * 4);
    unsigned int *pb = (unsigned int *)malloc(m * 4);
    unsigned int *pr = (unsigned int *)malloc((2 * m) * 4);
    if (pa == 0 || pb == 0 || pr == 0) {
        if (pa) free((void *)pa);
        if (pb) free((void *)pb);
        if (pr) free((void *)pr);
        __goclib_bi_zero(r, n);
        return;
    }
    for (long i = 0; i < n; i++) {
        pa[2 * i] = (unsigned int)a[i];
        pa[2 * i + 1] = (unsigned int)(a[i] >> 32);
        pb[2 * i] = (unsigned int)b[i];
        pb[2 * i + 1] = (unsigned int)(b[i] >> 32);
    }
    bi_mul_full(pr, pa, pb, m);
    for (long i = 0; i < n; i++) {
        r[i] = (unsigned long long)pr[2 * i] | ((unsigned long long)pr[2 * i + 1] << 32);
    }
    free((void *)pa);
    free((void *)pb);
    free((void *)pr);
}

/* ---- division (Knuth algorithm D over 32-bit limbs) --------------------------- */

static int bi_clz32(unsigned int x) {
    int n = 0;
    if (x == 0) return 32;
    while ((x & 0x80000000u) == 0) {
        x = x << 1;
        n++;
    }
    return n;
}

/* Unsigned quotient/remainder: q = a / b, rem = a % b. Division by zero
 * yields q = rem = 0 (the source was UB anyway; never crash the runtime).
 *
 * The work happens in 32-bit limbs: bi_divmod_limbs picks Knuth's schoolbook
 * or Burnikel-Ziegler's recursion by width, so wide divisions run at
 * multiplication speed instead of quadratic speed. */
static void bi_udivmod(unsigned long long *q, unsigned long long *rem,
                       const unsigned long long *a, const unsigned long long *b,
                       long n) {
    long m = 2 * n;
    /* Own buffers rather than the shared bi_scratch slots: those are freed
     * when grown, and callers (__goclib_bi_div_u and friends) hand us a slot
     * buffer as `rem` -- borrowing the same slot here would free it and leave
     * the caller writing through a dangling pointer. */
    unsigned int *al = (unsigned int *)malloc((m + 2) * 4);
    unsigned int *bl = (unsigned int *)malloc((m + 2) * 4);
    unsigned int *ql = (unsigned int *)malloc((m + 2) * 4);
    unsigned int *rl = (unsigned int *)malloc((m + 2) * 4);
    if (al == 0 || bl == 0 || ql == 0 || rl == 0) {
        if (al) free((void *)al);
        if (bl) free((void *)bl);
        if (ql) free((void *)ql);
        if (rl) free((void *)rl);
        __goclib_bi_zero(q, n);
        __goclib_bi_zero(rem, n);
        return;
    }
    for (long i = 0; i < m; i++) {
        al[i] = (unsigned int)(a[i / 2] >> (32 * (i & 1)));
        bl[i] = (unsigned int)(b[i / 2] >> (32 * (i & 1)));
        ql[i] = 0;
        rl[i] = 0;
    }
    al[m] = 0; al[m + 1] = 0;
    bl[m] = 0; bl[m + 1] = 0;
    ql[m] = 0; ql[m + 1] = 0;
    rl[m] = 0; rl[m + 1] = 0;
    bi_divmod_limbs(ql, rl, al, m, bl, m);
    for (long i = 0; i < n; i++) {
        q[i] = (unsigned long long)ql[2 * i] | ((unsigned long long)ql[2 * i + 1] << 32);
        rem[i] = (unsigned long long)rl[2 * i] | ((unsigned long long)rl[2 * i + 1] << 32);
    }
    free((void *)al);
    free((void *)bl);
    free((void *)ql);
    free((void *)rl);
}

void __goclib_bi_divmod_u(unsigned long long *q, unsigned long long *rem,
                          const unsigned long long *a,
                          const unsigned long long *b, long n) {
    bi_udivmod(q, rem, a, b, n);
}

void __goclib_bi_div_u(unsigned long long *r, const unsigned long long *a,
                       const unsigned long long *b, long n) {
    unsigned long long *bi_rem = (unsigned long long *)bi_scratch(3, n * 8);
    if (bi_rem == 0) { __goclib_bi_zero(r, n); return; }
    bi_udivmod(r, bi_rem, a, b, n);
}

void __goclib_bi_mod_u(unsigned long long *r, const unsigned long long *a,
                       const unsigned long long *b, long n) {
    unsigned long long *bi_q = (unsigned long long *)bi_scratch(3, n * 8);
    if (bi_q == 0) { __goclib_bi_zero(r, n); return; }
    bi_udivmod(bi_q, r, a, b, n);
}

/* Signed division with C truncation semantics (quotient toward zero,
 * remainder takes the dividend's sign): divide magnitudes, re-apply signs. */
void __goclib_bi_divmod_s(unsigned long long *q, unsigned long long *rem,
                          const unsigned long long *a,
                          const unsigned long long *b, long n) {
    unsigned long long *aa = (unsigned long long *)bi_scratch(3, n * 8);
    unsigned long long *bb = (unsigned long long *)bi_scratch(4, n * 8);
    unsigned long long *tq = (unsigned long long *)bi_scratch(5, n * 8);
    unsigned long long *tr = (unsigned long long *)bi_scratch(6, n * 8);
    if (aa == 0 || bb == 0 || tq == 0 || tr == 0) {
        __goclib_bi_zero(q, n);
        __goclib_bi_zero(rem, n);
        return;
    }
    int aneg = (a[n - 1] >> 63) & 1;
    int bneg = (b[n - 1] >> 63) & 1;
    if (aneg) __goclib_bi_neg(aa, a, n); else __goclib_bi_copy(aa, a, n);
    if (bneg) __goclib_bi_neg(bb, b, n); else __goclib_bi_copy(bb, b, n);
    bi_udivmod(tq, tr, aa, bb, n);
    if (aneg != bneg) __goclib_bi_neg(q, tq, n); else __goclib_bi_copy(q, tq, n);
    if (aneg) __goclib_bi_neg(rem, tr, n); else __goclib_bi_copy(rem, tr, n);
}

void __goclib_bi_div_s(unsigned long long *r, const unsigned long long *a,
                       const unsigned long long *b, long n) {
    unsigned long long *bi_rem2 = (unsigned long long *)bi_scratch(7, n * 8);
    if (bi_rem2 == 0) { __goclib_bi_zero(r, n); return; }
    __goclib_bi_divmod_s(r, bi_rem2, a, b, n);
}

void __goclib_bi_mod_s(unsigned long long *r, const unsigned long long *a,
                       const unsigned long long *b, long n) {
    unsigned long long *bi_q2 = (unsigned long long *)bi_scratch(7, n * 8);
    if (bi_q2 == 0) { __goclib_bi_zero(r, n); return; }
    __goclib_bi_divmod_s(bi_q2, r, a, b, n);
}

/* Widen (or truncate) a bitint of nSrc words into nDst words, extending
 * with the source's sign bit (_s variants) or zeros (_u). */
static void bi_widen_common(unsigned long long *r, const unsigned long long *a,
                            long nDst, long nSrc, long srcSigned) {
    long i = 0;
    for (; i < nSrc && i < nDst; i++) r[i] = a[i];
    unsigned long long fill = 0;
    if (srcSigned && nSrc > 0 && nSrc <= nDst && (a[nSrc - 1] >> 63) & 1) fill = ~0ULL;
    for (; i < nDst; i++) r[i] = fill;
}

void __goclib_bi_widen_u(unsigned long long *r, const unsigned long long *a,
                         long nDst, long nSrc) {
    bi_widen_common(r, a, nDst, nSrc, 0);
}

void __goclib_bi_widen_s(unsigned long long *r, const unsigned long long *a,
                         long nDst, long nSrc) {
    bi_widen_common(r, a, nDst, nSrc, 1);
}

/* ---- limb-level helpers shared by the decimal converter ---------------------- */

/* r[0..an+bn-1] = a * b.  bi_mul_full wants both operands the same width, so
 * the shorter one is zero-padded up to a common width.  bi_mul_full handles
 * any width (powers of two NOT required -- its split is ceil(m/2)), so pad
 * only to max(an,bn); rounding up to a power of two used to cost another
 * ~3x in Karatsuba work for widths just above a power of two. */
static void bi_lmul(unsigned int *r, const unsigned int *a, long an,
                    const unsigned int *b, long bn) {
    long m = an > bn ? an : bn;
    if (m < 2) m = 2;
    unsigned int *pa = (unsigned int *)malloc(m * 4);
    unsigned int *pb = (unsigned int *)malloc(m * 4);
    unsigned int *pr = (unsigned int *)malloc(2 * m * 4);
    if (pa == 0 || pb == 0 || pr == 0) {
        if (pa) free((void *)pa);
        if (pb) free((void *)pb);
        if (pr) free((void *)pr);
        for (long i = 0; i < an + bn; i++) r[i] = 0;
        return;
    }
    for (long i = 0; i < m; i++) { pa[i] = 0; pb[i] = 0; }
    for (long i = 0; i < an; i++) pa[i] = a[i];
    for (long i = 0; i < bn; i++) pb[i] = b[i];
    bi_mul_full(pr, pa, pb, m);
    for (long i = 0; i < an + bn; i++) r[i] = pr[i];
    free((void *)pa);
    free((void *)pb);
    free((void *)pr);
}

/* ---- Burnikel-Ziegler recursive division -------------------------------------
 *
 * Knuth D costs O(qn*vn) limb operations: at 16k words a single division is
 * already 1.3 s, and the divide-and-conquer decimal conversion above needs
 * thousands of them. Burnikel-Ziegler replaces one 2n-by-n division with two
 * 3h-by-2h divisions (h = n/2), each of which is one n/2-by-n/4 division plus
 * a half-size multiplication -- so the cost collapses to O(M(n)), the same
 * shape as Karatsuba multiplication.
 *
 * Two properties make the recursion terminate and stay correct for every
 * size, and both are load-bearing:
 *   - the divisor's limb count must be 2^d*m with m <= BZ_TH, so every
 *     halving stays even and the odd case only ever reaches the basecase;
 *   - the divisor must be normalised (top bit set), which bounds the
 *     quotient correction to at most two.
 */
#define BZ_TH  512    /* limbs: below this Knuth's schoolbook wins */
#define BZ_MIN 8192  /* limbs: below this the divisor is too narrow to pay */

static int bi_lcmp(const unsigned int *a, const unsigned int *b, long n) {
    for (long i = n - 1; i >= 0; i--) {
        if (a[i] != b[i]) return a[i] > b[i] ? 1 : -1;
    }
    return 0;
}

static int bi_lcmp_len(const unsigned int *a, long an, const unsigned int *b, long bn) {
    while (an > 0 && a[an - 1] == 0) an--;
    while (bn > 0 && b[bn - 1] == 0) bn--;
    if (an != bn) return an > bn ? 1 : -1;
    return bi_lcmp(a, b, an);
}

/* r[0..n-1] = a - b, modulo 2^(32n) (callers keep a >= b).
 * Alias-safe: callers pass r == a (bi_d3n2h subtracts b in place to derive a
 * small quotient), so a[i] must be latched BEFORE r[i] is written -- reading
 * it afterwards sees the just-stored difference and the borrow chain breaks,
 * which silently corrupts the whole row below the first borrow. */
static void bi_lsub(unsigned int *r, const unsigned int *a, const unsigned int *b, long n) {
    unsigned long long borrow = 0;
    for (long i = 0; i < n; i++) {
        unsigned long long ai = (unsigned long long)a[i];
        unsigned long long sub = (unsigned long long)b[i] + borrow;
        r[i] = (unsigned int)(ai - sub);
        borrow = (ai < sub) ? 1 : 0;
    }
}

/* Basecase Knuth D.  a has an limbs, b has bn (bn >= 1, high bit of b[bn-1]
 * set), and a < b * 2^(32*(an-bn+1)).  q gets an-bn+1 limbs, r gets bn. */
static void bi_knuth(unsigned int *q, unsigned int *r, const unsigned int *a,
                     long an, const unsigned int *b, long bn) {
    long qn = an - bn + 1;
    unsigned int *u = (unsigned int *)malloc((an + 2) * 4);
    if (u == 0) {
        for (long i = 0; i < bn; i++) r[i] = 0;
        return;
    }
    for (long i = 0; i < an; i++) u[i] = a[i];
    u[an] = 0;
    u[an + 1] = 0;
    for (long j = qn - 1; j >= 0; j--) {
        unsigned long long num = ((unsigned long long)u[j + bn] << 32) |
                                 (unsigned long long)u[j + bn - 1];
        unsigned long long qhat = num / b[bn - 1];
        unsigned long long rhat = num - qhat * (unsigned long long)b[bn - 1];
        while (qhat >= 0x100000000ULL) {
            qhat = qhat - 1;
            rhat = rhat + b[bn - 1];
            if (rhat >= 0x100000000ULL) break;
        }
        if (bn > 1) {
            unsigned long long lhs = qhat * (unsigned long long)b[bn - 2];
            unsigned long long rhs = (rhat << 32) | (unsigned long long)u[j + bn - 2];
            while (lhs > rhs) {
                qhat = qhat - 1;
                rhat = rhat + b[bn - 1];
                if (rhat >= 0x100000000ULL) break;
                lhs = qhat * (unsigned long long)b[bn - 2];
                rhs = (rhat << 32) | (unsigned long long)u[j + bn - 2];
            }
        }
        const unsigned int *vp = b;
        unsigned int *up = u + j;
        long long k = 0;
        for (long i = 0; i < bn; i++) {
            unsigned long long p = qhat * (unsigned long long)*vp++;
            long long t = (long long)(unsigned long long)*up -
                          (long long)(unsigned int)p - k;
            *up++ = (unsigned int)t;
            k = (long long)(p >> 32) - (t >> 32);
        }
        long long t = (long long)(unsigned long long)*up - k;
        *up = (unsigned int)t;
        q[j] = (unsigned int)qhat;
        if (t < 0) {
            q[j] = (unsigned int)(qhat - 1);
            vp = b;
            up = u + j;
            unsigned long long cy = 0;
            for (long i = 0; i < bn; i++) {
                unsigned long long sum = (unsigned long long)*up +
                                         (unsigned long long)*vp++ + cy;
                *up++ = (unsigned int)sum;
                cy = sum >> 32;
            }
            *up = (unsigned int)((unsigned long long)*up + cy);
        }
    }
    for (long i = 0; i < bn; i++) r[i] = u[i];
    free((void *)u);
}

/* D_{2n/1n}: a (2n limbs) / b (n limbs), a/b < 2^(32n).
 * q gets n limbs, r gets n limbs.  b is normalised. */
static void bi_d2n1n(unsigned int *q, unsigned int *r, const unsigned int *a,
                     const unsigned int *b, long n);

/* Knuth basecase for D_{3h/2h}.  Knuth on 3h-by-2h naturally emits h+1
 * quotient limbs, and the extra one is zero only under this routine's
 * precondition (A/B < beta^h).  Writing it straight into q would clobber the
 * caller's neighbouring half: bi_d2n1n stacks two D_{3h/2h} results back to
 * back, so limb h of q already belongs to the *other* block.  Route through a
 * temporary and land only the h limbs that are ours. */
static void bi_d3n2h_knuth(unsigned int *q, unsigned int *r, const unsigned int *a,
                           const unsigned int *b, long h) {
    unsigned int *qt = (unsigned int *)malloc((h + 2) * 4);
    if (qt == 0) {
        for (long i = 0; i < h; i++) q[i] = 0;
        for (long i = 0; i < 2 * h; i++) r[i] = 0;
        return;
    }
    bi_knuth(qt, r, a, 3 * h, b, 2 * h);
    for (long i = 0; i < h; i++) q[i] = qt[i];
    free((void *)qt);
}

/* D_{3h/2h}: a (3h limbs) / b (2h limbs), a/b < 2^(32h).
 * q gets h limbs, r gets 2h limbs.  b is normalised. */
static void bi_d3n2h(unsigned int *q, unsigned int *r, const unsigned int *a,
                     const unsigned int *b, long h) {
    if (h < BZ_TH || (h & 1)) { bi_d3n2h_knuth(q, r, a, b, h); return; }
    const unsigned int *A12 = a + h;   /* top 2h limbs */
    const unsigned int *A3 = a;        /* low h limbs  */
    const unsigned int *B1 = b + h;    /* top h limbs  */
    const unsigned int *B2 = b;        /* low h limbs  */

    /* Clamp case: A12 >= B1*2^(32h) would make floor(A12/B1) need h+1 limbs.
     * It only happens when the quotient is within a hair of 2^(32h) --
     * probability ~2^(-32h) -- so the basecase can absorb it. */
    int cmp = 0;
    for (long i = h - 1; i >= 0; i--) {
        if (A12[h + i] != B1[i]) { cmp = A12[h + i] > B1[i] ? 1 : -1; break; }
    }
    if (cmp == 0) {
        for (long i = h - 1; i >= 0; i--) if (A12[i] != 0) { cmp = 1; break; }
    }
    if (cmp >= 0) { bi_d3n2h_knuth(q, r, a, b, h); return; }

    unsigned int *q1 = (unsigned int *)malloc((h + 2) * 4);
    unsigned int *r1 = (unsigned int *)malloc((h + 2) * 4);
    unsigned int *dd = (unsigned int *)malloc((2 * h + 2) * 4);
    unsigned int *rm = (unsigned int *)malloc((2 * h + 2) * 4);
    unsigned int *pp = (unsigned int *)malloc((2 * h + 4) * 4);
    if (q1 == 0 || r1 == 0 || dd == 0 || rm == 0 || pp == 0) {
        if (q1) free((void *)q1);
        if (r1) free((void *)r1);
        if (dd) free((void *)dd);
        if (rm) free((void *)rm);
        if (pp) free((void *)pp);
        bi_d3n2h_knuth(q, r, a, b, h);
        return;
    }
    bi_d2n1n(q1, r1, A12, B1, h);              /* q1 = A12 / B1, r1 = A12 % B1 */
    for (long i = 0; i < h; i++) dd[i] = A3[i];
    for (long i = 0; i < h; i++) dd[h + i] = r1[i];
    /* rm = dd / b, by subtraction: dd/b is at most 4 (see the file header). */
    for (long i = 0; i < 2 * h; i++) rm[i] = dd[i];
    unsigned long q2 = 0;
    int fine = 1;
    while (bi_lcmp(rm, b, 2 * h) >= 0) {
        bi_lsub(rm, rm, b, 2 * h);
        q2++;
        if (q2 > 8) { fine = 0; break; }
    }
    if (!fine) {
        free((void *)q1); free((void *)r1); free((void *)dd);
        free((void *)rm); free((void *)pp);
        bi_d3n2h_knuth(q, r, a, b, h);
        return;
    }
    /* candidate quotient = q1 + q2 */
    for (long i = 0; i < h; i++) q[i] = q1[i];
    {
        unsigned long long cy = q2;
        for (long i = 0; i < h && cy != 0; i++) {
            unsigned long long s = (unsigned long long)q[i] + cy;
            q[i] = (unsigned int)s;
            cy = s >> 32;
        }
    }
    /* a = (q1+q2)*b + (rm - q1*B2): correct the (possibly negative) tail. */
    bi_lmul(pp, q1, h, B2, h);
    if (bi_lcmp(rm, pp, 2 * h) >= 0) {
        bi_lsub(r, rm, pp, 2 * h);
    } else {
        unsigned int *xx = (unsigned int *)malloc((2 * h + 2) * 4);
        if (xx == 0) {
            free((void *)q1); free((void *)r1); free((void *)dd);
            free((void *)rm); free((void *)pp);
            bi_d3n2h_knuth(q, r, a, b, h);
            return;
        }
        bi_lsub(xx, pp, rm, 2 * h);            /* X = q1*B2 - rm, 0 < X <= 2b */
        long d = 0;
        while (bi_lcmp(xx, b, 2 * h) >= 0 && d < 4) {
            bi_lsub(xx, xx, b, 2 * h);
            d++;
        }
        int xzero = 1;
        for (long i = 0; i < 2 * h; i++) if (xx[i] != 0) { xzero = 0; break; }
        if (xzero) {
            for (long i = 0; i < 2 * h; i++) r[i] = 0;
        } else {
            bi_lsub(r, b, xx, 2 * h);
            d++;
        }
        {
            unsigned long long bw = (unsigned long long)d;
            for (long i = 0; i < h && bw != 0; i++) {
                unsigned long long s = (unsigned long long)q[i] - bw;
                bw = (s > (unsigned long long)q[i]) ? 1 : 0;
                q[i] = (unsigned int)s;
            }
        }
        free((void *)xx);
    }
    free((void *)q1);
    free((void *)r1);
    free((void *)dd);
    free((void *)rm);
    free((void *)pp);
}

static void bi_d2n1n(unsigned int *q, unsigned int *r, const unsigned int *a,
                     const unsigned int *b, long n) {
    if (n < BZ_TH || (n & 1)) { bi_knuth(q, r, a, 2 * n, b, n); return; }
    long h = n / 2;
    unsigned int *r1 = (unsigned int *)malloc((2 * h + 2) * 4);
    unsigned int *d = (unsigned int *)malloc((3 * h + 2) * 4);
    if (r1 == 0 || d == 0) {
        if (r1) free((void *)r1);
        if (d) free((void *)d);
        bi_knuth(q, r, a, 2 * n, b, n);
        return;
    }
    /* a = A1*2^(32h) + A0 with A1 = a[h..4h-1] (3h limbs):
     *   A1 = Q1*b + R1   ->   q's high h limbs
     *   D  = R1*2^(32h) + A0 (3h limbs)  ->   q's low h limbs */
    bi_d3n2h(q + h, r1, a + h, b, h);
    for (long i = 0; i < h; i++) d[i] = a[i];
    for (long i = 0; i < 2 * h; i++) d[h + i] = r1[i];
    bi_d3n2h(q, r, d, b, h);
    free((void *)r1);
    free((void *)d);
}

/* q = a / b and r = a % b, on 32-bit limbs.  q must have room for am+1 limbs
 * (am = significant limbs of a), r for bn limbs. */
static void bi_divmod_limbs(unsigned int *q, unsigned int *r,
                            const unsigned int *a, long an,
                            const unsigned int *b, long bn) {
    for (long i = 0; i <= an; i++) q[i] = 0;
    for (long i = 0; i < bn; i++) r[i] = 0;
    long am = an;
    while (am > 0 && a[am - 1] == 0) am--;
    long bm = bn;
    while (bm > 0 && b[bm - 1] == 0) bm--;
    if (bm == 0) return;                        /* division by zero -> 0 */
    if (bi_lcmp_len(a, am, b, bm) < 0) {
        for (long i = 0; i < am && i < bn; i++) r[i] = a[i];
        return;
    }
    /* Normalise: shift left until the divisor's top bit is set. */
    unsigned int top = b[bm - 1];
    int sn = 0;
    while ((top & 0x80000000u) == 0) { top = top << 1; sn++; }

    /* Burnikel-Ziegler only pays off for wide divisors with a long quotient,
     * and it needs bn = 2^d*m (m <= BZ_TH) so every halving stays even.
     * Padding to that form costs at most bm/BZ_TH extra limbs (~3%). */
    long bn2 = bm, k = 0;
    int useBZ = 0;
    if (bm >= BZ_MIN) {
        long p2 = 1;
        while ((bm + p2 - 1) / p2 > BZ_TH) p2 = p2 * 2;
        bn2 = p2 * ((bm + p2 - 1) / p2);
        k = bn2 - bm;
        /* BZ costs ~3 Karatsuba products of bn2 limbs regardless of the
         * quotient length; Knuth costs qn*bn2 limb steps.  The two cross over
         * once qn is a few hundred limbs, so gate on the quotient width only.
         * Requiring a full bn2-limb block here (the old gate) silently
         * disabled BZ for the a ~ 2b shape -- quotient just under bn2 -- and
         * the whole division fell back to schoolbook. */
        useBZ = (am - bm + 1) >= 64;
    }
    unsigned int *aa = (unsigned int *)malloc((am + k + 3) * 4);
    unsigned int *bb = (unsigned int *)malloc((bn2 + 2) * 4);
    unsigned int *rem = (unsigned int *)malloc((bn2 + 2) * 4);
    if (aa == 0 || bb == 0 || rem == 0) {
        if (aa) free((void *)aa);
        if (bb) free((void *)bb);
        if (rem) free((void *)rem);
        return;
    }
    for (long i = 0; i < am + k + 3; i++) aa[i] = 0;
    for (long i = 0; i < bn2 + 2; i++) bb[i] = 0;
    for (long i = 0; i < bn2 + 2; i++) rem[i] = 0;
    {
        unsigned int carry = 0;
        for (long i = 0; i < am; i++) {
            unsigned int x = a[i];
            aa[i + k] = (sn > 0) ? (unsigned int)((x << sn) | carry) : x;
            carry = (sn > 0) ? (x >> (32 - sn)) : 0;
        }
        if (carry != 0) aa[am + k] = carry;
    }
    {
        unsigned int carry = 0;
        for (long i = 0; i < bm; i++) {
            unsigned int x = b[i];
            bb[i + k] = (sn > 0) ? (unsigned int)((x << sn) | carry) : x;
            carry = (sn > 0) ? (x >> (32 - sn)) : 0;
        }
    }
    long a_len = am + k + (((sn > 0) && (a[am - 1] >> (32 - sn)) != 0) ? 1 : 0);
    long cur_len = a_len;

    unsigned int *w = 0, *qb = 0;
    if (useBZ) {
        w = (unsigned int *)malloc((2 * bn2 + 2) * 4);
        qb = (unsigned int *)malloc((bn2 + 2) * 4);
    }
    if (useBZ && w != 0 && qb != 0) {
        /* Peel s quotient limbs per block.  Invariant: the remaining quotient
         * q_total is below 2^(32*cur_qn) and V never exceeds cur_qn+m limbs.
         * Then kk = cur_qn - s is the limb the block starts at: the window
         * floor(V/2^(32*kk)) holds at most m+s limbs (so it fits the 2m-limb
         * D_{2m/1m} window after top-padding) and its quotient is exactly
         * floor(q_total/2^(32*kk)), which needs s limbs.
         *
         * Deriving kk from cur_len instead -- cur_len-m-s+1 -- looks
         * equivalent but is not: cur_len drops whenever a remainder comes back
         * with leading zero limbs, and from then on the two disagree, the
         * quotient limbs drift out of alignment and the loop stops making
         * progress.  The same desync makes D_{2m/1m}'s precondition
         * (window's top m limbs < b) fail, since V <= b*2^(32*cur_qn)-1 gives
         * floor(window/2^(32m)) <= b*2^(32*(s-m)) - 1 < b only for this kk. */
        long cur_qn = a_len - bn2 + 1;
        while (cur_qn > 0) {
            if (bi_lcmp_len(aa, cur_len, bb, bn2) < 0) break;
            long s = cur_qn;
            if (s > bn2) s = bn2;
            long kk = cur_qn - s;
            for (long i = 0; i < 2 * bn2; i++) w[i] = 0;
            {
                long wl = cur_len - kk;
                if (wl > 2 * bn2) wl = 2 * bn2;
                if (wl < 0) wl = 0;
                for (long i = 0; i < wl; i++) w[i] = aa[kk + i];
            }
            bi_d2n1n(qb, rem, w, bb, bn2);
            for (long i = 0; i < s; i++) q[kk + i] = qb[i];
            /* V' = rem * 2^(32*kk) + (V mod 2^(32*kk)) */
            for (long i = 0; i < bn2; i++) aa[kk + i] = rem[i];
            for (long i = kk + bn2; i < cur_len; i++) aa[i] = 0;
            cur_len = kk + bn2;
            while (cur_len > 0 && aa[cur_len - 1] == 0) cur_len--;
            cur_qn = kk;
        }
        for (long i = 0; i < bn2; i++) rem[i] = (i < cur_len) ? aa[i] : 0;
    } else {
        bi_knuth(q, rem, aa, a_len, bb, bn2);
    }
    if (w) free((void *)w);
    if (qb) free((void *)qb);

    /* Un-normalise the remainder: drop the k whole limbs, then shift right. */
    {
        unsigned int car = 0;
        for (long i = bm - 1; i >= 0; i--) {
            unsigned int x = (i + k < bn2) ? rem[i + k] : 0;
            r[i] = (sn > 0) ? (unsigned int)((x >> sn) | car) : x;
            car = (sn > 0) ? (unsigned int)(x << (32 - sn)) : 0;
        }
    }
    free((void *)aa);
    free((void *)bb);
    free((void *)rem);
}
/* ---- decimal string ----------------------------------------------------------- */

/* Powers of ten: bi_p10[j] = 10^(9*2^j), built by repeated squaring and kept
 * across calls (they only ever grow).  Splitting a value by bi_p10[j] peels
 * 9*2^j decimal digits in one division, so conversion costs O(M(n) log n)-ish
 * work instead of one full-width pass per nine digits. */
#define BI_P10MAX 22
static unsigned int *bi_p10[BI_P10MAX];
static long bi_p10len[BI_P10MAX];
static long bi_p10cnt;

static int bi_p10_build(long jtop) {
    while (bi_p10cnt <= jtop) {
        long k = bi_p10cnt;
        if (k == 0) {
            unsigned int *p = (unsigned int *)malloc(4);
            if (p == 0) return 0;
            p[0] = 1000000000u;
            bi_p10[0] = p;
            bi_p10len[0] = 1;
            bi_p10cnt = 1;
            continue;
        }
        long n = 2 * bi_p10len[k - 1] + 2;
        unsigned int *p = (unsigned int *)malloc(n * 4);
        if (p == 0) return 0;
        bi_lmul(p, bi_p10[k - 1], bi_p10len[k - 1],
                   bi_p10[k - 1], bi_p10len[k - 1]);
        long len = 2 * bi_p10len[k - 1];
        while (len > 0 && p[len - 1] == 0) len--;
        bi_p10[k] = p;
        bi_p10len[k] = len;
        bi_p10cnt = k + 1;
    }
    return 1;
}

/* Write the decimal form of v (m limbs, v < 10^D) into out[0..D-1], zero
 * padded on the left to exactly D digits.  v is consumed. */
static void bi_emit_dec(unsigned int *v, long m, long D, char *out) {
    if (D <= 0) return;
    if (D <= 18) {                       /* v < 10^18: one 64-bit value */
        unsigned long long x = 0;
        for (long i = m - 1; i >= 0; i--) x = (x << 32) | (unsigned long long)v[i];
        for (long i = D - 1; i >= 0; i--) {
            out[i] = (char)('0' + (int)(x % 10));
            x = x / 10;
        }
        return;
    }
    /* Split off the low `lo` digits in one division; `lo` is the largest
     * 9*2^j not exceeding D/2, so both halves are strictly smaller. */
    long j = 0;
    for (long i = 1; i < bi_p10cnt; i++) {
        if ((9L << (i + 1)) <= D) j = i;
    }
    long lo = 9L << j;
    long hi = D - lo;
    unsigned int *qq = (unsigned int *)malloc((m + 2) * 4);
    unsigned int *rr = (unsigned int *)malloc((bi_p10len[j] + 2) * 4);
    if (qq == 0 || rr == 0) {
        if (qq) free((void *)qq);
        if (rr) free((void *)rr);
        for (long i = 0; i < D; i++) out[i] = '0';
        return;
    }
    for (long i = 0; i < m + 2; i++) qq[i] = 0;
    for (long i = 0; i < bi_p10len[j] + 2; i++) rr[i] = 0;
    bi_divmod_limbs(qq, rr, v, m, bi_p10[j], bi_p10len[j]);
    bi_emit_dec(qq, m, hi, out);
    bi_emit_dec(rr, bi_p10len[j], lo, out + hi);
    free((void *)qq);
    free((void *)rr);
}

/* Write the decimal form of a into buf (big enough for N*0.30103+3 bytes),
 * NUL-terminated.  Returns the string length.  isSigned selects the signed
 * reading. */
long __goclib_bi_str(char *buf, const unsigned long long *a, long n, long isSigned) {
    long m = 2 * n;
    unsigned int *v = (unsigned int *)malloc((m + 2) * 4);
    if (v == 0) { buf[0] = 0; return 0; }
    for (long i = 0; i < n; i++) {
        v[2 * i] = (unsigned int)a[i];
        v[2 * i + 1] = (unsigned int)(a[i] >> 32);
    }
    long neg = 0;
    if (isSigned && ((v[m - 1] >> 31) & 1)) {
        unsigned long long cy = 1;                 /* two's complement negate */
        for (long i = 0; i < m; i++) {
            unsigned long long s = (unsigned long long)(~v[i]) + cy;
            v[i] = (unsigned int)s;
            cy = s >> 32;
        }
        neg = 1;
    }
    long mz = m;
    while (mz > 0 && v[mz - 1] == 0) mz--;
    if (mz == 0) {
        free((void *)v);
        buf[0] = '0';
        buf[1] = 0;
        return 1;
    }
    long D = (mz * 9633) / 1000 + 3;      /* >= floor(mz*32*log10(2)) + 1 */
    long jtop = 0;
    while ((9L << (jtop + 2)) <= D) jtop++;
    if (jtop >= BI_P10MAX) jtop = BI_P10MAX - 1;
    if (!bi_p10_build(jtop)) { free((void *)v); buf[0] = 0; return 0; }
    char *tmp = (char *)malloc(D + 2);
    if (tmp == 0) { free((void *)v); buf[0] = 0; return 0; }
    bi_emit_dec(v, mz, D, tmp);
    long i = 0;
    while (i < D - 1 && tmp[i] == '0') i++;
    long out = 0;
    if (neg) buf[out++] = '-';
    for (; i < D; i++) buf[out++] = tmp[i];
    buf[out] = 0;
    free((void *)tmp);
    free((void *)v);
    return out;
}
