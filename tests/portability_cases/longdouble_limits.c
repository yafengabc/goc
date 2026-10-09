/* <float.h> for goc's binary128 long double (#50a).
 *
 * The LDBL_* macros describe the implementation's long double, so under goc
 * they have to be binary128: 113-bit significand, 15 exponent bits, subnormals
 * down to 2^-16494. When long double was a double in disguise these were
 * aliases of DBL_*, and a program that sized a buffer from LDBL_MAX_10_EXP got
 * a number 1600 decimal digits too small.
 *
 * The four value macros are pinned two ways, because a decimal golden only
 * proves the printed digits:
 *   - by bit pattern, memcpy'd out as the two little-endian halves. Any
 *     off-by-one in the exponent or a lost fraction bit shows up here even
 *     when the first six printed digits agree;
 *   - by the properties the constant is supposed to have: LDBL_EPSILON is the
 *     smallest x with 1+x > 1, LDBL_MIN/2 is still nonzero but subnormal,
 *     LDBL_TRUE_MIN/2 underflows to zero.
 *
 * Self-reporting: exit 0 and a final line of "OK".
 */
#include <stdio.h>
#include <float.h>
#include <string.h>

static int fails = 0;

static void checki(const char *name, long long got, long long want) {
    if (got != want) {
        printf("FAIL %s: got %lld want %lld\n", name, got, want);
        fails++;
    }
}

static void expects(const char *name, const char *got, const char *want) {
    if (strcmp(got, want) != 0) {
        printf("FAIL %s:\n  got  %s\n  want %s\n", name, got, want);
        fails++;
    }
}

/* True when x holds exactly the binary128 (hi, lo) bit pattern. */
static void checkbits(const char *name, long double x,
                      unsigned long long hi, unsigned long long lo) {
    unsigned long long w[2];
    memcpy(w, &x, 16);
    if (w[1] != hi || w[0] != lo) {
        printf("FAIL %s bits: got %016llx%016llx want %016llx%016llx\n",
               name, w[1], w[0], hi, lo);
        fails++;
    }
}

