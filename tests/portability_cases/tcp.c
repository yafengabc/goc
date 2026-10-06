#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <socket.h>
#include <in.h>

/* A loopback TCP round trip in one process: bind, listen, connect, accept,
 * send, recv. It exercises the two things a socket library has to get right
 * that nothing else in goclib touches -- the option and address numbers, and
 * the way a descriptor set is built.
 *
 * The port is left to the kernel (sin_port = 0) and read back with
 * getsockname(), so the test cannot collide with anything already listening.
 */

static int fails = 0;

static void check(int ok, const char *what) {
    printf("%s %s\n", ok ? "ok  " : "FAIL", what);
    if (!ok) fails++;
}

int main(void) {
    int srv, cli, acc;
    struct sockaddr_in sa;
    socklen_t alen;
    fd_set rd;
    struct timeval tv;
    char buf[64];
    long n;
    int port;
    int one = 1;

    srv = socket(AF_INET, SOCK_STREAM, 0);
    check(srv >= 0, "socket()");
    if (srv < 0) { printf("errno=%d\n", errno); return 1; }

    /* SO_REUSEADDR is the option whose number differs between the two targets
     * (2 on Linux, 4 on Windows), so setting it here is a real check that the
     * header translated it. */
    if (setsockopt(srv, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one)) < 0) {
        printf("     setsockopt errno=%d (%s) sizeof=%d\n", errno, strerror(errno), (int)sizeof(one));
        check(0, "setsockopt(SO_REUSEADDR)");
    } else {
        check(1, "setsockopt(SO_REUSEADDR)");
    }

    memset(&sa, 0, sizeof(sa));
    sa.sin_family = AF_INET;
    sa.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    sa.sin_port = 0;

    check(bind(srv, (struct sockaddr *)&sa, sizeof(sa)) == 0, "bind()");
    check(listen(srv, 1) == 0, "listen()");

    alen = sizeof(sa);
    check(getsockname(srv, (struct sockaddr *)&sa, &alen) == 0, "getsockname()");
    port = ntohs(sa.sin_port);
    printf("     port=%d\n", port);
    check(port > 0, "kernel chose a port");

    cli = socket(AF_INET, SOCK_STREAM, 0);
    check(cli >= 0, "socket() for client");
    check(connect(cli, (struct sockaddr *)&sa, sizeof(sa)) == 0, "connect()");

    /* select() on the listener, and the reason to use it: without it, accept()
     * on a socket with no pending connection blocks forever. */
    FD_ZERO(&rd);
    FD_SET(srv, &rd);
    tv.tv_sec = 2;
    tv.tv_usec = 0;
    n = select(srv + 1, &rd, 0, 0, &tv);
    printf("     select n=%d errno=%d tv=%d/%d\n", (int)n, errno, (int)tv.tv_sec, (int)tv.tv_usec);
    check(n == 1, "select() reports the listener readable");
    check(FD_ISSET(srv, &rd) != 0, "FD_ISSET() finds it");

    {
        struct sockaddr_in peer;
        alen = sizeof(peer);
        acc = accept(srv, (struct sockaddr *)&peer, &alen);
        if (acc < 0) printf("     accept errno=%d (%s)\n", errno, strerror(errno));
        else printf("     peer port=%d addr=%s\n", ntohs(peer.sin_port),
                    inet_ntoa(peer.sin_addr));
    }
    check(acc >= 0, "accept()");
    if (acc < 0) return 1;

    n = send(cli, "hello", 5, 0);
    check(n == 5, "send() returns 5");

    n = recv(acc, buf, 64, 0);
    printf("     recv n=%d errno=%d\n", (int)n, errno);
    check(n == 5, "recv() returns 5");
    if (n > 0) {
        buf[n] = 0;
        printf("     got=%s\n", buf);
        check(strcmp(buf, "hello") == 0, "the bytes survived");
    }

    /* Address text: the numbers have to come back in the order they were
     * written, which is what htonl/ntohl are for and what breaks first when
     * the byte order is wrong. */
    {
        struct in_addr a;
        char text[32];
        a.s_addr = htonl(INADDR_LOOPBACK);
        check(inet_ntop(AF_INET, &a, text, 32) != 0, "inet_ntop()");
        printf("     loopback=%s\n", text);
        check(strcmp(text, "127.0.0.1") == 0, "loopback text is 127.0.0.1");
        check(inet_addr("192.168.1.2") == htonl(0xc0a80102), "inet_addr()");
        check(ntohl(inet_addr("127.0.0.1")) == INADDR_LOOPBACK, "inet_addr() round trip");
    }

    /* A second round trip, this time through accept(fd, 0, 0) -- the spelling
     * that means "I do not care who connected". It is ordinary on Linux and
     * Winsock refuses it unless the null is passed through as a null, so this
     * is the check that the wrapper absorbed that difference. */
    {
        int s2 = socket(AF_INET, SOCK_STREAM, 0);
        int c2 = socket(AF_INET, SOCK_STREAM, 0);
        struct sockaddr_in sa2;
        socklen_t al2 = sizeof(sa2);
        int a2;
        memset(&sa2, 0, sizeof(sa2));
        sa2.sin_family = AF_INET;
        sa2.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        sa2.sin_port = 0;
        bind(s2, (struct sockaddr *)&sa2, sizeof(sa2));
        listen(s2, 1);
        getsockname(s2, (struct sockaddr *)&sa2, &al2);
        connect(c2, (struct sockaddr *)&sa2, sizeof(sa2));
        a2 = accept(s2, 0, 0);
        if (a2 < 0) printf("     accept(0,0) errno=%d (%s)\n", errno, strerror(errno));
        check(a2 >= 0, "accept(fd, 0, 0)");
        if (a2 >= 0) closesocket(a2);
        closesocket(c2);
        closesocket(s2);
    }

    check(shutdown(cli, SHUT_WR) == 0, "shutdown()");
    check(closesocket(cli) == 0, "closesocket(client)");
    check(closesocket(acc) == 0, "closesocket(accepted)");
    check(closesocket(srv) == 0, "closesocket(listener)");

    printf(fails ? "FAILED %d\n" : "OK\n", fails);
    return fails ? 1 : 0;
}
