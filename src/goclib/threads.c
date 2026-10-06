#include "goclib.h"
#include <threads.h>
#include <stdlib.h>
#include <string.h>
#ifndef __goc__
#include <stdatomic.h>   /* the spin lock's fallback (see gthr_spin_lock) */
#endif

/* =============================================================================
 * threads.c -- the C11/C23 threads library.
 *
 * Two implementations behind one header, selected by the target:
 *
 *   Windows  A thrd_t is a heap block wrapping a kernel32 thread HANDLE.
 *            "Which thread am I?" is answered by a private TlsAlloc slot,
 *            tss_t rides on further TlsAlloc indices, cnd_t is a Win32
 *            CONDITION_VARIABLE and mtx_t a CRITICAL_SECTION (see mtx.c --
 *            this file only calls mtx_lock/mtx_unlock).
 *
 *   Linux    clone() starts a thread. That one syscall is the whole reason
 *            this file contains assembly: raw clone resumes the child *at the
 *            instruction after the syscall* but on a brand-new stack, so the
 *            child has no return address and no frame -- it cannot fall out of
 *            the syscall stub the way a normal call would. goclib_clone() below
 *            therefore finishes the job by hand: the thread argument is planted
 *            at the word the new stack points at, and the child does
 *            `mov rdi, [rsp]; call __goc_thrd_body`. Blocking is futex-based
 *            (mtx.c and cnd_wait share that word), and termination is the
 *            CLONE_CHILD_CLEARTID contract -- the kernel zeroes the tid word
 *            and wakes anyone futex-waiting on it, which is what thrd_join
 *            waits for.
 *
 * mtx_* lives in mtx.c -- a separate translation unit, because goclib compiles
 * each .c on its own and the mutex needs the same per-target shape (a Win32
 * CRITICAL_SECTION vs a futex word) the rest of this file uses. This file only
 * calls mtx_lock/mtx_unlock/mtx_trylock where cnd_wait has to release and
 * reacquire one.
 *
 * Known limits, documented rather than silently wrong:
 *
 *   - On Linux a detached thread's stack is never unmapped: a thread cannot
 *     unmap the stack it is standing on, so only a joining thread can reclaim
 *     one. A joined thread's stack is munmap'd normally.
 *
 *   - On Linux _Thread_local variables are shared by every thread. goc points
 *     fs at the .tls block once, at process start; clone is called without
 *     CLONE_SETTLS, so a child inherits that fs and therefore the main
 *     thread's block instead of a copy of its own. Fixing it means giving each
 *     child its own block and an arch_prctl(ARCH_SET_FS) before it runs any C
 *     -- real work, and separate from starting the thread correctly.
 *
 *   - On Linux the heap is one brk bump allocator shared by every thread and
 *     its cursor is not yet updated atomically, so concurrent malloc/free is
 *     unsafe. Threads that do not allocate are unaffected, which is why the
 *     example sticks to printf.
 *
 *   - mtx_* lives in mtx.c (see the note above); cnd_wait calls it.
 * ========================================================================== */

/* -----------------------------------------------------------------------------
 * The one primitive both targets need: a test-and-set spinlock.
 *
 * goc has no <stdatomic.h> and goa emits no lock prefix, but `xchg` against a
 * memory operand is implicitly locked on x86-64, which is exactly the
 * test-and-set a single-word spinlock wants. This is the same trick goclib/rt.h
 * uses for its allocation registry. A spinlock is the right choice here and
 * not just the available one: every section it guards is a few instructions
 * long and never blocks, so a waiting thread never spins for more than the
 * time it takes the holder to finish.
 *
 * A zero-initialised gthr_spin_t is already unlocked, which matters more than
 * it looks -- library state has no initialiser running before main(), so any
 * lock that needed one would be a chicken-and-egg problem.
 * -------------------------------------------------------------------------- */

/* The lock word itself. It is a plain long on both hosts: goc's xchg acts on
 * one, and the host compiler's __atomic_* builtins (what atomic_exchange in
 * goclib/stdatomic.h lowers to) take a plain pointer too. So the two hosts see
 * the same object layout, and the spin lock means the same thing in both. */
typedef struct { long v; } gthr_spin_t;

/* Note on style: an inline __asm body is handed to goa almost verbatim, and
 * goa comments start with `;` -- a C comment inside the block would reach the
 * assembler as text. That is why the explanations stay out here.
 *
 * The two spin primitives below are the one part of this file that is not plain
 * C: an atomic test-and-set in hand-written assembly. Under a host compiler
 * the same operation is expressed with C11 <stdatomic.h>, which the compiler
 * lowers to the very same xchg -- so the spin lock keeps its meaning on both
 * hosts instead of being dropped from the build. */
