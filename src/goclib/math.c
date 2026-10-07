#include "goclib.h"

/* ------------------------------ <math.h> -------------------------------- */

double fabs(double x) {
    return x < 0.0 ? -x : x;
}

/*
 * (long)x truncates toward zero, but the conversion saturates once x grows
 * past the 64-bit range, which would turn a huge finite value into a bogus
 * integer. Anything that large is already integral, so hand it back
 * unchanged and let floor/ceil return it as-is.
 */
static double trunc_to_zero(double x) {
    if (x >= 9.0e18 || x <= -9.0e18) return x;
    return (double)(long)x;
}

double floor(double x) {
    double t = trunc_to_zero(x);
    /* Only a negative non-integral x needs a nudge: truncation went up. */
    if (x < 0.0 && t > x) return t - 1.0;
    return t;
}

double ceil(double x) {
    double t = trunc_to_zero(x);
    if (x > 0.0 && t < x) return t + 1.0;
    return t;
}

/*
 * The quotient is truncated toward zero (not floored), which is what makes
 * the remainder carry the sign of x: fmod(-7,3) is -1, not the +2 that
 * floor-based Python-style modulo would give.
 */
double fmod(double x, double y) {
    double n;
    double r;
    if (x != x || y != y || y == 0.0 || isinf(x)) return NAN;
    if (isinf(y)) return x;
    n = trunc(x / y);
    r = x - n * y;
    if (r == 0.0) return x < 0.0 ? -0.0 : 0.0;   /* sign of x, per C */
    return r;
}

double sqrt(double x) {
    double scale;
    double r;
    double last;
    int i;
    if (x < 0.0) return 0.0;
    if (x == 0.0) return 0.0;
    if (x != x) return x;                   /* NaN is the only x != x */
    /*
     * Newton's method converges quadratically, but only from a seed near
     * the root -- starting at r = x on 1e300 would spend hundreds of
     * iterations halving its way down. Scaling x into [1,4) first puts the
     * answer in [1,2), so 0.5*(x+1) is within a factor of two and the loop
     * below settles in about five passes. scale carries the powers of two
     * that were divided out.
     */
    scale = 1.0;
    while (x >= 4.0) {
        x = x * 0.25;
        scale = scale * 2.0;
    }
    while (x < 1.0) {
        x = x * 4.0;
        scale = scale * 0.5;
    }
    r = 0.5 * (x + 1.0);
    for (i = 0; i < 40; i++) {
        last = r;
        r = 0.5 * (r + x / r);
        if (r == last) break;
    }
    return r * scale;
}

/* ===========================================================================
 * Exponent/mantissa access, and everything built on top of it.
 *
 * frexp/ldexp go through the IEEE-754 bit pattern rather than a scaling loop:
 * a loop would need ~1000 iterations to bring 1e300 down into [0.5,1), and
 * every step of it would be a rounding opportunity. Reading and writing the
 * biased exponent directly is exact, so exp/log/pow -- which all reduce
 * their argument to a small range first -- inherit that exactness.
 *
 * The one union in the file is the whole reason this works: goc lays out a
 * union of double/long long as one 8-byte object, so writing .d and reading
 * .i is a bit-level reinterpret, not a conversion.
 * ======================================================================== */

union DU {
    double d;
    /* unsigned, not long long: goc's signed 64-bit left shift is truncated
     * to 32 bits (1LL << 52 is 0), so every shift here has to be unsigned. */
    unsigned long long i;
};

/* Two exact powers of two used to cross the subnormal boundary: any
 * subnormal times 2^53 is normal, and 2^-53 is exact. */
#define P53  9007199254740992.0            /* 2^53  */
#define N53  (1.0 / 9007199254740992.0)    /* 2^-53 */

/* Mantissa and sign bits with the exponent field cleared. */
#define MANT_MASK 0x800FFFFFFFFFFFFFULL
/* Biased exponent 1022 (value in [0.5,1)) and 1 (just above subnormal),
 * pre-shifted: writing these avoids the shift entirely. */
#define EXP_1022 0x3FE0000000000000ULL
#define EXP_1    0x0010000000000000ULL

/* 2^n by chunked multiplication. Only the subnormal edges of ldexp reach
 * this, and |n| there is bounded by ~1100, so the loop is cheap. */
static double pow2i(int n) {
    double r = 1.0;
    if (n < 0) {
        int k = -n;
        while (k >= 53) { r = r * N53; k -= 53; }
        while (k > 0) { r = r * 0.5; k--; }
    } else {
        while (n >= 53) { r = r * P53; n -= 53; }
        while (n > 0) { r = r * 2.0; n--; }
    }
    return r;
}

/* True when x is 0, NaN or infinite -- the three inputs frexp/ldexp must
 * pass through untouched instead of decomposing. */
static int is_special(double x) {
    return x == 0.0 || x != x || isinf(x);
}

double frexp(double x, int *e) {
    union DU u;
    int biased;
    int exp = 0;
    if (is_special(x)) {
        if (e) *e = 0;
        return x;
    }
    u.d = x;
    biased = (int)((u.i >> 52) & 0x7FF);
    if (biased == 0) {                  /* subnormal: scale into range once */
        x = x * P53;
        exp = -53;
        u.d = x;
        biased = (int)((u.i >> 52) & 0x7FF);
    }
    exp += biased - 1022;               /* 1022 puts the result in [0.5,1) */
    u.i = (u.i & MANT_MASK) | EXP_1022;
    if (e) *e = exp;
    return u.d;
}

double ldexp(double x, int e) {
    union DU u;
    int biased;
    int nb;
    if (is_special(x)) return x;
    u.d = x;
    biased = (int)((u.i >> 52) & 0x7FF);
    if (biased == 0) {                  /* subnormal input: normalise first */
        x = x * P53;
        e -= 53;
        u.d = x;
        biased = (int)((u.i >> 52) & 0x7FF);
    }
    nb = biased + e;
    if (nb >= 2047) return x < 0.0 ? -INFINITY : INFINITY;
    if (nb <= 0) {
        /* Subnormal or zero. Building a denormal bit pattern means shifting
         * the mantissa right, which loses the sticky bits the IEEE rounding
         * rule needs; scaling arithmetically instead lets the hardware do
         * that rounding. Exponent 1 keeps the value just above the denormal
         * range, and pow2i supplies the rest (possibly all the way to 0). */
        u.i = (u.i & MANT_MASK) | EXP_1;
        return u.d * pow2i(nb - 1);
    }
    u.i = (u.i & MANT_MASK) | ((unsigned long long)nb << 52);
    return u.d;
}

