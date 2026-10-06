#include "goclib.h"
#include <errno.h>

/* =============================================================================
 * errno.c -- the errno variable and strerror().
 *
 * errno lives behind a location function (the glibc trick): the macro in
 * errno.h expands to (*__goclib_errno_loc()), so `errno = 2` and reading it
 * both work as a plain lvalue. strerror() maps the small set of codes the
 * library and user code actually use to fixed English strings -- no locale
 * support, matching the rest of this teaching library.
 * ========================================================================== */

static int __goclib_errno_val;

int *__goclib_errno_loc(void) {
    return &__goclib_errno_val;
}

char *strerror(int errnum) {
    switch (errnum) {
    case 1: return "Numerical argument out of domain";
    case 2: return "Numerical result out of range";
    case 3: return "Illegal byte sequence";
    case 4: return "Invalid argument";
    case 5: return "No such file or directory";
    case 6: return "Permission denied";
    case 7: return "Cannot allocate memory";
    case 8: return "Input/output error";
    /* The networking codes, in the order errno.h lists them. A socket error
     * that arrives as 111 on one target and 10061 on the other has already
     * been translated to 12 by the time strerror sees it -- which is the only
     * reason one switch can serve both. */
    case 9:  return "Resource temporarily unavailable";
    case 10: return "Operation now in progress";
    case 11: return "Connection reset by peer";
    case 12: return "Connection refused";
    case 13: return "Connection timed out";
    case 14: return "Transport endpoint is not connected";
    case 15: return "Address already in use";
    case 16: return "Network is down";
    case 17: return "No route to host";
    case 18: return "Message too long";
    case 19: return "Interrupted system call";
    case 20: return "Bad file descriptor";
    case 21: return "Socket operation on non-socket";
    case 22: return "Operation not supported";
    case 23: return "No buffer space available";
    default: return "Unknown error";
    }
}