#ifdef __goc__
static void gthr_spin_lock(gthr_spin_t *l) {
    __asm {
        mov rax, l
    .Lgthr_spin:
        mov rdx, 1
        xchg rdx, [rax]
        test rdx, rdx
        jz .Lgthr_got
        pause
        jmp .Lgthr_spin
    .Lgthr_got:
    }
}

static void gthr_spin_unlock(gthr_spin_t *l) {
    __asm {
        mov rax, l
        mov rdx, 0
        mov [rax], rdx
    }
}
#else /* !__goc__: same semantics, written with the __atomic_* builtins that
       * goclib/stdatomic.h maps atomic_exchange onto -- one xchg, as before. */
static void gthr_spin_lock(gthr_spin_t *l) {
    while (atomic_exchange(&l->v, 1) != 0) {
        /* the assembly path spins on `pause`; a plain load is the portable
         * equivalent of that hint */
    }
}

static void gthr_spin_unlock(gthr_spin_t *l) {
    atomic_store(&l->v, 0);
}
#endif /* __goc__ */

/* -----------------------------------------------------------------------------
 * Shared state
 * -------------------------------------------------------------------------- */

#define GTHR_TSS_MAX 32      /* distinct tss keys                            */
#define GTHR_MAX     64      /* live threads tracked for thrd_current        */

struct __goc_thrd {
    thrd_start_t fn;
    void        *arg;
    int          result;
    int          done;       /* the thread function has returned             */
    int          detached;   /* thrd_detach was called                       */
    long         tid;
    void        *handle;     /* Windows: thread HANDLE                       */
    void        *stack;      /* Linux: mmap base                             */
    long         stacksz;
    int          ctid;       /* Linux: CHILD_SETTID/CLEARTID word            */
    int          pad;
#if !defined(_WIN32)
    void        *tss[GTHR_TSS_MAX];
#endif
};

static gthr_spin_t g_reg_lock;     /* done/detached transitions, thread table */

/* call_once needs a lock of its own: it must be independent of mtx_t so that
 * the very first call_once can happen before any mutex exists. */
static gthr_spin_t g_once_lock;

/* tss destructors are shared by both targets even though the storage is not:
 * Windows keeps a TlsAlloc index next to each one (its values live in the OS),
 * Linux keeps only the destructor (its values live in the thread's block). */
static tss_dtor_t  g_tss_dtors[GTHR_TSS_MAX];
static int         g_tss_used[GTHR_TSS_MAX];
static gthr_spin_t g_tss_lock;

/* A thrd_t for a thread we did not start (the main one, say) is created on
 * demand by thrd_current(). It is never freed -- it is one block per such
 * thread and it has to outlive everything that might still name it. */
static struct __goc_thrd *gthr_new(void) {
    struct __goc_thrd *t = (struct __goc_thrd *)malloc(sizeof(struct __goc_thrd));
    if (t == 0) return 0;
    memset(t, 0, sizeof(struct __goc_thrd));
    return t;
}

/* -----------------------------------------------------------------------------
 * Threads: the parts that do not depend on the OS
 * -------------------------------------------------------------------------- */

int thrd_equal(thrd_t lhs, thrd_t rhs) {
    return lhs == rhs;
}

/* call_once: exactly one thread runs func, and nobody returns until it has.
 *
 * state 0 fresh, 1 running, 2 done. The lock is released around func() so a
 * slow initialisation does not block every other caller on a spinlock -- they
 * poll instead, yielding so the runner gets the CPU it needs.
 */
void call_once(once_flag *flag, void (*func)(void)) {
    if (flag == 0 || func == 0) return;
    gthr_spin_lock(&g_once_lock);
    for (;;) {
        if (flag->state == 2) break;          /* already done              */
        if (flag->state == 0) {
            flag->state = 1;                  /* we are the runner         */
            gthr_spin_unlock(&g_once_lock);
            func();
            gthr_spin_lock(&g_once_lock);
            flag->state = 2;
            break;
        }
        /* Someone else is running it; let them get on with it. */
        gthr_spin_unlock(&g_once_lock);
        thrd_yield();
        gthr_spin_lock(&g_once_lock);
    }
    gthr_spin_unlock(&g_once_lock);
}

