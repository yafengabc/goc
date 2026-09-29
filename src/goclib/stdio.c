#include "goclib.h"
#include <stdarg.h>

/* ----------------------------- <stdio.h> --------------------------------- */
/*
 * Shared formatter. Writes the expansion of `fmt` (with `ap`) into `out`,
 * stopping after `limit` characters (limit < 0 means "no limit", used by
 * sprintf). Returns the number of characters written.
 */
static int vfmt(char *out, long limit, const char *fmt, va_list ap) {
    long n = 0;
    const char *p = fmt;
    while (*p) {
        if (*p != '%') {
            if (limit < 0 || n < limit) out[n] = *p;
            n++;
            p++;
            continue;
        }
        p++;
        if (*p == '%') {
            if (limit < 0 || n < limit) out[n] = '%';
            n++;
            p++;
            continue;
        }
        /* optional field width: parsed and ignored, exactly like the asm
         * vfmt ("%10.2f" prints "1.23", unpadded) */
        while (*p >= '0' && *p <= '9') p++;
        /* optional precision: ".NN" digits, or a bare "." for zero.
         * Only %f consumes it (fractional digit count); default 6. */
        int prec = 6;
        if (*p == '.') {
            p++;
            prec = 0;
            while (*p >= '0' && *p <= '9') { prec = prec * 10 + (*p - '0'); p++; }
        }
        /* length modifiers select long/short forms; every va slot is 8
         * bytes, so skipping the whole run is enough (same as the asm). */
        while (*p == 'l' || *p == 'h' || *p == 'L' ||
               *p == 'z' || *p == 'j' || *p == 't') p++;
        char spec = *p++;
        if (spec == 's') {
            const char *s = va_arg(ap, const char *);
            if (!s) s = "(null)";
            while (*s) {
                if (limit < 0 || n < limit) out[n] = *s;
                n++;
                s++;
            }
        } else if (spec == 'c') {
            int c = va_arg(ap, int);
            if (limit < 0 || n < limit) out[n] = (char)c;
            n++;
        } else if (spec == 'd' || spec == 'i' || spec == 'u' ||
                   spec == 'o' || spec == 'x' || spec == 'X') {
            /* integers (long on the varargs side) */
            unsigned long v;
            if (spec == 'd' || spec == 'i') {
                long sv = va_arg(ap, long);
                if (sv < 0 && spec != 'u') {
                    if (limit < 0 || n < limit) out[n] = '-';
                    n++;
                    v = (unsigned long)(-sv);
                } else {
                    v = (unsigned long)sv;
                }
                if (spec == 'u') v = (unsigned long)sv; /* unsigned %u */
            } else {
                v = va_arg(ap, unsigned long);
            }
            /* convert in the chosen base */
            int base = 10;
            if (spec == 'o') base = 8;
            else if (spec == 'x' || spec == 'X') base = 16;
            char tmp[32];
            int t = 0;
            if (v == 0) { tmp[t++] = '0'; }
            while (v > 0) {
                int d = (int)(v % base);
                v /= base;
                if (d < 10) tmp[t++] = (char)('0' + d);
                else tmp[t++] = (char)((spec == 'X' ? 'A' : 'a') + (d - 10));
            }
            while (t-- > 0) {
                if (limit < 0 || n < limit) out[n] = tmp[t];
                n++;
            }
        } else if (spec == 'f' || spec == 'g') {
            /* %f: <prec> fractional digits, rounded half to even, no exponent.
             * %g: the same conversion, then trailing zeros are stripped. */
            double x = va_arg(ap, double);
            int neg = 0;
            if (x < 0) { neg = 1; x = -x; }
            if (prec > 17) prec = 17;          /* past double's precision */
            long whole = (long)x;
            double frac = x - (double)whole;
            char dig[20];                      /* prec+1 digits to round on */
            int k;
            for (k = 0; k <= prec; k++) {
                frac *= 10.0;
                int d = (int)frac;
                dig[k] = (char)d;
                frac -= (double)d;
            }
            /* round half to even on the prec-th digit */
            {
                int tail = (int)(frac * 10.0 + 0.5); /* nonzero past prec+1? */
                if (prec == 0) {
                    int d0 = dig[0];
                    if (d0 > 5 || (d0 == 5 && (tail != 0 || (whole & 1) != 0)))
                        whole++;
                } else {
                    int dp = dig[prec];
                    if (dp > 5 || (dp == 5 && (tail != 0 || (dig[prec-1] & 1) != 0))) {
                        int j = prec - 1;
                        dig[j]++;
                        while (j > 0 && dig[j] > 9) { dig[j] = 0; dig[--j]++; }
                        if (dig[0] > 9) { dig[0] = 0; whole++; }
                    }
                }
            }
            /* %g strips trailing fractional zeros and a dangling decimal
             * point ("2.500000" -> "2.5", "1.000000" -> "1"). A simplified
             * %g: precision counts fractional digits like %f (C's %g counts
             * significant digits) and there is no exponent form. Rounding
             * runs first, so "0.999999" becomes "1". */
            if (spec == 'g' && prec > 0) {
                int last = prec - 1;
                while (last >= 0 && dig[last] == 0) last--;
                prec = last + 1; /* 0 -> no fractional part at all */
            }
            if (neg) {
                if (limit < 0 || n < limit) out[n] = '-';
                n++;
            }
            {
                char tmp[32];
                int t = 0;
                if (whole == 0) tmp[t++] = '0';
                long w = whole;
                while (w > 0) { tmp[t++] = (char)('0' + (w % 10)); w /= 10; }
                while (t-- > 0) {
                    if (limit < 0 || n < limit) out[n] = tmp[t];
                    n++;
                }
            }
            if (prec > 0) {
                if (limit < 0 || n < limit) out[n] = '.';
                n++;
                int k2;
                for (k2 = 0; k2 < prec; k2++) {
                    if (limit < 0 || n < limit) out[n] = (char)('0' + dig[k2]);
                    n++;
                }
            }
        } else if (spec == 'p') {
            /* %p: "0x" followed by 16 hex digits (full 64-bit address). */
            void *pv = va_arg(ap, void *);
            unsigned long v = (unsigned long)pv;
            if (limit < 0 || n < limit) out[n] = '0';
            n++;
            if (limit < 0 || n < limit) out[n] = 'x';
            n++;
            int shift;
            for (shift = 60; shift >= 0; shift -= 4) {
                int d = (int)((v >> shift) & 0xf);
                char ch = (char)(d < 10 ? '0' + d : 'a' + d - 10);
                if (limit < 0 || n < limit) out[n] = ch;
                n++;
            }
        } else {
            /* unknown specifier: emit it verbatim */
            if (limit < 0 || n < limit) out[n] = spec;
            n++;
        }
    }
    return n;
}

