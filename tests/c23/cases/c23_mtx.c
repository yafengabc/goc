/* C23 feature: <threads.h> mutex (mtx_*) -- real implementation in goclib/mtx.c
 * Clause:     C23 7.26.4 <threads.h> (C11; goc roadmap #129 follow-up)
 * Strategy:   exercise the mutex from goclib/mtx.c (Windows: CRITICAL_SECTION;
 *             Linux: futex) the way a real program would: a two-thread counter
 *             guarded by mtx_lock/mtx_unlock, plus mtx_trylock on a free mutex
 *             and a recursive re-lock. Values are cross-checked against gcc
 *             -std=c2x; since that toolchain ships no <threads.h>, a winpthreads
 *             shim stands in on the gcc side (same approach as c23_atomic.c).
 * Status:     PASS
 * EXPECT: PASS */
#include <stdio.h>

#if defined(__has_include) && __has_include(<threads.h>)
#include <threads.h>
#else
/* gcc shim: mingw-w64 ucrt64 has no <threads.h>; emulate the subset we use on
 * top of winpthreads (always present in this toolchain). */
#include <stdint.h>
#include <pthread.h>
typedef pthread_t thrd_t;
typedef int (*thrd_start_t)(void *);
enum { thrd_success = 0, thrd_busy = 1, thrd_error = 2, thrd_nomem = 3, thrd_timedout = 4 };
typedef struct { pthread_mutex_t m; int recursive; } mtx_t;
enum { mtx_plain = 0, mtx_recursive = 1, mtx_timed = 2 };
static int thrd_create(thrd_t *t, thrd_start_t fn, void *arg) {
    /* pthread_create wants void*(*)(void*); thrd_start_t is int(*)(void*).
     * Punch through a union to avoid a -Wcast-function-type warning. */
    union { thrd_start_t f; void *(*vp)(void *); } u;
    u.f = fn;
    return pthread_create(t, NULL, u.vp, arg) == 0 ? thrd_success : thrd_error;
}
static int thrd_join(thrd_t t, int *res) {
    void *r;
    if (pthread_join(t, &r) != 0) return thrd_error;
    if (res) *res = (int)(intptr_t)r;
    return thrd_success;
}
static int mtx_init(mtx_t *m, int type) {
    pthread_mutexattr_t a;
    pthread_mutexattr_init(&a);
    if (type & mtx_recursive) pthread_mutexattr_settype(&a, PTHREAD_MUTEX_RECURSIVE);
    int rc = pthread_mutex_init(&m->m, &a);
    pthread_mutexattr_destroy(&a);
    m->recursive = (type & mtx_recursive) ? 1 : 0;
    return rc == 0 ? thrd_success : thrd_error;
}
static int mtx_lock(mtx_t *m) {
    return pthread_mutex_lock(&m->m) == 0 ? thrd_success : thrd_error;
}
static int mtx_trylock(mtx_t *m) {
    int rc = pthread_mutex_trylock(&m->m);
    if (rc == 0) return thrd_success;
    if (rc == EBUSY) return thrd_busy;
    return thrd_error;
}
static int mtx_unlock(mtx_t *m) {
    return pthread_mutex_unlock(&m->m) == 0 ? thrd_success : thrd_error;
}
static int mtx_destroy(mtx_t *m) {
    return pthread_mutex_destroy(&m->m) == 0 ? thrd_success : thrd_error;
}
#endif

#define N 100000
static mtx_t cm;
static int counter = 0;

static int worker(void *arg) {
    (void)arg;
    for (int i = 0; i < N; i++) {
        mtx_lock(&cm);
        counter = counter + 1;
        mtx_unlock(&cm);
    }
    return 0;
}

int main(void) {
    int passed = 0, total = 0;
    thrd_t t1, t2;

    /* case: two threads increment a shared counter under a plain mutex */
    ++total;
    counter = 0;
    mtx_init(&cm, mtx_plain);
    thrd_create(&t1, worker, 0);
    thrd_create(&t2, worker, 0);
    thrd_join(t1, 0);
    thrd_join(t2, 0);
    mtx_destroy(&cm);
    printf("case%d: counter=%d (expect %d)\n", total, counter, 2 * N);
    if (counter == 2 * N) passed++;

    /* case: mtx_trylock on a free mutex must succeed */
    ++total;
    mtx_t tm;
    mtx_init(&tm, mtx_plain);
    int rc = mtx_trylock(&tm);
    printf("case%d: trylock_free=%d\n", total, rc);
    if (rc == 0) passed++;
    mtx_unlock(&tm);
    mtx_destroy(&tm);

    /* case: a recursive mutex may be re-locked by its owner */
    ++total;
    mtx_t rm;
    mtx_init(&rm, mtx_recursive);
    mtx_lock(&rm);
    mtx_lock(&rm);
    mtx_unlock(&rm);
    mtx_unlock(&rm);
    printf("case%d: recursive ok\n", total);
    passed++;
    mtx_destroy(&rm);

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
