/* =============================================================================
 * mtx.c -- the C11/C23 mutex implementation behind <threads.h> (7.26.4).
 *
 * This file is the "separate implementation" the header and threads.c point at:
 * threads.c only *calls* mtx_lock/mtx_unlock (from cnd_wait), it does not
 * define them. The mutex object is a different shape per target -- a Win32
 * CRITICAL_SECTION on Windows and a futex word on Linux -- so the two halves
 * below are selected by the target, exactly like threads.c.
 *
 * On Windows the whole thing is a thin wrapper over CRITICAL_SECTION, which is
 * already recursive and already correct, so mtx_recursive/mtx_timed only steer
 * small amounts of bookkeeping. On Linux the futex word carries the state
 * (0 free, 1 held, 2 held-with-waiters) and a couple of helper CAS/xchg
 * primitives -- both implicitly or explicitly locked -- do the atomic part.
 *
 * mtx_timedlock is where the two targets diverge in mechanism, not in contract:
 *   Windows  CRITICAL_SECTION has no timed form, so we try-and-yield until the
 *            deadline, returning thrd_timedout if it passes. Documented, not
 *            silently wrong.
 *   Linux    the futex wait itself takes a relative timeout and returns
 *            -ETIMEDOUT (-110), which we turn into thrd_timedout.
 * ========================================================================== */

#include "goclib.h"
#include <threads.h>
#include <time.h>

#if defined(_WIN32)

/* mtx_t.cs is a long[6] laid out to hold a CRITICAL_SECTION (see threads.h);
 * it sits at offset 0, so the mutex pointer doubles as the section pointer. */
#define MTX_CS(m) ((CRITICAL_SECTION *)(m))

int mtx_init(mtx_t *mtx, int type) {
    if (mtx == 0) return thrd_error;
    mtx->type   = type;
    mtx->inited = 1;
    InitializeCriticalSection(MTX_CS(mtx));
    return thrd_success;
}

void mtx_destroy(mtx_t *mtx) {
    if (mtx == 0 || !mtx->inited) return;
    DeleteCriticalSection(MTX_CS(mtx));
    mtx->inited = 0;
}

int mtx_lock(mtx_t *mtx) {
    if (mtx == 0) return thrd_error;
    EnterCriticalSection(MTX_CS(mtx));
    return thrd_success;
}

int mtx_trylock(mtx_t *mtx) {
    if (mtx == 0) return thrd_error;
    if (TryEnterCriticalSection(MTX_CS(mtx))) return thrd_success;
    return thrd_busy;
}

int mtx_unlock(mtx_t *mtx) {
    if (mtx == 0) return thrd_error;
    LeaveCriticalSection(MTX_CS(mtx));
    return thrd_success;
}

int mtx_timedlock(mtx_t *mtx, const struct timespec *ts) {
    struct timespec now;
    if (mtx == 0 || ts == 0) return thrd_error;
    for (;;) {
        if (TryEnterCriticalSection(MTX_CS(mtx))) return thrd_success;
        timespec_get(&now, 1);
        if (now.tv_sec > ts->tv_sec ||
            (now.tv_sec == ts->tv_sec && now.tv_nsec >= ts->tv_nsec))
            return thrd_timedout;
        Sleep(1);
    }
}

#else /* Linux */

/* goclib runtime primitives threads.c also uses. Declared here because goclib
 * compiles each .c as its own translation unit. */
extern long __goclib_futex(int *uaddr, long op, long val, void *timeout,
                           int *uaddr2, long val3);
extern long __goclib_gettid(void);

#define FUTEX_WAIT 0
#define FUTEX_WAKE 1

/* 32-bit atomic primitives over the int futex word. The state word is an int,
 * and a futex is a 32-bit word, so these must stay 32-bit -- an 8-byte xchg
 * would clobber the neighbouring type/inited fields. xchg is implicitly locked
 * on x86-64; cmpxchg gets the lock prefix by hand. */
static int gthr_lock_xchg_i(int *p, int v) {
    int r;
    __asm {
        mov eax, v
        mov rdx, p
        xchg eax, [rdx]
        mov r, eax
    }
    return r;
}

