#include "goclib.h"

/* --------------- binary128 <-> decimal, exact and rounded ------------------
 *
 * printf("%Lf/%Le/%Lg") and scanf("%Lf/...") need a decimal converter for
 * binary128, and it cannot reuse the double one: a double's decimal expansion
 * is at most ~1080 digits, a binary128's is 11525. The value is m * 2^e with
 * m a 113-bit integer and e spanning [-16494, 16384] -- so 2^-16494 has 4933
 * leading decimal places (the famous LDBL_MIN print) and 2^16384 has 4933
 * integer digits (LDBL_MAX). printf's %f of that prints every one of them.
 *
 * Everything here is exact integer arithmetic on 32-bit limbs, for the same
 * reasons fp128.c gives: no floating-point operation anywhere (a recompile
 * with a different FPU must not change a digit), and no 64-bit divide
 * (32-bit targets turn that into a libgcc call). A u32 * u32 product widened
 * to u64 is the widest multiply, and every divide is u32-by-u32.
 *
 * ## The shared middle representation (format side)
 *
 * The exact expansion lands in one shape, and it is the one the rounding and
 * the three printf modes all consume:
 *
 *     digits[]  the significant decimal digits, no leading zeros ("0" is the
 *               zero value), nd of them
 *     decpt     the decimal point position: the value is
 *               0.d1d2d3... * 10^decpt, so digits[i] carries 10^(decpt-1-i)
 *
 * A conversion picks a WINDOW -- the first `keep` digits, where each mode's
 * precision says how many -- and rounds it half-to-even against the one
 * look-ahead digit plus a sticky bit. dv_round then TRUNCATES the string to
 * the window, which is what makes the output paths simple: every digit they
 * read is inside the window, so a rounding rollover ("999" becoming "1" with
 * the exponent bumped) can never leak pre-rounding digits into the output,
 * and decpt == leading-exponent + 1 stays an invariant the %f path relies on.
 *
 * %g rounds ONCE (to prec significant digits), decides the style from the
 * rounded exponent -- per C99 the style decision uses the rounded value --
 * and only strips trailing zeros afterwards. Rounding again per output mode
 * would consult digits the first round already consumed and misround (found
 * the hard way while drafting: 999.9 with %.3g printed 1.01e+04 instead of
 * 1.00e+04).
 *
 * ## __goclib_tf128_parse: text -> binary128
 *
 * The digit string becomes an exact integer D and a decimal exponent k
 * (value = D * 10^k). k >= 0: M = D * 5^k is exact and value = M * 2^k --
 * round the integer M to 113 bits with the standard guard/sticky shift.
 *
 * k < 0: value = D / 5^m * 2^k. Pick a shift S so that
 * N = floor(D * 2^S / 5^m) lands somewhere above 113 bits (S is chosen from
 * an integer estimate of bitlen(5^m), so the quotient lands in [~117, ~144]
 * bits; S may go negative when D itself is already huge -- then D is
 * right-shifted instead, its dropped low bits folded into the sticky chain),
 * and divide D * 2^S by 5 exactly m times, folding every remainder
 * into one sticky flag. Because 5^m is odd, the dropped fraction
 * (D*2^S mod 5^m)/5^m is NEVER exactly 1/2, so its zero-ness is all the
 * information the final rounding needs: a tie at the 113-bit boundary can
 * then only come from the integer part's own dropped bits being exactly
 * 100...0 with fraction zero -- and that tie is resolved half-to-even
 * exactly. No double rounding anywhere: the comparison uses the exact
 * remainder, not a pre-rounded value.
 *
 * Inputs with more than ~SIG_KEEP significant digits keep the first batch
 * exactly and treat the rest as sticky (a nonzero dropped digit forces the
 * sticky bit). binary128 carries 34 decimal digits and correct rounding
 * needs ~40 plus sticky; the cap sits two orders of magnitude above that.
 *
 * Hexadecimal floats are not accepted here; the scanner that extracts the
 * token routes them separately.
 *
 * ## Stack budget
 *
 * The working big integers and the digit buffer total roughly 30 KB of stack
 * in the worst case (a 4933-digit integer or a full 11525-digit fraction).
 * printf and scanf already run on multi-hundred-KB stacks on every target goc
 * generates for; a static buffer would instead be live in every binary that
 * links the library, embedded ones included.
 * -------------------------------------------------------------------------- */

