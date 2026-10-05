#ifndef GOC_STDDEF_H
#define GOC_STDDEF_H

/* goc stddef.h -- size_t, ptrdiff_t, NULL.
 *
 * goc ships this header and injects it on `#include <stddef.h>`; no system
 * headers are consulted (the toolchain stays self-contained). The typedefs
 * below use goc's C subset: `long` is 64-bit on both backends, so size_t and
 * ptrdiff_t match the platform word size.
 */

typedef unsigned long size_t;
typedef long          ptrdiff_t;

/* wchar_t (C11 7.19 / 7.29). It lives in <stddef.h> because it is an integer
 * type, not a string type -- <wchar.h> merely adds the functions.
 *
 * The width is an ABI decision: 2 bytes unsigned on Windows (one UTF-16 code
 * unit, matching WCHAR in <windef.h>) and 4 bytes signed on Linux. goc emits
 * Windows first -- that is where a real ABI sits behind it -- so wchar_t is
 * unsigned short, and the UTF-16 data behind an L"..." literal agrees with it. */
typedef unsigned short wchar_t;
#define WCHAR_MIN 0
#define WCHAR_MAX 0xFFFF

/* C23 nullptr_t: the type of the nullptr keyword. goc models it as void*
 * (the keyword itself lowers to a null pointer constant, i.e. 0). */
typedef void* nullptr_t;

#define NULL ((void *)0)

/* Language-level feature macros. <stddef.h> is included by every other goc
 * header and is always present, so this is the one place that can define them
 * without a header-ordering dependency.
 *
 * __STDC_VERSION__ is the integer the standard prescribes: 199901L for C99,
 * 201112L for C11, 201710L for C17 and 202311L for C23. Real code gates on it
 * ("#if __STDC_VERSION__ >= 201112L") to pick between C99 and C11 interfaces, so
 * leaving it undefined silently takes the oldest branch on goc while the same
 * source takes the newest on gcc -- the classic "works on my compiler" split.
 * goc implements the C23 feature set, so it reports C23.
 *
 * The other four are required to be defined by every conforming implementation
 * (C23 5.1.1.2); they are macros rather than built-ins because that is what the
 * standard specifies. */
#define __STDC__ 1
#define __STDC_VERSION__ 202311L
#define __STDC_HOSTED__ 1
/* goc is freestanding-plus: hosted for hosted-headers purposes (it ships
   <stdio.h> and friends), but there is no OS FILE* concept beyond its own. */
#define __STDC_UTF_16__ 1
#define __STDC_UTF_32__ 1
#define __STDC_NO_ATOMICS__ 1
#define __STDC_NO_COMPLEX__ 1
#define __STDC_NO_VLA__ 1
/* __STDC_NO_THREADS__ is deliberately NOT defined: goclib ships <threads.h>
   with real thrd_/mtx_/cnd_/tss_ entry points. */

/* C23 unreachable(): marks an execution path as unreachable. goc has no
 * optimiser that consumes it, so it is a harmless no-op. */
#define unreachable() ((void)0)

/* C23 max_align_t: a type whose alignment is at least as strict as every
 * fundamental type. Every goc fundamental type is 8-aligned (long double
 * folds to double), so double is the strictest available. */
typedef double max_align_t;

/* offsetof(type, member): byte offset of a member within a struct
 * (C11 7.19.3). A null pointer cast keeps the expression constant. */
#define offsetof(type, member) ((size_t)&(((type *)0)->member))

#endif /* GOC_STDDEF_H */
