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
/* Resize a block from malloc/calloc/realloc, keeping the first min(old,new)
 * bytes. A null `ptr` makes this malloc(size). */
void *realloc(void *ptr, size_t size);
int   atoi(const char *s);
long  atol(const char *s);
double atof(const char *s);
int   abs(int x);
long  labs(long x);
long  strtol(const char *s, char **endp, int base);
/* Decimal floats, plus "inf"/"infinity"/"nan". Hexadecimal floats ("0x1p3")
 * are not recognised. On failure *endp is set to s itself. */
double strtod(const char *s, char **endp);

/* Integer division with quotient and remainder (C truncation semantics). */
typedef struct { int quot; int rem; } div_t;
typedef struct { long quot; long rem; } ldiv_t;
div_t  div(int numer, int denom);
ldiv_t ldiv(long numer, long denom);

/* Sort/search on raw memory. cmp(a, b) returns <0 / 0 / >0. qsort is an
 * in-place quicksort (median-of-three pivot, insertion sort on small runs);
 * bsearch requires the array sorted in ascending cmp order. */
void qsort(void *base, size_t nmemb, size_t size,
           int (*cmp)(const void *, const void *));
void *bsearch(const void *key, const void *base, size_t nmemb, size_t size,
              int (*cmp)(const void *, const void *));

/* Environment lookup. The returned string lives in a static buffer that the
 * next call overwrites. An unset or empty name yields 0. */
char *getenv(const char *name);

/* Register fn to run at exit() (LIFO, last registered runs first). Max 32. */
int atexit(void (*fn)(void));
/* Abnormal termination: exits with the conventional failure code. */
void abort(void);

int   rand(void);
void  srand(unsigned int seed);
void  exit(int code);

#endif /* GOC_STDLIB_H */
