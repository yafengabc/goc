#include "goclib.h"
#include <stdint.h>

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

char *strncat(char *dst, const char *src, size_t n) {
    char *d = dst + strlen(dst);
    size_t i = 0;
    while (i < n && src[i] != 0) { d[i] = src[i]; i++; }
    d[i] = 0;
    return dst;
}

char *strchr(const char *s, int c) {
    while (*s) {
        if ((unsigned char)*s == (unsigned char)c) return (char *)s;
        s++;
    }
    return 0;
}

char *strrchr(const char *s, int c) {
    const char *found = 0;
    while (*s) {
        if ((unsigned char)*s == (unsigned char)c) found = s;
        s++;
    }
    return (char *)found;
}

char *strstr(const char *hay, const char *needle) {
    size_t n = strlen(needle);
    if (n == 0) return (char *)hay;
    while (*hay) {
        if (*hay == *needle && strncmp(hay, needle, n) == 0) return (char *)hay;
        hay++;
    }
    return 0;
}

size_t strspn(const char *s, const char *accept) {
    size_t i = 0;
    while (s[i] && strchr(accept, s[i])) i++;
    return i;
}

size_t strcspn(const char *s, const char *reject) {
    size_t i = 0;
    while (s[i] && !strchr(reject, s[i])) i++;
    return i;
}

char *strpbrk(const char *s, const char *accept) {
    while (*s) {
        if (strchr(accept, *s)) return (char *)s;
        s++;
    }
    return 0;
}

static char *tok_save;

char *strtok(char *s, const char *delim) {
    char *start;
    if (s == 0) s = tok_save;
    if (s == 0) return 0;
    s += strspn(s, delim);                  /* skip leading delimiters */
    if (*s == 0) { tok_save = 0; return 0; }
    start = s;
    s += strcspn(s, delim);                 /* find the end of the token */
    if (*s != 0) { *s = 0; s++; }
    tok_save = s;
    return start;
}

/* The byte loops below look like the textbook definitions of memset, memcpy and
 * memmove, and that is the problem on a host compiler: at -O2, gcc recognises the
 * pattern, decides the loop is a call to the library function of that name, and
 * emits `call memset` inside memset. Measured on gcc 15.2 with musl, the result
 * disassembles as
 *
 *     memset: test %rdx,%rdx / je .L0 / sub $8,%rsp / call memset / add $8,%rsp
 *
 * which is unbounded recursion, reached from musl's own calloc the first time
 * anything calls it. It never showed under goc because goc's code generator
 * emits the loop verbatim.
 *
 * The fix is `volatile` on the destination pointer: it keeps the stores in the
 * order written, which is exactly what C23 asks of memset_explicit, and it denies
 * the built-in matcher the pattern it looks for. Verified on gcc 15.2 -- the same
 * source without volatile compiles to the self-call above, and with it the loop
 * survives. memset_explicit below is unchanged in behaviour; it was already
 * volatile for the dead-store reason, so it now reads the same way.
 */
void *memset(void *dst, int v, size_t n) {
    volatile unsigned char *p = (volatile unsigned char *)dst;
    unsigned char b = (unsigned char)v;
    size_t i;
    for (i = 0; i < n; i++) p[i] = b;
    return dst;
}

/* memset_explicit (C23): identical to memset, but the compiler must not optimize
 * the store away even if the buffer is never read again. goc has no DSE pass
 * that would elide a dead memset, so the volatile loop above is already
 * compliant -- same body, same reason for volatile. */
void *memset_explicit(void *dst, int v, size_t n) {
    volatile unsigned char *p = (volatile unsigned char *)dst;
    unsigned char b = (unsigned char)v;
    size_t i;
    for (i = 0; i < n; i++) p[i] = b;
    return dst;
}

void *memcpy(void *dst, const void *src, size_t n) {
    volatile unsigned char *d = (volatile unsigned char *)dst;
    const unsigned char *s = (const unsigned char *)src;
    size_t i;
    for (i = 0; i < n; i++) d[i] = s[i];
    return dst;
}

