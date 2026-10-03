#ifndef GOC_RT_H
#define GOC_RT_H

/* =============================================================================
 * rt.h -- compile-time optional diagnostic runtime (goc -rt / -rtdiag).
 *
 * Two layers, with deliberately different visibility:
 *
 * 1. The public heap-usage API (goc_rt_stats_t + runtime_stats/print/dump/
 *    scan) is declared UNCONDITIONALLY, and rt.c always defines it -- with
 *    -rt the real tracker backs it, without -rt it compiles to no-op stubs.
 *    So the same source builds and runs either way: with -rt you get leak
 *    reports, redzone OOB detection and a crash dump; without it the calls
 *    quietly do nothing and the program behaves like plain C. No #ifdef is
 *    needed at the call site.
 *
 * 2. The internals (spinlock, tracker entry points called by the instrumented
 *    stdlib.c) live under #ifdef GOC_RTDIAG and vanish without -rt, so a
 *    normal build pays nothing: rt.c compiles to almost nothing and
 *    malloc/free stay the plain __goclib_heap_* wrappers.
 * ========================================================================== */

/* ---- public heap-usage API (always available) --------------------------
 * Plain standard-C functions, no language extension: goc calls them by name.
 * With -rt they report real data; without it they are silent no-ops. */
typedef struct goc_rt_stats {
    long total_alloc;    /* cumulative tracked allocations */
    long total_free;     /* cumulative frees */
    long live_blocks;    /* blocks currently live */
    long live_bytes;     /* sum of live payload bytes (excludes redzones) */
    long peak_blocks;    /* high-water mark of live_blocks */
    long peak_bytes;     /* high-water mark of live_bytes */
    long total_realloc;  /* cumulative reallocs */
    long missed;         /* allocations not tracked (pool exhausted) */
    long pool_used;      /* tracker nodes in use (== live_blocks) */
    long pool_total;     /* tracker node capacity */
    long oob_blocks;     /* corrupted blocks found by the last runtime_scan() */
} goc_rt_stats_t;

void  runtime_stats(goc_rt_stats_t *out);   /* snapshot into *out */
void  runtime_print(void);                   /* one-line usage summary */
void  runtime_dump(void);                    /* summary + every live block */
long  runtime_scan(void);                    /* scan redzones; return #corrupt */

#ifdef GOC_RTDIAG

/* ---- L0: xchg-based spinlock -------------------------------------------
 * goc has no <stdatomic.h> and goa emits no lock prefix, but `xchg` with a
 * memory operand is implicitly locked on x86-64, which is exactly the
 * test-and-set we need for a single-word spinlock. `l` is a pointer parameter;
 * inside __asm it binds to its stack slot ([rbp+off]), so `mov rax, l` loads
 * the pointer and `xchg rdx, [rax]` exchanges with the word it points at. */
typedef struct { long v; } goc_spin_t;

static inline void goc_spin_lock(goc_spin_t *l) {
    __asm {
        mov rax, l
    .Lspin:
        mov rdx, 1
        xchg rdx, [rax]
        test rdx, rdx
        jz .Lgot
        pause
        jmp .Lspin
    .Lgot:
    }
}

static inline void goc_spin_unlock(goc_spin_t *l) {
    __asm {
        mov rax, l
        mov rdx, 0
        mov [rax], rdx
    }
}

/* ---- L1/L2 internals, called by the instrumented stdlib.c -------------
 * Each block is surrounded by redzones (see rt.c GOC_RT_REDZONE); any write
 * into them is reported at free / runtime_scan() / exit / crash time. */
void __goc_rt_init(void);
void __goc_rt_report(void);
void *__goc_rt_malloc(long size);
void *__goc_rt_calloc(long n, long size);
void  __goc_rt_free(void *user);
void *__goc_rt_realloc(void *user, long newsize);

#endif /* GOC_RTDIAG */
#endif /* GOC_RT_H */
