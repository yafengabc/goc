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

/* ---- Networking ----------------------------------------------------------
 * These are goclib's own numbers, like the eight above, and for the same
 * reason: the library's errno is a private numbering, not the host's. What
 * matters is that ECONNREFUSED means one thing on Windows and the same thing
 * on Linux, which is only true because socket.c translates -- Linux returns
 * 111, Winsock returns 10061, and neither is 12.
 *
 * EAGAIN and EWOULDBLOCK are one code here rather than the two some platforms
 * spell, because on both targets they are raised by the same condition (a
 * non-blocking call that would have waited) and a program testing for one has
 * to catch the other anyway.
 */
#define EAGAIN       9  /* would have blocked; == EWOULDBLOCK */
#define EWOULDBLOCK  9
#define EINPROGRESS 10  /* a non-blocking connect is under way */
#define ECONNRESET  11  /* the peer closed abruptly */
#define ECONNREFUSED 12 /* nothing was listening */
#define ETIMEDOUT   13  /* the peer did not answer in time */
#define ENOTCONN    14  /* the socket is not connected */
#define EADDRINUSE  15  /* the address is already bound */
#define ENETDOWN    16  /* the network is not reachable from here */
#define EHOSTUNREACH 17 /* no route to that host */
#define EMSGSIZE    18  /* the datagram is too large to send */
#define EINTR       19  /* interrupted */
#define EBADF       20  /* not an open descriptor */
#define ENOTSOCK    21  /* the descriptor is not a socket */
#define EOPNOTSUPP  22  /* the operation is not supported here */
#define ENOBUFS     23  /* out of buffer space */

/* Human-readable text for an error code. Unknown codes yield "unknown error". */
char *strerror(int errnum);

#endif /* GOC_ERRNO_H */
