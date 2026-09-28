#include "goclib.h"

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
