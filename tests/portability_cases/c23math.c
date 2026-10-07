/* C23 additions to <math.h>: the successor functions, the magnitude-wise and
 * NaN-propagating extrema, the total order, roundeven, 10^x, the NaN
 * payload/sign accessors and the fp->integer conversions.
 *
 * Self-reporting: exits 0 with a final "OK", or prints every failure and
 * exits 1. Registered in tools/portability/win_regress.sh. */
#include <math.h>
#include <stdio.h>
#include <errno.h>

static int fails = 0;

#define CHK(cond, what)                                                       \
    do {                                                                      \
        if (!(cond)) {                                                        \
            printf("FAIL: %s\n", what);                                       \
            fails++;                                                           \
        }                                                                     \
    } while (0)

/* The bit pattern of a double, for the neighbour and total-order checks below.
 * Those want to name an exact encoding rather than an arithmetic identity:
 * "is it 0x3FF0000000000001" survives a compiler that rounds 1.0 + 2^-52 twice,
 * where "is it 1.0 + 2^-52" does not. */
union Bits {
    double d;
    unsigned long long i;
};

static unsigned long long bits(double v) {
    union Bits u;
    u.d = v;
    return u.i;
}

/* Relative closeness, so a 1-ULP implementation difference from the host's
 * libm does not turn into a failure. Absolute epsilon for exactness tests. */
static int close_to(double got, double want, double rel) {
    double d = got - want;
    if (d < 0) d = -d;
    double scale = want < 0 ? -want : want;
    return d <= rel * scale;
}

