#include "goclib.h"
#include <socket.h>
#include <in.h>
#include <errno.h>

/* =============================================================================
 * socket.c -- goclib's TCP/IP layer.
 *
 * One file, two arms, and the split is the point: on Windows every call goes
 * through ws2_32, on Linux through a syscall stub, and neither arm knows about
 * the other's route. What the two share is the shape of the result -- 0 or -1
 * for the calls that answer a question, a byte count for the ones that move
 * data, and a goclib errno code that means the same thing either way.
 *
 * The functions are named __goc_socket and friends, with the POSIX spellings
 * as macros in socket.h. That is not a style choice; see the comment there.
 * The short version is that ws2_32 exports these names too, and a translation
 * unit that declares the import and defines the function of the same name
 * calls itself.
 * ========================================================================== */

#if defined(_WIN32)

/* winsock2.h undefines the public macros before it declares the real imports,
 * so from here on `socket', `bind' and the rest name ws2_32's functions. */
#include <winsock2.h>

/* ------------------------------------------------------------------ */
/* One-time startup                                                    */
/* ------------------------------------------------------------------ */
/*
 * Winsock is the only socket API with a "begin" call, and forgetting it
 * produces WSANOTINITIALISED from every call rather than a link error, so the
 * mistake is easy to make and hard to read. Doing it here means a program
 * never has to know: the first socket call pays for it once.
 *
 * The WSADATA is 408 bytes on x86-64 and version-dependent in its layout, so
 * it is a raw buffer -- nothing reads it, and naming a struct for it would
 * promise more than this library can keep.
 */
static int ws_started = 0;
static int ws_start_error = 0;

static void ws_ensure(void) {
    char wsadata[512];
    if (ws_started) return;
    ws_start_error = WSAStartup(WINSOCK_VERSION, wsadata);
    ws_started = 1;
}

/* ------------------------------------------------------------------ */
/* Error translation                                                   */
/* ------------------------------------------------------------------ */
/*
 * Winsock reports failure through WSAGetLastError() and its own numbering,
 * which starts at 10000 and shares nothing with Linux's errno: 10061 is
 * ECONNREFUSED there and 111 here. Translating here is what lets one
 * `if (errno == ECONNREFUSED)' work on both targets.
 *
 * An unmapped code becomes EIO rather than passing through. Passing through
 * would be worse than wrong: Winsock's numbers are all five digits and would
 * not collide, but the mapping has to be complete in one place or a program
 * that tests errno gets an answer it cannot interpret.
 */
static int ws_err(int e) {
    switch (e) {
    case 10035: return EAGAIN;        /* WSAEWOULDBLOCK */
    case 10036: return EINPROGRESS;   /* WSAEINPROGRESS */
    case 10054: return ECONNRESET;    /* WSAECONNRESET  */
    case 10061: return ECONNREFUSED;  /* WSAECONNREFUSED */
    case 10060: return ETIMEDOUT;     /* WSAETIMEDOUT   */
    case 10057: return ENOTCONN;      /* WSAENOTCONN    */
    case 10048: return EADDRINUSE;    /* WSAEADDRINUSE  */
    case 10050: return ENETDOWN;      /* WSAENETDOWN    */
    case 10065: return EHOSTUNREACH;  /* WSAEHOSTUNREACH */
    case 10040: return EMSGSIZE;      /* WSAEMSGSIZE    */
    case 10004: return EINTR;         /* WSAEINTR       */
    case 10009: return EBADF;         /* WSAEBADF       */
    case 10038: return ENOTSOCK;      /* WSAENOTSOCK    */
    case 10045: return EOPNOTSUPP;    /* WSAEOPNOTSUPP  */
    case 10055: return ENOBUFS;       /* WSAENOBUFS     */
    case 10022: return EINVAL;        /* WSAEINVAL      */
    case 10013: return EACCES;        /* WSAEACCES      */
    case 10024: return ENOMEM;        /* WSAEMFILE: out of descriptors */
    case 10093: return ENETDOWN;      /* WSANOTINITIALISED */
    default:    return EIO;
    }
}

static int ws_fail(void) {
    errno = ws_err(WSAGetLastError());
    return -1;
}

