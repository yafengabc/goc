#include "goclib.h"

/* ----------------------------- <stdlib.h> ------------------------------- */

void *malloc(size_t size) {
    return __goclib_heap_alloc((long)size);
}

void free(void *p) {
    __goclib_heap_free(p);
}

void *calloc(size_t n, size_t size) {
    size_t total = n * size;
    void *p = __goclib_heap_alloc((long)total);
    if (p) memset(p, 0, total);
    return p;
}

/* realloc needs the old block's size, so the platform primitive owns the
 * job: Windows hands it to HeapReAlloc, Linux reads the size header the
 * bump allocator keeps in front of every block. */
void *realloc(void *ptr, size_t size) {
    return __goclib_heap_realloc(ptr, (long)size);
}

int atoi(const char *s) {
    return (int)strtol(s, 0, 10);
}

int abs(int x) {
    return x < 0 ? -x : x;
}

long strtol(const char *s, char **endp, int base) {
    /* skip leading whitespace */
    while (*s == ' ' || *s == '\t' || *s == '\n' || *s == '\r' || *s == '\f' || *s == '\v')
        s++;
    int sign = 0;
    if (*s == '-') { sign = 1; s++; }
    else if (*s == '+') { s++; }
    /* determine base */
    if (base == 0) {
        if (*s == '0') {
            if (s[1] == 'x' || s[1] == 'X') { base = 16; s += 2; }
            else { base = 8; }
        } else {
            base = 10;
        }
    } else if (base == 16) {
        if (*s == '0' && (s[1] == 'x' || s[1] == 'X')) s += 2;
    }
    long value = 0;
    while (*s) {
        int digit;
        if (*s >= '0' && *s <= '9') digit = *s - '0';
        else if (*s >= 'a' && *s <= 'z') digit = *s - 'a' + 10;
        else if (*s >= 'A' && *s <= 'Z') digit = *s - 'A' + 10;
        else break;
        if (digit >= base) break;
        value = value * base + digit;
        s++;
    }
    if (sign) value = -value;
    if (endp) *endp = (char *)s;
    return value;
}

/*
 * 10^n by repeated squaring. Every power of ten up to 1e22 is exactly
 * representable, so small exponents -- by far the common case -- are built
 * by straight multiplication and are exact. Above that the squares are
 * themselves rounded (1e32 is not exact), so a huge exponent can land a
 * ULP or two off the correctly rounded answer.
 */
static double pow10i(int n) {
    double r = 1.0;
    double p = 10.0;
    int e = n;
    int neg = 0;
    if (e < 0) {
        neg = 1;
        e = -e;
    }
    if (e <= 22) {
        while (e > 0) {
            r = r * 10.0;
            e--;
        }
    } else {
        while (e > 0) {
            if (e & 1) r = r * p;
            p = p * p;
            e = e >> 1;
        }
    }
    if (neg) r = 1.0 / r;
    return r;
}

/*
 * IEEE infinity / NaN without a literal that overflows: a runtime division
 * by zero. Written through a variable so nothing here is constant-folded.
 */
static double mk_inf(void) {
    double zero = 0.0;
    return 1.0 / zero;
}

static double mk_nan(void) {
    double zero = 0.0;
    return zero / zero;
}

/*
 * strtod: decimal (and C99 inf/nan) only. Hexadecimal floats -- "0x1p3" --
 * are not recognised. On failure endp is set to the ORIGINAL string, not to
 * the first character, which is how a caller tells "no conversion" from
 * "converted zero" (cJSON relies on exactly that).
 */
double strtod(const char *s, char **endp) {
    const char *p = s;
    double val = 0.0;
    double frac = 0.0;
    double fscale = 1.0;
    double sign = 1.0;
    int digits = 0;
    int exp = 0;
    int edigits = 0;
    int eneg = 0;

    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r' || *p == '\v' || *p == '\f') p++;
    if (*p == '+') {
        p++;
    } else if (*p == '-') {
        sign = -1.0;
        p++;
    }

    if ((*p == 'i' || *p == 'I') && (p[1] == 'n' || p[1] == 'N') && (p[2] == 'f' || p[2] == 'F')) {
        p = p + 3;
        if ((p[0] == 'i' || p[0] == 'I') && (p[1] == 'n' || p[1] == 'N') &&
            (p[2] == 'i' || p[2] == 'I') && (p[3] == 't' || p[3] == 'T') &&
            (p[4] == 'y' || p[4] == 'Y')) {
            p = p + 5;
        }
        if (endp) *endp = (char *)p;
        return sign * mk_inf();
    }
    if ((*p == 'n' || *p == 'N') && (p[1] == 'a' || p[1] == 'A') && (p[2] == 'n' || p[2] == 'N')) {
        p = p + 3;
        if (endp) *endp = (char *)p;
        return mk_nan();
    }

    while (*p >= '0' && *p <= '9') {
        val = val * 10.0 + (double)(*p - '0');
        p++;
        digits++;
    }
    if (*p == '.') {
        p++;
        while (*p >= '0' && *p <= '9') {
            frac = frac * 10.0 + (double)(*p - '0');
            fscale = fscale * 10.0;
            p++;
            digits++;
        }
    }
    if (digits == 0) {
        if (endp) *endp = (char *)s;
        return 0.0;
    }
    if (*p == 'e' || *p == 'E') {
        const char *save = p;
        p++;
        if (*p == '+') {
            p++;
        } else if (*p == '-') {
            eneg = 1;
            p++;
        }
        while (*p >= '0' && *p <= '9') {
            exp = exp * 10 + (*p - '0');
            p++;
            edigits++;
        }
        if (edigits == 0) p = save;         /* "1e" with no digits: stop before it */
    }
    val = val + frac / fscale;
    if (exp != 0) {
        if (eneg) exp = -exp;
        if (exp > 0) {
            val = val * pow10i(exp);
        } else {
            val = val / pow10i(-exp);
        }
    }
    if (endp) *endp = (char *)p;
    return sign * val;
}

static unsigned long rand_state = 1;

int rand(void) {
    /* glibc-style LCG */
    rand_state = rand_state * 1103515245UL + 12345UL;
    return (int)((rand_state >> 16) & 0x7fff);
}

void srand(unsigned int seed) {
    rand_state = seed ? (unsigned long)seed : 1UL;
}

void exit(int code) {
    __goclib_exit((long)code);
}
