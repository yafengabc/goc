#ifndef GOC_UCHAR_H
#define GOC_UCHAR_H

#include <stddef.h>

/* goc uchar.h -- char16_t/char32_t and the UTF conversion entry points
 * (C11 7.28).
 *
 * char8_t is deliberately absent (goc has no u8 string storage distinct from
 * char). mbstate_t is an opaque struct the conversion functions thread
 * through; implementations live in goclib uchar.c and convert between UTF-8
 * multibyte input and UTF-16/32 code units with the C11 return protocol
 * ((size_t)-3 surrogate handling, -2 incomplete, -1 encoding error).
 */

typedef unsigned short char16_t;
typedef unsigned int   char32_t;

typedef struct __goc_mbstate { unsigned long __c; } mbstate_t;

size_t mbrtoc16(char16_t *restrict pc16, const char *restrict s,
                size_t n, mbstate_t *restrict ps);
size_t c16rtomb(char *restrict s, char16_t wc16, mbstate_t *restrict ps);
size_t mbrtoc32(char32_t *restrict pc32, const char *restrict s,
                size_t n, mbstate_t *restrict ps);
size_t c32rtomb(char *restrict s, char32_t wc32, mbstate_t *restrict ps);

#endif /* GOC_UCHAR_H */