/* ------------------------------------------------------------------ */
/* Descriptor sets                                                     */
/* ------------------------------------------------------------------ */
/*
 * Winsock's fd_set is an array, not a bitmap, so these four cannot be the
 * shift-and-mask operations they are on Linux. FD_SET has to append and
 * FD_ISSET has to scan.
 *
 * FD_SET also refuses to add a descriptor twice: Winsock's own macro does not
 * check, and a set containing one handle twice makes select() report it twice,
 * which shows up as a program that reads the same socket two times for one
 * arrival.
 */
void FD_ZERO(fd_set *set) {
    if (set) set->fd_count = 0;
}

void FD_SET(int fd, fd_set *set) {
    unsigned int i;
    if (!set) return;
    for (i = 0; i < set->fd_count; i++) {
        if (set->fd_array[i] == (long)fd) return;
    }
    if (set->fd_count >= FD_SETSIZE) return;
    set->fd_array[set->fd_count] = (long)fd;
    set->fd_count++;
}

void FD_CLR(int fd, fd_set *set) {
    unsigned int i, j;
    if (!set) return;
    for (i = 0; i < set->fd_count; i++) {
        if (set->fd_array[i] == (long)fd) {
            for (j = i + 1; j < set->fd_count; j++) {
                set->fd_array[j - 1] = set->fd_array[j];
            }
            set->fd_count--;
            return;
        }
    }
}

int FD_ISSET(int fd, fd_set *set) {
    unsigned int i;
    if (!set) return 0;
    for (i = 0; i < set->fd_count; i++) {
        if (set->fd_array[i] == (long)fd) return 1;
    }
    return 0;
}

/* ------------------------------------------------------------------ */
/* The calls                                                           */
/* ------------------------------------------------------------------ */

int __goc_socket(int domain, int type, int protocol) {
    long r;
    ws_ensure();
    if (ws_start_error) { errno = ENETDOWN; return -1; }
    r = socket(domain, type, protocol);
    if (r == INVALID_SOCKET) return ws_fail();
    return (int)r;
}

int __goc_bind(int fd, const struct sockaddr *addr, socklen_t addrlen) {
    int r;
    ws_ensure();
    r = bind((long)fd, addr, (int)addrlen);
    if (r == SOCKET_ERROR) return ws_fail();
    return 0;
}

int __goc_listen(int fd, int backlog) {
    int r;
    ws_ensure();
    r = listen((long)fd, backlog);
    if (r == SOCKET_ERROR) return ws_fail();
    return 0;
}

int __goc_accept(int fd, struct sockaddr *addr, socklen_t *addrlen) {
    long r;
    int alen = 0;
    ws_ensure();
    if (addrlen) alen = (int)*addrlen;
    /* Winsock rejects a null address paired with a zero length: it insists the
     * buffer be at least sizeof(sockaddr) whenever a length is supplied, and
     * reads a zero as "no room". `accept(fd, 0, 0)' -- "I do not care who
     * connected" -- is ordinary usage on Linux and fails on Windows for that
     * reason alone, so this arm has to hand Winsock two nulls or nothing. */
    if (!addr) {
        r = accept((long)fd, 0, 0);
    } else {
        r = accept((long)fd, addr, &alen);
    }
    if (r == INVALID_SOCKET) return ws_fail();
    if (addrlen) *addrlen = (socklen_t)alen;
    return (int)r;
}

int __goc_connect(int fd, const struct sockaddr *addr, socklen_t addrlen) {
    int r;
    ws_ensure();
    r = connect((long)fd, addr, (int)addrlen);
    if (r == SOCKET_ERROR) return ws_fail();
    return 0;
}

/* Winsock's lengths are int and its buffers are char*, so a long length has to
 * be checked before it is narrowed: a caller asking to send more than 2GB gets
 * EINVAL here rather than a negative length the kernel would reinterpret. */
long __goc_send(int fd, const void *buf, long len, int flags) {
    int r;
    ws_ensure();
    if (len < 0 || len > 0x7fffffff) { errno = EINVAL; return -1; }
    r = send((long)fd, (const char *)buf, (int)len, flags);
    if (r == SOCKET_ERROR) return ws_fail();
    return (long)r;
}

