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
#include <errno.h>
#include <assert.h>
#include <math.h>
/* The goclib implementation files call into kernel32/user32/gdi32 (ExitProcess,
 * CreateFileA, ...). Those imports are now self-describing: each prototype in
 * the windows.h family names its DLL inline (e.g. `extern void ExitProcess(DWORD),
 * kernel32;`). Pulling windows.h in here makes every binding visible to the
 * library translation units, which is what the old central win32.def provided
 * globally. User code that never touches the OS simply does not call them.
 *
 * That inline-DLL syntax ("extern BOOL f(HANDLE), kernel32;") is goc's own, and
 * no other compiler can parse it -- clang reads the trailing ", kernel32" as a
 * second declarator declaring a *function named kernel32*, so every prototype
 * in the family collides with the first. The windows.h family is therefore goc-
 * only, and under any other host compiler the Win32 calls are already dead code
 * anyway: every one of them sits inside a `#if defined(_WIN32)` block, so on a
 * POSIX host the preprocessor discards them and goclib needs no Win32 header at
 * all. */
#ifdef __goc__
#include <windows.h>
#endif

/* ---- platform primitives (implemented in goclib.c, OS glue) ---------------- */
/* Write `len` bytes from `buf` to standard output. Returns bytes written. */
long __goclib_write(const char *buf, long len);
/* Terminate the process with `code` (does not return). _Noreturn because the
 * callers that end in this call -- exit, abort, quick_exit -- are themselves
 * _Noreturn, and a compiler cannot see through a plain call to conclude their
 * control flow stops here. */
_Noreturn void __goclib_exit(long code);
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
