#ifndef GOCLIB_H
#define GOCLIB_H

/* =============================================================================
 * goclib.h -- internal umbrella header for the goclib C library.
 *
 * goc ships real standard headers (stddef.h / stdarg.h / stdio.h / stdlib.h /
 * string.h) under goclib/ and injects them on `#include <name.h>`; user code
 * includes those, never this header. goclib/goclib.c includes goclib.h to pull
 * in the standard declarations it implements, plus the __goclib_* platform
 * primitives below, which are the only pieces that must stay in assembly
 * (goclib/goclib.asm) because they touch the OS (I/O, heap, process exit).
 *
 * goclib.c is DORMANT today: the assembly backend under goclib.asm is what goc
 * embeds and links, under the same public C names. Once goc compiles goclib.c
 * end to end, delete the public functions from the asm and keep only the
 * __goclib_* primitives; goclib.c calls those.
 * ========================================================================== */

#include <stddef.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* ---- platform primitives (supplied by goclib/goclib.asm) ------------------- */
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

#endif /* GOCLIB_H */
