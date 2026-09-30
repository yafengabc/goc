#ifndef GOC_ASSERT_H
#define GOC_ASSERT_H

/* goc assert.h -- the diagnostic assert macro.
 *
 * A failed assertion prints
 *     assertion "expr" failed: file "f.c", line N
 * to stderr and calls abort(). Defining NDEBUG before including this header
 * compiles assert() away entirely (expands to nothing, no evaluation).
 */

void __goclib_assert_fail(const char *expr, const char *file, int line);
void abort(void);

#ifdef NDEBUG
#define assert(e) ((void)0)
#else
#define assert(e) ((e) ? (void)0 : __goclib_assert_fail(#e, __FILE__, __LINE__))
#endif

#endif /* GOC_ASSERT_H */
