#ifndef GOCLIB_H
#define GOCLIB_H

/* =============================================================================
 * goclib — the canonical, cross-platform C library for goc.
 *
 * This header + goclib.c are the *intended* backend for goc. They are written in
 * plain, portable C (char / pointer / static / for / stdarg) and implement the
 * whole <stddef.h>/<string.h>/<stdlib.h>/<stdio.h> subset in one place.
 *
 * They are DORMANT today: goc's C subset (stage 4 and earlier) cannot compile
 * them — it lacks char, real pointers, global/static variables, for-loops and
 * variadic functions. The backend that actually runs right now is the assembly
 * under goclib/windows/ and goclib/linux/ (same C names, different bodies), which
 * goc embeds and links.
 *
 * Once goc grows those features (the stage-5 plan), the migration is:
 *   1. Make goc compile goclib/goclib.c into the user program's translation unit.
 *   2. Carve the five platform primitives out of the asm and expose them with
 *      the __goclib_ prefix on BOTH targets (see the extern block below).
 *   3. Delete the public functions from goclib/windows/ and goclib/linux/ — they
 *      are now provided by goclib.c, which calls the __goclib_* primitives.
 *
 * The __goclib_* primitives are the ONLY things that must stay in assembly,
 * because they touch the OS (I/O, heap, process exit, stdin). Everything else
 * is portable C.
 * ========================================================================== */

typedef unsigned long size_t;
typedef long          ptrdiff_t;

/* ---- platform primitives (supplied by goclib/windows and goclib/linux) -------- */
/* Write `len` bytes from `buf` to standard output. Returns bytes written. */
long __goclib_write(const char *buf, long len);
/* Terminate the process with `code` (does not return). */
void __goclib_exit(long code);
/* Allocate `size` bytes from the process heap; returns 0 on failure. */
void *__goclib_heap_alloc(long size);
/* Free a block previously returned by __goclib_heap_alloc. */
void  __goclib_heap_free(void *p);
/* Read up to `len` bytes from standard input into `buf`; <=0 at EOF. */
long __goclib_read(char *buf, long len);

/* ----------------------------- <stddef.h> ------------------------------- */
#define NULL ((void *)0)

/* ----------------------------- <string.h> ------------------------------- */
size_t strlen(const char *s);
char  *strcpy(char *dst, const char *src);
char  *strncpy(char *dst, const char *src, size_t n);
int    strcmp(const char *a, const char *b);
int    strncmp(const char *a, const char *b, size_t n);
char  *strcat(char *dst, const char *src);
char  *strchr(const char *s, int c);
void  *memset(void *dst, int v, size_t n);
void  *memcpy(void *dst, const void *src, size_t n);
void  *memmove(void *dst, const void *src, size_t n);
int    memcmp(const void *a, const void *b, size_t n);

/* ----------------------------- <stdlib.h> ------------------------------- */
void  *malloc(size_t size);
void   free(void *p);
void  *calloc(size_t n, size_t size);
long   atoi(const char *s);
int    abs(int x);
long   strtol(const char *s, char **endp, int base);
int    rand(void);
void   srand(unsigned int seed);
void   exit(int code);

/* ----------------------------- <stdio.h> --------------------------------- */
int  printf(const char *fmt, ...);
int  sprintf(char *buf, const char *fmt, ...);
int  puts(const char *s);
int  putchar(int c);
int  getchar(void);

#endif /* GOCLIB_H */