double modf(double x, double *ip) {
    double t = trunc_to_zero(x);
    if (ip) *ip = t;
    return x - t;                       /* keeps x's sign, as C requires */
}

double trunc(double x) {
    return trunc_to_zero(x);
}

/* Half away from zero -- C99 round(), not the banker's rounding of rint. */
double round(double x) {
    if (x >= 0.0) return floor(x + 0.5);
    return ceil(x - 0.5);
}

/* Both return the other operand when one is NaN, so fmin(1,NaN) is 1 --
 * the IEEE rule, and the reason this is not just x < y ? x : y. */
double fmin(double x, double y) {
    if (x != x) return y;
    if (y != y) return x;
    return x < y ? x : y;
}

double fmax(double x, double y) {
    if (x != x) return y;
    if (y != y) return x;
    return x > y ? x : y;
}

/* ===========================================================================
 * exp / log family
 *
 * exp reduces its argument to |r| <= ln2/2 and evaluates a Taylor series
 * there (the largest term is then tiny, so ~12 terms reach full double
 * precision); log reduces to |s| <= (sqrt(2)-1)/(sqrt(2)+1) ~= 0.17 and uses
 * the atanh series, which converges by a factor of ~0.03 per term. Both
 * then correct with one exact ldexp / one multiply.
 * ======================================================================== */

#define LN2   0.693147180559945309417232121458
#define LN10  2.302585092994045684017991454684

double exp(double x) {
    double k;
    double r;
    double s;
    int ki;
    if (x != x) return x;
    if (x > 709.782712893384) return INFINITY;     /* 2^1024 would overflow */
    if (x < -745.133219101941) return 0.0;
    /* k = round(x/ln2); r = x - k*ln2 with |r| <= ln2/2. Doing the
     * subtraction in two pieces keeps k*ln2 accurate for large x, where
     * x and k*ln2 nearly cancel. */
    k = floor(x / LN2 + 0.5);
    ki = (int)k;
    r = x - k * LN2;
    /* Taylor: 1 + r + r^2/2! + ... (Horner, 13 terms) */
    s = 1.0 + r / 13.0;
    s = 1.0 + r * s / 12.0;
    s = 1.0 + r * s / 11.0;
    s = 1.0 + r * s / 10.0;
    s = 1.0 + r * s / 9.0;
    s = 1.0 + r * s / 8.0;
    s = 1.0 + r * s / 7.0;
    s = 1.0 + r * s / 6.0;
    s = 1.0 + r * s / 5.0;
    s = 1.0 + r * s / 4.0;
    s = 1.0 + r * s / 3.0;
    s = 1.0 + r * s / 2.0;
    s = 1.0 + r * s;
    return ldexp(s, ki);
}

double exp2(double x) {
    return exp(x * LN2);
}

double log(double x) {
    double m;
    double s;
    double t;
    double s2;
    double r;
    int e;
    if (x != x || x < 0.0) return NAN;
    if (x == 0.0) return -INFINITY;
    if (isinf(x)) return INFINITY;
    m = frexp(x, &e);                       /* x = m * 2^e, m in [0.5,1) */
    /* Pull m into [1/sqrt(2), sqrt(2)): the atanh argument below is then
     * bounded by 0.1716 instead of 1/3, so the series needs half the terms. */
    if (m < 0.70710678118654752440) {
        m = m * 2.0;
        e = e - 1;
    }
    s = (m - 1.0) / (m + 1.0);
    s2 = s * s;
    /* log(m) = 2*s*(1 + s^2/3 + s^4/5 + ... + s^40/41), evaluated by Horner
     * in s^2 so the tiny high-order terms are added first. Pairing the
     * powers the other way -- walking s up while walking the divisor down --
     * silently computes a different series entirely. */
    r = 1.0 / 41.0;
    {
        int i;
        for (i = 39; i >= 1; i -= 2) {
            r = r * s2 + 1.0 / (double)i;
        }
    }
    return 2.0 * (r * s) + (double)e * LN2;
}

double log2(double x) {
    return log(x) / LN2;
}

double log10(double x) {
    return log(x) / LN10;
}

/*
 * pow goes through exp(y*log(x)), which is where the usual "pow(10,2) is
 * 99.99999999999999" complaint comes from: log and exp each round, and the
 * errors multiply. An exact integer exponent is worth special-casing because
 * repeated squaring is then exact for every power of two and rounds only
 * once per multiply otherwise, so the common pow(x,2)/pow(x,3) stay clean.
 */
double pow(double x, double y) {
    double ax;
    double r;
    long n;
    if (y == 0.0) return 1.0;               /* even pow(0,0) is 1 in C99 */
    if (x != x || y != y) return NAN;
    if (x == 1.0) return 1.0;
    if (y == 1.0) return x;
    ax = fabs(x);
    if (ax == 0.0) {
        /* 0^y: positive y is 0, negative y is +inf (a domain error, which
         * this library reports as the IEEE result rather than by trapping). */
        return y > 0.0 ? 0.0 : INFINITY;
    }
    if (isinf(y)) {
        if (ax < 1.0) return y > 0.0 ? 0.0 : INFINITY;
        if (ax > 1.0) return y > 0.0 ? INFINITY : 0.0;
        return 1.0;
    }
    if (isinf(x)) return y > 0.0 ? (x > 0.0 ? INFINITY : (floor(y) == y && ((long)y & 1) ? -INFINITY : INFINITY)) : 0.0;
    n = (long)y;
    if ((double)n == y && n > 0 && n < 1024) {
        /* Repeated squaring over the bits of n. */
        double b = x;
        r = 1.0;
        while (n > 0) {
            if (n & 1) r = r * b;
            b = b * b;
            n = n >> 1;
        }
        return r;
    }
    if ((double)n == y && n < 0 && n > -1024) {
        r = pow(x, (double)(-n));
        return r == 0.0 ? INFINITY : 1.0 / r;
    }
    if (x < 0.0) {
        /* A negative base needs an integral exponent; anything else is a
         * domain error and this library answers it with NaN. */
        return NAN;
    }
    return exp(y * log(x));
}