/* Big integer: n limbs, little-endian, base 2^32. BI_MAX covers the widest
 * intermediate either direction builds: 5^16494 (11525 decimal digits) under
 * a 113-bit significand, in 32-bit limbs, with slack. */
#define BI_MAX 1290

typedef struct {
    int n;                      /* limbs in use; 0 means the value zero */
    unsigned int w[BI_MAX];
} bi;

/* --- small primitives (all O(limbs), no 64-bit divide anywhere) ----------- */

static void bi_set(bi *x, unsigned int v) {
    if (v == 0) { x->n = 0; return; }
    x->n = 1;
    x->w[0] = v;
}

static int bi_is_zero(const bi *x) {
    return x->n == 0;
}

/* x += v (v any u32). Limbs past n hold garbage, so a fresh limb contributes
 * zero, not its uninitialized word (adding to w[i] with i >= n made D start
 * from stack noise: parse("1") returned 181.0 in one build). */
static void bi_add_small(bi *x, unsigned int v) {
    unsigned long long carry = v;
    int i = 0;
    while (carry && i < BI_MAX) {
        unsigned long long s = carry;
        if (i < x->n) s += x->w[i];
        else          x->n = i + 1;
        x->w[i] = (unsigned int)s;
        carry = s >> 32;
        i++;
    }
}

/* x *= m (m any u32). The carry chain is u64 but every store is one limb. */
static void bi_mul_small(bi *x, unsigned int m) {
    unsigned long long carry = 0;
    int i;
    if (m == 0) { x->n = 0; return; }
    for (i = 0; i < x->n; i++) {
        unsigned long long p = (unsigned long long)x->w[i] * m + carry;
        x->w[i] = (unsigned int)p;
        carry = p >> 32;
    }
    while (carry) {
        if (x->n >= BI_MAX) return; /* callers size BI_MAX so this cannot fire */
        x->w[x->n] = (unsigned int)carry;
        carry >>= 32;
        x->n++;
    }
}

/* x /= d (d nonzero u32), returns the remainder. */
static unsigned int bi_div_small(bi *x, unsigned int d) {
    unsigned long long rem = 0;
    int i;
    for (i = x->n - 1; i >= 0; i--) {
        unsigned long long cur = (rem << 32) | x->w[i];
        x->w[i] = (unsigned int)(cur / d);
        rem = cur % d;
    }
    while (x->n > 0 && x->w[x->n - 1] == 0) x->n--;
    return (unsigned int)rem;
}

/* x mod d (d nonzero u32), x untouched. */
static unsigned int bi_mod_small(const bi *x, unsigned int d) {
    unsigned long long rem = 0;
    int i;
    for (i = x->n - 1; i >= 0; i--)
        rem = ((rem << 32) | x->w[i]) % d;
    return (unsigned int)rem;
}

/* x <<= s bit positions. s may be negative (a right shift). The write
 * direction follows the shift direction so an in-place shift never reads
 * limbs it has already overwritten: a right shift written top-down consumed
 * its own output as input (every S < 0 parse produced garbage -- the top
 * limbs came out right and everything below them was recycled data). */
static void bi_shr_round(bi *x, int s, int *round, int *sticky);

