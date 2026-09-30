#ifndef GOC_STDIO_H
#define GOC_STDIO_H

#include <stddef.h>

/* goc stdio.h -- the printf/scanf families and the file-I/O primitives goc
 * provides. printf / sprintf / fprintf / scanf are variadic; goc's checker
 * accepts any number of trailing arguments against the `...` in the prototype.
 * All implementations come from goclib and are linked in on demand -- this
 * header only carries declarations, as in a real libc.
 *
 * FILE is an opaque struct (defined in file.c); user code manipulates it only
 * through FILE *. The three standard streams are macros expanding to goclib
 * accessors, so they work as expressions (e.g. inside fprintf(stdout, ...))
 * while their OS handles are resolved at runtime.
 */

typedef struct __goclib_FILE FILE;

#define EOF (-1)
#define SEEK_SET 0
#define SEEK_CUR 1
#define SEEK_END 2
#define BUFSIZ 4096
#define FOPEN_MAX 20

#define stdin  __goclib_stdin()
#define stdout __goclib_stdout()
#define stderr __goclib_stderr()

/* formatted output */
int printf(const char *fmt, ...);
int fprintf(FILE *stream, const char *fmt, ...);
int sprintf(char *buf, const char *fmt, ...);
/* snprintf/vsnprintf write at most n-1 characters plus a NUL and return the
 * length the fully-formatted text would have had (possibly > n-1). */
int snprintf(char *buf, size_t n, const char *fmt, ...);
int vsnprintf(char *buf, size_t n, const char *fmt, va_list ap);
int vfprintf(FILE *stream, const char *fmt, va_list ap);
int vprintf(const char *fmt, va_list ap);

/* getc/putc are the traditional macro spellings of fgetc/fputc. */
#define getc(f)  fgetc(f)
#define putc(c, f) fputc(c, f)

/* Position pairs: goc stores the offset directly, so fpos_t is a long and
 * these are expression macros on top of ftell/fseek. */
typedef long fpos_t;
#define fgetpos(f, p) (*(fpos_t *)(p) = ftell(f))
#define fsetpos(f, p) fseek((f), *(fpos_t *)(p), SEEK_SET)

/* formatted input */
int sscanf(const char *s, const char *fmt, ...);
int fscanf(FILE *stream, const char *fmt, ...);

int puts(const char *s);
int putchar(int c);
int getchar(void);
/* perror(s) prints "s: <strerror(errno)>" (or just the message when s is
 * null or empty) to stderr, then leaves errno unchanged. */
void perror(const char *s);

/* file I/O */
FILE *fopen(const char *path, const char *mode);
/* Reopen `stream` on `path` with `mode`, closing its current association;
 * the FILE * itself stays valid (the freopen(stdout, ...) idiom). */
FILE *freopen(const char *path, const char *mode, FILE *stream);
int fclose(FILE *stream);
long fread(void *ptr, long size, long nmemb, FILE *stream);
long fwrite(const void *ptr, long size, long nmemb, FILE *stream);
int fgetc(FILE *stream);
int fputc(int c, FILE *stream);
char *fgets(char *s, long n, FILE *stream);
int fputs(const char *s, FILE *stream);
int fflush(FILE *stream);
long ftell(FILE *stream);
int fseek(FILE *stream, long offset, int whence);
void rewind(FILE *stream);
int feof(FILE *stream);
int ferror(FILE *stream);
void clearerr(FILE *stream);
int ungetc(int c, FILE *stream);
int remove(const char *path);
int rename(const char *oldp, const char *newp);
FILE *tmpfile(void);
int setvbuf(FILE *stream, char *buf, int mode, long size);

/* internal accessors backing the stdin/stdout/stderr macros */
FILE *__goclib_stdin(void);
FILE *__goclib_stdout(void);
FILE *__goclib_stderr(void);

/* Thin printing: no format interpreter involved. A program that prints
 * only strings/integers links neither vfmt nor the floating-point
 * converter (measured: 2.0KB exe vs 13.3KB for a printf program). All
 * three names are UFCS spellings (T_print), so the scalar method syntax
 * x.print() on an int/long rewrites to int_print / long_print(x), and
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