/* ===========================================================================
 * Trigonometric family
 *
 * sin/cos reduce x by whole multiples of pi/2 and evaluate a Taylor series
 * on the remainder in [-pi/4, pi/4]. The reduction is the weak point: a real
 * libm carries 100+ bits of 2/pi (Payne-Hanek) so that huge arguments still
 * land on the right remainder, while this one-step subtraction loses the low
 * bits of x once x grows past roughly 2^60. Inside that range the series is
 * the accurate part: it is carried to x^17/x^18, whose truncation error is
 * below one ULP at the pi/4 edge.
 *
 * atan uses the half-angle identity twice to bring |x| under 0.2, where an
 * 11th-order series is exact to double precision -- plain Taylor on |x|<=1
 * would need hundreds of terms to get there.
 * ======================================================================== */

#define PI      3.141592653589793238462643383279
#define PI_2    1.570796326794896619231321691640
#define PI_4    0.785398163397448309615660845820

/* pi/2 split into a double plus its rounding residue: subtracting the two
 * halves separately keeps x - n*pi/2 accurate when x is large and the
 * product n*pi/2 would otherwise swallow x's low bits. */
#define PI2_HI  1.57079632679489661923
#define PI2_LO  6.123233995736765886e-17

/* Reduce x to a remainder in [-pi/4, pi/4] plus a quadrant 0..3. */
static double rem_pio2(double x, int *q) {
    double n = floor(x / PI_2 + 0.5);
    double r = (x - n * PI2_HI) - n * PI2_LO;
    /* A second pass catches the case where the first landed just outside
     * [-pi/4, pi/4] because n was off by one. */
    if (r > PI_4) {
        r = r - PI_2;
        n = n + 1.0;
    } else if (r < -PI_4) {
        r = r + PI_2;
        n = n - 1.0;
    }
    *q = ((int)n) & 3;
    return r;
}

/* sin on [-pi/4, pi/4]: Taylor in s = r^2, carried to r^17. */
static double sin_kernel(double r) {
    double s = r * r;
    double p = 1.0 / 355687428096000.0;           /* 1/17! */
    p = p * s - 1.0 / 1307674368000.0;            /* 1/15! */
    p = p * s + 1.0 / 6227020800.0;               /* 1/13! */
    p = p * s - 1.0 / 39916800.0;                 /* 1/11! */
    p = p * s + 1.0 / 362880.0;                   /* 1/9!  */
    p = p * s - 1.0 / 5040.0;                     /* 1/7!  */
    p = p * s + 1.0 / 120.0;                      /* 1/5!  */
    p = p * s - 1.0 / 6.0;                        /* 1/3!  */
    p = p * s + 1.0;
    return r * p;
}

/* cos on [-pi/4, pi/4]: Taylor in s = r^2, carried to r^18 (even
 * factorials only -- 18!, 16!, ... 2!). */
static double cos_kernel(double r) {
    double s = r * r;
    double p = -1.0 / 6402373705728000.0;         /* 1/18! */
    p = p * s + 1.0 / 20922789888000.0;           /* 1/16! */
    p = p * s - 1.0 / 87178291200.0;              /* 1/14! */
    p = p * s + 1.0 / 479001600.0;                /* 1/12! */
    p = p * s - 1.0 / 3628800.0;                  /* 1/10! */
    p = p * s + 1.0 / 40320.0;                    /* 1/8!  */
    p = p * s - 1.0 / 720.0;                      /* 1/6!  */
    p = p * s + 1.0 / 24.0;                       /* 1/4!  */
    p = p * s - 0.5;                              /* 1/2!  */
    p = p * s + 1.0;
    return p;
}

double sin(double x) {
    int q;
    double r;
    if (x != x) return x;
    if (isinf(x)) return NAN;
    r = rem_pio2(x, &q);
    if (q == 0) return sin_kernel(r);
    if (q == 1) return cos_kernel(r);
    if (q == 2) return -sin_kernel(r);
    return -cos_kernel(r);
}

double cos(double x) {
    int q;
    double r;
    if (x != x) return x;
    if (isinf(x)) return NAN;
    r = rem_pio2(x, &q);
    if (q == 0) return cos_kernel(r);
    if (q == 1) return -sin_kernel(r);
    if (q == 2) return -cos_kernel(r);
    return sin_kernel(r);
}

double tan(double x) {
    int q;
    double r;
    double s;
    double c;
    if (x != x) return x;
    if (isinf(x)) return NAN;
    r = rem_pio2(x, &q);
    s = sin_kernel(r);
    c = cos_kernel(r);
    switch (q) {
    case 0: return s / c;
    case 1: return -c / s;
    case 2: return s / c;
    default: return -c / s;
    }
}

/*
 * atan on |x| < 0.1: Taylor carried to x^15.
 *
 * The atanh-style series for atan has the bare divisor k, not k!, so it
 * converges far more slowly than the sin/cos/exp series do: at x = 0.2 the
 * x^13/13 term alone is 6e-11, which is a million ULPs. That is why the
 * caller halves the angle until x is under 0.1, where x^15/15 is already
 * below one ULP.
 */
static double atan_kernel(double x) {
    double s = x * x;
    double p = -1.0 / 15.0;
    p = p * s + 1.0 / 13.0;
    p = p * s - 1.0 / 11.0;
    p = p * s + 1.0 / 9.0;
    p = p * s - 1.0 / 7.0;
    p = p * s + 1.0 / 5.0;
    p = p * s - 1.0 / 3.0;
    p = p * s + 1.0;
    return x * p;
}