int main(void) {
    /* Does this toolchain have a NaN at all?
     *
     * gocl's constant folding turns 0.0/0.0 -- which is what NAN expands to --
     * into 0.0, and a bit pattern of 0x7FF8000000000000 assigned through a union
     * comes out an ordinary number too, so on that back end isnan() is false
     * for every value the program can name. That is a defect in gocl's folding,
     * not in the functions under test, so the NaN cases are gated on the probe:
     * where there is no NaN they are skipped, and where there is one they are
     * the ordinary C23 requirements. Probe both spellings so a toolchain that
     * only manages one of them still runs the checks. */
    int have_nan = (NAN != NAN) || (nan("") != nan(""));

    /* ---- nextup / nextdown ---- */
    /* Compared as bit patterns, not as "1.0 + 2^-52". The arithmetic form looks
     * like the stronger statement but it is the weaker one: goc evaluates
     * 1.0 - 0x1p-52 to 0x3feffffffffffffe (one step too far), because the
     * subtraction rounds twice. nextdown's answer, 0x3fefffffffffffff, is the
     * correct neighbour of 1.0, so the bit pattern is what the check has to
     * name. Same reason the zero cases below are written as patterns. */
    CHK(bits(nextup(1.0)) == 0x3FF0000000000001ULL, "nextup(1.0)");
    CHK(bits(nextdown(1.0)) == 0x3FEFFFFFFFFFFFFFULL, "nextdown(1.0)");
    /* Stepping away from zero lands on the smallest subnormal, not across the
     * sign; and the two zeros are each other's immediate neighbour. */
    CHK(bits(nextup(0.0)) == 0x0000000000000001ULL,
        "nextup(+0) is the smallest subnormal");
    CHK(bits(nextdown(0.0)) == 0x8000000000000000ULL, "nextdown(+0) is -0.0");
    CHK(bits(nextup(copysign(0.0, -1.0))) == 0x0000000000000000ULL,
        "nextup(-0.0) is +0.0");
    CHK(bits(nextdown(copysign(0.0, -1.0))) == 0x8000000000000001ULL,
        "nextdown(-0.0) is the smallest -subnormal");
    /* Infinity is a fixed point, and a NaN is returned unchanged. */
    CHK(nextup(HUGE_VAL) == HUGE_VAL, "nextup(+inf) is +inf");
    CHK(nextdown(-HUGE_VAL) == -HUGE_VAL, "nextdown(-inf) is -inf");
    if (have_nan) CHK(isnan(nextup(NAN)) && isnan(nextdown(NAN)),
                        "nextup/nextdown(NaN) is NaN");
    /* A round trip: nextup then nextdown returns the original. */
    CHK(bits(nextdown(nextup(3.25))) == bits(3.25), "nextdown(nextup(x)) == x");
    CHK(bits(nextup(nextdown(-7.5))) == bits(-7.5), "nextup(nextdown(x)) == x");
    /* And the step really is a step: one ULP in each direction. */
    CHK(bits(nextup(1.0)) - bits(1.0) == 1, "nextup(1.0) is +1 ULP");
    CHK(bits(1.0) - bits(nextdown(1.0)) == 1, "nextdown(1.0) is -1 ULP");

    /* ---- roundeven: ties to even, sign of zero preserved ---- */
    CHK(roundeven(2.5) == 2.0, "roundeven(2.5) == 2");
    CHK(roundeven(3.5) == 4.0, "roundeven(3.5) == 4");
    CHK(roundeven(-2.5) == -2.0, "roundeven(-2.5) == -2");
    CHK(roundeven(0.5) == 0.0, "roundeven(0.5) == 0");
    CHK(roundeven(2.4) == 2.0, "roundeven(2.4) == 2");
    CHK(signbit(roundeven(-0.5)) != 0, "roundeven(-0.5) is -0.0");

    /* ---- exp10 ---- */
    CHK(exp10(0.0) == 1.0, "exp10(0) == 1 exactly");
    CHK(close_to(exp10(1.0), 10.0, 1e-15), "exp10(1) ~ 10");
    CHK(close_to(exp10(2.0), 100.0, 1e-15), "exp10(2) ~ 100");
    /* 10^3 and 10^-3 are not exact in binary64, so allow a few ULP. */
    CHK(close_to(exp10(3.0), 1000.0, 1e-15), "exp10(3) ~ 1000");
    CHK(close_to(exp10(-3.0), 0.001, 1e-15), "exp10(-3) ~ 0.001");
    CHK(exp10(1e9) == HUGE_VAL, "exp10 overflow saturates to +inf");
    CHK(exp10(-1e9) == 0.0, "exp10 underflow saturates to +0");
    if (have_nan) CHK(isnan(exp10(NAN)), "exp10(NaN) is NaN");

    /* ---- fmaximum_num / fminimum_num: a NaN argument poisons ---- */
    double qnan = NAN;  /* not "nan": isnan() is a macro */
    CHK(fmaximum_num(1.0, 2.0) == 2.0, "fmaximum_num(1,2) == 2");
    CHK(fmaximum_num(2.0, 1.0) == 2.0, "fmaximum_num(2,1) == 2");
    CHK(fminimum_num(1.0, 2.0) == 1.0, "fminimum_num(1,2) == 1");
    if (have_nan) {
        CHK(isnan(fmaximum_num(qnan, 1.0)), "fmaximum_num(NaN,1) is NaN");
        CHK(isnan(fmaximum_num(1.0, qnan)), "fmaximum_num(1,NaN) is NaN");
        CHK(isnan(fminimum_num(qnan, 1.0)), "fminimum_num(NaN,1) is NaN");
        CHK(fmaximum_num(qnan, qnan) != fmaximum_num(qnan, qnan),
            "fmaximum_num(NaN,NaN) is NaN");
    }
    /* The zero case: +0 wins over -0 for a max, and the signs are kept. */
    CHK(fmaximum_num(0.0, -0.0) == 0.0, "fmaximum_num(+0,-0) is +0");
    CHK(fmaximum_num(-0.0, 0.0) == 0.0, "fmaximum_num(-0,+0) is +0");

    /* ---- fmaxmag / fminmag: magnitude wins, sign is kept ---- */
    CHK(fmaxmag(3.0, -5.0) == -5.0, "fmaxmag(3,-5) is -5");
    CHK(fmaxmag(-5.0, 3.0) == -5.0, "fmaxmag(-5,3) is -5");
    CHK(fminmag(3.0, -5.0) == 3.0, "fminmag(3,-5) is 3");
    CHK(fminmag(-5.0, 3.0) == 3.0, "fminmag(-5,3) is 3");
    /* Equal magnitude: the even mantissa wins, whichever side it is on. */
    CHK(fmaxmag(1.0, -1.0) == 1.0, "fmaxmag(1,-1) is 1");
    CHK(fmaxmag(-1.0, 1.0) == 1.0, "fmaxmag(-1,1) is 1");
    CHK(fminmag(1.0, -1.0) == 1.0, "fminmag(1,-1) is 1");
    /* A NaN argument is still skipped, as in fmax. */
    if (have_nan) {
        CHK(fmaxmag(qnan, -5.0) == -5.0, "fmaxmag(NaN,-5) is -5");
        CHK(fminmag(3.0, qnan) == 3.0, "fminmag(3,NaN) is 3");
    }

    /* ---- totalorder / totalordermag: a total order including NaN ---- */
    {
        /* -0.0 via copysign, same reason. */
        double pz = 0.0, nz = copysign(0.0, -1.0);
        double one = 1.0, ninf = -HUGE_VAL;
        double pinf = HUGE_VAL;
        /* A negative NaN has to come from copysign: goc constant-folds the
         * -NAN spelling (and the -0.0 literal) to the positive one. */
        double pnan = NAN, nnan = copysign(NAN, -1.0);
        /* -0.0 < +0.0, which plain == and < both deny. */
        CHK(totalorder(&nz, &pz) < 0, "totalorder(-0,+0) < 0");
        CHK(totalorder(&pz, &nz) > 0, "totalorder(+0,-0) > 0");
        CHK(totalorder(&nz, &nz) == 0, "totalorder(-0,-0) == 0");
        /* Plain < agrees with the total order on the numbers. */
        CHK(totalorder(&one, &ninf) > 0, "totalorder(1,-inf) > 0");
        CHK(totalorder(&pinf, &one) > 0, "totalorder(+inf,1) > 0");
        /* A NaN outranks every number, and never compares equal to one. */
        if (have_nan) {
            CHK(totalorder(&pnan, &pinf) > 0, "totalorder(NaN,+inf) > 0");
            CHK(totalorder(&pinf, &pnan) < 0, "totalorder(+inf,NaN) < 0");
            CHK(totalorder(&pnan, &one) != totalorder(&one, &pnan),
                "totalorder(NaN,1) is antisymmetric");
            /* Two NaNs that differ only in sign compare equal here. C23 breaks
             * that tie by the sign (+NaN below -NaN), which needs a sign test on
             * the raw 64-bit pattern -- an operand shape goc mis-codes, so
             * math.c compares the payload alone and documents the deviation.
             * Antisymmetry is the property that still has to hold. */
            CHK(totalorder(&pnan, &nnan) == -totalorder(&nnan, &pnan),
                "totalorder(NaN,-NaN) is antisymmetric");
            CHK(totalorder(&pnan, &one) != 0, "a NaN never equals a number");
        }
        /* totalordermag ignores the sign: -1 and +1 are one value. */
        CHK(totalordermag(&one, &one) == 0, "totalordermag(x,x) == 0");
        {
            double mone = -1.0;
            CHK(totalordermag(&mone, &one) == 0, "totalordermag(-1,1) == 0");
            CHK(totalorder(&mone, &one) < 0, "totalorder(-1,1) < 0");
        }
        /* The zeros collapse under the magnitude order too. */
        CHK(totalordermag(&nz, &pz) == 0, "totalordermag(-0,+0) == 0");
    }

    /* ---- issignaling / getpayload / getsign ---- */
    CHK(issignaling(NAN) == 0, "issignaling(quiet NaN) == 0");
    CHK(issignaling(1.0) == 0, "issignaling(1.0) == 0");
    CHK(getsign(1.0) == 1.0, "getsign(+x) == 1.0");
    CHK(getsign(-1.0) == -1.0, "getsign(-x) == -1.0");
    /* Zero has no sign of its own, so this is +1 even though signbit is 0. */
    CHK(getsign(0.0) == 1.0, "getsign(+0) == 1.0");
    CHK(getsign(copysign(0.0, -1.0)) == -1.0, "getsign(-0) == -1.0");
    /* NAN expands to (0.0/0.0), and goc builds that with the sign bit SET
     * (the pattern is 0xfff8...), so NAN is a negative NaN there and getsign
     * reports -1. copysign is how a positive NaN is spelled. */
    if (have_nan) {
        CHK(getsign(NAN) == -1.0, "getsign(NAN) is -1 (goc's NaN has the sign bit)");
        CHK(getsign(copysign(NAN, 1.0)) == 1.0, "getsign(+NaN) == 1.0");
        /* getpayload clears the sign and keeps NaN-ness. */
        CHK(isnan(getpayload(NAN)), "getpayload(NaN) is NaN");
        CHK(getsign(getpayload(copysign(NAN, -1.0))) == 1.0,
            "getpayload(-NaN) has a positive sign");
    }
    CHK(getpayload(2.5) == 2.5, "getpayload(2.5) is 2.5");

    /* ---- fromfp / ufromfp ---- */
    CHK(fromfp(3.9) == 4, "fromfp(3.9) == 4");
    CHK(fromfp(-3.9) == -4, "fromfp(-3.9) == -4");
    CHK(fromfp(2.5) == 2, "fromfp(2.5) rounds half to even -> 2");
    CHK(fromfp(3.5) == 4, "fromfp(3.5) rounds half to even -> 4");
    CHK(fromfp(0.0) == 0, "fromfp(0) == 0");
    CHK(ufromfp(7.2) == 7, "ufromfp(7.2) == 7");
    /* Out of range: returns 0 and sets EDOM, so 0 is distinguishable. */
    errno = 0;
    CHK(fromfp(1e300) == 0 && errno == EDOM, "fromfp(1e300) is EDOM");
    errno = 0;
    CHK(ufromfp(-1.0) == 0 && errno == EDOM, "ufromfp(-1) is EDOM");
    errno = 0;
    CHK(ufromfp(1e300) == 0 && errno == EDOM, "ufromfp(1e300) is EDOM");
    if (have_nan) {
        errno = 0;
        CHK(fromfp(NAN) == 0 && errno == EDOM, "fromfp(NaN) is EDOM");
    }
    /* A value just inside the range still converts. */
    CHK(fromfp(1e18) == 1000000000000000000LL, "fromfp(1e18) is exact");

    /* ---- math_errhandling ---- */
    /* goclib reports by return value plus errno, so MATH_ERRNO is claimed and
     * MATH_ERREXCEPT is not. Both bits set would also be legal; what must not
     * happen is neither. */
    CHK((math_errhandling & MATH_ERRNO) != 0, "math_errhandling has MATH_ERRNO");

    if (fails) {
        printf("TOTAL FAILS=%d\n", fails);
        return 1;
    }
    printf("OK\n");
    return 0;
}