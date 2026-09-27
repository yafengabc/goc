#include "c0lib.h"
#include <stdarg.h>

/* =============================================================================
 * c0lib.c — portable implementation of the c0 C library.
 *
 * See c0lib.h for the layering and the migration plan. Everything here is plain
 * C layered on the five __clib_* platform primitives. This file is dormant
 * until c0 can compile it (stage 5); the working backend today is the assembly
 * under clib/windows/ and clib/linux/.
 * ========================================================================== */

/* ----------------------------- <string.h> ------------------------------- */

size_t strlen(const char *s) {
    const char *p = s;
    while (*p) p++;
    return (size_t)(p - s);
}

char *strcpy(char *dst, const char *src) {
    char *d = dst;
    while ((*d++ = *src++) != 0)
        ;
    return dst;
}

char *strncpy(char *dst, const char *src, size_t n) {
    size_t i = 0;
    while (i < n && src[i] != 0) { dst[i] = src[i]; i++; }
    while (i < n) { dst[i] = 0; i++; }
    return dst;
}

int strcmp(const char *a, const char *b) {
    while (*a && *a == *b) { a++; b++; }
    return (unsigned char)*a - (unsigned char)*b;
}

int strncmp(const char *a, const char *b, size_t n) {
    while (n > 0 && *a && *a == *b) { a++; b++; n--; }
    if (n == 0) return 0;
    return (unsigned char)*a - (unsigned char)*b;
}

char *strcat(char *dst, const char *src) {
    strcpy(dst + strlen(dst), src);
    return dst;
}

char *strchr(const char *s, int c) {
    while (*s) {
        if ((unsigned char)*s == (unsigned char)c) return (char *)s;
        s++;
    }
    return NULL;
}

void *memset(void *dst, int v, size_t n) {
    unsigned char *p = (unsigned char *)dst;
    unsigned char b = (unsigned char)v;
    size_t i;
    for (i = 0; i < n; i++) p[i] = b;
    return dst;
}

void *memcpy(void *dst, const void *src, size_t n) {
    unsigned char *d = (unsigned char *)dst;
    const unsigned char *s = (const unsigned char *)src;
    size_t i;
    for (i = 0; i < n; i++) d[i] = s[i];
    return dst;
}

void *memmove(void *dst, const void *src, size_t n) {
    unsigned char *d = (unsigned char *)dst;
    const unsigned char *s = (const unsigned char *)src;
    if (d < s) {
        size_t i;
        for (i = 0; i < n; i++) d[i] = s[i];
    } else if (d > s) {
        size_t i = n;
        while (i-- > 0) d[i] = s[i];
    }
    return dst;
}

int memcmp(const void *a, const void *b, size_t n) {
    const unsigned char *x = (const unsigned char *)a;
    const unsigned char *y = (const unsigned char *)b;
    size_t i;
    for (i = 0; i < n; i++)
        if (x[i] != y[i]) return (int)x[i] - (int)y[i];
    return 0;
}

/* ----------------------------- <stdlib.h> ------------------------------- */

void *malloc(size_t size) {
    return __clib_heap_alloc((long)size);
}

void free(void *p) {
    __clib_heap_free(p);
}

void *calloc(size_t n, size_t size) {
    size_t total = n * size;
    void *p = __clib_heap_alloc((long)total);
    if (p) memset(p, 0, total);
    return p;
}

long atoi(const char *s) {
    return strtol(s, NULL, 10);
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
    __clib_exit((long)code);
}

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
        /* optional length modifier */
        int is_long = 0;
        if (*p == 'l') { is_long = 1; p++; }
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
        } else if (spec == 'f') {
            /* %f: 6 fractional digits, no exponent. */
            double x = va_arg(ap, double);
            if (x < 0) {
                if (limit < 0 || n < limit) out[n] = '-';
                n++;
                x = -x;
            }
            double ip;
            long whole = (long)x;
            double frac = x - (double)whole;
            /* integer part */
            char tmp[32];
            int t = 0;
            if (whole == 0) tmp[t++] = '0';
            long w = whole;
            while (w > 0) {
                tmp[t++] = (char)('0' + (w % 10));
                w /= 10;
            }
            while (t-- > 0) {
                if (limit < 0 || n < limit) out[n] = tmp[t];
                n++;
            }
            /* decimal point + 6 fractional digits */
            if (limit < 0 || n < limit) out[n] = '.';
            n++;
            int k;
            for (k = 0; k < 6; k++) {
                frac *= 10.0;
                int d = (int)frac;
                frac -= (double)d;
                if (limit < 0 || n < limit) out[n] = (char)('0' + d);
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
    __clib_write(buf, n);
    return n;
}

int puts(const char *s) {
    long n = (long)strlen(s);
    __clib_write(s, n);
    __clib_write("\n", 1);
    return 0;
}

int putchar(int c) {
    char b = (char)c;
    __clib_write(&b, 1);
    return c;
}

int getchar(void) {
    char b;
    long n = __clib_read(&b, 1);
    if (n <= 0) return -1;
    return (unsigned char)b;
}