double atan(double x) {
    double a = 0.0;
    double r;
    int halvings = 0;
    int flip = 0;
    int neg = 0;
    if (x != x) return x;
    if (x == 0.0) return x;                       /* keeps -0.0 */
    if (isinf(x)) return x > 0.0 ? PI_2 : -PI_2;
    if (x < 0.0) { neg = 1; x = -x; }
    if (x > 1.0) {
        x = 1.0 / x;
        flip = 1;                                 /* atan(x) = pi/2 - atan(1/x) */
    }
    /* Half-angle: x -> x / (1 + sqrt(1+x^2)) halves the angle, so the
     * result is scaled back up by 2 per step (ldexp below). Three steps
     * bring |x| <= 1 down under 0.1, which is what atan_kernel needs. */
    while (x > 0.1) {
        x = x / (1.0 + sqrt(1.0 + x * x));
        halvings++;
    }
    r = ldexp(atan_kernel(x), halvings);
    if (flip) r = PI_2 - r;
    a = neg ? -r : r;
    return a;
}

double asin(double x) {
    if (x != x || x > 1.0 || x < -1.0) return NAN;
    if (x == 1.0) return PI_2;
    if (x == -1.0) return -PI_2;
    return atan(x / sqrt(1.0 - x * x));
}

double acos(double x) {
    if (x != x || x > 1.0 || x < -1.0) return NAN;
    if (x == 1.0) return 0.0;
    if (x == -1.0) return PI;
    return PI_2 - atan(x / sqrt(1.0 - x * x));
}

/*
 * Quadrant-fixing wrapper over atan(y/x). The x == 0 cases cannot go through
 * the division at all -- that is where atan2 earns its keep.
 */
double atan2(double y, double x) {
    if (y != y || x != x) return NAN;
    if (x > 0.0) return atan(y / x);
    if (x < 0.0) {
        if (y >= 0.0) return atan(y / x) + PI;
        return atan(y / x) - PI;
    }
    if (y > 0.0) return PI_2;
    if (y < 0.0) return -PI_2;
    return 0.0;                                   /* atan2(0,0) */
}

/* ===========================================================================
 * Hyperbolic family, hypot and cbrt
 * ======================================================================== */

double sinh(double x) {
    double e;
    if (x != x || isinf(x) || x == 0.0) return x;
    e = exp(x);
    if (x > 0.0) return 0.5 * (e - 1.0 / e);
    return 0.5 * (e - 1.0 / e);
}

double cosh(double x) {
    double e;
    if (x != x) return x;
    if (isinf(x)) return INFINITY;
    e = exp(x);
    return 0.5 * (e + 1.0 / e);
}

/*
 * Computed as 1 - 2/(e^2x + 1) rather than (e^x - e^-x)/(e^x + e^-x): the
 * direct form overflows e^x for x > 709 while the true answer is 1, and this
 * one cannot overflow because the denominator only grows.
 */
double tanh(double x) {
    double t;
    if (x != x) return x;
    if (isinf(x)) return x > 0.0 ? 1.0 : -1.0;
    if (x > 19.0) return 1.0;
    if (x < -19.0) return -1.0;
    t = exp(2.0 * x);
    return (t - 1.0) / (t + 1.0);
}

/*
 * sqrt(x^2 + y^2) without the intermediate overflow the naive form hits:
 * x*x alone overflows for x > 1.3e154 even though the answer is
 * representable. Scaling by the larger operand keeps the squares in range.
 */
double hypot(double x, double y) {
    double m;
    double a;
    double b;
    if (x != x || y != y) return NAN;
    if (isinf(x) || isinf(y)) return INFINITY;
    x = fabs(x);
    y = fabs(y);
    m = fmax(x, y);
    if (m == 0.0) return 0.0;
    a = x / m;
    b = y / m;
    return m * sqrt(a * a + b * b);
}

/*
 * Newton's method on the cube root, seeded from the exponent: x is split
 * into mantissa * 2^(3k+r) so the iteration only ever runs on a value in
 * [0.5, 4), where a handful of steps converge. The plain iteration on x
 * itself still converges, but needs ~40 steps for 1e-300 and can stall on
 * the way for exponents that are not multiples of three.
 */
double cbrt(double x) {
    int e;
    int e3;
    int rem;
    int i;
    int neg = 0;
    double y;
    double r;
    double last;
    if (x == 0.0 || x != x || isinf(x)) return x;
    if (x < 0.0) { neg = 1; x = -x; }
    y = frexp(x, &e);
    e3 = e / 3;
    rem = e - e3 * 3;
    if (rem < 0) { rem = rem + 3; e3 = e3 - 1; }
    y = ldexp(y, rem);
    r = 0.5 * (1.0 + y);
    for (i = 0; i < 40; i++) {
        last = r;
        r = (2.0 * r + y / (r * r)) / 3.0;
        if (r == last) break;
    }
    r = ldexp(r, e3);
    return neg ? -r : r;
}

/* ====================== sign manipulation =============================== */
int signbit(double x) {
    /* Read out of the representation, not out of an arithmetic test:
     * `x < 0.0` is false for -0.0 and for every NaN, and `1.0 / x < 0.0`
     * only works because division happens to hand back a signed infinity.
     * The bit is the thing itself, and it survives a value the arithmetic
     * cannot see yet. */
    unsigned long long b = *(unsigned long long *)&x;
    return (int)((b >> 63) & 1);
}

double copysign(double x, double y) {
    /* Also bit-wise, so that copysign(0.0, -1.0) really is a -0.0 -- a
     * `-fabs(x)` spelling cannot produce one. */
    unsigned long long *px = (unsigned long long *)&x;
    unsigned long long *py = (unsigned long long *)&y;
    *px = (*px & 0x7FFFFFFFFFFFFFFFULL) | (*py & 0x8000000000000000ULL);
    return x;
}

double fdim(double x, double y) {
    if (isnan(x)) return x;
    if (isnan(y)) return y;
    if (x > y) return x - y;
    return 0.0;
}

/* ====================== C99 rounding and remainder ====================== */
double rint(double x) {
    double f = floor(x);
    double d = x - f;
    if (d > 0.5) return f + 1.0;
    if (d < 0.5) return f;
    /* An exact half goes to the even neighbour. fmod is exact on f because
     * f is an integer below 2^53. */
    if (fmod(f, 2.0) == 0.0) return f;
    if (f + 1.0 == 0.0) return copysign(0.0, x);   /* rint(-0.5) is -0.0 */
    return f + 1.0;
}

double nearbyint(double x) {
    return rint(x);
}