static void bi_shl_bits(bi *x, int s) {
    int limb = s >> 5, bit = s & 31, i, newn;
    if (s == 0 || x->n == 0) return;
    if (s < 0) {
        /* right shift: delegate (dropped bits discarded, as before) */
        int r2, s2;
        bi_shr_round(x, -s, &r2, &s2);
        return;
    }
    newn = x->n + limb + 1;
    if (newn > BI_MAX) newn = BI_MAX; /* guarded by caller sizing, like above */
    for (i = newn - 1; i >= 0; i--) {
        /* dest bit d of limb i pulls source bit 32i+d-s: the high part
         * comes from limb i-limb, the low part from limb i-limb-1. The two
         * are independent -- the high part's bits beyond 32 are re-derived
         * by limb i+1's low part, so a u32 shift's truncation is harmless;
         * but each part needs its OWN range guard (the topmost destination
         * limb has src == x->n, out of range, yet still pulls a low part
         * from limb x->n-1 -- losing that limb lost the implicit bit and
         * every 1e300-class digit was wrong). Dest limbs below the first
         * source bit are pure zeros: a left shift has no low-side source. */
        unsigned int v = 0;
        int src = i - limb;
        if (src >= 0 && src < x->n)
            v = x->w[src] << bit;
        if (bit && src - 1 >= 0 && src - 1 < x->n)
            v |= x->w[src - 1] >> (32 - bit);
        x->w[i] = v;
    }
    x->n = newn;
    while (x->n > 0 && x->w[x->n - 1] == 0) x->n--;
}

static int bi_bitlen(const bi *x) {
    unsigned int t;
    int b;
    if (x->n == 0) return 0;
    t = x->w[x->n - 1];
    b = 31;
    while (b > 0 && !(t & (1u << b))) b--;
    return (x->n - 1) * 32 + b + 1;
}

/* x >>= s, reporting what fell off: *round is the highest dropped bit,
 * *sticky is set when anything at all beyond it dropped. */
static void bi_shr_round(bi *x, int s, int *round, int *sticky) {
    int limb = s >> 5, bit = s & 31, i, newn;
    *round = 0;
    *sticky = 0;
    if (s == 0 || x->n == 0) return;
    if (s >= bi_bitlen(x)) {
        /* everything drops: the round bit is the old top bit only when s
         * lands exactly on it; anything nonzero anywhere is sticky */
        *round = (s == bi_bitlen(x)) ? (int)((x->w[x->n - 1] >> 31) & 1) : 0;
        for (i = 0; i < x->n; i++) if (x->w[i]) { *sticky = 1; break; }
        x->n = 0;
        return;
    }
    if (bit) *round = (int)((x->w[limb] >> (bit - 1)) & 1);
    else     *round = (int)((x->w[limb - 1] >> 31) & 1);
    /* the dropped tail: limbs [0, limb) whole, plus w[limb]'s low `bit`
     * bits -- scan BEFORE the write loop below overwrites them (scanning
     * after the shift read recycled data and sticky could silently drop) */
    for (i = 0; i < limb; i++) if (x->w[i]) { *sticky = 1; break; }
    if (!*sticky && bit && (x->w[limb] & ((1u << bit) - 1u))) *sticky = 1;
    newn = x->n - limb;
    for (i = 0; i < newn; i++) {
        unsigned int v;
        if (bit) {
            v = x->w[i + limb] >> bit;
            if (i + limb + 1 < x->n) v |= x->w[i + limb + 1] << (32 - bit);
        } else {
            v = x->w[i + limb];
        }
        x->w[i] = v;
    }
    x->n = newn;
    while (x->n > 0 && x->w[x->n - 1] == 0) x->n--;
}

/* --- decimal expansion ---------------------------------------------------- */

/* The longest exact decimal expansion: 5^16494 has 11525 digits and the
 * significand can contribute 35 more; round up. */
#define DIGITS_MAX 11600

/* Expand x into digits, most significant first, digit count returned.
 * Repeated division by 10^9 produces nine digits per division, low group
 * first -- reversed in place at the end. */
static int bi_to_digits(const bi *x, char *digits) {
    bi t = *x;
    int rn = 0, i, j;
    if (t.n == 0) { digits[0] = '0'; return 1; }
    while (t.n > 0) {
        unsigned int r = bi_div_small(&t, 1000000000u);
        int k;
        for (k = 0; k < 9; k++) {
            digits[rn++] = (char)('0' + r % 10);
            r /= 10;
        }
    }
    while (rn > 1 && digits[rn - 1] == '0') rn--;
    for (i = 0, j = rn - 1; i < j; i++, j--) {
        char c = digits[i];
        digits[i] = digits[j];
        digits[j] = c;
    }
    return rn;
}

