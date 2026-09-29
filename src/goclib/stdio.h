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

/* Thin printing: no format interpreter involved. A program that prints
 * only strings/integers links neither vfmt nor the floating-point
 * converter (measured: 2.0KB exe vs 13.3KB for a printf program). All
 * three names are UFCS spellings (T_print), so the scalar method syntax
 * x.print() on an int/long rewrites to int_print(x) / long_print(x), and
 * the print(...) builtin lowers single-argument calls to int_print /
 * long_print / str_print. Each prints one line -- conversion plus a
 * newline -- and returns the character count, exactly like the printf
 * lowering would. */
int int_print(int v);
int long_print(long v);
int str_print(const char *s);

#endif /* GOC_STDIO_H */