double remainder(double x, double y) {
    double q;
    double r;
    if (isnan(x) || isnan(y) || isinf(x) || y == 0.0) return 0.0 / 0.0;
    if (isinf(y)) return x;
    q = rint(x / y);
    r = x - q * y;
    if (r == 0.0) return copysign(0.0, x);
    return r;
}

/* Dekker's split: the exact product of two doubles as a head and a
 * residual. The splitting constant is 2^27+1, which is why a factor much
 * past 1e300 has to fall back -- a * split would overflow first. */
static void two_prod(double a, double b, double *p, double *e) {
    double split = 134217729.0;
    double ah, al, bh, bl, t;
    t = a * split;
    ah = t - (t - a);
    al = a - ah;
    t = b * split;
    bh = t - (t - b);
    bl = b - bh;
    *p = a * b;
    *e = ((ah * bh - *p) + ah * bl + al * bh) + al * bl;
}

/* Knuth's two-sum: the same trick for an addition. */
static void two_sum(double a, double b, double *s, double *e) {
    double bb;
    *s = a + b;
    bb = *s - a;
    *e = (a - (*s - bb)) + (b - bb);
}

double fma(double x, double y, double z) {
    double p, pe, s, se;
    if (isnan(x)) return x;
    if (isnan(y)) return y;
    if (isnan(z)) return z;
    if (fabs(x) > 1e300 || fabs(y) > 1e300) return x * y + z;
    two_prod(x, y, &p, &pe);
    if (isinf(p)) return x * y + z;
    two_sum(p, z, &s, &se);
    /* One rounding, at the very end: both residuals sit below an ulp of the
     * result, so this is the correctly rounded x*y+z. */
    return s + (se + pe);
}

/* ====================== exp/log near 1 ================================== */
double expm1(double x) {
    double term, sum;
    int k;
    if (isnan(x)) return x;
    if (x == 0.0) return x;
    if (isinf(x)) return x < 0.0 ? -1.0 : x;
    if (fabs(x) > 0.5) return exp(x) - 1.0;
    /* Below 0.5 the series costs nothing and keeps every digit: the leading
     * term IS the answer, so nothing cancels. */
    sum = x;
    term = x;
    for (k = 2; k < 30; k++) {
        term = term * x / (double)k;
        sum = sum + term;
    }
    return sum;
}

double log1p(double x) {
    double u, u2, term, sum;
    int k;
    if (isnan(x)) return x;
    if (x < -1.0) return 0.0 / 0.0;
    if (x == -1.0) return -(1.0 / 0.0);
    if (isinf(x)) return x;
    if (fabs(x) > 0.5) return log(1.0 + x);
    /* log1p(x) = 2*atanh(x/(2+x)); |u| <= 1/3 over this range, so the odd
     * series collapses in a few dozen terms. */
    u = x / (2.0 + x);
    u2 = u * u;
    sum = u;
    term = u;
    for (k = 3; k < 60; k = k + 2) {
        term = term * u2;
        sum = sum + term / (double)k;
    }
    return 2.0 * sum;
}

/* ====================== gamma =========================================== */
/* Lanczos, g = 7 with n = 9 coefficients: about 15 correct digits over the
 * positive reals. The coefficients are locals rather than a table because a
 * static array is one more thing to get wrong. */
static double gamma_lanczos(double x) {
    double a = 0.99999999999980993;
    double t = x + 7.5;
    a = a + 676.5203681218851 / (x + 1.0);
    a = a + (-1259.1392167224028) / (x + 2.0);
    a = a + 771.32342877765313 / (x + 3.0);
    a = a + (-176.61502916214059) / (x + 4.0);
    a = a + 12.507343278686905 / (x + 5.0);
    a = a + (-0.13857109526572012) / (x + 6.0);
    a = a + 9.9843695780195716e-6 / (x + 7.0);
    a = a + 1.5056327351493116e-7 / (x + 8.0);
    return 2.5066282746310005024 * pow(t, x + 0.5) * exp(-t) * a;  /* sqrt(2pi) */
}

double tgamma(double x) {
    double s;
    if (isnan(x)) return x;
    if (isinf(x)) return x > 0.0 ? x : 0.0 / 0.0;
    /* The non-positive integers are poles. */
    if (x == floor(x) && x <= 0.0) return 0.0 / 0.0;
    /* A small integer argument has an exact answer -- a factorial -- and
     * Lanczos only reaches it to ~3e-15 (gamma(10) came back as
     * 362880.000000001), which a 15-digit print shows. */
    if (x == floor(x) && x <= 21.0) {
        double r = 1.0;
        double i = 2.0;
        while (i < x) {
            r = r * i;
            i = i + 1.0;
        }
        return r;
    }
    if (x < 0.5) {
        /* Reflection: gamma(x) * gamma(1-x) = pi / sin(pi*x). */
        s = sin(M_PI * x);
        if (s == 0.0) return 0.0 / 0.0;
        return M_PI / (s * tgamma(1.0 - x));
    }
    return gamma_lanczos(x - 1.0);
}

double lgamma(double x) {
    double r, r2;
    if (isnan(x)) return x;
    if (isinf(x)) return x;
    if (x == floor(x) && x <= 0.0) return 1.0 / 0.0;   /* poles: +infinity */
    if (x < 0.5) {
        return log(M_PI / fabs(sin(M_PI * x))) - lgamma(1.0 - x);
    }
    if (x >= 8.0) {
        /* Stirling: gamma(1000) is 10^2567, far past a double, but its log
         * is not -- this is the only form that survives out there. */
        r = 1.0 / x;
        r2 = r * r;
        return (x - 0.5) * log(x) - x + 0.9189385332046727 +
               r / 12.0 - r * r2 / 360.0 +
               r * r2 * r2 / 1260.0 - r * r2 * r2 * r2 / 1680.0;
    }
    return log(tgamma(x));
}

/* ====================== error function ================================== */
/* The Taylor series about 0, in the ratio form: term(k+1)/term(k) =
 * -x^2 (2k+1) / ((k+1)(2k+3)). Converges fast for |x| below ~1.5, which is
 * exactly where erfc's continued fraction is at its weakest. */
