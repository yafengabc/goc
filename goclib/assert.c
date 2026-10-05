#include "goclib.h"
#include <assert.h>

/* =============================================================================
 * assert.c -- the assertion failure handler behind assert().
 *
 * The macro in assert.h stringises the expression and captures __FILE__ /
 * __LINE__ at the call site; this function only reports and aborts. Output
 * goes to stderr, per the C standard.
 * ========================================================================== */

void __goclib_assert_fail(const char *expr, const char *file, int line) {
    fprintf(stderr, "assertion \"%s\" failed: file \"%s\", line %d\n",
            expr, file, line);
    abort();
}