/* --- the shared digit-string shape ---------------------------------------- */

typedef struct {
    char digits[DIGITS_MAX];    /* significant digits, no leading zeros */
    int nd;                     /* count */
    int decpt;                  /* value = 0.digits * 10^decpt */
} decval;

/* The digit at 10^p (p any integer): positions the string does not cover are
 * exact zeros -- below it (p < decpt-nd) those are trailing zeros of a finite
 * decimal, above (p >= decpt) leading zeros of a value < 1. Output paths only
 * read inside the post-rounding window; everything else is exact zero. */
static char dv_at(const decval *d, int p) {
    int i = d->decpt - 1 - p;
    if (i >= 0 && i < d->nd) return d->digits[i];
    return '0';
}

/* Round in place to the `keep` most significant digits, half-to-even, against
 * the one look-ahead digit plus everything after it. On return the string
 * holds exactly the window (nd == keep, or 1 after a full rollover of an
 * empty window), so no pre-rounding digit can leak into any output; and
 * *decExp carries the leading exponent, with decpt == *decExp + 1 restored
 * by the %f caller. keep == 0 (a %.0f of a value below 1) is allowed: a
 * round-up grows the window to the single digit "1" and bumps the exponent. */
static void dv_round(decval *d, int keep, int *decExp) {
    char next = (keep < d->nd) ? d->digits[keep] : '0';
    int sticky = 0, roundUp, i;
    for (i = keep + 1; i < d->nd; i++)
        if (d->digits[i] != '0') { sticky = 1; break; }
    if (next > '5') roundUp = 1;
    else if (next < '5') roundUp = 0;
    else if (sticky) roundUp = 1;
    else roundUp = (keep > 0) ? ((d->digits[keep - 1] - '0') & 1) : 0;
    if (roundUp) {
        i = keep - 1;
        while (i >= 0 && d->digits[i] == '9') { d->digits[i] = '0'; i--; }
        if (i >= 0) d->digits[i]++;
        else {
            d->digits[0] = '1';
            for (i = 1; i < keep; i++) d->digits[i] = '0';
            (*decExp)++;
            if (keep == 0) keep = 1;
        }
    }
    for (i = d->nd; i < keep; i++) d->digits[i] = '0';
    d->nd = keep;
    /* restore the decpt == leading-exponent + 1 invariant after a rollover:
     * every output path reads digits through decExp (or decpt) and assumes
     * the two agree */
    d->decpt = *decExp + 1;
}

/* --- the formatter --------------------------------------------------------- */

/* spec is the printf conversion letter ('f','e','g' or the uppercase forms).
 * prec is the precision; hasPrec says whether '.' was written at all (a
 * missing precision defaults to 6, per C99). alt is '#'. Returns the character
 * count; when the text needs more than cap it returns the NEGATED need so the
 * caller can retry with a bigger buffer. Exponent letter and nan/inf follow
 * the conversion's case. */