long __goc_recv(int fd, void *buf, long len, int flags) {
    int r;
    ws_ensure();
    if (len < 0 || len > 0x7fffffff) { errno = EINVAL; return -1; }
    r = recv((long)fd, (char *)buf, (int)len, flags);
    if (r == SOCKET_ERROR) return ws_fail();
    return (long)r;
}

long __goc_sendto(int fd, const void *buf, long len, int flags,
                  const struct sockaddr *to, socklen_t tolen) {
    int r;
    ws_ensure();
    if (len < 0 || len > 0x7fffffff) { errno = EINVAL; return -1; }
    r = sendto((long)fd, (const char *)buf, (int)len, flags, to, (int)tolen);
    if (r == SOCKET_ERROR) return ws_fail();
    return (long)r;
}

long __goc_recvfrom(int fd, void *buf, long len, int flags,
                    struct sockaddr *from, socklen_t *fromlen) {
    int r;
    int flen = 0;
    ws_ensure();
    if (len < 0 || len > 0x7fffffff) { errno = EINVAL; return -1; }
    if (fromlen) flen = (int)*fromlen;
    r = recvfrom((long)fd, (char *)buf, (int)len, flags, from, &flen);
    if (r == SOCKET_ERROR) return ws_fail();
    if (fromlen) *fromlen = (socklen_t)flen;
    return (long)r;
}

int __goc_shutdown(int fd, int how) {
    int r;
    ws_ensure();
    r = shutdown((long)fd, how);
    if (r == SOCKET_ERROR) return ws_fail();
    return 0;
}

int __goc_closesocket(int fd) {
    int r;
    ws_ensure();
    r = closesocket((long)fd);
    if (r == SOCKET_ERROR) return ws_fail();
    return 0;
}

int __goc_setsockopt(int fd, int level, int optname, const void *val, socklen_t len) {
    int r;
    ws_ensure();
    r = setsockopt((long)fd, level, optname, (const char *)val, (int)len);
    if (r == SOCKET_ERROR) return ws_fail();
    return 0;
}

int __goc_getsockopt(int fd, int level, int optname, void *val, socklen_t *len) {
    int r;
    int l = 0;
    ws_ensure();
    if (len) l = (int)*len;
    r = getsockopt((long)fd, level, optname, (char *)val, &l);
    if (r == SOCKET_ERROR) return ws_fail();
    if (len) *len = (socklen_t)l;
    return 0;
}

int __goc_getpeername(int fd, struct sockaddr *addr, socklen_t *addrlen) {
    int r;
    int alen = 0;
    ws_ensure();
    if (addrlen) alen = (int)*addrlen;
    r = getpeername((long)fd, addr, &alen);
    if (r == SOCKET_ERROR) return ws_fail();
    if (addrlen) *addrlen = (socklen_t)alen;
    return 0;
}

int __goc_getsockname(int fd, struct sockaddr *addr, socklen_t *addrlen) {
    int r;
    int alen = 0;
    ws_ensure();
    if (addrlen) alen = (int)*addrlen;
    r = getsockname((long)fd, addr, &alen);
    if (r == SOCKET_ERROR) return ws_fail();
    if (addrlen) *addrlen = (socklen_t)alen;
    return 0;
}

/* nfds is passed through but Winsock ignores it -- it reads the sets. Passing
 * it anyway keeps the call site identical on both targets. */
/* Winsock refuses to select on nothing. Three null sets, or three sets that
 * are merely empty, both come back WSAEINVAL -- the manuals mention the null
 * case; the empty case was measured here, and it fails the same way.
 *
 * On Linux that call is the ordinary way to sleep with better than
 * millisecond resolution, so it has to work on Windows too, and Sleep is what
 * stands in for it. Sleep is a kernel32 import in every goc program already,
 * so substituting it costs nothing but the branch. */
extern void Sleep(long ms), kernel32;

int __goc_select(int nfds, fd_set *readfds, fd_set *writefds,
                 fd_set *exceptfds, struct timeval *timeout) {
    int r;
    ws_ensure();
    if ((!readfds   || readfds->fd_count == 0) &&
        (!writefds  || writefds->fd_count == 0) &&
        (!exceptfds || exceptfds->fd_count == 0)) {
        if (!timeout) {
            Sleep(0xFFFFFFFF);  /* INFINITE: what Linux's null timeout means */
        } else {
            long ms = timeout->tv_sec * 1000 + timeout->tv_usec / 1000;
            if (ms < 0) ms = 0;
            Sleep(ms);
        }
        return 0;
    }
    r = select(nfds, readfds, writefds, exceptfds, timeout);
    if (r == SOCKET_ERROR) return ws_fail();
    return r;
}