#if defined(_WIN32)
/* =============================================================================
 * Windows
 * ========================================================================== */

/* "Which thrd_t is the current thread?" A single TlsAlloc slot holds the
 * answer; threads.c sets it when it starts a thread and thrd_current fills it
 * in for any thread it did not start. TlsAlloc itself needs calling once, and
 * a spinlock is enough to make that once -- which is why this does not have
 * to reach for InitOnceExecuteOnce and its callback. */
static unsigned int g_self_tls;
static int          g_self_tls_inited;
static gthr_spin_t  g_self_lock;

static unsigned int gthr_self_tls(void) {
    unsigned int k;
    gthr_spin_lock(&g_self_lock);
    if (!g_self_tls_inited) {
        g_self_tls = TlsAlloc();
        g_self_tls_inited = 1;
    }
    k = g_self_tls;
    gthr_spin_unlock(&g_self_lock);
    return k;
}

/* tss_create hands out a TlsAlloc index. Windows has no per-slot destructor,
 * so destructors are recorded alongside the index and run by hand when a
 * thread finishes -- see gthr_run_dtors. The destructor array itself is the
 * shared one above; only the index is Windows-specific. */
static unsigned int g_tss_keys[GTHR_TSS_MAX];

static void gthr_run_dtors(void) {
    int iter;
    for (iter = 0; iter < TSS_DTOR_ITERATIONS; iter++) {
        int ran = 0;
        for (;;) {
            unsigned int k = 0;
            tss_dtor_t dt = 0;
            void *v = 0;
            int i;
            gthr_spin_lock(&g_tss_lock);
            for (i = 0; i < GTHR_TSS_MAX; i++) {
                if (!g_tss_used[i] || g_tss_dtors[i] == 0) continue;
                v = TlsGetValue(g_tss_keys[i]);
                if (v == 0) continue;
                TlsSetValue(g_tss_keys[i], (void *)0);
                k = g_tss_keys[i];
                dt = g_tss_dtors[i];
                break;
            }
            gthr_spin_unlock(&g_tss_lock);
            if (dt == 0) break;
            ran = 1;
            dt(v);
        }
        if (!ran) break;
    }
}

static void thrd_start_routine_body(struct __goc_thrd *t);

static int thrd_start_routine(void *p) {
    struct __goc_thrd *t = (struct __goc_thrd *)p;
    unsigned int k = gthr_self_tls();
    if (k != TLS_OUT_OF_INDEXES) TlsSetValue(k, (void *)t);
    thrd_start_routine_body(t);
    return t->result;      /* not reached: the body exits the thread */
}

/* Everything that happens when a thread function returns, whether by falling
 * off the end or through thrd_exit. */
static void thrd_start_routine_body(struct __goc_thrd *t) {
    int d;
    int res;
    res = t->fn(t->arg);
    t->result = res;
    gthr_run_dtors();
    gthr_spin_lock(&g_reg_lock);
    t->done = 1;
    d = t->detached;
    gthr_spin_unlock(&g_reg_lock);
    if (d) {
        /* Detached: nobody will join, so this is the last reference. */
        if (t->handle != 0) CloseHandle(t->handle);
        free(t);
    }
}

int thrd_create(thrd_t *thr, thrd_start_t func, void *arg) {
    struct __goc_thrd *t;
    void *h;
    unsigned int id = 0;
    if (thr == 0 || func == 0) return thrd_error;
    t = gthr_new();
    if (t == 0) return thrd_nomem;
    t->fn = func;
    t->arg = arg;
    h = CreateThread((void *)0, 0, thrd_start_routine, (void *)t, 0, &id);
    if (h == 0) {
        free(t);
        return thrd_error;
    }
    t->handle = h;
    t->tid = (long)id;
    *thr = t;
    return thrd_success;
}

thrd_t thrd_current(void) {
    struct __goc_thrd *t;
    unsigned int k = gthr_self_tls();
    if (k == TLS_OUT_OF_INDEXES) return 0;
    t = (struct __goc_thrd *)TlsGetValue(k);
    if (t == 0) {
        t = gthr_new();
        if (t == 0) return 0;
        t->tid = (long)GetCurrentThreadId();
        TlsSetValue(k, (void *)t);
    }
    return t;
}

int thrd_detach(thrd_t thr) {
    if (thr == 0) return thrd_error;
    gthr_spin_lock(&g_reg_lock);
    if (thr->done) {
        /* Already finished while still joinable: the thread left the block
         * for us, so this call is what releases it. */
        gthr_spin_unlock(&g_reg_lock);
        if (thr->handle != 0) CloseHandle(thr->handle);
        free(thr);
        return thrd_success;
    }
    thr->detached = 1;
    gthr_spin_unlock(&g_reg_lock);
    return thrd_success;
}