int main(void) {
    char buf[128];
    long double back;
    /* 36 significant digits: one more than LDBL_DIG claims round-trips, which
     * is what LDBL_DECIMAL_DIG (36) is for. */
    long double pi = 3.14159265358979323846264338327950288L;

    /* ---- integer macros ------------------------------------------------- */
    checki("FLT_RADIX", FLT_RADIX, 2);
    checki("FLT_ROUNDS", FLT_ROUNDS, 1);
    checki("LDBL_MANT_DIG", LDBL_MANT_DIG, 113);
    checki("LDBL_DIG", LDBL_DIG, 33);
    checki("LDBL_DECIMAL_DIG", LDBL_DECIMAL_DIG, 36);
    checki("LDBL_MIN_EXP", LDBL_MIN_EXP, -16381);
    checki("LDBL_MAX_EXP", LDBL_MAX_EXP, 16384);
    checki("LDBL_MIN_10_EXP", LDBL_MIN_10_EXP, -4931);
    checki("LDBL_MAX_10_EXP", LDBL_MAX_10_EXP, 4932);
    checki("LDBL_HAS_SUBNORM", LDBL_HAS_SUBNORM, 1);
    checki("LDBL_IS_IEC_60559", LDBL_IS_IEC_60559, 1);
    /* The float/double families gained the C23 width macros alongside. */
    checki("FLT_DECIMAL_DIG", FLT_DECIMAL_DIG, 9);
    checki("DBL_DECIMAL_DIG", DBL_DECIMAL_DIG, 17);
    checki("FLT_MIN_10_EXP", FLT_MIN_10_EXP, -37);
    checki("FLT_MAX_10_EXP", FLT_MAX_10_EXP, 38);
    checki("DBL_MIN_10_EXP", DBL_MIN_10_EXP, -307);
    checki("DBL_MAX_10_EXP", DBL_MAX_10_EXP, 308);

    /* ---- value macros, by bit pattern ----------------------------------- */
    /* LDBL_MAX: biased exponent 32766 (32767 is inf/nan), fraction all ones. */
    checkbits("LDBL_MAX", LDBL_MAX, 0x7FFEFFFFFFFFFFFFULL, 0xFFFFFFFFFFFFFFFFULL);
    /* LDBL_MIN: smallest normal -- exponent field 1, fraction 0. */
    checkbits("LDBL_MIN", LDBL_MIN, 0x0001000000000000ULL, 0x0000000000000000ULL);
    /* LDBL_EPSILON = 2^-112: biased 16383-112 = 16271 = 0x3F8F. */
    checkbits("LDBL_EPSILON", LDBL_EPSILON, 0x3F8F000000000000ULL, 0x0000000000000000ULL);
    /* LDBL_TRUE_MIN: exponent field 0, lowest fraction bit set. */
    checkbits("LDBL_TRUE_MIN", LDBL_TRUE_MIN, 0x0000000000000000ULL, 0x0000000000000001ULL);
    checkbits("LDBL_NORM_MAX", LDBL_NORM_MAX, 0x7FFEFFFFFFFFFFFFULL, 0xFFFFFFFFFFFFFFFFULL);
    /* DBL_TRUE_MIN is 2^-1074; widened to binary128 that is exponent field
     * 16383-1074 = 15309 = 0x3BCD, not a subnormal pattern. */
    checkbits("DBL_TRUE_MIN widened", (long double)DBL_TRUE_MIN,
              0x3BCD000000000000ULL, 0x0000000000000000ULL);

    /* ---- the same four, as text ----------------------------------------- */
    sprintf(buf, "%.6Le", LDBL_MAX);
    expects("LDBL_MAX %.6Le", buf, "1.189731e+4932");
    sprintf(buf, "%.6Le", LDBL_MIN);
    expects("LDBL_MIN %.6Le", buf, "3.362103e-4932");
    sprintf(buf, "%.8Le", LDBL_EPSILON);
    expects("LDBL_EPSILON %.8Le", buf, "1.92592994e-34");
    sprintf(buf, "%.6Le", LDBL_TRUE_MIN);
    expects("LDBL_TRUE_MIN %.6Le", buf, "6.475175e-4966");

    /* ---- properties ------------------------------------------------------ */
    checki("LDBL_MAX*2 overflows", LDBL_MAX * 2.0L > LDBL_MAX, 1);
    checki("LDBL_MAX+LDBL_MAX overflows", LDBL_MAX + LDBL_MAX > LDBL_MAX, 1);
    checki("1+LDBL_EPSILON > 1", 1.0L + LDBL_EPSILON > 1.0L, 1);
    /* 1 + 2^-113 is exactly halfway: ties-to-even keeps it at 1.0. */
    checki("1+LDBL_EPSILON/2 == 1", 1.0L + LDBL_EPSILON / 2.0L == 1.0L, 1);
    checki("LDBL_MIN/2 > 0", LDBL_MIN / 2.0L > 0.0L, 1);
    checki("LDBL_MIN/2 < LDBL_MIN", LDBL_MIN / 2.0L < LDBL_MIN, 1);
    checki("LDBL_TRUE_MIN > 0", LDBL_TRUE_MIN > 0.0L, 1);
    checki("LDBL_TRUE_MIN/2 == 0", LDBL_TRUE_MIN / 2.0L == 0.0L, 1);
    checki("LDBL_TRUE_MIN < LDBL_MIN", LDBL_TRUE_MIN < LDBL_MIN, 1);
    checki("DBL_TRUE_MIN > 0", DBL_TRUE_MIN > 0.0, 1);
    checki("DBL_TRUE_MIN/2 == 0", DBL_TRUE_MIN / 2.0 == 0.0, 1);

    /* ---- LDBL_DIG and LDBL_DECIMAL_DIG -----------------------------------
     * The two macros promise opposite directions, and conflating them is the
     * usual mistake:
     *   LDBL_DIG  (33): a 33-significant-digit DECIMAL survives text -> LD ->
     *                   text. (The reverse -- LD -> 33 digits -> LD -- is NOT
     *                    guaranteed: 33 digits do not pin down a 113-bit
     *                    significand, which needs 34-35.)
     *   LDBL_DECIMAL_DIG (36): a binary128 value survives LD -> 36 digits ->
     *                   LD, i.e. 36 digits is enough to name any value. */
    {
        const char *d33 = "1.23456789012345678901234567890123e-300";
        long double v;
        if (sscanf(d33, "%Lf", &v) != 1) {
            printf("FAIL scanf of the 33-digit literal\n");
            fails++;
        } else {
            sprintf(buf, "%.32Le", v);
            expects("LDBL_DIG round-trip", buf, d33);
        }
    }
    sprintf(buf, "%.35Le", pi);
    if (sscanf(buf, "%Lf", &back) != 1) {
        printf("FAIL scanf %.35Le: no conversion\n");
        fails++;
    } else if (back != pi) {
        printf("FAIL LDBL_DECIMAL_DIG round-trip (%s)\n", buf);
        fails++;
    }

    if (fails == 0) printf("OK\n");
    return fails == 0 ? 0 : 1;
}
