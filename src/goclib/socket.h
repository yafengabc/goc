#ifndef GOC_SOCKET_H
#define GOC_SOCKET_H

/* =============================================================================
 * socket.h -- a BSD sockets surface that means the same thing on both targets.
 *
 * This is goclib's TCP/IP layer. It exists because the two platforms disagree
 * in three places that a program cannot paper over, and each disagreement is
 * silent: the code compiles either way and only misbehaves at run time.
 *
 *   1. The option numbers. SO_REUSEADDR is 2 on Linux and 4 on Windows;
 *      SO_KEEPALIVE is 9 and 8; SO_ERROR is 4 and 0x1007; SOL_SOCKET is 1 and
 *      0xffff. Passing Linux's numbers to Winsock does not fail -- it sets or
 *      reads whatever option happens to live at that number.
 *   2. fd_set. Linux uses a 128-byte bitmap of 1024 descriptors. Windows uses
 *      a count plus an array of 64 handles, 520 bytes -- a different structure
 *      *and* a different idea, because on Windows the set has to be built by
 *      appending. FD_SET is not a bit operation there.
 *   3. Closing. On Windows a socket is not a file handle and CloseHandle on one
 *      fails; it needs closesocket(). goclib's own close() (file.c) goes
 *      through CloseHandle on that target, so it is the wrong call for a socket
 *      even though it is the right call for everything else in the library.
 *
 * All three are absorbed here. The names and the signatures are POSIX's, so
 * ordinary socket code reads unchanged; only the numbers behind the names
 * differ, and they differ in this file rather than in every caller.
 *
 * Sockets are `int' on both targets. Winsock's SOCKET is a 64-bit handle, but
 * the values Windows hands out are small integers, so they survive the trip --
 * and the alternative (a platform-varying type in the signature) is worse than
 * the assumption.
 *
 * Included on demand rather than from goclib.h: a program that never opens a
 * socket should not pay for the type definitions.
 * ========================================================================== */

#include <stddef.h>

/* ------------------------------------------------------------------ */
/* Descriptor sets                                                     */
/* ------------------------------------------------------------------ */

#if defined(_WIN32) || defined(_WIN64)
#define FD_SETSIZE 64
#else
#define FD_SETSIZE 1024
#endif

#if defined(_WIN32) || defined(_WIN64)
/* Winsock's fd_set: how many, then which. 4 bytes of count, 4 of padding,
 * then FD_SETSIZE 8-byte handles -- 520 bytes. select() reads fd_count and
 * ignores the nfds argument entirely. */
typedef struct fd_set {
    unsigned int fd_count;
    long         fd_array[FD_SETSIZE];
} fd_set;
#else
/* Linux's fd_set: a flat bitmap, one bit per descriptor. 1024 bits is 16
 * eight-byte words -- spelled as 16 rather than computed from sizeof(long),
 * because this is a layout the kernel reads and a literal cannot disagree with
 * it. nfds is not the number of set descriptors: it is the highest descriptor
 * in any set plus one, which is what tells the kernel how much to scan. */
typedef struct fd_set {
    long fds_bits[16];
} fd_set;
#endif

/* These four are functions rather than macros because the Windows set is not a
 * bitmap: FD_SET there appends to an array and FD_ISSET scans it, and a macro
 * cannot express that without evaluating its arguments more than once. */
void FD_ZERO(fd_set *set);
void FD_SET(int fd, fd_set *set);
void FD_CLR(int fd, fd_set *set);
int  FD_ISSET(int fd, fd_set *set);

/* select()'s timeout, and the only struct the kernel and Winsock agree on:
 * both spell it exactly this way, 16 bytes, microseconds. A null pointer means
 * wait forever; a zeroed one means poll and return immediately. */
struct timeval {
    long tv_sec;
    long tv_usec;
};

/* ------------------------------------------------------------------ */
/* Address families, socket types, protocols                          */
/* ------------------------------------------------------------------ */

#define AF_UNSPEC 0
#define AF_INET   2
#define AF_INET6  10

#define SOCK_STREAM 1
#define SOCK_DGRAM  2
#define SOCK_RAW    3

#define IPPROTO_IP  0
#define IPPROTO_TCP 6
#define IPPROTO_UDP 17

