#ifndef GOC_THREADS_H
#define GOC_THREADS_H

/* =============================================================================
 * goc threads.h -- C11/C23 threads (7.26), implemented on both targets.
 *
 * This header used to declare the opaque types and none of the functions, so
 * every thrd_/mtx_/cnd_ call was a clean "unknown function" error. The real
 * thing is now in goclib/threads.c and works on both platforms, though the
 * two get there by different routes:
 *
 *   Windows  CreateThread for thrd_t, CRITICAL_SECTION for mtx_t,
 *            CONDITION_VARIABLE for cnd_t, the Tls* family for tss_t and
 *            InitOnceExecuteOnce for call_once -- all kernel32 imports.
 *   Linux    clone() for thrd_t (each thread gets an mmap'd stack and is
 *            started through an assembly trampoline, because the raw clone
 *            syscall resumes the child on a fresh stack with no return
 *            address), futex() for mtx_t and cnd_t, and a per-thread slot
 *            table for tss_t.
 *
 * The types are sized per platform rather than padded to one common shape:
 * the header is preprocessed for the selected target, so a Windows build sees
 * a CRITICAL_SECTION-sized mtx_t and a Linux build a futex word. That means
 * sizeof(mtx_t) differs between targets -- which is what a hosted
 * implementation does too, and why portable code never assumes a size.
 *
 * Storage for the platform objects is spelled as `long` arrays instead of
 * including <windows.h> from a user-visible header: a program that includes
 * <threads.h> should not also acquire the entire Win32 surface.
 * ========================================================================== */

#include <time.h>

/* -----------------------------------------------------------------------------
 * Status codes and mutex types
 * -------------------------------------------------------------------------- */

#define thrd_success  0   /* the operation worked                            */
#define thrd_busy     1   /* a resource is already in use                    */
#define thrd_error    2   /* the operation failed                            */
#define thrd_nomem    3   /* out of memory                                   */
#define thrd_timedout 4   /* a timed wait expired first                      */

#define mtx_plain     0   /* neither timed nor recursive                     */
#define mtx_recursive 1   /* may be locked again by its owner                */
#define mtx_timed     2   /* supports mtx_timedlock                          */

#define TSS_DTOR_ITERATIONS 4

/* -----------------------------------------------------------------------------
 * Types
 * -------------------------------------------------------------------------- */

/* thrd_t is a pointer to a heap block threads.c allocates on thrd_create and
 * releases on join or detach, so the value stays valid for thrd_join after
 * the thread itself has finished. thrd_current() hands out the same pointer
 * every time it is called on one thread, including the main one. */
struct __goc_thrd;
typedef struct __goc_thrd *thrd_t;

typedef int (*thrd_start_t)(void *arg);
typedef void (*tss_dtor_t)(void *val);

/* call_once is implemented the same way on both targets -- a global spinlock
 * guarding a small state word -- so once_flag carries no platform object and
 * lives outside the split below. */
typedef struct {
    int state;            /* 0 fresh, 1 running, 2 done                      */
    int pad;
} once_flag;

#define ONCE_FLAG_INIT { 0, 0 }

#if defined(_WIN32)

/* cs[] is a Win32 CRITICAL_SECTION (40 bytes, 8-byte aligned) and cv[] a
 * CONDITION_VARIABLE (8 bytes). */
typedef struct {
    long cs[6];
    int  type;
    int  inited;
} mtx_t;

typedef struct {
    long cv[2];
    int  inited;
} cnd_t;

typedef struct {
    unsigned int key;     /* a TlsAlloc index                                */
    int          inited;
} tss_t;

#else

/* state is the futex word: 0 free, 1 held with no waiters, 2 held with
 * waiters (so an unlock knows whether to make the wake syscall). */
typedef struct {
    int  state;
    int  type;
    int  inited;
    long owner;           /* gettid() of the holder, 0 when free             */
    int  depth;           /* recursion count, mtx_recursive only             */
} mtx_t;

/* A generation counter. cnd_wait samples it, unlocks the mutex and waits
 * until it changes; a signal that lands between the sample and the wait
 * still makes the wait return immediately, so no wakeup is lost. */
typedef struct {
    int seq;
    int inited;
} cnd_t;

typedef struct {
    int key;
    int inited;
} tss_t;

#endif /* _WIN32 */

#define thread_local _Thread_local

/* -----------------------------------------------------------------------------
 * Threads
 * -------------------------------------------------------------------------- */

int    thrd_create(thrd_t *thr, thrd_start_t func, void *arg);
int    thrd_equal(thrd_t lhs, thrd_t rhs);
thrd_t thrd_current(void);
int    thrd_detach(thrd_t thr);
int    thrd_join(thrd_t thr, int *res);
int    thrd_sleep(const struct timespec *duration, struct timespec *remaining);
void   thrd_yield(void);
void   thrd_exit(int res);

/* -----------------------------------------------------------------------------
 * Mutexes
 * -------------------------------------------------------------------------- */

int mtx_init(mtx_t *mtx, int type);
void mtx_destroy(mtx_t *mtx);
int mtx_lock(mtx_t *mtx);
int mtx_timedlock(mtx_t *mtx, const struct timespec *ts);
int mtx_trylock(mtx_t *mtx);
int mtx_unlock(mtx_t *mtx);

/* -----------------------------------------------------------------------------
 * Condition variables
 * -------------------------------------------------------------------------- */

int  cnd_init(cnd_t *cond);
void cnd_destroy(cnd_t *cond);
int  cnd_signal(cnd_t *cond);
int  cnd_broadcast(cnd_t *cond);
int  cnd_wait(cnd_t *cond, mtx_t *mtx);
int  cnd_timedwait(cnd_t *cond, mtx_t *mtx, const struct timespec *ts);

/* -----------------------------------------------------------------------------
 * Thread-specific storage
 * -------------------------------------------------------------------------- */

int    tss_create(tss_t *key, tss_dtor_t dtor);
void   tss_delete(tss_t key);
void  *tss_get(tss_t key);
int    tss_set(tss_t key, void *val);

/* -----------------------------------------------------------------------------
 * One-time initialisation
 * -------------------------------------------------------------------------- */

/* func takes no arguments and returns nothing, per the standard. */
void call_once(once_flag *flag, void (*func)(void));

#endif /* GOC_THREADS_H */