/* ------------------------------------------------------------------ */
/* The two non-POSIX conveniences                                      */
/* ------------------------------------------------------------------ */

int sock_set_nonblock(int fd, int on) {
    unsigned long mode;
    ws_ensure();
    mode = on ? 1 : 0;
    if (ioctlsocket((long)fd, FIONBIO, &mode) == SOCKET_ERROR) return ws_fail();
    return 0;
}

/* Windows takes a millisecond count in a 4-byte int where Linux takes a
 * struct timeval -- a different type, not a different number, which is why
 * this cannot be done by translating the option constant alone. */
int sock_set_timeout(int fd, int recv_ms, int send_ms) {
    int ms;
    ws_ensure();
    if (recv_ms >= 0) {
        ms = recv_ms;
        if (setsockopt((long)fd, SOL_SOCKET, SO_RCVTIMEO,
                       (const char *)&ms, 4) == SOCKET_ERROR) return ws_fail();
    }
    if (send_ms >= 0) {
        ms = send_ms;
        if (setsockopt((long)fd, SOL_SOCKET, SO_SNDTIMEO,
                       (const char *)&ms, 4) == SOCKET_ERROR) return ws_fail();
    }
    return 0;
}

#elif defined(__linux__)

#include <syscall.h>

/* ------------------------------------------------------------------ */
/* Error translation                                                   */
/* ------------------------------------------------------------------ */
/*
 * A Linux syscall reports failure by returning the negated errno, so -111 is a
 * refused connection. These are the kernel's numbers, and they are not
 * goclib's: 11 is EAGAIN to the kernel and ENOTSOCK is 88, while here 9 is
 * EAGAIN and 21 is ENOTSOCK.
 *
 * An untranslated code would therefore not merely be unrecognisable, it would
 * be misleading -- passing 11 through would read as ENOBUFS. So an unmapped
 * value becomes EIO, the one answer that is honest about being a guess.
 */
static int xlat(long r) {
    switch (-r) {
    case 11:  return EAGAIN;
    case 115: return EINPROGRESS;
    case 104: return ECONNRESET;
    case 111: return ECONNREFUSED;
    case 110: return ETIMEDOUT;
    case 107: return ENOTCONN;
    case 98:  return EADDRINUSE;
    case 100: return ENETDOWN;
    case 113: return EHOSTUNREACH;
    case 90:  return EMSGSIZE;
    case 4:   return EINTR;
    case 9:   return EBADF;
    case 88:  return ENOTSOCK;
    case 95:  return EOPNOTSUPP;
    case 105: return ENOBUFS;
    case 22:  return EINVAL;
    case 13:  return EACCES;
    case 12:  return ENOMEM;
    case 2:   return ENOENT;
    case 5:   return EIO;
    default:  return EIO;
    }
}

/* Two shapes of success: a descriptor or a status, and a byte count. They
 * cannot share one helper -- a successful send of 0 bytes is not an error, and
 * reading it through the status helper would return -1 for it. */
static int sys_stat(long r) {
    if (r < 0) { errno = xlat(r); return -1; }
    return (int)r;
}

/* Every data call goes through sys_io rather than a plain "negative means
 * error" helper, because a byte count needs one question the status calls never
 * ask: was that a timeout?
 *
 * A read or write that did not complete reports EAGAIN from the kernel whether
 * the socket is non-blocking or has a timeout set. Those are different events
 * to a program -- one is "nothing is there right now", the other is "I waited
 * and nothing came" -- and Winsock distinguishes them, answering WSAETIMEDOUT
 * for the second. So Linux has to say which it is.
 *
 * The test is whether O_NONBLOCK is set: a blocking socket cannot return EAGAIN
 * except by exhausting its timeout. It costs one fcntl, and only on the path
 * that already failed.
 */