/* ------------------------------------------------------------------ */
/* Socket options                                                      */
/* ------------------------------------------------------------------ */
/*
 * Every number below is written twice, once per target. The pairs are not
 * typos: SO_REUSEADDR 2/4 and SO_KEEPALIVE 9/8 are the two most likely to be
 * noticed by a program that sets them, and getting either wrong produces a
 * socket that behaves differently on the two platforms with no error anywhere.
 */
#if defined(_WIN32) || defined(_WIN64)
#define SOL_SOCKET    0xffff
#define SO_DEBUG      0x0001
#define SO_REUSEADDR  0x0004
#define SO_KEEPALIVE  0x0008
#define SO_DONTROUTE  0x0010
#define SO_BROADCAST  0x0020
#define SO_OOBINLINE  0x0100
#define SO_SNDBUF     0x1001
#define SO_RCVBUF     0x1002
#define SO_SNDLOWAT   0x1003
#define SO_RCVLOWAT   0x1004
#define SO_SNDTIMEO   0x1005
#define SO_RCVTIMEO   0x1006
#define SO_ERROR      0x1007
#define SO_TYPE       0x1008
#else
#define SOL_SOCKET    1
#define SO_DEBUG      1
#define SO_REUSEADDR  2
#define SO_TYPE       3
#define SO_ERROR      4
#define SO_DONTROUTE  5
#define SO_BROADCAST  6
#define SO_SNDBUF     7
#define SO_RCVBUF     8
#define SO_KEEPALIVE  9
#define SO_OOBINLINE  10
#define SO_LINGER     13
#define SO_RCVLOWAT   18
#define SO_SNDLOWAT   19
#define SO_RCVTIMEO   20
#define SO_SNDTIMEO   21
#endif

/* TCP_NODELAY happens to be 1 on both. It is spelled out rather than assumed
 * so that the pair can be checked the same way as the ones that differ. */
#define TCP_NODELAY 1

/* ------------------------------------------------------------------ */
/* send/recv flags and shutdown directions                            */
/* ------------------------------------------------------------------ */

#define MSG_OOB      1
#define MSG_PEEK     2
#if defined(_WIN32) || defined(_WIN64)
/* Winsock has no non-blocking flag: a socket is blocking or it is not, decided
 * by ioctlsocket(FIONBIO). MSG_DONTWAIT is therefore defined as 0, so code
 * that passes it compiles and simply blocks. MSG_WAITALL is 0x8 there. */
#define MSG_DONTWAIT 0
#define MSG_WAITALL  0x8
#else
#define MSG_DONTWAIT 0x40
#define MSG_WAITALL  0x100
#endif

#define SHUT_RD   0
#define SHUT_WR   1
#define SHUT_RDWR 2

/* ------------------------------------------------------------------ */
/* Addresses                                                           */
/* ------------------------------------------------------------------ */

typedef unsigned short sa_family_t;
typedef int            socklen_t;

/* The generic address every socket call takes. 16 bytes: the family, then 14
 * bytes whose meaning the specific family defines -- for AF_INET that is a
 * port and an IPv4 address, which is what <in.h> overlays on it. */
struct sockaddr {
    sa_family_t sa_family;
    char        sa_data[14];
};

/* ------------------------------------------------------------------ */
/* Calls                                                               */
/* ------------------------------------------------------------------ */
/*
 * Every call below is a macro over a __goc_-prefixed function, and that is not
 * decoration -- it is forced by the Windows side.
 *
 * ws2_32 exports `socket', `bind', `listen', `accept', `connect', `send',
 * `recv', `sendto', `recvfrom', `shutdown', `closesocket', `setsockopt',
 * `getsockopt', `getpeername', `getsockname' and `select' under exactly those
 * names, and goc's import table has no way to import a symbol under one name
 * and call it by another. A translation unit that both declares the import and
 * defines the C function of the same name takes the local definition -- measured
 * here, not assumed: such a program calls its own wrapper, forever. So the
 * library's functions carry the __goc_ prefix and the POSIX spellings are
 * macros over them, which leaves the import names free.
 *
 * The macro is defined on *both* targets even though only Windows needs it.
 * Making it conditional would mean `&socket' compiles on Linux and fails on
 * Windows -- a difference that shows up in the other platform's build, which is
 * the kind of thing this header exists to prevent. Losing the ability to take
 * the address of a socket function is the price, and it is a small one.
 *
 * winsock2.h undefines all of these before it declares the real imports, so
 * socket.c -- the one file that includes it -- still reaches ws2_32.
 */
