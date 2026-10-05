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

/* buffering modes for setvbuf / setbuf */
#define _IOFBF 0   /* fully buffered */
#define _IOLBF 1   /* line buffered */
#define _IONBF 2   /* unbuffered */

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

/* _TRUNCATE is the MSVC sentinel for the secure *_s string functions meaning
 * "write at most the buffer size, then NUL-terminate" (the snprintf contract).
 * Real-world Windows C leans on it, so goclib defines it alongside the
 * functions it pairs with. */
#ifndef _TRUNCATE
#define _TRUNCATE ((size_t)-1)
#endif
int vfprintf(FILE *stream, const char *fmt, va_list ap);
int vprintf(const char *fmt, va_list ap);

/* ---- MSVC secure-CRT spellings ---------------------------------------- */
/* MSVC's "secure" CRT renames the bounded string functions with a _s suffix
 * and adds a _TRUNCATE size sentinel. The bounds are the same as the standard
 * functions, so these are declared here and implemented in stdio.c as thin
 * wrappers -- porting existing Windows code should not require rewriting every
 * snprintf call. _snprintf_s differs from snprintf only in that it returns 0
 * on truncation (-1) rather than the would-be length.
 *
 * The MSVC prototypes take (buffer, sizeOfBuffer, count, ...): sizeOfBuffer is
 * the buffer's total element count and is the bound passed to vsnprintf, while
 * count is the requested maximum (or _TRUNCATE to fill the buffer). goc has a
 * single calling convention, so count is honoured only as a secondary clamp;
 * _TRUNCATE is the standard "fill the buffer" request. */
int _snprintf_s(char *buf, size_t sizeOfBuffer, size_t count, const char *fmt, ...);
int _vsnprintf_s(char *buf, size_t sizeOfBuffer, size_t count, const char *fmt, va_list ap);

/* Internal: printf for a format the compiler proved is "lite" -- only %s,
 * integers, %c and %f, with no field width, precision or flags (see the
 * vfmt_lite comment in stdio.c). Writes straight to the OS handle, bypassing
 * the FILE layer. Not called by user code: codegen rewrites those printf
 * calls to this, and anything richer keeps the real printf. */
int __goclib_printf_lite(const char *fmt, ...);
int __goclib_printf_lite_f(const char *fmt, ...);

/* getc/putc are the traditional macro spellings of fgetc/fputc. */
#define getc(f)  fgetc(f)
#define putc(c, f) fputc(c, f)

/* Position pairs: goc stores the offset directly, so fpos_t is a long and
 * these are expression macros on top of ftell/fseek. */
typedef long fpos_t;
#define fgetpos(f, p) (*(fpos_t *)(p) = ftell(f))
#define fsetpos(f, p) fseek((f), *(fpos_t *)(p), SEEK_SET)

/* formatted input */
int scanf(const char *fmt, ...);
int vscanf(const char *fmt, va_list ap);
int vfscanf(FILE *stream, const char *fmt, va_list ap);
int vsscanf(const char *s, const char *fmt, va_list ap);
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
/* fopen_s is the MSVC secure fopen: it reports failure through the FILE*
 * out-parameter plus an errno-style return (0 on success) instead of a bare
 * NULL, which is what makes the "was the open rejected?" question impossible
 * to forget at a call site. A failed open leaves *fp NULL. */
int   fopen_s(FILE **fp, const char *path, const char *mode);
/* _wfopen takes a UTF-16 path, which is what a Win32 GUI program already has
 * in hand (GetOpenFileNameW, wWinMain's command line). On Windows it opens
 * with CreateFileW so non-ANSI path characters survive; the mode string is
 * still narrow-ASCII, as in the real CRT. */
FILE *_wfopen(const wchar_t *path, const wchar_t *mode);
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
/* setbuf(stream, buf) is the two-argument form of setvbuf: a null `buf`
 * requests no buffering, otherwise full buffering is used. */
void setbuf(FILE *stream, char *buf);
/* tmpnam writes a unique (not-yet-created) file name into `s`, or into an
 * internal static buffer when `s` is null, and returns it. */
char *tmpnam(char *s);

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
