#ifndef GOC_THREADS_H
#define GOC_THREADS_H

/* goc threads.h -- opaque thread/mutex/cond types (C11 7.26).
 *
 * goc is single-threaded and implements none of the threads.h functions, so
 * a call to any thrd_, mtx_ or cnd_ function is a clean "unknown function"
 * compile error. The types below are the same opaque structs the suite's
 * fallback typedefs use (sizeof stays identical whether <threads.h> is
 * present or not, on goc and on a gcc that lacks the header).
 */

typedef struct { int opaque; } thrd_t;
typedef struct { int mtx; } mtx_t;
typedef struct { int cnd; } cnd_t;
typedef struct { int once; } once_flag;
typedef struct { int val; } tss_t;

#define ONCE_FLAG_INIT {0}
#define TSS_DTOR_ITERATIONS 4

#define thread_local _Thread_local

#endif /* GOC_THREADS_H */
