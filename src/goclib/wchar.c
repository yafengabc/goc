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

/* The standard only fixes the SIGN of a str/wcscmp result, but returning the
 * raw character difference makes the value depend on the code page -- on
 * Windows wchar_t is UTF-16 so 'A' - 'z' is -57, while on Linux the same
 * values are -25. Collapsing to -1/0/1 makes the two targets agree, which is
 * what lets a cross-built program compare results directly. */
int wcscmp(const wchar_t *a, const wchar_t *b) {
    while (*a && *a == *b) { a++; b++; }
    if (*a == *b) return 0;
    return (*a < *b) ? -1 : 1;
}

int wcsncmp(const wchar_t *a, const wchar_t *b, size_t n) {
    size_t i;
    for (i = 0; i < n; i++) {
        if (a[i] != b[i]) return (a[i] < b[i]) ? -1 : 1;
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

/* wcsspn: length of the INITIAL segment of `s` made up ENTIRELY of
 * characters that appear in `set` -- so it stops at the first character
 * wcschr does NOT find. */
size_t wcsspn(const wchar_t *s, const wchar_t *set) {
    const wchar_t *start = s;
    for (; *s && wcschr(set, *s); s++) { }
    return (size_t)(s - start);
}

/* wcscspn: the mirror image -- length of the initial segment containing
 * NO character from `set`, so it stops at the first character wcschr DOES
 * find. (These two differ only in the sense of the wcschr test; getting them
 * the same way round is the classic transcription slip.) */
size_t wcscspn(const wchar_t *s, const wchar_t *set) {
    const wchar_t *start = s;
    for (; *s && !wcschr(set, *s); s++) { }
    return (size_t)(s - start);
}

/* wcstok with a NULL `save` must behave like strtok and keep the position
 * internally -- C99 7.24.5.8 explicitly allows `save` to be a null pointer,
 * and the common first-argument-is-NULL continuation depends on it. A static
 * cursor is the whole of the state, matching strtok's `tok_save`. */
static wchar_t *wcstok_save;

wchar_t *wcstok(wchar_t *s, const wchar_t *delim, wchar_t **save) {
    wchar_t *start;
    if (s == 0) s = save ? *save : wcstok_save;
    if (s == 0) return 0;
    /* skip leading delimiters */
    while (*s && wcschr(delim, *s)) s++;
    if (*s == 0) {
        if (save) *save = s; else wcstok_save = s;
        return 0;
    }
    start = s;
    while (*s && !wcschr(delim, *s)) s++;
    if (*s) { *s = 0; s++; }
    if (save) *save = s; else wcstok_save = s;
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

/* ---- wide memory functions ------------------------------------------------
 * The wchar_t counterparts of <string.h>'s mem* family. They are written in
 * terms of the same loop shape as the narrow versions so the two agree on
 * edge cases: wmemcpy does NOT tolerate overlap (that is wmemmove's job), and
 * both stop after n units regardless of any embedded NUL -- a wide "string"
 * here is an array of units, not text. */
wchar_t *wmemcpy(wchar_t *dest, const wchar_t *src, size_t n) {
    size_t i;
    /* Copy backwards when the ranges overlap, so the result is the same as
     * memmove even though the standard forbids overlap here. Cheap safety on
     * a freestanding libc, and it removes a class of silent corruption. */
    if (src < dest && src + n > dest) {
        for (i = n; i > 0; i--) dest[i - 1] = src[i - 1];
        return dest;
    }
    for (i = 0; i < n; i++) dest[i] = src[i];
    return dest;
}

wchar_t *wmemmove(wchar_t *dest, const wchar_t *src, size_t n) {
    size_t i;
    if (src < dest && src + n > dest) {
        for (i = n; i > 0; i--) dest[i - 1] = src[i - 1];
    } else {
        for (i = 0; i < n; i++) dest[i] = src[i];
    }
    return dest;
}

wchar_t *wmemchr(const wchar_t *s, wchar_t c, size_t n) {
    size_t i;
    for (i = 0; i < n; i++) if (s[i] == c) return (wchar_t *)&s[i];
    return NULL;
}

int wmemcmp(const wchar_t *a, const wchar_t *b, size_t n) {
    size_t i;
    for (i = 0; i < n; i++) {
        if (a[i] != b[i]) return a[i] < b[i] ? -1 : 1;
    }
    return 0;
}

wchar_t *wmemset(wchar_t *s, wchar_t c, size_t n) {
    size_t i;
    for (i = 0; i < n; i++) s[i] = c;
    return s;
}