static long sys_io(long fd, long r) {
    long flags;
    if (r >= 0) return r;
    if (r != -11) { errno = xlat(r); return -1; }
    flags = __goclib_fcntl(fd, 3, 0);
    if (flags >= 0 && (flags & 0x800) == 0) {
        errno = ETIMEDOUT;
        return -1;
    }
    errno = EAGAIN;
    return -1;
}

/* ------------------------------------------------------------------ */
/* Descriptor sets                                                     */
/* ------------------------------------------------------------------ */
/*
 * Linux's fd_set is a bitmap of 1024 bits, so these four are the shift and
 * mask operations the array-shaped Windows set does not have. A descriptor at
 * or beyond FD_SETSIZE is dropped rather than written out of bounds: select()
 * cannot represent it anyway, and a caller who hits the limit wants to know
 * through errno rather than through memory corruption.
 */
void FD_ZERO(fd_set *set) {
    int i;
    if (!set) return;
    for (i = 0; i < 16; i++) set->fds_bits[i] = 0;
}

void FD_SET(int fd, fd_set *set) {
    if (!set || fd < 0 || fd >= FD_SETSIZE) return;
    set->fds_bits[fd >> 6] |= (1L << (fd & 63));
}

void FD_CLR(int fd, fd_set *set) {
    if (!set || fd < 0 || fd >= FD_SETSIZE) return;
    set->fds_bits[fd >> 6] &= ~(1L << (fd & 63));
}

int FD_ISSET(int fd, fd_set *set) {
    if (!set || fd < 0 || fd >= FD_SETSIZE) return 0;
    return (set->fds_bits[fd >> 6] & (1L << (fd & 63))) != 0;
}

/* ------------------------------------------------------------------ */
/* The calls                                                           */
/* ------------------------------------------------------------------ */

int __goc_socket(int domain, int type, int protocol) {
    return sys_stat(__goclib_socket((long)domain, (long)type, (long)protocol));
}

int __goc_bind(int fd, const struct sockaddr *addr, socklen_t addrlen) {
    return sys_stat(__goclib_bind((long)fd, (const void *)addr, (long)addrlen));
}

int __goc_listen(int fd, int backlog) {
    return sys_stat(__goclib_listen((long)fd, (long)backlog));
}

/* The kernel wants a pointer to the length even when the caller passed no
 * address, so this always passes a local one and copies it back only if the
 * caller asked. Passing the caller's socklen_t* directly is not possible
 * anyway: it is 4 bytes and the syscall writes 8. */
int __goc_accept(int fd, struct sockaddr *addr, socklen_t *addrlen) {
    long alen = 0;
    long r;
    if (addrlen) alen = (long)*addrlen;
    r = __goclib_accept((long)fd, (void *)addr, &alen);
    if (r < 0) { errno = xlat(r); return -1; }
    if (addrlen) *addrlen = (socklen_t)alen;
    return (int)r;
}

int __goc_connect(int fd, const struct sockaddr *addr, socklen_t addrlen) {
    return sys_stat(__goclib_connect((long)fd, (const void *)addr, (long)addrlen));
}

/* Linux has no send(2) or recv(2): sendto(2) and recvfrom(2) with a null
 * address are how the connected-socket forms are spelled. */
long __goc_send(int fd, const void *buf, long len, int flags) {
    if (len < 0) { errno = EINVAL; return -1; }
    return sys_io((long)fd, __goclib_sendto((long)fd, buf, len, (long)flags, 0, 0));
}

long __goc_recv(int fd, void *buf, long len, int flags) {
    if (len < 0) { errno = EINVAL; return -1; }
    return sys_io((long)fd, __goclib_recvfrom((long)fd, buf, len, (long)flags, 0, 0));
}

long __goc_sendto(int fd, const void *buf, long len, int flags,
                  const struct sockaddr *to, socklen_t tolen) {
    if (len < 0) { errno = EINVAL; return -1; }
    return sys_io((long)fd, __goclib_sendto((long)fd, buf, len, (long)flags,
                                            (const void *)to, (long)tolen));
}

long __goc_recvfrom(int fd, void *buf, long len, int flags,
                    struct sockaddr *from, socklen_t *fromlen) {
    long flen = 0;
    long r;
    if (len < 0) { errno = EINVAL; return -1; }
    if (fromlen) flen = (long)*fromlen;
    r = __goclib_recvfrom((long)fd, buf, len, (long)flags, (void *)from, &flen);
    if (r < 0) return sys_io((long)fd, r);
    if (fromlen) *fromlen = (socklen_t)flen;
    return r;
}

