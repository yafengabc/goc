#ifndef GOC_STRING_H
#define GOC_STRING_H

#include <stddef.h>

/* goc string.h -- byte and string manipulation (C11 signatures).
 *
 * strlen returns size_t and the copy functions return the destination
 * pointer, exactly as in the standard. Implementations are linked in from
 * goclib on demand; this header only carries declarations.
 */

size_t strlen(const char *s);
char  *strcpy(char *dest, const char *src);
char  *strncpy(char *dest, const char *src, size_t n);
int    strcmp(const char *s1, const char *s2);
int    strncmp(const char *s1, const char *s2, size_t n);
char  *strcat(char *dest, const char *src);
char  *strncat(char *dest, const char *src, size_t n);
char  *strchr(const char *s, int c);
char  *strrchr(const char *s, int c);
char  *strstr(const char *haystack, const char *needle);
size_t strspn(const char *s, const char *accept);
size_t strcspn(const char *s, const char *reject);
char  *strpbrk(const char *s, const char *accept);
char  *strtok(char *str, const char *delim);
void  *memset(void *s, int c, size_t n);
void  *memset_explicit(void *s, int c, size_t n); /* C23: clearing that the implementation must not elide */
void  *memcpy(void *dest, const void *src, size_t n);
void  *memmove(void *dest, const void *src, size_t n);
int    memcmp(const void *s1, const void *s2, size_t n);
/* First byte equal to (unsigned char)c in the first n bytes, else 0.
 * Searching for a NUL makes this strnchr. */
void  *memchr(const void *s, int c, size_t n);

/* ---- extensions beyond C89 --------------------------------------------- */
/* Both come from POSIX (and strnlen is C23); they are here because they are
 * the two string functions programs reach for most after the C89 set.
 * strdup mallocs its copy, so the caller frees it. */
char  *strdup(const char *s);
/* Length of s, stopping at n -- never reads past n bytes, so it is safe on a
 * buffer that is not NUL-terminated. */
size_t strnlen(const char *s, size_t n);

/* ---- more POSIX / GNU extensions -------------------------------------- */
/* stpcpy copies src into dest and returns a pointer to the terminating NUL
 * it wrote (handy for chaining appends). */
char  *stpcpy(char *dest, const char *src);
/* strndup copies at most n bytes (then NUL-terminates) into a fresh malloc. */
char  *strndup(const char *s, size_t n);
/* memrchr: last byte equal to (unsigned char)c in the first n bytes, else 0. */
void  *memrchr(const void *s, int c, size_t n);
/* memccpy copies src->dest, stopping after the first byte == (unsigned char)c.
 * Returns dest + (index_of_c + 1), or NULL if c is not found within n bytes. */
void  *memccpy(void *dest, const void *src, int c, size_t n);

#endif /* GOC_STRING_H */