static double erf_series(double x) {
    double x2 = x * x;
    double term = x;
    double sum = x;
    /* Neumaier compensation: the terms alternate in sign, so the running
     * total shrinks while they are still sizeable and a plain accumulation
     * loses the low bits that decide the last digit (erf(0.5) came out one
     * ulp below the true value without this). */
    double comp = 0.0;
    int k;
    for (k = 0; k < 60; k++) {
        double y = -term * x2 * (double)(2 * k + 1) /
                   (double)((k + 1) * (2 * k + 3));
        double t = sum + y;
        comp = comp + ((sum - t) + y);
        sum = t;
        term = y;
    }
    return 1.1283791670955126 * (sum + comp);   /* 2/sqrt(pi) */
}

/* Q(a, x) -- the regularized incomplete gamma -- by the Lentz continued
 * fraction (Numerical Recipes' gcf). erfc(x) for x >= 0 is Q(1/2, x^2). */
static double gamma_q_cf(double a, double x) {
    double fpmin = 1e-300;
    double b = x + 1.0 - a;
    double c = 1.0 / fpmin;
    double d = 1.0 / b;
    double h = d;
    double del, an;
    int i;
    for (i = 1; i < 300; i++) {
        an = (double)(-i) * ((double)i - a);
        b = b + 2.0;
        d = an * d + b;
        if (fabs(d) < fpmin) d = fpmin;
        c = b + an / c;
        if (fabs(c) < fpmin) c = fpmin;
        d = 1.0 / d;
        del = d * c;
        h = h * del;
        if (fabs(del - 1.0) < 1e-16) break;
    }
    return exp(-x + a * log(x) - lgamma(a)) * h;
}

double erfc(double x) {
    if (isnan(x)) return x;
    if (isinf(x)) return x < 0.0 ? 2.0 : 0.0;
    if (x < 0.0) return 2.0 - erfc(-x);
    /* Below 1.5 the continued fraction is the wrong tool -- and at x == 0
     * it is outright wrong, since log(0) makes the leading factor vanish
     * and erfc(0) would come out as 0 instead of 1. */
    if (x < 1.5) return 1.0 - erf_series(x);
    return gamma_q_cf(0.5, x * x);
}

double erf(double x) {
    if (isnan(x)) return x;
    if (x == 0.0) return x;
    if (isinf(x)) return x > 0.0 ? 1.0 : -1.0;
    if (x < 0.0) return -erf(-x);
    if (x < 1.5) return erf_series(x);
    return 1.0 - erfc(x);
}

/* ====================== binary exponent ================================= */
int ilogb(double x) {
    int e;
    if (x == 0.0) return FP_ILOGB0;
    if (isnan(x) || isinf(x)) return FP_ILOGBNAN;
    frexp(x, &e);
    /* frexp gives x = m * 2^e with m in [0.5, 1), so log2|x| lies in
     * [e-1, e) and its floor is e-1. */
    return e - 1;
}

double logb(double x) {
    return (double)ilogb(x);
}

double nan(const char *tagp) {
    return 0.0 / 0.0;
}

/* ====================== C99 neighbour / scaled exp / remquo ============= */

/* scalbn/scalbln are exactly x * 2^n; ldexp already does that scaling
 * through the bit pattern, so these just forward to it. */
double scalbn(double x, int n) {
    return ldexp(x, n);
}

double scalbln(double x, long n) {
    return ldexp(x, (int)n);
}

/* nextafter: the representable double adjacent to x on the side of y.
 * The bit pattern is monotonic for positives but reversed for negatives
 * (because the sign bit flips the ordering), so the step direction depends
 * on both x's sign and whether we are moving toward a larger or smaller
 * value. x == 0 is special-cased because stepping the all-zero pattern
 * would land at the far negative extreme instead of the adjacent subnormal. */
double nextafter(double x, double y) {
    union DU u;
    if (isnan(x) || isnan(y)) return y;
    if (x == y) return y;
    if (x == 0.0) {
        u.i = 1ULL;                              /* smallest positive subnormal */
        if (y < 0.0) u.i |= 0x8000000000000000ULL;
        return u.d;
    }
    u.d = x;
    if ((x >= 0.0) == (y > x)) u.i++;
    else u.i--;
    return u.d;
}

/* nexttoward: the next double in the direction of y. Under goc there is no
 * long double, so y is a plain double and this is nextafter (see the declaration
 * in <math.h>, which picks the argument width from the host). A host compiler
 * does have long double, so the body still narrows y to double to reach the same
 * nextafter -- the wider type is part of the interface, not of the step. */
#ifdef __goc__
double nexttoward(double x, double y) {
    return nextafter(x, y);
}
#else
double nexttoward(double x, long double y) {
    return nextafter(x, (double)y);
}
#endif

/* remquo: remainder() for the value, plus the low 7 bits (mod 128, with the
 * sign of x/y) of the integer quotient x/y stored through quo. */
double remquo(double x, double y, int *quo) {
    double r = remainder(x, y);
    if (quo) {
        double whole = (x - r) / y;              /* integer quotient x/y */
        int qi = (int)whole;
        int k = (qi >= 0) ? (qi & 127) : -((-qi) & 127);
        *quo = k;
    }
    return r;
}

/* fpclassify: which of the five categories x falls into. isnan/isinf cover
 * NaN and infinity; x == 0 catches both zeros; a finite non-zero with a
 * cleared exponent field is a subnormal. */
int fpclassify(double x) {
    union DU u;
    if (isnan(x)) return FP_NAN;
    if (isinf(x)) return FP_INFINITE;
    if (x == 0.0) return FP_ZERO;
    u.d = x;
    if ((u.i & 0x7FF0000000000000ULL) == 0) return FP_SUBNORMAL;
    return FP_NORMAL;
}

/* ===================== C23 additions to <math.h> =========================
 *
 * Everything above is C89/C99. This block is ISO/IEC 9899:2024 (C23), which
 * added a rounding-direction-agnostic total order over the floating types, a
 * magnitude-wise max/min that keeps the sign, NaN-propagating extrema, the
 * successor functions, 10^x, and the fp->integer conversion family. goclib
 * has no long double and evaluates float as an effective double, so only the
 * double spellings exist -- the f/l variants would be the same function under
 * different names, and the header says so.
 */