int __goclib_tf128_fmt(char *buf, unsigned long cap, const goc_tf128 *v,
                       int spec, int prec, int hasPrec, int alt) {
    unsigned long long hi = v->hi, lo = v->lo;
    int sign = (int)(hi >> 63);
    int upper = (spec == 'E' || spec == 'F' || spec == 'G');
    unsigned int e = (unsigned int)((hi >> 48) & 0x7FFFull);
    int mode = (spec == 'f' || spec == 'F') ? 0
             : (spec == 'e' || spec == 'E') ? 1 : 2;
    char out[DIGITS_MAX + 64];
    int on = 0;
    decval dv;
    int decExp, isZero, printExp = 0;

    if (prec < 0) { prec = 0; hasPrec = 1; }

    /* Fixed stack buffer: refuse up front what cannot fit (the caller retries
     * from a heap buffer). %f is bounded by 4933 integer digits plus prec
     * fraction digits; the exponential forms by prec plus punctuation. */
    {
        long need = (mode == 0) ? 4935 + 2 + (long)prec
                                : (long)prec + 24;
        if (need > (long)(DIGITS_MAX + 32)) return (int)(-need);
    }

    /* inf / nan: spelled per the conversion's case. */
    if (e == 0x7FFFull) {
        const char *s = (((hi & 0x0000FFFFFFFFFFFFull) | lo) == 0) ? "inf" : "nan";
        if (sign) out[on++] = '-';
        for (; *s; s++) {
            char c = *s;
            if (upper && c >= 'a' && c <= 'z') c = (char)(c - 'a' + 'A');
            out[on++] = c;
        }
        goto copyout;
    }

    /* Build the exact decimal expansion of |value| = m * 2^e2. */
    {
        unsigned long long s_lo = lo;
        unsigned long long s_hi = (e != 0 ? 0x0001000000000000ull : 0ull)
                                 | (hi & 0x0000FFFFFFFFFFFFull);
        int e2 = (e != 0 ? (int)e : 1) - 16383 - 112;
        bi N;
        N.w[0] = (unsigned int)s_lo;
        N.w[1] = (unsigned int)(s_lo >> 32);
        N.w[2] = (unsigned int)s_hi;
        N.w[3] = (unsigned int)(s_hi >> 32);
        N.n = 4;
        while (N.n > 0 && N.w[N.n - 1] == 0) N.n--;
        if (N.n == 0) {
            /* signed zero */
            dv.digits[0] = '0';
            dv.nd = 1;
            dv.decpt = 1;
        } else if (e2 >= 0) {
            bi_shl_bits(&N, e2);
            dv.nd = bi_to_digits(&N, dv.digits);
            dv.decpt = dv.nd;             /* an integer: point after the last digit */
        } else {
            int k = -e2;
            int i;
            for (i = 0; i < k; i++) bi_mul_small(&N, 5);
            dv.nd = bi_to_digits(&N, dv.digits);
            dv.decpt = dv.nd - k;         /* the point sits k places from the right */
        }
    }

    isZero = (dv.nd == 1 && dv.digits[0] == '0');
    decExp = isZero ? 0 : dv.decpt - 1;

    if (mode == 2) {
        /* %g: precision is significant digits (0 -> 1, default 6). Round ONCE,
         * decide the style from the ROUNDED exponent (C99), then only strip
         * trailing zeros when '#' is absent. */
        if (!hasPrec) prec = 6;
        if (prec == 0) prec = 1;
        if (!isZero) dv_round(&dv, prec, &decExp);
        if (decExp < -4 || decExp >= prec) {
            /* exponential style: prec-1 fraction digits */
            int frac = prec - 1;
            if (sign) out[on++] = '-';
            out[on++] = dv.digits[0];
            if (!alt) {
                while (frac > 0 && dv_at(&dv, decExp - frac) == '0') frac--;
            }
            if (frac > 0) {
                int j;
                out[on++] = '.';
                for (j = 1; j <= frac; j++) out[on++] = dv_at(&dv, decExp - j);
            } else if (alt) {
                out[on++] = '.';
            }
            printExp = 1;
        } else {
            /* fixed style: prec-1-decExp fraction digits */
            int frac = prec - 1 - decExp;
            int j;
            if (sign) out[on++] = '-';
            if (decExp < 0) out[on++] = '0';
            else {
                for (j = 0; j <= decExp; j++) out[on++] = dv_at(&dv, decExp - j);
            }
            if (!alt) {
                while (frac > 0 && dv_at(&dv, -frac) == '0') frac--;
            }
            if (frac > 0) {
                out[on++] = '.';
                for (j = 1; j <= frac; j++) out[on++] = dv_at(&dv, -j);
            } else if (alt) {
                out[on++] = '.';
            }
        }
    } else if (mode == 1) {
        /* %e: d.ddd...e±XX, prec fraction digits (default 6). */
        if (!hasPrec) prec = 6;
        if (!isZero) dv_round(&dv, prec + 1, &decExp);
        if (sign) out[on++] = '-';
        out[on++] = dv.digits[0];
        if (prec > 0 || alt) {
            int j;
            out[on++] = '.';
            for (j = 1; j <= prec; j++) out[on++] = dv_at(&dv, decExp - j);
        }
        printExp = 1;
    } else {
        /* %f: integer part, point, prec fraction digits (default 6).
         * The rounding cut sits at position -prec, i.e. string index
         * decpt-1+prec -- keep = decpt+prec, NOT prec: for a value below
         * 1 the string's leading digits start deep in the fraction, and
         * rounding at string index prec would land in the exact-zero
         * region beyond the expansion, silently turning %.1000f into a
         * truncation (libquadmath's answer showed the missing +1). */
        long keepL;
        int j;
        if (!hasPrec) prec = 6;
        keepL = (long)dv.decpt + prec;
        if (keepL > DIGITS_MAX + 16) return (int)(-keepL);
        if (!isZero && keepL >= 0) dv_round(&dv, (int)keepL, &decExp);
        if (sign) out[on++] = '-';
        if (dv.decpt <= 0) {
            out[on++] = '0';
        } else {
            for (j = 0; j < dv.decpt; j++) out[on++] = dv_at(&dv, dv.decpt - 1 - j);
        }
        if (prec > 0 || alt) {
            out[on++] = '.';
            for (j = 0; j < prec; j++) out[on++] = dv_at(&dv, -1 - j);
        }
    }

    if (printExp) {
        out[on++] = upper ? 'E' : 'e';
        out[on++] = decExp < 0 ? '-' : '+';
        {
            int a = decExp < 0 ? -decExp : decExp;
            char eb[8];
            int en = 0;
            if (a == 0) eb[en++] = '0';
            while (a > 0) { eb[en++] = (char)('0' + a % 10); a /= 10; }
            while (en < 2) eb[en++] = '0';
            while (en > 0) out[on++] = eb[--en];
        }
    }

copyout:
    {
        unsigned long need = (unsigned long)on;
        unsigned long j;
        if (need > cap) return (int)(-(long)need);
        for (j = 0; j < need; j++) buf[j] = out[j];
        return (int)on;
    }
}