int __goc_shutdown(int fd, int how) {
    return sys_stat(__goclib_shutdown((long)fd, (long)how));
}

/* close(2), and deliberately not goclib's close(): that one calls CloseHandle
 * on Windows, which is right for a file and wrong for a socket. Here both
 * arms agree because a Linux socket really is a descriptor. */
int __goc_closesocket(int fd) {
    return sys_stat(close((long)fd));
}

int __goc_setsockopt(int fd, int level, int optname, const void *val, socklen_t len) {
    return sys_stat(__goclib_setsockopt((long)fd, (long)level, (long)optname,
                                        val, (long)len));
}

int __goc_getsockopt(int fd, int level, int optname, void *val, socklen_t *len) {
    long l = 0;
    long r;
    if (len) l = (long)*len;
    r = __goclib_getsockopt((long)fd, (long)level, (long)optname, val, &l);
    if (r < 0) { errno = xlat(r); return -1; }
    if (len) *len = (socklen_t)l;
    return 0;
}

int __goc_getpeername(int fd, struct sockaddr *addr, socklen_t *addrlen) {
    long alen = 0;
    long r;
    if (addrlen) alen = (long)*addrlen;
    r = __goclib_getpeername((long)fd, (void *)addr, &alen);
    if (r < 0) { errno = xlat(r); return -1; }
    if (addrlen) *addrlen = (socklen_t)alen;
    return 0;
}

int __goc_getsockname(int fd, struct sockaddr *addr, socklen_t *addrlen) {
    long alen = 0;
    long r;
    if (addrlen) alen = (long)*addrlen;
    r = __goclib_getsockname((long)fd, (void *)addr, &alen);
    if (r < 0) { errno = xlat(r); return -1; }
    if (addrlen) *addrlen = (socklen_t)alen;
    return 0;
}

int __goc_select(int nfds, fd_set *readfds, fd_set *writefds,
                 fd_set *exceptfds, struct timeval *timeout) {
    return sys_stat(__goclib_select((long)nfds, (void *)readfds,
                                    (void *)writefds, (void *)exceptfds,
                                    (void *)timeout));
}

/* ------------------------------------------------------------------ */
/* The two non-POSIX conveniences                                      */
/* ------------------------------------------------------------------ */

/* fcntl(2): F_GETFL is 3 and F_SETFL is 4, and O_NONBLOCK is 0x800 on Linux
 * -- not 0x4000 as it is on some other systems, which is the usual surprise
 * for anyone who has written this against a BSD. Read the flags, change the
 * one bit, write them back: F_SETFL's argument is the whole flag word, so
 * skipping the read would clear everything else. */
int sock_set_nonblock(int fd, int on) {
    long flags = __goclib_fcntl((long)fd, 3, 0);
    long r;
    if (flags < 0) { errno = xlat(flags); return -1; }
    if (on) flags |= 0x800;
    else    flags &= ~0x800;
    r = __goclib_fcntl((long)fd, 4, flags);
    if (r < 0) { errno = xlat(r); return -1; }
    return 0;
}

/* Linux takes a struct timeval where Windows takes a millisecond count, so
 * this is the one place the two arms cannot share even the shape of the
 * argument. Negative means "leave that direction alone" -- it cannot mean
 * "no timeout", which is a zeroed timeval. */
int sock_set_timeout(int fd, int recv_ms, int send_ms) {
    struct timeval tv;
    if (recv_ms >= 0) {
        tv.tv_sec = (long)(recv_ms / 1000);
        tv.tv_usec = (long)((recv_ms % 1000) * 1000);
        if (sys_stat(__goclib_setsockopt((long)fd, (long)SOL_SOCKET,
                                         (long)SO_RCVTIMEO, &tv,
                                         (long)sizeof(struct timeval))) < 0) return -1;
    }
    if (send_ms >= 0) {
        tv.tv_sec = (long)(send_ms / 1000);
        tv.tv_usec = (long)((send_ms % 1000) * 1000);
        if (sys_stat(__goclib_setsockopt((long)fd, (long)SOL_SOCKET,
                                         (long)SO_SNDTIMEO, &tv,
                                         (long)sizeof(struct timeval))) < 0) return -1;
    }
    return 0;
}

