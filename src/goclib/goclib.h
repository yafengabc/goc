#ifndef GOCLIB_H
#define GOCLIB_H

/* =============================================================================
 * goclib.h -- internal umbrella header for the goclib C library.
 *
 * goc ships real standard headers (ctype.h / stddef.h / stdarg.h / stdio.h /
 * stdlib.h / string.h / math.h, plus the macro-only limits.h and float.h)
 * under goclib/ and injects them on `#include <name.h>`; user code includes
 * those, never this header. Each goclib/*.c implementation
 * file (os.c / stdio.c / stdlib.c / string.c / ctype.c) includes goclib.h to
 * pull in the standard declarations it implements, plus the __goclib_*
 * platform primitives declared below -- the only pieces that touch the OS
 * directly (I/O, heap, process exit).
 *
 * goc compiles the goclib .c files at start-up for the selected target and
 * emits the functions a program actually calls through its regular code
 * generator; the __goclib_* primitives reach the OS via extern imports
 * (kernel32 on Windows, goa syscall stubs on Linux).
 * ========================================================================== */

#include <ctype.h>
#include <stddef.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* ---- platform primitives (implemented in goclib.c, OS glue) ---------------- */
/* Write `len` bytes from `buf` to standard output. Returns bytes written. */
long __goclib_write(const char *buf, long len);
/* Terminate the process with `code` (does not return). */
void __goclib_exit(long code);
/* Allocate `size` bytes from the process heap; returns 0 on failure. */
void *__goclib_heap_alloc(long size);
/* Free a block previously returned by __goclib_heap_alloc. */
void  __goclib_heap_free(void *p);
/* Resize a block from __goclib_heap_alloc, preserving its first min(old,new)
 * bytes; a null `p` behaves like __goclib_heap_alloc. */
void *__goclib_heap_realloc(void *p, long size);
/* Read up to `len` bytes from standard input into `buf`; <=0 at EOF. */
long __goclib_read(char *buf, long len);

#endif /* GOCLIB_H */