int sprintf(char *buf, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = vfmt(buf, -1, fmt, ap);
    buf[n] = 0;
    va_end(ap);
    return n;
}

int printf(const char *fmt, ...) {
    char buf[512];
    va_list ap;
    va_start(ap, fmt);
    int n = vfmt(buf, 512, fmt, ap);
    va_end(ap);
    __goclib_write(buf, n);
    return n;
}

int puts(const char *s) {
    long n = (long)strlen(s);
    __goclib_write(s, n);
    __goclib_write("\n", 1);
    return 0;
}

int putchar(int c) {
    char b = (char)c;
    __goclib_write(&b, 1);
    return c;
}

/* ------------------- thin integer printing ------------------------------- */
/*
 * int_print / long_print are the weightless counterparts of printf("%d\n"):
 * a digits-to-buffer conversion, one write for the digits and one for the
 * newline, no vfmt. A program that prints only integers links neither the
 * format interpreter nor the floating-point converter. Returns the number
 * of characters written, newline included.
 *
 * Both the UFCS scalar method x.print() and the print(...) builtin lower
 * here: int_print / long_print are the single source for integer output,
 * and their newline matches the print() builtin's "print a line" semantic.
 *
 * The sign is handled in unsigned arithmetic, so the most negative value
 * prints correctly: negating LONG_MIN overflows long but is exact modulo
 * 2^64, and C's unsigned arithmetic is defined modulo 2^64.
 */
int long_print(long v) {
    char buf[21]; /* sign + 20 digits (LONG_MIN's full width) */
    int n = 0;
    unsigned long u = (unsigned long)v;
    if (v < 0) {
        u = (unsigned long)0 - u;
        buf[n] = '-';
        n++;
    }
    /* digits come out least-significant first */
    do {
        buf[n] = (char)('0' + (int)(u % 10));
        u /= 10;
        n++;
    } while (u > 0);
    /* reverse the digit run (past the sign, if any) */
    {
        int lo = 0;
        int hi = n - 1;
        if (buf[0] == '-') {
            lo = 1;
        }
        while (lo < hi) {
            char t = buf[lo];
            buf[lo] = buf[hi];
            buf[hi] = t;
            lo++;
            hi--;
        }
    }
    __goclib_write(buf, n);
    __goclib_write("\n", 1);
    return n + 1;
}

int int_print(int v) {
    return long_print((long)v);
}

/* Thin target for the print(...) builtin (see stdio.h): the "%s\n" case,
 * with vfmt's "(null)" guard kept. The integer case lowers straight to
 * int_print / long_print (which now carry their own newline). Everything
 * returns the total character count so the builtin keeps its
 * printf-lowering return semantics. Its name keeps the UFCS spelling
 * (T_print): str_print. */
int str_print(const char *s) {
    if (!s) s = "(null)";
    long n = 0;
    while (s[n]) n++;
    __goclib_write(s, n);
    __goclib_write("\n", 1);
    return (int)(n + 1);
}

int getchar(void) {
    char b;
    long n = __goclib_read(&b, 1);
    if (n <= 0) return -1;
    return (unsigned char)b;
}