#endif /* _WIN32 / __linux__ */

/* =============================================================================
 * Portable: byte order and address text.
 *
 * Neither arm above is involved. The byte-order functions are shifts and the
 * address conversions are arithmetic, because the platform's own versions are
 * exactly what differs between the two: Winsock exports htons and htonl from
 * ws2_32, Linux has no syscall for them, and inet_ntoa is a static buffer in
 * one libc and a thread-local one in another. Doing it here means the same
 * bytes come out on both.
 * ========================================================================== */

unsigned short __goclib_bswap16(unsigned short v) {
    return (unsigned short)(((v >> 8) & 0x00ff) | ((v << 8) & 0xff00));
}

unsigned int __goclib_bswap32(unsigned int v) {
    return ((v >> 24) & 0x000000ff) | ((v >> 8) & 0x0000ff00) |
           ((v << 8) & 0x00ff0000) | ((v << 24) & 0xff000000);
}

/* One parser, two callers. It has to return "parsed" separately from the value
 * because INADDR_NONE is both the failure value and a real address
 * (255.255.255.255), so inet_pton cannot decide from the value alone whether
 * "1.2.3.4.5" failed -- which is what the obvious shape gets wrong. */
static int parse_inet4(const char *cp, unsigned int *out) {
    unsigned int parts[4];
    unsigned int v = 0;
    int n = 0;
    int i;
    if (!cp) return 0;
    for (i = 0; ; i++) {
        char c = cp[i];
        if (c >= '0' && c <= '9') {
            v = v * 10 + (unsigned int)(c - '0');
            if (v > 255) return 0;
        } else if (c == '.') {
            if (n >= 3) return 0;
            parts[n] = v;
            n++;
            v = 0;
        } else if (c == 0) {
            parts[n] = v;
            n++;
            break;
        } else {
            return 0;
        }
    }
    if (n != 4) return 0;
    *out = (parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3];
    return 1;
}

in_addr_t inet_addr(const char *cp) {
    unsigned int a;
    if (!parse_inet4(cp, &a)) return INADDR_NONE;
    /* Built as the host-order integer the address means, then converted: the
     * struct wants network order, and forgetting the htonl() here is what puts
     * 1.0.0.127 on the wire for "127.0.0.1". */
    return htonl(a);
}

char *inet_ntoa(struct in_addr addr) {
    static char buf[INET_ADDRSTRLEN];
    if (!inet_ntop(AF_INET, &addr, buf, INET_ADDRSTRLEN)) buf[0] = 0;
    return buf;
}

int inet_pton(int af, const char *src, void *dst) {
    unsigned int a;
    if (af != AF_INET) return -1;
    if (!dst) return 0;
    if (!parse_inet4(src, &a)) return 0;
    ((struct in_addr *)dst)->s_addr = htonl(a);
    return 1;
}

const char *inet_ntop(int af, const void *src, char *dst, socklen_t size) {
    unsigned int v;
    unsigned int b[4];
    int i;
    int n = 0;
    char tmp[4];
    if (af != AF_INET || !src || !dst || size < 16) return 0;
    v = ntohl(((const struct in_addr *)src)->s_addr);
    b[0] = (v >> 24) & 0xff;
    b[1] = (v >> 16) & 0xff;
    b[2] = (v >> 8) & 0xff;
    b[3] = v & 0xff;
    for (i = 0; i < 4; i++) {
        unsigned int x = b[i];
        if (i > 0) dst[n++] = '.';
        if (x >= 100) {
            tmp[0] = (char)('0' + (x / 100));
            tmp[1] = (char)('0' + ((x / 10) % 10));
            tmp[2] = (char)('0' + (x % 10));
            dst[n] = tmp[0]; dst[n+1] = tmp[1]; dst[n+2] = tmp[2];
            n += 3;
        } else if (x >= 10) {
            tmp[0] = (char)('0' + (x / 10));
            tmp[1] = (char)('0' + (x % 10));
            dst[n] = tmp[0]; dst[n+1] = tmp[1];
            n += 2;
        } else {
            dst[n] = (char)('0' + x);
            n += 1;
        }
    }
    dst[n] = 0;
    return dst;
}
