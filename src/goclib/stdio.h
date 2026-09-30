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
/* Supported conversions: d i u o x X (h/l/ll lengths), f e g a, c, s, and
 * "%%", with width and "*" suppression. No scansets, %p or %n. Returns the
 * number of items assigned, or -1 if the input ends before the first one
 * completes. */
int sscanf(const char *s, const char *fmt, ...);
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

/* Array content printing, Python style: "[1, 2, 3]" plus newline. The
 * print(...) builtin lowers print(arr) here when the argument is an array
 * identifier -- the length is a compile-time constant the builtin passes
 * in (a C array carries no runtime length). One printer per element type,
 * generated from a shared skeleton in stdio.c; the unsigned* arrays reuse
 * the same-width signed printer (an unsigned int above INT_MAX prints
 * negative, the printf %d caveat). char arrays are deliberately not here:
 * they are strings and stay on str_print. Each returns the number of
 * characters written. */
int short_array_print(short *a, long n);
int int_array_print(int *a, long n);
int long_array_print(long *a, long n);
int bool_array_print(_Bool *a, long n);
int float_array_print(float *a, long n);
int double_array_print(double *a, long n);

#endif /* GOC_STDIO_H */
