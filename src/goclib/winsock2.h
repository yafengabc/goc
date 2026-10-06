#ifndef GOC_WINSOCK2_H
#define GOC_WINSOCK2_H

/* =============================================================================
 * winsock2.h -- the Winsock imports goclib's socket layer is built on.
 *
 * Included only from the Win32 arm of socket.c, and only under goc: the
 * `, ws2_32' on each prototype is goc's own inline-DLL annotation, the same
 * spelling winbase.h uses for kernel32. A host compiler reads that as a second
 * declarator, which is why <windows.h> is guarded by __goc__ in goclib.h and
 * why this header is reached the same way.
 *
 * The handles are `long' here and `int' in socket.h. That is deliberate:
 * Winsock's SOCKET is a 64-bit handle type, so the import has to be declared
 * wide enough to carry one, while the POSIX surface goclib publishes spells
 * descriptors as int. socket.c is where the two meet, and the conversion is
 * sound because the values Windows hands out are small integers.
 * ========================================================================== */

#include <socket.h>
#include <in.h>

/* Undefine the public spellings before declaring the imports.
 *
 * socket.h makes `socket', `bind', `connect' and the rest macros over
 * __goc_-prefixed functions, because ws2_32 exports those same names and a
 * translation unit that declares the import and defines the function of the
 * same name calls itself. Declaring `extern long socket(...), ws2_32' with the
 * macro still live would rewrite the import's own name to __goc_socket -- which
 * ws2_32 does not export.
 *
 * So this header, the one file that includes it, takes the macros back off.
 * Every other translation unit keeps them and reaches goclib's wrapper.
 */
#undef socket
#undef bind
#undef listen
#undef accept
#undef connect
#undef send
#undef recv
#undef sendto
#undef recvfrom
#undef shutdown
#undef closesocket
#undef setsockopt
#undef getsockopt
#undef getpeername
#undef getsockname
#undef select

/* Winsock's error and invalid-handle sentinels. Both are -1, and both are
 * distinct from a successful return of 0, which is what lets socket.c tell
 * "failed" from "worked" without asking for the error first. */
#define INVALID_SOCKET (-1)
#define SOCKET_ERROR   (-1)

/* WSAStartup's requested version: 2.2, packed the way MAKEWORD would. Winsock
 * has accepted 2.2 since Windows 98 and still does, so there is no reason to
 * negotiate downwards. */
#define WINSOCK_VERSION 0x0202

/* ioctlsocket's "set blocking mode" command. The high bit marks it as an
 * input-output control with a pointer argument; the low bits encode it. There
 * is no portable name for this, so it is spelled out. */
#define FIONBIO 0x8004667E

/* ------------------------------------------------------------------ */
/* Startup and teardown                                                */
/* ------------------------------------------------------------------ */
/*
 * WSAStartup must run before any other Winsock call -- the one rule Winsock
 * has that BSD sockets does not -- so socket.c calls it lazily from its first
 * socket(). Its second argument is a WSADATA, 408 bytes on x86-64, which
 * socket.c supplies as a plain buffer: nothing here ever reads it, and the
 * layout is version-dependent, so naming a struct for it would be a promise
 * this library cannot keep.
 */
extern int  WSAStartup(int version, void *data), ws2_32;
extern int  WSACleanup(void), ws2_32;
extern int  WSAGetLastError(void), ws2_32;

/* ------------------------------------------------------------------ */
/* The calls                                                           */
/* ------------------------------------------------------------------ */

extern long socket(int af, int type, int protocol), ws2_32;
extern int  closesocket(long s), ws2_32;
extern int  bind(long s, const struct sockaddr *name, int namelen), ws2_32;
extern int  listen(long s, int backlog), ws2_32;
extern long accept(long s, struct sockaddr *addr, int *addrlen), ws2_32;
extern int  connect(long s, const struct sockaddr *name, int namelen), ws2_32;

/* Winsock's buffer arguments are char*, and its lengths are int -- not
 * size_t, which is the BSD spelling. The mismatch is why socket.c passes the
 * lengths as int and checks them before it gets here. */
extern int  send(long s, const char *buf, int len, int flags), ws2_32;
extern int  recv(long s, char *buf, int len, int flags), ws2_32;
extern int  sendto(long s, const char *buf, int len, int flags,
                   const struct sockaddr *to, int tolen), ws2_32;
extern int  recvfrom(long s, char *buf, int len, int flags,
                     struct sockaddr *from, int *fromlen), ws2_32;

extern int  shutdown(long s, int how), ws2_32;

extern int  setsockopt(long s, int level, int optname,
                       const char *optval, int optlen), ws2_32;
extern int  getsockopt(long s, int level, int optname,
                       char *optval, int *optlen), ws2_32;

extern int  getpeername(long s, struct sockaddr *name, int *namelen), ws2_32;
extern int  getsockname(long s, struct sockaddr *name, int *namelen), ws2_32;

/* Winsock's select ignores nfds -- it reads the sets themselves -- and takes
 * the timeout as a pointer to const. The sets are its own fd_set, the one
 * socket.h defines for this target, so the layout matches by construction. */
extern int  select(int nfds, fd_set *readfds, fd_set *writefds,
                   fd_set *exceptfds, const struct timeval *timeout), ws2_32;

/* Blocking mode. Linux reaches the same knob through fcntl, which goclib does
 * not have; this is the whole of the difference between the two arms of
 * sock_set_nonblock(). */
extern int  ioctlsocket(long s, long cmd, unsigned long *argp), ws2_32;

#endif /* GOC_WINSOCK2_H */
