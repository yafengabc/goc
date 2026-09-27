#ifndef GOC_STDIO_H
#define GOC_STDIO_H

#include <stddef.h>

/* goc stdio.h -- the printf family and the stdio primitives goc provides.
 *
 * printf / sprintf are variadic; goc's checker accepts any number of trailing
 * arguments against the `...` in the prototype. All implementations come from
 * goclib (the assembly backend today, the C one later) and are linked in on
 * demand -- this header only carries declarations, as in a real libc.
 */

int printf(const char *fmt, ...);
int sprintf(char *buf, const char *fmt, ...);
int puts(const char *s);
int putchar(int c);
int getchar(void);

#endif /* GOC_STDIO_H */
