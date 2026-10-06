#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <socket.h>
#include <in.h>

/* The parts of the socket layer the TCP round trip does not reach: datagrams,
 * non-blocking I/O, timeouts, and the error translation.
 *
 * The last one is the reason this file exists. A program that says
 * `if (errno == ECONNREFUSED)' is relying on goclib having translated -- Linux
 * answers 111 and Winsock answers 10061 -- and nothing about that line tells
 * you whether it worked.
 */

static int fails = 0;

static void check(int ok, const char *what) {
    printf("%s %s\n", ok ? "ok  " : "FAIL", what);
    if (!ok) fails++;
}

/* Bind a loopback socket and return the port the kernel chose. */
static int bind_loopback(int fd, struct sockaddr_in *sa) {
    socklen_t alen = sizeof(*sa);
    memset(sa, 0, sizeof(*sa));
    sa->sin_family = AF_INET;
    sa->sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    sa->sin_port = 0;
    if (bind(fd, (struct sockaddr *)sa, sizeof(*sa)) < 0) return -1;
    if (getsockname(fd, (struct sockaddr *)sa, &alen) < 0) return -1;
    return ntohs(sa->sin_port);
}

int main(void) {
    /* ---- UDP round trip ---- */
    {
        int a = socket(AF_INET, SOCK_DGRAM, 0);
        int b = socket(AF_INET, SOCK_DGRAM, 0);
        struct sockaddr_in sa, from;
        socklen_t flen;
        char buf[64];
        long n;
        check(a >= 0 && b >= 0, "two datagram sockets");
        check(bind_loopback(b, &sa) > 0, "bind() a datagram socket");

        n = sendto(a, "ping", 4, 0, (struct sockaddr *)&sa, sizeof(sa));
        printf("     sendto n=%ld errno=%d (%s)\n", n, errno, strerror(errno));
        check(n == 4, "sendto() returns 4");

        flen = sizeof(from);
        n = recvfrom(b, buf, 64, 0, (struct sockaddr *)&from, &flen);
        check(n == 4, "recvfrom() returns 4");
        if (n == 4) {
            buf[4] = 0;
            check(strcmp(buf, "ping") == 0, "the datagram survived");
            check(ntohs(from.sin_port) > 0, "recvfrom() reports the sender's port");
        }
        closesocket(a);
        closesocket(b);
    }

    /* ---- A read timeout ---- */
    {
        int s = socket(AF_INET, SOCK_DGRAM, 0);
        struct sockaddr_in sa;
        struct timeval zero;
        long n;
        char buf[8];
        bind_loopback(s, &sa);
        /* 200ms, then read a socket nobody will ever write to. */
        check(sock_set_timeout(s, 200, -1) == 0, "sock_set_timeout()");
        n = recv(s, buf, 8, 0);
        printf("     recv n=%ld errno=%d (%s)\n", n, errno, strerror(errno));
        check(n < 0, "a read with nothing arriving fails");
        /* ETIMEDOUT on both: Linux's kernel says EAGAIN for a read timeout, and
         * the wrapper asks fcntl whether the socket is non-blocking to tell that
         * apart from "no data right now". */
        check(errno == ETIMEDOUT, "and reports ETIMEDOUT");
        /* A zero timeout is "do not wait at all", not "wait forever". */
        zero.tv_sec = 0;
        zero.tv_usec = 0;
        {
            long sr = select(s + 1, 0, 0, 0, &zero);
            printf("     select(all-null) r=%ld errno=%d\n", sr, errno);
            check(sr == 0, "select() with a zero timeout returns 0");
        }
        closesocket(s);
    }

    /* ---- Non-blocking ---- */
    {
        int l = socket(AF_INET, SOCK_STREAM, 0);
        struct sockaddr_in sa;
        int r;
        bind_loopback(l, &sa);
        listen(l, 1);
        check(sock_set_nonblock(l, 1) == 0, "sock_set_nonblock(1)");
        r = accept(l, 0, 0);
        /* (long) rather than the int it is: goclib's %d reads a 64-bit vararg
         * slot, which is right under goc (whose varargs are all 64-bit) and
         * reads the zero upper half of a 32-bit int under gcc. Harmless here,
         * wrong-looking in the output. */
        printf("     accept on a quiet non-blocking listener: %ld errno=%d (%s)\n",
               (long)r, errno, strerror(errno));
        check(r < 0, "accept() with nothing pending fails");
        check(errno == EAGAIN, "and reports EAGAIN, not a timeout");
        check(sock_set_nonblock(l, 0) == 0, "sock_set_nonblock(0)");
        closesocket(l);
    }

    /* ---- Error translation ---- */
    {
        /* Nothing is listening on the port this is about to use, so connect()
         * must be refused -- and must say so in goclib's numbering, which is
         * neither the 111 Linux returns nor the 10061 Winsock returns. */
        int l = socket(AF_INET, SOCK_STREAM, 0);
        int c = socket(AF_INET, SOCK_STREAM, 0);
        struct sockaddr_in sa;
        int port = bind_loopback(l, &sa);
        closesocket(l);
        printf("     connecting to a closed port %d\n", port);
        if (connect(c, (struct sockaddr *)&sa, sizeof(sa)) < 0) {
            printf("     errno=%d (%s)\n", errno, strerror(errno));
            check(errno == ECONNREFUSED, "connect() to a closed port reports ECONNREFUSED");
        } else {
            check(0, "connect() to a closed port should have failed");
        }
        closesocket(c);
    }

    /* ---- Bad input ---- */
    {
        check(socket(AF_INET, SOCK_STREAM, 0) >= 0, "socket() again");
        check(send(999, "x", 1, 0) < 0, "send() on a closed descriptor fails");
        /* Deliberately not one code: descriptor 999 is not open on Linux, so
         * the kernel answers EBADF, while Winsock answers ENOTSOCK because it
         * never had such a handle. Both are the truth for their platform and
         * neither is worth lying about to make them agree. */
        printf("     errno=%d (%s)\n", errno, strerror(errno));
        check(errno == EBADF || errno == ENOTSOCK, "and reports EBADF or ENOTSOCK");
        check(inet_addr("1.2.3") == INADDR_NONE, "inet_addr() rejects 1.2.3");
        check(inet_addr("1.2.3.4.5") == INADDR_NONE, "inet_addr() rejects 1.2.3.4.5");
        check(inet_addr("300.1.1.1") == INADDR_NONE, "inet_addr() rejects 300.1.1.1");
        check(inet_pton(AF_INET6, "::1", 0) < 0, "inet_pton() rejects AF_INET6");
    }

    printf(fails ? "FAILED %d\n" : "OK\n", fails);
    return fails ? 1 : 0;
}
