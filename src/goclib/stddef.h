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
