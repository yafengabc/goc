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
char  *strchr(const char *s, int c);
void  *memset(void *s, int c, size_t n);
void  *memcpy(void *dest, const void *src, size_t n);
void  *memmove(void *dest, const void *src, size_t n);
int    memcmp(const void *s1, const void *s2, size_t n);

#endif /* GOC_STRING_H */