static int gthr_lock_cas_i(int *p, int old, int newv) {
    int r;
    __asm {
        mov eax, old
        mov edx, newv
        mov rcx, p
        lock cmpxchg [rcx], edx
        mov r, eax
    }
    return r;
}

int mtx_init(mtx_t *mtx, int type) {
    if (mtx == 0) return thrd_error;
    mtx->state  = 0;
    mtx->type   = type;
    mtx->inited = 1;
    mtx->owner  = 0;
    mtx->depth  = 0;
    return thrd_success;
}

void mtx_destroy(mtx_t *mtx) {
    if (mtx == 0) return;
    mtx->inited = 0;
}

int mtx_lock(mtx_t *mtx) {
    long self;
    if (mtx == 0) return thrd_error;
    self = __goclib_gettid();
    /* Recursive fast path: already held by this thread. */
    if ((mtx->type & 1) && mtx->owner == self) {
        mtx->depth++;
        return thrd_success;
    }
    /* Acquire: exchange state 0->1. A non-zero return means it was held. */
    while (gthr_lock_xchg_i(&mtx->state, 1) != 0) {
        /* Mark waiters (1->2) so unlock knows to wake, then sleep on the
         * word while it still reads 2. If it changed underneath us the wait
         * returns at once and we retry. */
        if (gthr_lock_cas_i(&mtx->state, 1, 2) == 1) {
            /* now 2 */
        }
        __goclib_futex(&mtx->state, FUTEX_WAIT, 2, (void *)0, (int *)0, 0);
    }
    mtx->owner = self;
    mtx->depth = 1;
    return thrd_success;
}

int mtx_trylock(mtx_t *mtx) {
    long self;
    if (mtx == 0) return thrd_error;
    self = __goclib_gettid();
    if ((mtx->type & 1) && mtx->owner == self) {
        mtx->depth++;
        return thrd_success;
    }
    if (gthr_lock_cas_i(&mtx->state, 0, 1) == 0) {
        mtx->owner = self;
        mtx->depth = 1;
        return thrd_success;
    }
    return thrd_busy;
}

int mtx_unlock(mtx_t *mtx) {
    long c;
    if (mtx == 0) return thrd_error;
    if (mtx->type & 1) {
        mtx->depth--;
        if (mtx->depth > 0) return thrd_success;  /* still held by us */
        mtx->owner = 0;
    }
    c = gthr_lock_xchg_i(&mtx->state, 0);   /* release: 0, learn old value */
    if (c == 2) {                            /* had waiters: wake one */
        __goclib_futex(&mtx->state, FUTEX_WAKE, 1, (void *)0, (int *)0, 0);
    }
    return thrd_success;
}

int mtx_timedlock(mtx_t *mtx, const struct timespec *ts) {
    struct timespec now, rel;
    long self;
    if (mtx == 0 || ts == 0) return thrd_error;
    self = __goclib_gettid();
    if ((mtx->type & 1) && mtx->owner == self) {
        mtx->depth++;
        return thrd_success;
    }
    for (;;) {
        if (gthr_lock_cas_i(&mtx->state, 0, 1) == 0) {
            mtx->owner = self;
            mtx->depth = 1;
            return thrd_success;
        }
        timespec_get(&now, 1);
        if (now.tv_sec > ts->tv_sec ||
            (now.tv_sec == ts->tv_sec && now.tv_nsec >= ts->tv_nsec))
            return thrd_timedout;
        rel.tv_sec  = ts->tv_sec - now.tv_sec;
        rel.tv_nsec = ts->tv_nsec - now.tv_nsec;
        if (rel.tv_nsec < 0) {
            rel.tv_nsec += 1000000000L;
            rel.tv_sec--;
        }
        if (rel.tv_sec < 0) { rel.tv_sec = 0; rel.tv_nsec = 0; }
        if (gthr_lock_cas_i(&mtx->state, 1, 2) == 1) {
            /* marked waiters */
        }
        /* Relative timeout; -ETIMEDOUT (-110) just means loop and re-check. */
        __goclib_futex(&mtx->state, FUTEX_WAIT, 2, (void *)&rel, (int *)0, 0);
    }
}

#endif /* _WIN32 */
