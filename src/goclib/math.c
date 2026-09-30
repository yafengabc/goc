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

double fmod(double x, double y) {
    double n;
    if (y == 0.0) return 0.0;
    n = floor(x / y);
    return x - n * y;
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