int  __goc_socket(int domain, int type, int protocol);
int  __goc_bind(int fd, const struct sockaddr *addr, socklen_t addrlen);
int  __goc_listen(int fd, int backlog);
int  __goc_accept(int fd, struct sockaddr *addr, socklen_t *addrlen);
int  __goc_connect(int fd, const struct sockaddr *addr, socklen_t addrlen);

long __goc_send(int fd, const void *buf, long len, int flags);
long __goc_recv(int fd, void *buf, long len, int flags);
long __goc_sendto(int fd, const void *buf, long len, int flags,
                  const struct sockaddr *to, socklen_t tolen);
long __goc_recvfrom(int fd, void *buf, long len, int flags,
                    struct sockaddr *from, socklen_t *fromlen);

int  __goc_shutdown(int fd, int how);
/* Not close(), and the reason is worth stating: on Windows a socket is not a
 * kernel file handle, and goclib's close() calls CloseHandle there -- which on
 * a socket fails and leaks it. This is the call that does the right thing on
 * each target (closesocket on Windows, close(2) on Linux). */
int  __goc_closesocket(int fd);

int  __goc_setsockopt(int fd, int level, int optname, const void *val, socklen_t len);
int  __goc_getsockopt(int fd, int level, int optname, void *val, socklen_t *len);
int  __goc_getpeername(int fd, struct sockaddr *addr, socklen_t *addrlen);
int  __goc_getsockname(int fd, struct sockaddr *addr, socklen_t *addrlen);

int  __goc_select(int nfds, fd_set *readfds, fd_set *writefds, fd_set *exceptfds,
                  struct timeval *timeout);

#define socket(a, b, c)       __goc_socket(a, b, c)
#define bind(a, b, c)         __goc_bind(a, b, c)
#define listen(a, b)          __goc_listen(a, b)
#define accept(a, b, c)       __goc_accept(a, b, c)
#define connect(a, b, c)      __goc_connect(a, b, c)
#define send(a, b, c, d)      __goc_send(a, b, c, d)
#define recv(a, b, c, d)      __goc_recv(a, b, c, d)
#define sendto(a, b, c, d, e, f)   __goc_sendto(a, b, c, d, e, f)
#define recvfrom(a, b, c, d, e, f) __goc_recvfrom(a, b, c, d, e, f)
#define shutdown(a, b)        __goc_shutdown(a, b)
#define closesocket(a)        __goc_closesocket(a)
#define setsockopt(a, b, c, d, e)  __goc_setsockopt(a, b, c, d, e)
#define getsockopt(a, b, c, d, e)  __goc_getsockopt(a, b, c, d, e)
#define getpeername(a, b, c)  __goc_getpeername(a, b, c)
#define getsockname(a, b, c)  __goc_getsockname(a, b, c)
#define select(a, b, c, d, e) __goc_select(a, b, c, d, e)

/* ------------------------------------------------------------------ */
/* Two conveniences that are not POSIX                                 */
/* ------------------------------------------------------------------ */
/*
 * Both exist because the underlying knob is expressed differently on the two
 * platforms in a way a caller cannot portably spell.
 *
 * SO_RCVTIMEO / SO_SNDTIMEO take a struct timeval on Linux and a millisecond
 * DWORD on Windows -- different type, not merely different number -- so a
 * program that wants "give up after five seconds" has no portable way to say
 * it with setsockopt() alone. These take milliseconds and pack whatever the
 * target wants; they are two calls rather than one because a server usually
 * wants them set to different values, or wants only one of them.
 */
int sock_set_timeout(int fd, int recv_ms, int send_ms);

/* Non-blocking I/O is the other one: Linux reaches it through fcntl(F_SETFL)
 * and Windows through ioctlsocket(FIONBIO), and goclib has no fcntl. A
 * negative argument asks for blocking again -- the only portable way back,
 * since there is no "clear this flag" that both spell the same. */
int sock_set_nonblock(int fd, int on);

#endif /* GOC_SOCKET_H */
