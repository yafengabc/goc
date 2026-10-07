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
/* Same rules as strtol, unsigned: a leading '-' negates modulo 2^64. */
unsigned long strtoul(const char *s, char **endp, int base);
/* long is 64-bit on both goc targets, so these are atol/labs in another
 * spelling -- declared because portable source uses these names. */
long long atoll(const char *s);
long long llabs(long long x);
/* Decimal floats, plus "inf"/"infinity"/"nan". Hexadecimal floats ("0x1p3")
 * are not recognised. On failure *endp is set to s itself. */
double strtod(const char *s, char **endp);

/* Integer division with quotient and remainder (C truncation semantics). */
typedef struct { int quot; int rem; } div_t;
typedef struct { long quot; long rem; } ldiv_t;
typedef struct { long long quot; long long rem; } lldiv_t;
div_t  div(int numer, int denom);
ldiv_t ldiv(long numer, long denom);
lldiv_t lldiv(long long numer, long long denom);

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
/* Abnormal termination: exits with the conventional failure code.
 *
 * abort, exit and quick_exit are _Noreturn by C11 7.22.4.1/7.22.4.4/7.22.4.7, and
 * the annotation is what lets a caller drop the unreachable code a compiler
 * would otherwise keep -- and what lets goclib's own definitions pass the check:
 * without it clang reports "function declared 'noreturn' should not return" for
 * both exit() and abort(), because the __goclib_exit() call at the end of each
 * body is not itself annotated and the compiler cannot see that it never
 * returns. */
_Noreturn void abort(void);

int   rand(void);
void  srand(unsigned int seed);
_Noreturn void exit(int code);

/* Largest value rand() can return (7.22.2.1). Matches the compared gcc. */
#define RAND_MAX 32767

/* ---- C11 additions ----------------------------------------------------- */
/* aligned_alloc: size bytes aligned to `alignment` (a power of two). goc's
 * heap is already 16-byte aligned, so alignment <= 16 is exact; larger
 * requests are best-effort and documented as such. */
void *aligned_alloc(size_t alignment, size_t size);
/* quick_exit runs only the handlers registered with at_quick_exit (LIFO) and
 * then terminates, skipping the atexit chain and stream flushing. */
int  at_quick_exit(void (*fn)(void));
_Noreturn void quick_exit(int code);

/* system runs `command` through the host shell and returns the child's exit
 * status (or -1 if the shell could not be started); system(NULL) returns a
 * non-zero value when a command processor is available. */
int system(const char *command);

/* ---- C23 additions ------------------------------------------------------ */
/* reallocarray: realloc(ptr, nmemb*size) with overflow-checked multiplication.
 * On overflow it sets errno to ENOMEM and returns NULL, exactly like the
 * standard, so a caller cannot accidentally allocate a wrapped-small block. */
void *reallocarray(void *ptr, size_t nmemb, size_t size);
/* free_sized / free_aligned_sized (C23 7.22.3.3/.4): sized deallocation. The
 * size / alignment arguments are a contract the caller makes about the block
 * that was returned by malloc/calloc/realloc/aligned_alloc; the allocator
 * ignores them and frees ptr exactly as free(ptr) would. */
void free_sized(void *ptr, size_t size);
void free_aligned_sized(void *ptr, size_t alignment, size_t size);

#endif /* GOC_STDLIB_H */