int thrd_join(thrd_t thr, int *res) {
    if (thr == 0) return thrd_error;
    if (thr->detached || thr->handle == 0) return thrd_error;
    if (WaitForSingleObject(thr->handle, 0xFFFFFFFFUL) == WAIT_FAILED) return thrd_error;
    if (res != 0) *res = thr->result;
    CloseHandle(thr->handle);
    free(thr);
    return thrd_success;
}

void thrd_exit(int res) {
    struct __goc_thrd *t = thrd_current();
    if (t != 0) {
        int d;
        t->result = res;
        gthr_run_dtors();
        gthr_spin_lock(&g_reg_lock);
        t->done = 1;
        d = t->detached;
        gthr_spin_unlock(&g_reg_lock);
        if (d) {
            if (t->handle != 0) CloseHandle(t->handle);
            free(t);
        }
    }
    ExitThread((unsigned int)res);
}

void thrd_yield(void) {
    SwitchToThread();
}

int thrd_sleep(const struct timespec *duration, struct timespec *remaining) {
    unsigned long ms;
    if (duration == 0) return -1;
    /* Round up: sleeping 1ns must not turn into sleeping 0ms. */
    ms = (unsigned long)duration->tv_sec * 1000UL
       + ((unsigned long)duration->tv_nsec + 999999UL) / 1000000UL;
    Sleep(ms);
    if (remaining != 0) {
        remaining->tv_sec = 0;
        remaining->tv_nsec = 0;
    }
    return 0;
}

/* ---- condition variables ------------------------------------------------- */

int cnd_init(cnd_t *cond) {
    if (cond == 0) return thrd_error;
    InitializeConditionVariable((CONDITION_VARIABLE *)cond->cv);
    cond->inited = 1;
    return thrd_success;
}

void cnd_destroy(cnd_t *cond) {
    if (cond == 0) return;
    cond->inited = 0;
}

int cnd_signal(cnd_t *cond) {
    if (cond == 0) return thrd_error;
    WakeConditionVariable((CONDITION_VARIABLE *)cond->cv);
    return thrd_success;
}

int cnd_broadcast(cnd_t *cond) {
    if (cond == 0) return thrd_error;
    WakeAllConditionVariable((CONDITION_VARIABLE *)cond->cv);
    return thrd_success;
}

int cnd_wait(cnd_t *cond, mtx_t *mtx) {
    if (cond == 0 || mtx == 0) return thrd_error;
    SleepConditionVariableCS((CONDITION_VARIABLE *)cond->cv,
                             (CRITICAL_SECTION *)mtx->cs, 0xFFFFFFFFUL);
    return thrd_success;
}

int cnd_timedwait(cnd_t *cond, mtx_t *mtx, const struct timespec *ts) {
    struct timespec now;
    long long ms;
    int r;
    if (cond == 0 || mtx == 0 || ts == 0) return thrd_error;
    timespec_get(&now, 1);
    ms = ((long long)ts->tv_sec - (long long)now.tv_sec) * 1000LL
       + ((long long)ts->tv_nsec - (long long)now.tv_nsec) / 1000000LL;
    if (ms < 0) {
        /* Already past the deadline: still take and release the mutex so the
         * caller's lock state is unchanged either way. */
        ms = 0;
    }
    r = SleepConditionVariableCS((CONDITION_VARIABLE *)cond->cv,
                                 (CRITICAL_SECTION *)mtx->cs,
                                 (unsigned int)ms);
    if (r == 0 && GetLastError() == 1460) return thrd_timedout;   /* ERROR_TIMEOUT */
    return thrd_success;
}

/* ---- thread-specific storage --------------------------------------------- */

int tss_create(tss_t *key, tss_dtor_t dtor) {
    unsigned int k;
    int i;
    int slot = -1;
    if (key == 0) return thrd_error;
    k = TlsAlloc();
    if (k == TLS_OUT_OF_INDEXES) return thrd_error;
    gthr_spin_lock(&g_tss_lock);
    for (i = 0; i < GTHR_TSS_MAX; i++) {
        if (!g_tss_used[i]) {
            g_tss_used[i] = 1;
            g_tss_keys[i] = k;
            g_tss_dtors[i] = dtor;
            slot = i;
            break;
        }
    }
    gthr_spin_unlock(&g_tss_lock);
    if (slot < 0) {
        TlsFree(k);
        return thrd_error;
    }
    key->key = k;
    key->inited = 1;
    return thrd_success;
}

