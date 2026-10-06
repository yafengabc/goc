/* ============================================================
   c11_threads_basic.c - <threads.h>: create/join, call_once, tss
   Standard   : ISO/IEC 9899:2011 (C11) 7.26
   Strategy   : start four threads, each of which joins the same once_flag,
                yields, writes its own slot in results[] and returns a
                distinct code; join them all in order and print. Every value
                printed is therefore fixed by the join, not by scheduling,
                which is what makes the output deterministic.
                Used to be Windows-only in the regression suite (see winOnly
                in tools/gocregress): its Linux leg runs under ucrun, which
                has no clone/futex. With the Linux syscall ABI fix (arg4 in
                r10, not rcx) this now passes on a REAL Linux kernel -- verified
                under WSL. It stays in winOnly only because the bundled ucrun
                Linux leg still cannot run clone/futex; a WSL-based Linux leg
                would exercise it.
   Status     : PASS (goc, Windows); PASS (goc -target linux, real kernel / WSL)
   ============================================================ */
#include <stdio.h>
#include <threads.h>

static int results[4];
static once_flag once = ONCE_FLAG_INIT;
static int once_runs;

static void once_fn(void) {
    once_runs = once_runs + 1;
}

static int worker(void *arg) {
    long i = (long)arg;
    call_once(&once, once_fn);
    thrd_yield();
    results[i] = (int)(i * 10 + 1);
    return (int)(100 + i);
}

int main(void) {
    thrd_t t[4];
    tss_t key;
    int rc[4];
    int i;

    for (i = 0; i < 4; i++) {
        if (thrd_create(&t[i], worker, (void *)(long)i) != thrd_success) {
            printf("create %d FAILED\n", i);
            return 1;
        }
    }
    for (i = 0; i < 4; i++) {
        rc[i] = -1;
        if (thrd_join(t[i], &rc[i]) != thrd_success) {
            printf("join %d FAILED\n", i);
            return 1;
        }
    }
    for (i = 0; i < 4; i++) {
        printf("thread %d: rc=%d val=%d\n", i, rc[i], results[i]);
    }
    printf("once_runs=%d\n", once_runs);

    if (tss_create(&key, 0) != thrd_success) {
        printf("tss_create FAILED\n");
        return 1;
    }
    tss_set(key, (void *)(long)12345);
    printf("tss=%d\n", (int)(long)tss_get(key));
    tss_delete(key);

    printf("self=%d\n", thrd_equal(thrd_current(), thrd_current()));
    return 0;
}