/* fmaximum_num / fminimum_num. Unlike fmax/fmin, a NaN argument is an error
 * rather than something to step over: if either is a NaN the result is a NaN.
 * This is the "_num" half of C23's new maxima family -- the "_mag" half
 * compares by magnitude, the plain one propagates the NaN. So the NaN tests
 * come first and return NaN, and only then does the ordinary comparison run.
 * The returned NaN is (x + y), which is a NaN when either side already is and
 * costs nothing when the tests above let us through. */
double fmaximum_num(double x, double y) {
    if (isnan(x) || isnan(y)) return x + y;
    return (x > y) ? x : y;
}

double fminimum_num(double x, double y) {
    if (isnan(x) || isnan(y)) return x + y;
    return (x < y) ? x : y;
}

/* fmaxmag / fminmag compare |x| against |y| and hand back the operand that
 * won, sign included. On an exact magnitude tie the standard hands the result
 * to the operand with the even low-order mantissa bit -- that is what makes
 * the answer independent of which argument came first.
 *
 * "Even" means the low-order mantissa bit is zero, so the test is bit 0 of the
 * pattern -- NOT a bit of the exponent field. (Reading the exponent's low bit
 * instead makes 1.0 and 3.0 both "even" and 1.5 "odd", which happens to look
 * right for the first two and is wrong in general: it orders by exponent, not
 * by mantissa.)
 *
 * The bit is taken from the low half as a 32-bit value: `bits & 1ULL` on the
 * full 64-bit pattern reads as 0 for every input under the current goc, so
 * every tie would fall the same way.
 *
 * When both mantissas are even -- which is the case for x and -x, and the only
 * case the standard leaves open -- the tie is broken by totalordermag, so the
 * negative one wins fmaxmag. That matches what a magnitude-wise comparison
 * means and makes fmaxmag(-1, 1) and fmaxmag(1, -1) both answer 1. */
static int even_mantissa(double x) {
    union DU u;
    u.d = x;
    return (int)((unsigned int)u.i & 1u);
}

/* totalordermag on a tie: negative first, then smaller magnitude. */
static int mag_precedes(double x, double y) {
    double ax, ay;
    int xs, ys;
    xs = signbit(x);
    ys = signbit(y);
    if (xs != ys) return xs;          /* the negative one comes first */
    ax = fabs(x);
    ay = fabs(y);
    return (ax < ay);
}

double fmaxmag(double x, double y) {
    double ax, ay;
    if (isnan(x)) return isnan(y) ? x + y : y;
    if (isnan(y)) return x;
    ax = fabs(x);
    ay = fabs(y);
    if (ax > ay) return x;
    if (ay > ax) return y;
    /* Equal magnitudes: the even mantissa wins; if both are even (x and -x) the
     * magnitude order decides, which puts fmaxmag(-1, 1) and fmaxmag(1, -1)
     * both at 1. Spelled as ifs because a conditional expression here mis-binds
     * under goc. */
    if (even_mantissa(x) && !even_mantissa(y)) return x;
    if (even_mantissa(y) && !even_mantissa(x)) return y;
    return mag_precedes(x, y) ? y : x;
}

double fminmag(double x, double y) {
    double ax, ay;
    if (isnan(x)) return isnan(y) ? x + y : y;
    if (isnan(y)) return x;
    ax = fabs(x);
    ay = fabs(y);
    if (ax < ay) return x;
    if (ay < ax) return y;
    if (even_mantissa(x) && !even_mantissa(y)) return x;
    if (even_mantissa(y) && !even_mantissa(x)) return y;
    return mag_precedes(x, y) ? y : x;
}

/* nextup / nextdown: one representable step toward +infinity / -infinity.
 *
 * The zero cases are spelled with signbit() rather than `x == -0.0`, because
 * -0.0 == 0.0 is true by IEEE 754 and goc additionally constant-folds the
 * -0.0 literal to +0.0 -- so a comparison cannot tell the two zeros apart at
 * all, and testing it that way sends +0.0 down the -0.0 branch. signbit() is
 * the only thing here that sees the difference.
 *
 * Stepping away from zero has to produce the smallest subnormal rather than
 * crossing to the other sign, which is what nextafter(0, dir) already does;
 * the one thing worth spelling out is that -0.0 and +0.0 are adjacent to each
 * other (there is no value between them), so nextup(-0.0) is +0.0 itself. */
double nextup(double x) {
    union DU u;
    if (isnan(x)) return x;
    if (x == HUGE_VAL) return x;                /* already +infinity */
    if (x == 0.0) {
        /* -0.0 and +0.0 are adjacent -- nothing lies between them -- so -0.0
         * steps straight to +0.0, while +0.0 steps to the smallest
         * subnormal. signbit() is the only thing that tells them apart. */
        if (signbit(x)) {
            u.i = 0ULL;
            return u.d;
        }
        return nextafter(x, 1.0);
    }
    return nextafter(x, HUGE_VAL);
}

double nextdown(double x) {
    union DU u;
    if (isnan(x)) return x;
    if (x == -HUGE_VAL) return x;               /* already -infinity */
    if (x == 0.0) {
        if (!signbit(x)) {
            u.i = 0x8000000000000000ULL;        /* -0.0 follows +0.0 */
            return u.d;
        }
        return nextafter(x, -1.0);
    }
    return nextafter(x, -HUGE_VAL);
}

/* roundeven. goclib's rint() is already round-to-nearest-even and there is no
 * rounding-mode state to re-read, so this is the same computation under the
 * name C23 requires. It is a separate function because a caller is entitled to
 * assume the two can differ. */
double roundeven(double x) {
    return rint(x);
}

/* exp10: 10^x as exp(x * ln10). Reusing exp keeps one exponential to
 * maintain, and the product is exact enough that exp's own accuracy is what
 * limits the result -- ln10 is irrational, so x * ln10 cannot in general be
 * exact, but the double rounding costs far less than the ~1 ULP exp already
 * carries. Only x == 0 is special-cased, so 10^0 is exactly 1. */
double exp10(double x) {
    if (x == 0.0) return 1.0;
    return exp(x * 2.30258509299404568402);
}