void tss_delete(tss_t key) {
    int i;
    gthr_spin_lock(&g_tss_lock);
    for (i = 0; i < GTHR_TSS_MAX; i++) {
        if (g_tss_used[i] && g_tss_keys[i] == key.key) {
            g_tss_used[i] = 0;
            g_tss_keys[i] = 0;
            g_tss_dtors[i] = 0;
            break;
        }
    }
    gthr_spin_unlock(&g_tss_lock);
    if (key.key != TLS_OUT_OF_INDEXES) TlsFree(key.key);
}

void *tss_get(tss_t key) {
    if (key.key == TLS_OUT_OF_INDEXES) return 0;
    return TlsGetValue(key.key);
}

int tss_set(tss_t key, void *val) {
    if (key.key == TLS_OUT_OF_INDEXES) return thrd_error;
    if (TlsSetValue(key.key, val)) return thrd_success;
    return thrd_error;
}

#elif defined(__linux__)
/* =============================================================================
 * Linux
 * ========================================================================== */

/* The raw system calls: goa's stubs under goc, libc's functions (and, for
 * clone/futex, syscall(2)) under a host compiler. <syscall.h> carries both. */
#include <syscall.h>

#define FUTEX_WAIT 0
#define FUTEX_WAKE 1

#define GTHR_STACK (1L << 20)      /* 1 MiB per thread                        */

#define PROT_READ     1
#define PROT_WRITE    2
#define MAP_PRIVATE   0x02
#define MAP_ANONYMOUS 0x20
#define MAP_STACK     0x20000

/* clone flags. CLONE_THREAD makes the new task a thread of this process
 * rather than a child of it, which is what makes one exit_group() at the end
 * of main take the lot with it. CLONE_CHILD_SETTID publishes the tid and
 * CLONE_CHILD_CLEARTID takes it away again -- and wakes anyone futex-waiting
 * on that word -- when the thread dies however it dies, which is the whole of
 * thrd_join. */
#define CLONE_VM              0x00000100
#define CLONE_FS              0x00000200
#define CLONE_FILES           0x00000400
#define CLONE_SIGHAND         0x00000800
#define CLONE_THREAD          0x00010000
#define CLONE_SYSVSEM         0x00040000
#define CLONE_PARENT_SETTID   0x00100000
#define CLONE_CHILD_CLEARTID  0x00200000
#define CLONE_CHILD_SETTID    0x01000000

#define GTHR_CLONE_FLAGS (CLONE_VM | CLONE_FS | CLONE_FILES | CLONE_SIGHAND | \
                          CLONE_THREAD | CLONE_SYSVSEM | CLONE_CHILD_SETTID | \
                          CLONE_CHILD_CLEARTID)

static struct __goc_thrd *g_slots[GTHR_MAX];

static void __goc_thrd_body(struct __goc_thrd *t);

/* Start one thread.
 *
 * The child resumes at the instruction after `syscall` with rsp = stack and
 * every other register inherited, so the whole hand-off is done here rather
 * than in the syscall stub: `t` is planted at the word the new stack points
 * to, and the child loads it into rdi (the SysV first argument) and calls the
 * body. The parent skips that with a jnz on clone's return value -- 0 means
 * "you are the child", a positive value "you are the parent and this is the
 * new tid".
 *
 * Only scratch registers are touched, and nothing after the syscall reads
 * memory the parent might be writing: the child is on its own stack from the
 * moment clone returns.
 */
#ifdef __goc__
static long gthr_clone(void *stack, int *ctid) {
    long r;
    /* Held in a variable rather than written as GTHR_CLONE_FLAGS inside the
     * block: an inline __asm body is captured as source text, and a
     * parenthesised macro would be a text substitution the assembler then has
     * to parse. A variable binds to its stack slot like any other operand. */
    long flags = GTHR_CLONE_FLAGS;
    __asm {
        mov rax, 56
        mov rdi, flags
        mov rsi, stack
        mov rdx, 0
        mov r10, ctid
        mov r8, 0
        syscall
        test rax, rax
        jnz .Lgthr_parent
        mov rdi, [rsp]
        mov rax, [rsp+8]
        call rax
        mov rdi, rax
        mov rax, 60
        syscall
    .Lgthr_parent:
        mov r, rax
    }
    return r;
}
#else /* !__goc__ */
/* The same clone(2), reached through the host's syscall() rather than goa's
 * hand-written stub. The child half of the contract is identical: thrd_create
 * plants the thread record at sp[0] and the thread body at sp[1], and the
 * assembly path reads them in exactly that order (`mov rdi,[rsp]`,
 * `mov rax,[rsp+8]`). Reaching clone() with the same argument order goa's stub
 * uses keeps the thread model the rest of this file is written against --
 * detached by flag, no TLS bookkeeping, tid == the kernel's own pid -- so
 * nothing below has to know which of the two paths built the thread. */