void *memmove(void *dst, const void *src, size_t n) {
    volatile unsigned char *d = (volatile unsigned char *)dst;
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

void *memchr(const void *s, int c, size_t n) {
    const unsigned char *p = (const unsigned char *)s;
    unsigned char want = (unsigned char)c;
    size_t i;
    for (i = 0; i < n; i++)
        if (p[i] == want) return (void *)(p + i);
    return 0;
}

size_t strnlen(const char *s, size_t n) {
    size_t i;
    for (i = 0; i < n; i++)
        if (s[i] == '\0') return i;
    return n;
}

/* malloc + memcpy. A null argument is passed through as null rather than
 * being turned into a one-byte allocation, so callers that treat strdup(0)
 * as "nothing to copy" keep working. */
char *strdup(const char *s) {
    size_t n;
    char *p;
    if (s == 0) return 0;
    n = strlen(s) + 1;
    p = (char *)malloc(n);
    if (p == 0) return 0;
    memcpy(p, s, n);
    return p;
}

/* ---- MSVC spellings --------------------------------------------------- */
/* _strdup is deprecated-but-widely-used MSVC spelling of strdup; the
 * leading underscore is the only difference. */
char *_strdup(const char *s) {
    return strdup(s);
}

/* _stricmp / _strnicmp are the MSVC case-insensitive compares. They fold only
 * ASCII, which is what the CRT's "C" locale does; that is enough for the
 * ASCII-only identifiers and paths this is normally used on. */
static int lower_ascii(int c) {
    return (c >= 'A' && c <= 'Z') ? c - 'A' + 'a' : c;
}

int _stricmp(const char *a, const char *b) {
    int x, y;
    do {
        x = lower_ascii((unsigned char)*a++);
        y = lower_ascii((unsigned char)*b++);
    } while (x && x == y);
    return x - y;
}

int _strnicmp(const char *a, const char *b, size_t n) {
    int x = 0, y = 0;
    while (n-- > 0) {
        x = lower_ascii((unsigned char)*a++);
        y = lower_ascii((unsigned char)*b++);
        if (!x || x != y) return x - y;
    }
    return 0;
}

char *stpcpy(char *dest, const char *src) {
    while ((*dest++ = *src++) != 0)
        ;
    return dest - 1;            /* point at the NUL we just wrote */
}

char *strndup(const char *s, size_t n) {
    size_t i;
    char *p;
    if (s == 0) return 0;
    for (i = 0; i < n && s[i] != 0; i++)
        ;
    p = (char *)malloc(i + 1);
    if (p == 0) return 0;
    memcpy(p, s, i);
    p[i] = 0;
    return p;
}

void *memrchr(const void *s, int c, size_t n) {
    const unsigned char *p = (const unsigned char *)s;
    unsigned char want = (unsigned char)c;
    size_t i = n;
    while (i-- > 0)
        if (p[i] == want) return (void *)(p + i);
    return 0;
}

/* memccpy copies bytes from src to dest, stopping after the first byte equal
 * to (unsigned char)c. Returns a pointer to the byte in dest just past that
 * copy (i.e. dest + position_of_c + 1), or NULL if c is not found within the
 * first n bytes. Unlike memchr/memrchr it writes while it scans. */
void *memccpy(void *dest, const void *src, int c, size_t n) {
    unsigned char *d = (unsigned char *)dest;
    const unsigned char *s = (const unsigned char *)src;
    unsigned char want = (unsigned char)c;
    size_t i;
    for (i = 0; i < n; i++) {
        d[i] = s[i];
        if (s[i] == want) return (void *)(d + i + 1);
    }
    return 0;
}

/* memalignment (C23): the largest power of two dividing the address. Reading
 * (a & 1) tells us the lowest bit; each time it is zero we fold the address
 * right by one and double the result. A null pointer (a == 0) returns 1, which
 * is the safe degenerate the standard prescribes. uintptr_t comes from stdint.h,
 * included at the top of this file. */
size_t memalignment(const void *p) {
    uintptr_t a = (uintptr_t)p;
    if (a == 0) return 1;
    size_t r = 1;
    while ((a & 1) == 0) { a >>= 1; r <<= 1; }
    return r;
}