/* --- the parser ------------------------------------------------------------ */

/* Significant digits kept exactly; beyond that a nonzero dropped digit only
 * forces the sticky bit (see the header comment for why that preserves
 * correct rounding far beyond binary128's 34 digits). The limb-based test
 * holds at most ~1290 digits. */
#define SIG_KEEP 1200

/* Decimal position of the leading digit beyond +-this saturates to inf/zero
 * before any heavy arithmetic, keeping every big integer bounded (5^m with
 * m <= ~6400). The true decimal range of binary128 is [-4966, 4932]; the
 * clamp sits far enough out that everything between -- subnormal rounding
 * included -- is decided by the exact arithmetic. */
#define EXP_CLAMP 5200

/* Parse a decimal floating-point token: [-+]?digits[.digits][(e|E)[+-]digits].
 * Returns 0 and fills *v on success; returns 1 when the token carries no
 * digit at all (the caller decides whether that is an error). Overflow
 * produces the correctly signed infinity, underflow the correctly signed
 * zero -- both are the scanf-obligatory results. */
int __goclib_tf128_parse(const char *s, goc_tf128 *v) {
    int sign = 0, seenSig = 0, anyDigit = 0, tailSticky = 0;
    long fracCount = 0, exp10 = 0, k, kept = 0, dropped = 0, i;
    bi D;

    if (*s == '+') s++;
    else if (*s == '-') { sign = 1; s++; }

    bi_set(&D, 0);
    for (; *s >= '0' && *s <= '9'; s++) {
        anyDigit = 1;
        if (!seenSig && *s == '0') continue;       /* leading zero: no value */
        seenSig = 1;
        if (D.n * 9 < SIG_KEEP) {
            bi_mul_small(&D, 10);
            bi_add_small(&D, (unsigned int)(*s - '0'));
            kept++;
        } else {
            /* Dropped digits keep their decimal weight: value = D*10^k with
             * k gaining one per dropped digit. Without this, a 1300-digit
             * token parsed 10^(dropped) too small -- tailSticky only said
             * "something nonzero was dropped", not how many decades. */
            dropped++;
            if (*s != '0') tailSticky = 1;
        }
    }
    if (*s == '.') {
        s++;
        for (; *s >= '0' && *s <= '9'; s++) {
            anyDigit = 1;
            fracCount++;
            if (!seenSig && *s == '0') continue;
            seenSig = 1;
            if (D.n * 9 < SIG_KEEP) {
                bi_mul_small(&D, 10);
                bi_add_small(&D, (unsigned int)(*s - '0'));
                kept++;
            } else {
                dropped++;
                if (*s != '0') tailSticky = 1;
            }
        }
    }
    if (!anyDigit) {
        /* "inf", "infinity", "nan" (case-insensitive), what scanf %f also
         * accepts; the optional sign was consumed above */
        if ((s[0] == 'i' || s[0] == 'I') &&
            (s[1] == 'n' || s[1] == 'N') && (s[2] == 'f' || s[2] == 'F')) {
            s += 3;
            if ((s[0] == 'i' || s[0] == 'I') &&
                (s[1] == 'n' || s[1] == 'N') &&
                (s[2] == 'i' || s[2] == 'I') &&
                (s[3] == 't' || s[3] == 'T') &&
                (s[4] == 'y' || s[4] == 'Y')) s += 5;
            v->lo = 0;
            v->hi = 0x7FFF000000000000ull | (sign ? 0x8000000000000000ull : 0ull);
            return 0;
        }
        if ((s[0] == 'n' || s[0] == 'N') &&
            (s[1] == 'a' || s[1] == 'A') && (s[2] == 'n' || s[2] == 'N')) {
            /* quiet NaN: sign | exp all ones | the quiet bit; the payload
             * beyond the quiet bit is unspecified anyway */
            v->lo = 0;
            v->hi = 0x7FFF800000000000ull | (sign ? 0x8000000000000000ull : 0ull);
            return 0;
        }
        return 1;
    }
    if (*s == 'e' || *s == 'E') {
        int esign = 1, edigit = 0;
        long ev = 0;
        s++;
        if (*s == '+') s++;
        else if (*s == '-') { esign = -1; s++; }
        while (*s >= '0' && *s <= '9') {
            if (ev < 100000) ev = ev * 10 + (*s - '0');
            edigit = 1;
            s++;
        }
        if (edigit) exp10 = esign * ev;
    }

    k = exp10 - fracCount + dropped;
    /* Trailing zeros of D are whole decades: fold them into k. The test must
     * be non-destructive: bi_div_small turns a single-digit D into zero even
     * when the remainder is nonzero, and the zero check below would then
     * swallow "1", "2" or "1e300" (all found by the differential test). */
    while (!bi_is_zero(&D) && bi_mod_small(&D, 10) == 0) {
        bi_div_small(&D, 10);
        k++;
        kept--;
    }
    if (bi_is_zero(&D)) {
        v->lo = 0;
        v->hi = sign ? 0x8000000000000000ull : 0ull;
        return 0;
    }

    /* Decimal position of the leading digit; saturate far outside the
     * representable range before doing any heavy arithmetic. */
    {
        long leadPos = kept - 1 + k;
        if (leadPos > EXP_CLAMP) {
            v->lo = 0;
            v->hi = 0x7FFF000000000000ull | (sign ? 0x8000000000000000ull : 0ull);
            return 0;
        }
        if (leadPos < -EXP_CLAMP) {
            v->lo = 0;
            v->hi = sign ? 0x8000000000000000ull : 0ull;
            return 0;
        }
    }

    /* Round to 113 significand bits. sig ends with its top bit at position
     * 112 and binexp is the power of two that bit carries:
     * value = sig * 2^binexp (+ a fraction the sticky flags describe). */
    {
        bi sig;
        long binexp, unbiased;
        int round = 0, sticky = 0, up;

        if (k >= 0) {
            /* M = D * 5^k, value = M * 2^k: an exact integer, plain shift. */
            long L;
            sig = D;
            for (i = 0; i < k; i++) bi_mul_small(&sig, 5);
            L = bi_bitlen(&sig);
            if (L > 113) bi_shr_round(&sig, (int)(L - 113), &round, &sticky);
            else         bi_shl_bits(&sig, (int)(113 - L));
            binexp = k + (L - 113);
            if (tailSticky) sticky = 1;
        } else {
            /* value = D / 5^m * 2^k. N = floor(D * 2^S / 5^m) with the
             * quotient aimed above 113 bits; the exact remainder's zero-ness
             * is the fraction sticky (never an exact half: 5^m is odd). */
            long m = -k, S, L;
            int fracSticky = 0;
            /* bitlen(5^m) = floor(m * log2(5)) + 1; 232/100 is log2(5)
             * truncated to two decimals, so the estimate never overstates */
            S = 130 + m * 232 / 100 + 1 - bi_bitlen(&D);
            sig = D;
            if (S >= 0) {
                bi_shl_bits(&sig, (int)S);
            } else {
                /* D is already above the 2^130 target: right-shift instead.
                 * The dropped low bits are real value -- fold their non-zero
                 * into the sticky chain (they are far below the final guard
                 * bit, so sticky is all the rounding ever needs from them). */
                int r2, s2;
                bi_shr_round(&sig, (int)-S, &r2, &s2);
                if (r2 || s2) fracSticky = 1;
            }
            for (i = 0; i < m; i++)
                if (bi_div_small(&sig, 5) != 0) fracSticky = 1;
            L = bi_bitlen(&sig);
            if (L > 113) bi_shr_round(&sig, (int)(L - 113), &round, &sticky);
            else         bi_shl_bits(&sig, (int)(113 - L));
            binexp = k - S + (L - 113);
            if (fracSticky || tailSticky) sticky = 1;
        }

        /* Half-to-even at 113 bits. */
        if (round) {
            if (sticky) up = 1;
            else up = (int)(sig.w[0] & 1u);    /* exact tie: to even */
            if (up) {
                bi_add_small(&sig, 1);
                if (bi_bitlen(&sig) == 114) {  /* carried into 2^113 */
                    int r2, s2;
                    bi_shr_round(&sig, 1, &r2, &s2);
                    binexp++;
                }
            }
        }

        unbiased = binexp + 112;   /* value = 1.f * 2^unbiased */
        if (unbiased > 16383) {
            v->lo = 0;
            v->hi = 0x7FFF000000000000ull | (sign ? 0x8000000000000000ull : 0ull);
            return 0;
        }
        {
            unsigned long long fl = 0, fh = 0, expField;
            if (unbiased >= -16382) {
                expField = (unsigned long long)(unbiased + 16383);
            } else {
                /* subnormal: sig' = sig >> (-(unbiased + 16382)), rounded
                 * half-to-even. The shift is by a power of two, so exact
                 * ties are possible and the guard/sticky must stay exact. */
                long sh = -(unbiased + 16382);
                int r2, s2;
                if (sh > 0) {
                    bi_shr_round(&sig, (int)sh, &r2, &s2);
                    if (r2 && (s2 || (sig.w[0] & 1u))) bi_add_small(&sig, 1);
                }
                if (bi_bitlen(&sig) > 112) expField = 1;  /* smallest normal */
                else                       expField = 0;
            }
            /* stored fraction = the low 112 bits of sig (the 113th bit, the
             * implicit one at position 112, is not stored) */
            if (sig.n > 0) fl = sig.w[0];
            if (sig.n > 1) fl |= (unsigned long long)sig.w[1] << 32;
            if (sig.n > 2) fh = sig.w[2];
            if (sig.n > 3) fh |= (unsigned long long)sig.w[3] << 32;
            v->lo = fl;
            v->hi = ((unsigned long long)sign << 63)
                  | (expField << 48)
                  | (fh & 0x0000FFFFFFFFFFFFull);
            return 0;
        }
    }
}