static long gthr_clone(void *stack, int *ctid) {
    long r = __goclib_clone(GTHR_CLONE_FLAGS, stack, 0, ctid, 0);
    if (r != 0) return r;              /* parent: the new tid, or -1   */
    /* Child. The new stack is already live, and its bottom two words are the
     * argument and the entry point, planted by thrd_create before the clone. */
    __goc_thrd_body(((void **)stack)[0]);
    _exit(0);
    return 0;
}
#endif /* __goc__ */

/* Register a thread so thrd_current() can find it by tid. Threads started by
 * thrd_create are registered up front; anything else gets an entry on first
 * ask. */
static int gthr_register(struct __goc_thrd *t) {
    int i;
    int ok = 0;
    gthr_spin_lock(&g_reg_lock);
    for (i = 0; i < GTHR_MAX; i++) {
        if (g_slots[i] == 0) {
            g_slots[i] = t;
            ok = 1;
            break;
        }
    }
    gthr_spin_unlock(&g_reg_lock);
    return ok;
}

static void gthr_unregister(struct __goc_thrd *t) {
    int i;
    gthr_spin_lock(&g_reg_lock);
    for (i = 0; i < GTHR_MAX; i++) {
        if (g_slots[i] == t) {
            g_slots[i] = 0;
            break;
        }
    }
    gthr_spin_unlock(&g_reg_lock);
}

/* Pull `t` out of the table and mark it finished, releasing whatever the
 * detach/join protocol says is ours to release. Never returns. */
static void gthr_thread_end(struct __goc_thrd *t, int res) {
    int d;
    int i;
    void *stack;
    long stacksz;

    t->result = res;

    /* Run destructors, newest key first is not required; the standard only
     * asks that they are attempted, up to TSS_DTOR_ITERATIONS times. */
    for (i = 0; i < TSS_DTOR_ITERATIONS; i++) {
        int ran = 0;
        int k;
        for (k = 0; k < GTHR_TSS_MAX; k++) {
            if (t->tss[k] != 0 && g_tss_dtors[k] != 0) {
                void *v = t->tss[k];
                tss_dtor_t dt = g_tss_dtors[k];
                t->tss[k] = 0;
                ran = 1;
                dt(v);
            }
        }
        if (!ran) break;
    }

    gthr_unregister(t);
    gthr_spin_lock(&g_reg_lock);
    t->done = 1;
    d = t->detached;
    gthr_spin_unlock(&g_reg_lock);

    /* Publish the exit before leaving: the tid word is what thrd_join waits
     * on. The kernel does this too via CLEARTID; doing it here as well means
     * join never depends on which of the two gets there first. */
    t->ctid = 0;
    __goclib_futex(&t->ctid, FUTEX_WAKE, 0x7fffffffL, (void *)0, (int *)0, 0);

    stack = t->stack;
    stacksz = t->stacksz;
    if (d) {
        /* Detached: this is the last reference. The stack cannot be unmapped
         * from the thread running on it, so it stays mapped until exit. */
        free(t);
    }
    __goclib_exit_thread((long)res);
}

static void __goc_thrd_body(struct __goc_thrd *t) {
    int r = t->fn(t->arg);
    gthr_thread_end(t, r);
}

