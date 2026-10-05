#ifndef GOC_WCHAR_H
#define GOC_WCHAR_H

#include <stddef.h>

/* goc wchar.h -- the C95 wide-character string functions.
 *
 * wchar_t is `unsigned short` (see <stddef.h>): on Win64 that is exactly what
 * the OS uses (UTF-16LE), so a wide string from GetCommandLineW or
 * GetOpenFileNameW can be passed straight to these with no conversion. On
 * Linux wchar_t is 32-bit, which is what the ELF target's wchar_t already is
 * -- the definitions below are written in terms of the type, so both work.
 *
 * The MSVC "secure" _s variants are included because Windows code uses them
 * pervasively; they differ only in the return value (0 on success, -1 when the
 * destination is too small), which is what makes them safe to ignore errors
 * from. */

/* ---- C95 wide string functions ---------------------------------------- */

/* Length in wchar_t units, not bytes. */
size_t wcslen(const wchar_t *s);

/* Copy including the terminating NUL; returns the number of units written. */
wchar_t *wcscpy(wchar_t *dest, const wchar_t *src);
int      wcsncpy(wchar_t *dest, const wchar_t *src, size_t n);
wchar_t *wcscat(wchar_t *dest, const wchar_t *src);
int      wcsncat(wchar_t *dest, const wchar_t *src, size_t n);

/* Three-way compare, like strcmp: <0, 0, >0. */
int      wcscmp(const wchar_t *a, const wchar_t *b);
int      wcsncmp(const wchar_t *a, const wchar_t *b, size_t n);
int      wcscoll(const wchar_t *a, const wchar_t *b);
size_t   wcsxfrm(wchar_t *dest, const wchar_t *src, size_t n);

/* Search. wcsrchr returns the last occurrence; wcsstr a substring. */
wchar_t *wcschr(const wchar_t *s, wchar_t c);
wchar_t *wcsrchr(const wchar_t *s, wchar_t c);
wchar_t *wcsstr(const wchar_t *hay, const wchar_t *needle);
size_t   wcsspn(const wchar_t *s, const wchar_t *set);
size_t   wcscspn(const wchar_t *s, const wchar_t *set);
wchar_t *wcspbrk(const wchar_t *s, const wchar_t *set);
wchar_t *wcstok(wchar_t *s, const wchar_t *delim, wchar_t **save);

/* ---- MSVC secure variants -------------------------------------------- */
/* Return 0 on success and -1 if the result would not fit, leaving dest
 * unchanged on failure (unlike the C95 functions, which always truncate). */
int wcscpy_s(wchar_t *dest, size_t destsz, const wchar_t *src);
int wcscat_s(wchar_t *dest, size_t destsz, const wchar_t *src);
int wcsncpy_s(wchar_t *dest, size_t destsz, const wchar_t *src, size_t n);
int wcsncat_s(wchar_t *dest, size_t destsz, const wchar_t *src, size_t n);

#endif /* GOC_WCHAR_H */
