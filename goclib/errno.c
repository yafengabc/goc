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
    default: return "Unknown error";
    }
}
