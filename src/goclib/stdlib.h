#ifndef GOC_STDLIB_H
#define GOC_STDLIB_H

#include <stddef.h>

/* goc stdlib.h -- memory management and conversion utilities.
 *
 * Signatures follow C11 (atoi returns int; strtol returns long), and the
 * declarations are the ones goclib implements. Implementations are linked in
 * from goclib on demand; this header only carries declarations.
 */

void *malloc(size_t size);
void  free(void *ptr);
void *calloc(size_t n, size_t size);
int   atoi(const char *s);
int   abs(int x);
long  strtol(const char *s, char **endp, int base);
int   rand(void);
void  srand(unsigned int seed);
void  exit(int code);

#endif /* GOC_STDLIB_H */
