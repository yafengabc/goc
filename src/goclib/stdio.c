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
            /* %f: <prec> fractional digits, rounded half to even, no
             * exponent. %g: the same conversion, then trailing fractional
             * zeros are stripped. Both share __goclib_double_to_buf with
             * the array printers, so the floating-point conversion lives
             * in exactly one place (see below). */
            double x = va_arg(ap, double);
            char tmp[40];
            int t = __goclib_double_to_buf(tmp, x, prec);
            if (spec == 'g') t = __goclib_double_strip_g(tmp, t);
            int k2;
            for (k2 = 0; k2 < t; k2++) {
                if (limit < 0 || n < limit) out[n] = tmp[k2];
                n++;
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

/* ----------------- shared floating-point conversion -----------------------
 * The %f/%g machinery of vfmt, extracted so the array printers can reuse it:
 * a floating-point array is printed element by element with the very same
 * digits printf("%g") would emit. __goclib_double_to_buf is the %f form
 * (<prec> fractional digits, rounded half to even, no exponent);
 * __goclib_double_strip_g drops trailing fractional zeros (the %g form);
 * __goclib_double_g is the default-%g shorthand the array printers use.
 * Rounding runs first, so "0.999999" becomes "1". */
int __goclib_double_to_buf(char *buf, double x, int prec) {
    int n = 0;
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
    if (neg) {
        buf[n] = '-';
        n++;
    }
    {
        char tmp[32];
        int t = 0;
        if (whole == 0) tmp[t++] = '0';
        long w = whole;
        while (w > 0) { tmp[t++] = (char)('0' + (w % 10)); w /= 10; }
        while (t-- > 0) { buf[n] = tmp[t]; n++; }
    }
    if (prec > 0) {
        buf[n] = '.';
        n++;
        int k2;
        for (k2 = 0; k2 < prec; k2++) {
            buf[n] = (char)('0' + dig[k2]);
            n++;
        }
    }
    return n;
}

/* Strips trailing fractional zeros after the '.' in buf[0..n): the "%g"
 * shape. Integer tails survive ("10.000000" -> "10") and a lone point goes
 * too ("1.000000" -> "1"). */
int __goclib_double_strip_g(char *buf, int n) {
    int i;
    for (i = 0; i < n; i++) {
        if (buf[i] == '.') {
            int last = n - 1;
            while (last > i && buf[last] == '0') last--;
            if (last == i) last--; /* nothing but zeros after the point */
            return last + 1;
        }
    }
    return n; /* no '.', nothing to strip */
}

/* __goclib_double_g: the %g conversion with the default six fractional
 * digits -- exactly what printf("%g") prints for a double. */
int __goclib_double_g(char *buf, double x) {
    return __goclib_double_strip_g(buf, __goclib_double_to_buf(buf, x, 6));
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

/* __goclib_long_to_buf converts v to decimal digits in buf (sign first,
 * digits big-endian) and returns the character count. Shared by long_print
 * and the array printers, which supply the newline / brackets themselves. */
int __goclib_long_to_buf(char *buf, long v) {
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
    return n;
}

int long_print(long v) {
    char buf[21]; /* sign + 20 digits (LONG_MIN's full width) */
    int n = __goclib_long_to_buf(buf, v);
    __goclib_write(buf, n);
    __goclib_write("\n", 1);
    return n + 1;
}

int int_print(int v) {
    return long_print((long)v);
}

/* Python-style array printing: "[1, 2, 3]" plus newline. The print builtin
 * passes the element count as a compile-time constant -- a C array carries
 * no length at runtime. One shared skeleton walks the array as raw bytes
 * and hands each element to a per-type converter; each public printer is a
 * thin wrapper picking the converter for its element type. Elements reuse
 * the shared digit converters (__goclib_long_to_buf for integers,
 * __goclib_double_g for floats). Adding a new element type is one converter
 * plus one wrapper; specialising a type (radix, width, ...) touches only
 * its own converter. On-demand emission links only the referenced printers
 * (and their converters, pulled in by the function-pointer reference).
 * Every function returns the number of characters written.
 *
 * The unsigned* arrays reuse the signed printers: int_array_print reads
 * ints at their true width and the int->long conversion sign-extends, so an
 * unsigned int value above INT_MAX prints negative -- the same caveat
 * printf %d has (likewise an unsigned long above LONG_MAX). Values within
 * the signed range print correctly. */

typedef int (*__goclib_array_conv)(char *buf, void *p);

/* shared skeleton: "[e1, e2, ...]" plus newline, returning chars written */
int __goclib_array_print(void *a, long n, long elem_size,
                         __goclib_array_conv conv) {
    char buf[40];
    char *p = (char *)a;
    long i;
    int total = 1; /* "[" */
    __goclib_write("[", 1);
    for (i = 0; i < n; i++) {
        int k;
        if (i > 0) {
            __goclib_write(", ", 2);
            total += 2;
        }
        k = conv(buf, p);
        __goclib_write(buf, k);
        total += k;
        p += elem_size;
    }
    __goclib_write("]\n", 2);
    return total + 2;
}

/* per-type converters: render the element at p into buf, return its length */
static int __goclib_conv_short(char *buf, void *p) {
    return __goclib_long_to_buf(buf, *(short *)p);
}
static int __goclib_conv_int(char *buf, void *p) {
    return __goclib_long_to_buf(buf, *(int *)p);
}
static int __goclib_conv_long(char *buf, void *p) {
    return __goclib_long_to_buf(buf, *(long *)p);
}
static int __goclib_conv_bool(char *buf, void *p) {
    return __goclib_long_to_buf(buf, *(_Bool *)p);
}
/* float widens to double so the %g converter serves it too. */
static int __goclib_conv_float(char *buf, void *p) {
    return __goclib_double_g(buf, (double)*(float *)p);
}
static int __goclib_conv_double(char *buf, void *p) {
    return __goclib_double_g(buf, *(double *)p);
}

int short_array_print(short *a, long n) {
    return __goclib_array_print(a, n, sizeof(short), __goclib_conv_short);
}
int int_array_print(int *a, long n) {
    return __goclib_array_print(a, n, sizeof(int), __goclib_conv_int);
}
int long_array_print(long *a, long n) {
    return __goclib_array_print(a, n, sizeof(long), __goclib_conv_long);
}
int bool_array_print(_Bool *a, long n) {
    return __goclib_array_print(a, n, sizeof(_Bool), __goclib_conv_bool);
}
int float_array_print(float *a, long n) {
    return __goclib_array_print(a, n, sizeof(float), __goclib_conv_float);
}
int double_array_print(double *a, long n) {
    return __goclib_array_print(a, n, sizeof(double), __goclib_conv_double);
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

/* ------------------------------- sscanf ---------------------------------- */
/*
 * The scanf conversions goc supports: d i u o x X (with h/l/ll lengths),
 * f e g a and friends, c, s, and "%%". Widths and "*" suppression are
 * honoured; scansets (%[...]), %p and %n are not. Floating point is parsed
 * by strtod, so the two agree by construction.
 *
 * Returns the number of items ASSIGNED (suppressed conversions do not
 * count), or -1 if the input ends before the first conversion completes.
 * That distinction is the whole reason scanf returns "assigned" rather
 * than "matched": callers test "!= 1" after asking for one item.
 */
static const char *scan_ws(const char *p) {
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r' || *p == '\v' || *p == '\f') p++;
    return p;
}

/*
 * Read an unsigned integer of the given base (0 = autodetect: 0x/0X means
 * hex, a leading 0 means octal, otherwise decimal). Stops at the first
 * character that is not a digit in that base. *ok reports whether at least
 * one digit was consumed.
 */
static unsigned long scan_uint(const char **pp, int base, int width, int *ok) {
    const char *p = *pp;
    unsigned long v = 0;
    int n = 0;
    int d;
    *ok = 0;
    if (base == 0) {
        if (p[0] == '0' && (p[1] == 'x' || p[1] == 'X')) {
            base = 16;
            p = p + 2;
        } else if (p[0] == '0') {
            base = 8;
        } else {
            base = 10;
        }
    } else if (base == 16 && p[0] == '0' && (p[1] == 'x' || p[1] == 'X')) {
        /* An explicit "%x" still accepts the 0x prefix -- scanf's %x and %i
         * agree here, and "0x1f" must not be read as the integer 0. */
        p = p + 2;
    }
    while (*p != '\0' && (width <= 0 || n < width)) {
        if (*p >= '0' && *p <= '9') {
            d = *p - '0';
        } else if (*p >= 'a' && *p <= 'z') {
            d = *p - 'a' + 10;
        } else if (*p >= 'A' && *p <= 'Z') {
            d = *p - 'A' + 10;
        } else {
            break;
        }
        if (d >= base) break;
        v = v * (unsigned long)base + (unsigned long)d;
        p++;
        n++;
        *ok = 1;
    }
    *pp = p;
    return v;
}

int sscanf(const char *s, const char *fmt, ...) {
    va_list ap;
    const char *sp = s;
    const char *fp = fmt;
    int assigned = 0;
    int suppress;
    int width;
    int lmod;
    int base;
    int ok;
    int neg;
    unsigned long uv;
    char *dst;

    va_start(ap, fmt);
    while (*fp != '\0') {
        /* Whitespace in the format matches any run of whitespace in the
         * input, including none -- it never causes a mismatch. */
        if (*fp == ' ' || *fp == '\t' || *fp == '\n') {
            sp = scan_ws(sp);
            fp++;
            continue;
        }
        if (*fp != '%') {
            if (*sp != *fp) break;
            sp++;
            fp++;
            continue;
        }
        fp++;
        if (*fp == '%') {
            if (*sp != '%') break;
            sp++;
            fp++;
            continue;
        }
        suppress = 0;
        if (*fp == '*') {
            suppress = 1;
            fp++;
        }
        width = 0;
        while (*fp >= '0' && *fp <= '9') {
            width = width * 10 + (*fp - '0');
            fp++;
        }
        /* goc's long is already 64 bits, so "l", "ll" and "L" all name the
         * same 8-byte target for an integer conversion. */
        lmod = 0;
        if (*fp == 'h' || *fp == 'l' || *fp == 'L') {
            lmod = *fp;
            fp++;
            if ((lmod == 'l' && *fp == 'l') || (lmod == 'h' && *fp == 'h')) fp++;
        }

        if (*fp == 'c') {
            int n = width > 0 ? width : 1;
            int i;
            if (suppress) {
                sp = sp + n;
            } else {
                dst = (char *)va_arg(ap, char *);
                for (i = 0; i < n && *sp != '\0'; i++) {
                    dst[i] = *sp;
                    sp++;
                }
                assigned++;
            }
            fp++;
            continue;
        }
        if (*fp == 's') {
            int n = 0;
            if (!suppress) dst = (char *)va_arg(ap, char *);
            sp = scan_ws(sp);
            while (*sp != '\0' && *sp != ' ' && *sp != '\t' && *sp != '\n' && *sp != '\r' &&
                   (width <= 0 || n < width)) {
                if (!suppress) dst[n] = *sp;
                sp++;
                n++;
            }
            if (n == 0) break;
            if (!suppress) {
                dst[n] = '\0';
                assigned++;
            }
            fp++;
            continue;
        }
        if (*fp == 'f' || *fp == 'F' || *fp == 'e' || *fp == 'E' ||
            *fp == 'g' || *fp == 'G' || *fp == 'a' || *fp == 'A') {
            char *endp;
            double dv;
            sp = scan_ws(sp);
            dv = strtod(sp, &endp);
            if (endp == sp) break;
            sp = endp;
            if (!suppress) {
                void *out = va_arg(ap, void *);
                if (lmod == 0) {
                    *(float *)out = (float)dv;
                } else {
                    *(double *)out = dv;
                }
                assigned++;
            }
            fp++;
            continue;
        }

        base = -1;
        if (*fp == 'd' || *fp == 'u') base = 10;
        else if (*fp == 'i') base = 0;
        else if (*fp == 'o') base = 8;
        else if (*fp == 'x' || *fp == 'X') base = 16;
        if (base < 0) break;                /* unknown conversion: stop */

        sp = scan_ws(sp);
        neg = 0;
        if (*sp == '+') {
            sp++;
        } else if (*sp == '-') {
            neg = 1;
            sp++;
        }
        uv = scan_uint(&sp, base, width, &ok);
        if (!ok) break;
        /* The five pointer targets below deliberately carry distinct names
         * (i16/i32/i64/f32/f64): goc resolves a local by name alone, so two
         * sibling blocks declaring "out" with different pointee types would
         * collide and store through the wrong width. See README/known bugs.
         */
        if (!suppress) {
            /* A single target pointer; goc now scopes block-local declarations
             * correctly, so the old i16/i32/i64/f32/f64 name-mangling workaround
             * is no longer needed. */
            void *out = va_arg(ap, void *);
            if (lmod == 'h') {
                *(short *)out = (short)(neg ? -(long)uv : (long)uv);
            } else if (lmod == 0) {
                *(int *)out = (int)(neg ? -(long)uv : (long)uv);
            } else {
                *(long *)out = neg ? -(long)uv : (long)uv;
            }
            assigned++;
        }
        fp++;
        continue;
    }
    va_end(ap);
    /* Nothing assigned AND nothing consumed means the input ran out before
     * the first conversion could finish -- the C "EOF" answer. */
    if (assigned == 0 && sp == s) return -1;
    return assigned;
}
