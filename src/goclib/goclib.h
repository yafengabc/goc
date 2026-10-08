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

/* ---- binary128: goc's long double, in software (fp128.c) ------------------ *
 *
 * `long double` is IEEE binary128 on every target, so its arithmetic cannot be
 * an instruction anywhere and lives in fp128.c as integer code over the bit
 * pattern. A value crosses this interface as two words rather than as a C type
 * because the front end does not have the type yet (#45 lands the type system,
 * #47/#48 the codegen): lo is bits 63..0 and hi bits 127..64, which is both a
 * little-endian 128-bit store and LLVM's <2 x i64> lane order.
 *
 * These are deliberately not named __addtf3 and friends. That name is the
 * compiler's own calling convention -- LLVM lowers `fadd fp128` to a call with
 * an fp128 in XMM0 -- and this ABI is not that one. The one-line wrappers get
 * added when the back ends can emit the type and the ABI can be checked. */
typedef struct {
    unsigned long long lo;
    unsigned long long hi;
} goc_tf128;

goc_tf128          goc_tf_add(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_sub(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_mul(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_div(goc_tf128 a, goc_tf128 b);
goc_tf128          goc_tf_neg(goc_tf128 a);
/* -1, 0 or 1 by magnitude (so +0 == -0); 2 when either operand is a NaN. */
int                goc_tf_cmp(goc_tf128 a, goc_tf128 b);
/* Widen and narrow. The double/float forms take the raw 64/32-bit pattern,
 * not a `double`, so nothing here ever performs a floating-point operation. */
goc_tf128          goc_tf_from_double(unsigned long long bits);
goc_tf128          goc_tf_from_float(unsigned int bits);
unsigned long long goc_tf_to_double(goc_tf128 a);
unsigned int       goc_tf_to_float(goc_tf128 a);
/* Integer conversions: out-of-range and NaN saturate rather than raise, since
 * C leaves both undefined and a program cannot portably depend on either. */
goc_tf128          goc_tf_from_ll(long long v);
goc_tf128          goc_tf_from_ull(unsigned long long v);
long long          goc_tf_to_ll(goc_tf128 a);
unsigned long long goc_tf_to_ull(goc_tf128 a);
int                goc_tf_to_int(goc_tf128 a);
unsigned int       goc_tf_to_uint(goc_tf128 a);

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