int thrd_create(thrd_t *thr, thrd_start_t func, void *arg) {
    struct __goc_thrd *t;
    void *base;
    char *sp;
    long r;
    if (thr == 0 || func == 0) return thrd_error;
    t = gthr_new();
    if (t == 0) return thrd_nomem;
    base = mmap((void *)0, GTHR_STACK, PROT_READ | PROT_WRITE,
                MAP_PRIVATE | MAP_ANONYMOUS | MAP_STACK, -1, 0);
    /* A raw Linux syscall reports failure as a *negative errno* (-EBADF,
     * -ENOMEM, ...), not as the -1 a libc wrapper would hand back, and there
     * is no libc here to do that translation. Testing only 0 and -1 lets a
     * real failure through as a plausible pointer, and the next thing this
     * function does with it is a store near address -9. Every error value a
     * Linux mmap can return is negative, so one signed test covers them. */
    if ((long)base < 0) {
        free(t);
        return thrd_nomem;
    }
    t->fn = func;
    t->arg = arg;
    t->stack = base;
    t->stacksz = GTHR_STACK;
    t->ctid = 0;                       /* CLONE_CHILD_SETTID fills this in  */

    /* Top of the new stack, 16-aligned, then two words down: the child's rsp
     * lands on the first and finds `t` in it, with the entry point beside it.
     * The entry goes through a pointer rather than a `call __goc_thrd_body`
     * in the assembly because the body is reached from nowhere else -- code
     * generation cannot see into an __asm block, so a body named only there
     * is invisible to it and gets pruned, leaving an undefined symbol. Taking
     * the address here in ordinary C is what makes it a real reference. */
    sp = (char *)(((unsigned long)base + (unsigned long)GTHR_STACK) & ~15UL);
    sp -= 16;
    *((void **)sp) = (void *)t;
    *((void **)(sp + 8)) = (void *)__goc_thrd_body;

    if (!gthr_register(t)) {
        munmap(base, GTHR_STACK);
        free(t);
        return thrd_nomem;
    }
    r = gthr_clone((void *)sp, &t->ctid);
    if (r < 0) {
        gthr_unregister(t);
        munmap(base, GTHR_STACK);
        free(t);
        return thrd_error;
    }
    t->tid = r;
    *thr = t;
    return thrd_success;
}

thrd_t thrd_current(void) {
    long tid;
    struct __goc_thrd *t = 0;
    int i;
    tid = __goclib_gettid();
    gthr_spin_lock(&g_reg_lock);
    for (i = 0; i < GTHR_MAX; i++) {
        if (g_slots[i] != 0 && g_slots[i]->tid == tid) {
            t = g_slots[i];
            break;
        }
    }
    gthr_spin_unlock(&g_reg_lock);
    if (t != 0) return t;
    /* A thread we did not start. Give it a block so tss_ and thrd_exit work
     * for it too. */
    t = gthr_new();
    if (t == 0) return 0;
    t->tid = tid;
    if (!gthr_register(t)) {
        free(t);
        return 0;
    }
    return t;
}

int thrd_detach(thrd_t thr) {
    if (thr == 0) return thrd_error;
    gthr_spin_lock(&g_reg_lock);
    if (thr->done) {
        gthr_spin_unlock(&g_reg_lock);
        if (thr->stack != 0) munmap(thr->stack, thr->stacksz);
        free(thr);
        return thrd_success;
    }
    thr->detached = 1;
    gthr_spin_unlock(&g_reg_lock);
    return thrd_success;
}

int thrd_join(thrd_t thr, int *res) {
    long v;
    if (thr == 0 || thr->detached) return thrd_error;
    /* Wait for the tid word to be cleared. Any value still there means the
     * thread is alive; futex returns at once when the word has already
     * changed, so a thread that finished before we got here costs no wait. */
    for (;;) {
        if (thr->done) break;
        v = (long)thr->ctid;
        if (v == 0) {
            /* CLONE_CHILD_SETTID has not published the tid yet, so there is
             * nothing to wait on. Yield until it does -- the thread cannot
             * finish without having been given a tid. */
            __goclib_sched_yield();
            continue;
        }
        __goclib_futex(&thr->ctid, FUTEX_WAIT, v, (void *)0, (int *)0, 0);
    }
    if (res != 0) *res = thr->result;
    if (thr->stack != 0) munmap(thr->stack, thr->stacksz);
    free(thr);
    return thrd_success;
}

void thrd_exit(int res) {
    struct __goc_thrd *t = thrd_current();
    if (t != 0) gthr_thread_end(t, res);
    __goclib_exit_thread((long)res);
}

void thrd_yield(void) {
    __goclib_sched_yield();
}

int thrd_sleep(const struct timespec *duration, struct timespec *remaining) {
    long r;
    if (duration == 0) return -1;
    r = nanosleep(duration, remaining);
    if (r == 0) return 0;
    return -1;
}

/* ---- condition variables ------------------------------------------------- */
/* A generation counter on a futex. cnd_wait samples the counter, releases the
 * mutex and waits for the counter to change; a signal that arrives between the
 * sample and the wait still shows up as a changed word, which makes the wait
 * return immediately instead of hanging -- that is the whole reason this is a
 * counter and not a boolean. */

