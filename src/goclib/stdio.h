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

/* Thin integer printing: digits-to-buffer conversion plus one write, with
 * no format interpreter involved. A program that prints only integers links
 * neither vfmt nor the floating-point converter (measured: 2.5KB exe vs
 * 13.8KB for a printf("%d") program). The names are the UFCS spellings, so
 * x.print() on an int/long rewrites to int_print(x) / long_print(x).
 * Returns characters written. */
int int_print(int v);
int long_print(long v);

/* print(...) builtin thin targets. The compiler lowers a single-argument
 * print call to one of these instead of printf, so the common print("str")
 * / print(int) / print(long) / print() cases stay off the format
 * interpreter. Each appends the newline print() implies and returns the
 * character count, exactly like the printf lowering would. */
int print_str(const char *s);
int print_int_line(int v);
int print_long_line(long v);

#endif /* GOC_STDIO_H */
