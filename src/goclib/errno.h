#ifndef GOC_ERRNO_H
#define GOC_ERRNO_H

/* goc errno.h -- the error code variable and its names.
 *
 * errno is a modifiable lvalue: a macro over a location function, exactly
 * like glibc's (*__errno_location()). The library sets it sparingly (the
 * conversion routines set ERANGE on overflow); users can set it directly.
 * Error codes follow the classic values; anything not modelled keeps its
 * number but strerror() prints a generic message for it.
 */

int *__goclib_errno_loc(void);

#define errno (*__goclib_errno_loc())

#define EDOM    1  /* domain error (e.g. sqrt of a negative) */
#define ERANGE  2  /* result out of range */
#define EILSEQ  3  /* illegal byte sequence (reserved) */
#define EINVAL  4  /* invalid argument */
#define ENOENT  5  /* no such file or directory */
#define EACCES  6  /* permission denied */
#define ENOMEM  7  /* out of memory */
#define EIO     8  /* I/O error */

/* Human-readable text for an error code. Unknown codes yield "unknown error". */
char *strerror(int errnum);

#endif /* GOC_ERRNO_H */
