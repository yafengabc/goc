#ifndef GOC_STDDEF_H
#define GOC_STDDEF_H

/* goc stddef.h -- size_t, ptrdiff_t, NULL.
 *
 * goc ships this header and injects it on `#include <stddef.h>`; no system
 * headers are consulted (the toolchain stays self-contained). The typedefs
 * below use goc's C subset: `long` is 64-bit on both backends, so size_t and
 * ptrdiff_t match the platform word size.
 *
 * offsetof is deliberately not provided: goc has no struct type yet.
 */

typedef unsigned long size_t;
typedef long          ptrdiff_t;

/* C23 nullptr_t: the type of the nullptr keyword. goc models it as void*
 * (the keyword itself lowers to a null pointer constant, i.e. 0). */
typedef void* nullptr_t;

#define NULL ((void *)0)

/* C23 unreachable(): marks an execution path as unreachable. goc has no
 * optimiser that consumes it, so it is a harmless no-op. */
#define unreachable() ((void)0)

#endif /* GOC_STDDEF_H */