int cnd_init(cnd_t *cond) {
    if (cond == 0) return thrd_error;
    cond->seq = 0;
    cond->inited = 1;
    return thrd_success;
}

void cnd_destroy(cnd_t *cond) {
    if (cond == 0) return;
    cond->inited = 0;
}

int cnd_signal(cnd_t *cond) {
    if (cond == 0) return thrd_error;
    cond->seq = cond->seq + 1;
    __goclib_futex(&cond->seq, FUTEX_WAKE, 1, (void *)0, (int *)0, 0);
    return thrd_success;
}

int cnd_broadcast(cnd_t *cond) {
    if (cond == 0) return thrd_error;
    cond->seq = cond->seq + 1;
    __goclib_futex(&cond->seq, FUTEX_WAKE, 0x7fffffffL, (void *)0, (int *)0, 0);
    return thrd_success;
}

int cnd_wait(cnd_t *cond, mtx_t *mtx) {
    int s;
    if (cond == 0 || mtx == 0) return thrd_error;
    s = cond->seq;
    mtx_unlock(mtx);
    __goclib_futex(&cond->seq, FUTEX_WAIT, s, (void *)0, (int *)0, 0);
    mtx_lock(mtx);
    return thrd_success;
}

/* Deadline minus now, as a relative timespec -- futex takes a relative one
 * while cnd_timedwait is given an absolute TIME_UTC deadline. */
static void gthr_ts_rel(const struct timespec *abs_ts, struct timespec *out) {
    struct timespec now;
    long long dsec;
    long long dnsec;
    timespec_get(&now, 1);
    dsec = (long long)abs_ts->tv_sec - (long long)now.tv_sec;
    dnsec = (long long)abs_ts->tv_nsec - (long long)now.tv_nsec;
    if (dnsec < 0) {
        dnsec += 1000000000LL;
        dsec -= 1;
    }
    if (dsec < 0) {
        out->tv_sec = 0;
        out->tv_nsec = 0;
        return;
    }
    out->tv_sec = (long)dsec;
    out->tv_nsec = (long)dnsec;
}

int cnd_timedwait(cnd_t *cond, mtx_t *mtx, const struct timespec *ts) {
    struct timespec rel;
    long r;
    int s;
    if (cond == 0 || mtx == 0 || ts == 0) return thrd_error;
    gthr_ts_rel(ts, &rel);
    s = cond->seq;
    mtx_unlock(mtx);
    r = __goclib_futex(&cond->seq, FUTEX_WAIT, s, (void *)&rel, (int *)0, 0);
    mtx_lock(mtx);
    if (r == -110) return thrd_timedout;      /* -ETIMEDOUT */
    return thrd_success;
}

/* ---- thread-specific storage --------------------------------------------- */
/* No TlsAlloc equivalent here, so the values live in the thread's own block
 * and the shared key array says which destructor goes with which. */

int tss_create(tss_t *key, tss_dtor_t dtor) {
    int i;
    int slot = -1;
    if (key == 0) return thrd_error;
    gthr_spin_lock(&g_tss_lock);
    for (i = 0; i < GTHR_TSS_MAX; i++) {
        if (!g_tss_used[i]) {
            g_tss_used[i] = 1;
            g_tss_dtors[i] = dtor;
            slot = i;
            break;
        }
    }
    gthr_spin_unlock(&g_tss_lock);
    if (slot < 0) return thrd_error;
    key->key = slot;
    key->inited = 1;
    return thrd_success;
}

void tss_delete(tss_t key) {
    if (key.key < 0 || key.key >= GTHR_TSS_MAX) return;
    gthr_spin_lock(&g_tss_lock);
    g_tss_used[key.key] = 0;
    g_tss_dtors[key.key] = 0;
    gthr_spin_unlock(&g_tss_lock);
}

void *tss_get(tss_t key) {
    struct __goc_thrd *t;
    if (key.key < 0 || key.key >= GTHR_TSS_MAX) return 0;
    t = thrd_current();
    if (t == 0) return 0;
    return t->tss[key.key];
}

int tss_set(tss_t key, void *val) {
    struct __goc_thrd *t;
    if (key.key < 0 || key.key >= GTHR_TSS_MAX) return thrd_error;
    t = thrd_current();
    if (t == 0) return thrd_error;
    t->tss[key.key] = val;
    return thrd_success;
}

#endif /* __linux__ */
