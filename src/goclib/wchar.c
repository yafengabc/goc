#include "goclib.h"
#include <wchar.h>

/* =============================================================================
 * wchar.c -- the C95 wide-character string functions.
 *
 * wchar_t is `unsigned short` on Win64 (UTF-16LE, the OS's own encoding) and
 * 32-bit on Linux, so every loop here is written in terms of wchar_t rather
 * than assuming a width. The algorithms mirror the byte versions in
 * string.c one-for-one, so the two have the same edge-case behaviour.
 * ========================================================================== */

size_t wcslen(const wchar_t *s) {
    const wchar_t *p = s;
    while (*p) p++;
    return (size_t)(p - s);
}

wchar_t *wcscpy(wchar_t *dest, const wchar_t *src) {
    wchar_t *d = dest;
    while ((*d++ = *src++) != 0) { }
    return dest;
}

int wcsncpy(wchar_t *dest, const wchar_t *src, size_t n) {
    size_t i = 0;
    for (; i < n && src[i]; i++) dest[i] = src[i];
    /* pad the remainder with NUL, as C99 requires, rather than stopping */
    for (; i < n; i++) dest[i] = 0;
    return 0;
}

wchar_t *wcscat(wchar_t *dest, const wchar_t *src) {
    wchar_t *d = dest + wcslen(dest);
    while ((*d++ = *src++) != 0) { }
    return dest;
}

int wcsncat(wchar_t *dest, const wchar_t *src, size_t n) {
    wchar_t *d = dest + wcslen(dest);
    size_t i;
    for (i = 0; i < n && src[i]; i++) d[i] = src[i];
    d[i] = 0;
    return 0;
}

int wcscmp(const wchar_t *a, const wchar_t *b) {
    while (*a && *a == *b) { a++; b++; }
    return (int)*a - (int)*b;
}

int wcsncmp(const wchar_t *a, const wchar_t *b, size_t n) {
    size_t i;
    for (i = 0; i < n; i++) {
        if (a[i] != b[i]) return (int)a[i] - (int)b[i];
        if (a[i] == 0) return 0;
    }
    return 0;
}

int wcscoll(const wchar_t *a, const wchar_t *b) {
    return wcscmp(a, b);
}

size_t wcsxfrm(wchar_t *dest, const wchar_t *src, size_t n) {
    size_t i = 0;
    for (; i < n && src[i]; i++) dest[i] = src[i];
    if (i < n) dest[i] = 0;
    return i;
}

wchar_t *wcschr(const wchar_t *s, wchar_t c) {
    for (; *s; s++) {
        if (*s == c) return (wchar_t *)s;
    }
    return c == 0 ? (wchar_t *)s : 0;
}

wchar_t *wcsrchr(const wchar_t *s, wchar_t c) {
    const wchar_t *last = 0;
    for (;; s++) {
        if (*s == c) last = s;
        if (*s == 0) break;
    }
    return (wchar_t *)last;
}

wchar_t *wcsstr(const wchar_t *hay, const wchar_t *needle) {
    size_t nl;
    if (*needle == 0) return (wchar_t *)hay;
    nl = wcslen(needle);
    for (; *hay; hay++) {
        if (wcsncmp(hay, needle, nl) == 0) return (wchar_t *)hay;
    }
    return 0;
}

wchar_t *wcspbrk(const wchar_t *s, const wchar_t *set) {
    for (; *s; s++) {
        if (wcschr(set, *s)) return (wchar_t *)s;
    }
    return 0;
}

size_t wcsspn(const wchar_t *s, const wchar_t *set) {
    const wchar_t *start = s;
    for (; *s && !wcschr(set, *s); s++) { }
    return (size_t)(s - start);
}

size_t wcscspn(const wchar_t *s, const wchar_t *set) {
    const wchar_t *start = s;
    for (; *s && !wcschr(set, *s); s++) { }
    return (size_t)(s - start);
}

wchar_t *wcstok(wchar_t *s, const wchar_t *delim, wchar_t **save) {
    wchar_t *start;
    if (s == 0) s = *save;
    if (s == 0) return 0;
    /* skip leading delimiters */
    while (*s && wcschr(delim, *s)) s++;
    if (*s == 0) { *save = s; return 0; }
    start = s;
    while (*s && !wcschr(delim, *s)) s++;
    if (*s) { *s = 0; *save = s + 1; }
    else   { *save = s; }
    return start;
}

/* ---- MSVC secure variants -------------------------------------------- */

int wcscpy_s(wchar_t *dest, size_t destsz, const wchar_t *src) {
    size_t n = wcslen(src);
    if (destsz == 0) return -1;
    /* MSVC leaves dest untouched (and sets the invalid-parameter handler) on
     * overflow; returning -1 with no write is the useful half of that. */
    if (n >= destsz) return -1;
    wcscpy(dest, src);
    return 0;
}

int wcscat_s(wchar_t *dest, size_t destsz, const wchar_t *src) {
    size_t dn = wcslen(dest);
    size_t sn = wcslen(src);
    if (dn >= destsz) return -1;
    if (sn >= destsz - dn) return -1;
    wcscat(dest, src);
    return 0;
}

int wcsncpy_s(wchar_t *dest, size_t destsz, const wchar_t *src, size_t n) {
    size_t sl = wcslen(src);
    size_t cnt = (sl < n) ? sl : n;   /* never copies the NUL */
    if (destsz == 0) return -1;
    if (cnt >= destsz) return -1;
    wcsncpy(dest, src, cnt);
    dest[cnt] = 0;
    return 0;
}

int wcsncat_s(wchar_t *dest, size_t destsz, const wchar_t *src, size_t n) {
    size_t dn = wcslen(dest);
    size_t sl = wcslen(src);
    size_t cnt = (sl < n) ? sl : n;
    if (dn >= destsz) return -1;
    if (cnt >= destsz - dn) return -1;
    wcsncat(dest, src, cnt);
    return 0;
}
