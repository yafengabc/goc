#include "goclib.h"
#include <stdarg.h>

/* =============================================================================
 * goclib.c — portable implementation of the goc C library.
 *
 * See goclib.h for the layering. Everything here is plain C: goc compiles this
 * file at start-up (per target) and emits exactly the functions a program
 * needs, through the same code generator it uses for user code. The only
 * OS-aware code is the five __goclib_* primitives at the top of this file --
 * kernel32 calls on Windows, raw syscall stubs on Linux.
 * ========================================================================== */

/* ------------------------- platform primitives ---------------------------- */
/*
 * The five __goclib_* OS primitives. Everything below this section is
 * portable C; only here does the library know the OS. On Windows the calls
 * go through the win32.def import table; on Linux goa turns each extern
 * name into a "mov rax,N; syscall; ret" stub (see externLinux in
 * codegen.go).
 */
#if defined(_WIN32)

extern void *GetStdHandle(long which);
extern long  WriteFile(void *h, const void *buf, long n, long *written, long overlapped);
extern long  ReadFile(void *h, void *buf, long n, long *got, long overlapped);
extern void  ExitProcess(long code);
extern void *GetProcessHeap(void);
extern void *HeapAlloc(void *heap, long flags, long bytes);
extern long  HeapFree(void *heap, long flags, void *block);

long __goclib_write(const char *buf, long len) {
    long written = 0;
    void *h = GetStdHandle(-11);            /* STD_OUTPUT_HANDLE */
    if (h == 0) return -1;
    if (len > 0) {
        if (!WriteFile(h, buf, len, &written, 0)) return -1;
    }
    return written;
}

long __goclib_read(char *buf, long len) {
    long got = 0;
    void *h = GetStdHandle(-10);            /* STD_INPUT_HANDLE */
    if (h == 0) return -1;
    if (len > 0) {
        if (!ReadFile(h, buf, len, &got, 0)) return -1;
    }
    return got;
}

void __goclib_exit(long code) {
    ExitProcess(code);
}

void *__goclib_heap_alloc(long size) {
    if (size <= 0) size = 1;
    return HeapAlloc(GetProcessHeap(), 0, size);
}

void __goclib_heap_free(void *p) {
    if (p != 0) HeapFree(GetProcessHeap(), 0, p);
}

#elif defined(__linux__)

/*
 * exit_group (231) instead of exit (60): the library itself defines a
 * function named exit, and one output cannot carry both symbols. The entry
 * stub calls the C exit, which calls __goclib_exit, which lands here.
 */
extern long write(long fd, const void *buf, long n);
extern long read(long fd, void *buf, long n);
extern void *brk(void *addr);
extern void exit_group(long code);

static char *heap_cur;                      /* brk bump-allocator cursor */

long __goclib_write(const char *buf, long len) {
    if (len <= 0) return 0;
    return write(1, buf, len);
}

long __goclib_read(char *buf, long len) {
    if (len <= 0) return 0;
    return read(0, buf, len);
}

void __goclib_exit(long code) {
    exit_group(code);
}

void *__goclib_heap_alloc(long size) {
    long need;
    char *next;
    char *p;
    if (size <= 0) size = 1;
    need = (size + 15) / 16 * 16;           /* 16-byte aligned */
    if (heap_cur == 0) {
        heap_cur = (char *)brk((void *)0);  /* query the current break */
    }
    next = heap_cur + need;
    if ((char *)brk((void *)next) != next) {
        return 0;                           /* failed: brk returns the old break */
    }
    p = heap_cur;
    heap_cur = next;
    return p;
}

void __goclib_heap_free(void *p) {
    /* bump allocator: nothing to do until the process exits. */
}

#else
#error "goclib: unknown target (need _WIN32 or __linux__)"
#endif

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

int atoi(const char *s) {
    return (int)strtol(s, NULL, 10);
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
    __goclib_exit((long)code);
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
            /* decimal point + prec fractional digits (prec 0: no point) */
            if (prec > 0) {
                if (limit < 0 || n < limit) out[n] = '.';
                n++;
            }
            int k;
            for (k = 0; k < prec; k++) {
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

int getchar(void) {
    char b;
    long n = __goclib_read(&b, 1);
    if (n <= 0) return -1;
    return (unsigned char)b;
}