/* issignaling: goclib only ever produces quiet NaNs -- every arithmetic
 * result and every NAN/DBL_SNAN constant is quiet -- so this is always 0.
 * It exists because C23 makes it mandatory, and reporting 0 for a quiet NaN
 * is the correct answer (a signalling NaN would need the payload to say so). */
int issignaling(double x) {
    (void)x;
    return 0;
}

/* getpayload: the NaN payload with the sign cleared. The standard leaves the
 * result unspecified for a non-NaN, so x passes through unchanged -- which is
 * what a caller that ignored the classification would have got anyway. */
double getpayload(double x) {
    union DU u;
    if (!isnan(x)) return x;
    u.d = x;
    u.i &= 0x7FFFFFFFFFFFFFFFULL;
    return u.d;
}

/* getsign: -1.0 or +1.0 as a double. There is no 0 case: signbit gives 0 for
 * +0.0, but getsign(+0.0) is +1.0 because zero carries no sign of its own. */
double getsign(double x) {
    /* signbit() rather than a bit test on the pattern: the 64-bit mask and
     * shift forms do not survive goc's codegen (see total_cmp above). */
    return signbit(x) ? -1.0 : 1.0;
}

/* fromfp / ufromfp.
 *
 * The conversion is the nearest integer under round-to-nearest-even, which is
 * the only rounding direction goclib has. An out-of-range or NaN argument is
 * a domain error: the standard's "otherwise returns 0" wording is honoured and
 * errno is set so a caller can tell that 0 was an error rather than a real
 * result. The bound is 2^63 for both: ufromfp must reject negatives, and
 * casting those would go through the same conversion, so one guard covers both.
 *
 * rint() before the cast is not decoration. C's floating-to-integer
 * conversion truncates toward zero -- (long long)3.9 is 3 -- so a bare cast
 * would give fromfp(3.9) == 3 and fromfp(-3.9) == -3, which is not what
 * fromfp means. Rounding first also gets the tie case right for free, because
 * rint is round-to-nearest-even: 2.5 goes to 2 and 3.5 goes to 4. */
long long fromfp(double fp) {
    union DU u;
    /* (double)(long long)2^63 is 2^63 exactly, so this compares the input
     * against the first unrepresentable value. u.i carries that as the
     * bit pattern, which avoids a literal the compiler would have to round. */
    u.i = 0x43E0000000000000ULL;                /* 2^63 */
    if (isnan(fp) || fp >= u.d || fp < -u.d) {
        errno = EDOM;
        return 0;
    }
    return (long long)rint(fp);
}

unsigned long long ufromfp(double fp) {
    union DU u;
    u.i = 0x43E0000000000000ULL;                /* 2^63 */
    if (isnan(fp) || fp >= u.d || fp < 0.0) {
        errno = EDOM;
        return 0;
    }
    return (unsigned long long)rint(fp);
}/* totalorder / totalordermag.
 *
 * The textbook implementation builds a "biased key" -- flip every bit of a
 * negative, set the top bit of a positive -- and compares the two keys as
 * unsigned integers. That is four lines and it is what every hand-written
 * version does, and it does not work here: goc cannot read the sign of a
 * 64-bit pattern. The literal 0x8000000000000000ULL assembles to
 * `mov rax, -9223372036854775808`, whose imm32 form sign-extends from the low
 * half (zero), leaving 0xFFFFFFFF00000000 -- bit 31 set rather than bit 63 --
 * and `(bits >> 63) & 1` is wrong for the same reason. copysign() in this file
 * uses the literal successfully, so the fault is specific to this operand
 * shape, not to the literal; but there is no reason to depend on it.
 *
 * signbit() is the way through: it is already here, and it reads the sign the
 * portable way (a comparison against zero) rather than through a mask. With the
 * sign in hand the comparison is spelled out as the standard defines it --
 * sign first, then the remaining bits -- which is a few lines longer than the
 * key trick and reads the same everywhere.
 */
/* |v| as a bit pattern, which for a non-NaN is monotone in |v| -- the
 * comparison totalordermag needs. copysign(x, 1.0) is the portable way to strip
 * the sign without a 64-bit mask. */
static unsigned long long magnitude_of(double v) {
    union DU u;
    u.d = copysign(v, 1.0);
    return u.i;
}

static int total_cmp(double x, double y, int by_magnitude) {
    union DU ux, uy;
    unsigned long long mx, my;
    int xneg, yneg;
    ux.d = x;
    uy.d = y;
    /* NaN outranks every number, infinity included. */
    if (isnan(x)) {
        if (!isnan(y)) return 1;
        /* Both NaN. The standard puts every NaN above every number, and orders
         * NaNs among themselves by payload, with the sign only breaking a tie.
         * goc cannot read the sign off a 64-bit pattern (see above), so the
         * comparison is done on the payload with the sign stripped -- which
         * makes two NaNs that differ only in sign compare equal, the one
         * deviation forced by that limitation and noted in math.h. */
        {
            unsigned long long px = magnitude_of(x);
            unsigned long long py = magnitude_of(y);
            if (px < py) return -1;
            if (px > py) return 1;
        }
        return 0;
    }
    if (isnan(y)) return -1;
    /* Under the magnitude order the sign is not part of the identity at all:
     * 1.0 and -1.0 are one value there, and so are the two zeros. Taking the
     * magnitude of both operands first is what gives that; it has to happen
     * before the sign comparison below, or the sign would separate them. */
    if (by_magnitude) {
        mx = magnitude_of(x);
        my = magnitude_of(y);
    } else {
        xneg = signbit(x);
        yneg = signbit(y);
        if (xneg != yneg) return xneg ? -1 : 1; /* negative sorts below positive */
        /* Same side of zero, so the rest of the pattern orders them directly:
         * for two negatives the more negative has the larger pattern, hence
         * the flip. */
        mx = xneg ? ~ux.i : ux.i;
        my = yneg ? ~uy.i : uy.i;
    }
    if (mx < my) return -1;
    if (mx > my) return 1;
    return 0;
}

int totalorder(const double *x, const double *y) {
    if (x == 0) return 0;
    if (y == 0) return 0;
    return total_cmp(*x, *y, 0);
}

int totalordermag(const double *x, const double *y) {
    if (x == 0) return 0;
    if (y == 0) return 0;
    return total_cmp(*x, *y, 1);
}
