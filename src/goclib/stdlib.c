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
