#include "goclib.h"
#include "rt.h"

/* =============================================================================
 * rt.c -- the diagnostic runtime (goc -rtdiag).
 *
 * A thread-safe registry of live heap blocks, guarded by an xchg spinlock
 * (L0). malloc/calloc/realloc/free in stdlib.c route through __goc_rt_*
 * so every live block is on an intrusive list (L1). Each block is wrapped in
 * redzones (L2): any write past its boundaries corrupts a magic pattern that
 * is checked at free / runtime_scan() / exit / crash, reporting the offending
 * block, direction (under/over-flow) and overrun size. On Windows a vectored
 * exception handler (L3) dumps the same list straight to stderr on a crash,
 * before the OS terminates the process.
 *
 * The node pool is a fixed static array, never obtained through malloc, so the
 * tracker cannot recurse into itself. When the pool is exhausted tracking is
 * best-effort (the allocation still happens via the underlying primitive).
 * ========================================================================== */

#ifdef GOC_RTDIAG

#define GOC_RT_POOL 16384

/* Redzone size (bytes) before and after each user payload. Filled with a
 * magic pattern; any modification seen at free/scan/crash time means the
 * program wrote outside its allocation. Kept small (16) so the overhead is
 * bounded; raise it for finer underflow/overflow byte counts. */
#define GOC_RT_REDZONE 16
#define GOC_RT_RED_MAGIC 0xBD

typedef struct goc_rt_node {
    void *ptr;     /* user pointer returned to the caller (real + REDZONE) */
    void *real;    /* base returned by the underlying heap primitive */
    long size;     /* user payload size in bytes */
    long seq;
    struct goc_rt_node *next;
} goc_rt_node_t;

static goc_rt_node_t g_pool[GOC_RT_POOL];
static goc_rt_node_t *g_free;   /* free-list head */
static goc_rt_node_t *g_live;   /* live allocations, most-recent first */
static goc_spin_t g_lock = {0};
static long g_inited;
static long g_seq;
static long g_total_alloc, g_total_free, g_cur_live, g_peak_live, g_missed, g_total_realloc;
static long g_live_bytes, g_peak_bytes, g_last_oob;

/* ---- raw stderr output (safe inside a crash handler) -------------------- */
#ifdef _WIN32
extern void *GetStdHandle(long which);
extern long  WriteFile(void *h, const void *buf, long n, long *written, long overlapped);
typedef long (*goc_rt_veh_t)(void *);
extern void *AddVectoredExceptionHandler(long first, goc_rt_veh_t handler), kernel32;

static void rt_emit(const char *s) {
    const char *p = s;
    long n = 0;
    void *h;
    long w = 0;
    while (*p) { p++; n++; }
    if (n == 0) return;
    h = GetStdHandle(-12); /* STD_ERROR_HANDLE */
    if (h) WriteFile(h, s, n, &w, 0);
}
#else
#include <syscall.h>   /* write(2), reached per host */
static void rt_emit(const char *s) {
    const char *p = s;
    long n = 0;
    while (*p) { p++; n++; }
    if (n > 0) write(2, s, n);
}
#endif

/* decimal long -> buf, NUL-terminated (buf must hold >= 24 bytes). */
static void rt_decbuf(long v, char *buf, int *len) {
    char tmp[24];
    int i = 0, j;
    unsigned long u;
    int neg = 0;
    if (v < 0) { neg = 1; u = (unsigned long)(-(v + 1)) + 1; }
    else u = (unsigned long)v;
    if (u == 0) tmp[i++] = '0';
    while (u > 0) { tmp[i++] = (char)('0' + (int)(u % 10)); u /= 10; }
    if (neg) tmp[i++] = '-';
    for (j = 0; j < i; j++) buf[j] = tmp[i - 1 - j];
    buf[i] = '\0';
    *len = i;
}

/* 64-bit pointer -> 16 hex chars in buf, NUL-terminated (buf >= 17 bytes). */
static void rt_hexbuf(unsigned long v, char *buf) {
    const char *hx = "0123456789abcdef";
    int i;
    for (i = 15; i >= 0; i--) { buf[i] = hx[(int)(v & 0xf)]; v >>= 4; }
    buf[16] = '\0';
}

/* ---- node pool (caller holds g_lock) ------------------------------------ */
static goc_rt_node_t *pop_free(void) {
    goc_rt_node_t *n = g_free;
    if (n) g_free = n->next;
    return n;
}
static void push_free(goc_rt_node_t *n) {
    n->next = g_free;
    g_free = n;
}

/* ---- init (idempotent; builds the pool, installs VEH on Windows) ------- */
#ifdef _WIN32
static long goc_rt_veh(void *ep);
#endif

static void goc_rt_init_locked(void) {
    long i;
    if (g_inited) return;
    g_free = 0;
    for (i = 0; i < GOC_RT_POOL; i++) {
        g_pool[i].next = g_free;
        g_free = &g_pool[i];
    }
    g_inited = 1;
#ifdef _WIN32
    AddVectoredExceptionHandler(1, goc_rt_veh);
#endif
}

void __goc_rt_init(void) {
    goc_spin_lock(&g_lock);
    if (g_inited) { goc_spin_unlock(&g_lock); return; }
    goc_rt_init_locked();
    goc_spin_unlock(&g_lock);
}

/* ---- redzone poison / check -------------------------------------------- */
static void rt_poison(void *real, void *user, long size) {
    unsigned char *f = (unsigned char *)real;
    unsigned char *b = (unsigned char *)user + size;
    long i;
    for (i = 0; i < GOC_RT_REDZONE; i++) {
        f[i] = (unsigned char)GOC_RT_RED_MAGIC;
        b[i] = (unsigned char)GOC_RT_RED_MAGIC;
    }
}

/* Returns 0 if intact; otherwise a bitmask (1=underflow, 2=overflow, 3=both)
 * and, in *off, the overrun magnitude in bytes (negative = before start,
 * positive = past end). */
static int rt_check_redzones(void *real, void *user, long size, long *off) {
    unsigned char *f = (unsigned char *)real;
    unsigned char *b = (unsigned char *)user + size;
    long i, under = 0, over = 0;
    int bad = 0;
    for (i = 0; i < GOC_RT_REDZONE; i++) {
        if (f[i] != (unsigned char)GOC_RT_RED_MAGIC) { if (under == 0) under = GOC_RT_REDZONE - i; bad |= 1; }
    }
    for (i = 0; i < GOC_RT_REDZONE; i++) {
        if (b[i] != (unsigned char)GOC_RT_RED_MAGIC) { if (over == 0) over = i + 1; bad |= 2; }
    }
    *off = (bad & 1) ? -under : over;
    return bad;
}

/* ---- tracking (caller decides locking; insert/update/remove nodes) ------ */
static void rt_track_insert(void *real, void *user, long size) {
    goc_rt_node_t *n;
    if (!g_inited) __goc_rt_init();
    goc_spin_lock(&g_lock);
    n = pop_free();
    if (n) {
        n->real = real;
        n->ptr = user;
        n->size = size;
        n->seq = ++g_seq;
        n->next = g_live;
        g_live = n;
        g_total_alloc++;
        g_cur_live++;
        g_live_bytes += size;
        if (g_cur_live > g_peak_live) g_peak_live = g_cur_live;
        if (g_live_bytes > g_peak_bytes) g_peak_bytes = g_live_bytes;
    } else {
        g_missed++;
    }
    goc_spin_unlock(&g_lock);
}

/* find the live node for `user`; check its redzones (report OOB), remove it
 * and return its real base. Returns 0 if not found (double/invalid free). */
static void *rt_track_remove(void *user) {
    goc_rt_node_t *prev, *cur;
    void *real = 0;
    if (user == 0) return 0;
    goc_spin_lock(&g_lock);
    prev = 0;
    cur = g_live;
    while (cur) {
        if (cur->ptr == user) {
            long off = 0;
            int bad = rt_check_redzones(cur->real, cur->ptr, cur->size, &off);
            if (bad) rt_emit_oob(cur, bad, off);
            real = cur->real;
            if (prev) prev->next = cur->next;
            else g_live = cur->next;
            push_free(cur);
            g_total_free++;
            g_cur_live--;
            g_live_bytes -= cur->size;
            goc_spin_unlock(&g_lock);
            return real;
        }
        prev = cur;
        cur = cur->next;
    }
    goc_spin_unlock(&g_lock);
    return 0;
}

/* ---- OOB report (crash-safe emit) -------------------------------------- */
static void rt_emit_oob(goc_rt_node_t *n, int bad, long off) {
    char line[64];
    int L;
    rt_emit("[goc-rt] *** heap buffer ");
    rt_emit((bad == 1) ? "UNDERFLOW" : (bad == 2) ? "OVERFLOW"
                                                      : "UNDER/OVER-FLOW");
    rt_emit(" in block #");
    rt_decbuf(n->seq, line, &L); rt_emit(line);
    rt_emit(" ptr=0x");
    rt_hexbuf((unsigned long)(unsigned long long)n->ptr, line); rt_emit(line);
    rt_emit(" size=");
    rt_decbuf(n->size, line, &L); rt_emit(line);
    rt_emit(" (");
    if (off < 0) {
        rt_emit("-"); rt_decbuf(-off, line, &L); rt_emit(line);
        rt_emit(" bytes before start)");
    } else {
        rt_decbuf(off, line, &L); rt_emit(line);
        rt_emit(" bytes past end)");
    }
    rt_emit("\n");
}

/* ---- high-level allocation wrappers (own the redzones + tracking) ------ */
void *__goc_rt_malloc(long size) {
    void *real, *user;
    if (size <= 0) size = 1;
    real = __goclib_heap_alloc(size + 2 * GOC_RT_REDZONE);
    if (real == 0) return 0;
    user = (void *)((unsigned char *)real + GOC_RT_REDZONE);
    rt_poison(real, user, size);
    rt_track_insert(real, user, size);
    return user;
}

void *__goc_rt_calloc(long n, long size) {
    void *p = __goc_rt_malloc(n * size);
    if (p) memset(p, 0, (unsigned long)(n * size));
    return p;
}

void __goc_rt_free(void *user) {
    void *real;
    if (user == 0) return;
    real = rt_track_remove(user);
    if (real == 0) {
        char line[64];
        int L;
        rt_emit("[goc-rt] *** invalid or duplicate free at ptr=0x");
        rt_hexbuf((unsigned long)(unsigned long long)user, line); rt_emit(line);
        rt_emit("\n");
        return;
    }
    __goclib_heap_free(real);
}

void *__goc_rt_realloc(void *user, long newsize) {
    goc_rt_node_t *cur;
    void *newreal, *newuser;
    int found = 0;
    if (user == 0) return __goc_rt_malloc(newsize);
    if (newsize <= 0) newsize = 1;
    goc_spin_lock(&g_lock);
    cur = g_live;
    while (cur) {
        if (cur->ptr == user) {
            long off = 0;
            int bad = rt_check_redzones(cur->real, cur->ptr, cur->size, &off);
            if (bad) rt_emit_oob(cur, bad, off);
            newreal = __goclib_heap_realloc(cur->real, newsize + 2 * GOC_RT_REDZONE);
            if (newreal == 0) { goc_spin_unlock(&g_lock); return 0; }
            newuser = (void *)((unsigned char *)newreal + GOC_RT_REDZONE);
            cur->real = newreal;
            cur->ptr = newuser;
            cur->size = newsize;
            rt_poison(newreal, newuser, newsize);
            g_total_realloc++;
            found = 1;
            break;
        }
        cur = cur->next;
    }
    goc_spin_unlock(&g_lock);
    if (!found) {
        rt_emit("[goc-rt] *** realloc on untracked pointer ignored\n");
        return 0;
    }
    return newuser;
}

/* ---- public API: snapshot / print / scan -------------------------------- */

/* Fill *out with the current tracked-allocation statistics. Safe to call at
 * any time; takes the lock so the numbers are self-consistent. */
void runtime_stats(goc_rt_stats_t *out) {
    if (out == 0) return;
    goc_spin_lock(&g_lock);
    out->total_alloc   = g_total_alloc;
    out->total_free    = g_total_free;
    out->live_blocks   = g_cur_live;
    out->live_bytes    = g_live_bytes;
    out->peak_blocks   = g_peak_live;
    out->peak_bytes    = g_peak_bytes;
    out->total_realloc = g_total_realloc;
    out->missed        = g_missed;
    out->pool_used     = g_cur_live;
    out->pool_total    = GOC_RT_POOL;
    out->oob_blocks    = g_last_oob;
    goc_spin_unlock(&g_lock);
}

/* Scan redzones of every live block. Returns the number of corrupted blocks
 * and prints one diagnostic line per hit. Records the count so a later
 * runtime_stats() reports it in oob_blocks. */
long runtime_scan(void) {
    goc_rt_node_t *n;
    long hits = 0;
    goc_spin_lock(&g_lock);
    for (n = g_live; n; n = n->next) {
        long off = 0;
        int bad = rt_check_redzones(n->real, n->ptr, n->size, &off);
        if (bad) { hits++; rt_emit_oob(n, bad, off); }
    }
    g_last_oob = hits;
    goc_spin_unlock(&g_lock);
    return hits;
}

/* Print the one-line usage summary (same numbers as __goc_rt_report's header)
 * to the diagnostic stream. Cheap enough to call interactively. */
void runtime_print(void) {
    char line[64];
    int L;
    goc_spin_lock(&g_lock);
    rt_emit("[goc-rt] mem: live=");
    rt_decbuf(g_cur_live, line, &L); rt_emit(line);
    rt_emit(" blocks / ");
    rt_decbuf(g_live_bytes, line, &L); rt_emit(line);
    rt_emit(" bytes   peak=");
    rt_decbuf(g_peak_live, line, &L); rt_emit(line);
    rt_emit(" blocks / ");
    rt_decbuf(g_peak_bytes, line, &L); rt_emit(line);
    rt_emit(" bytes   total=");
    rt_decbuf(g_total_alloc, line, &L); rt_emit(line);
    rt_emit(" allocs, realloc=");
    rt_decbuf(g_total_realloc, line, &L); rt_emit(line);
    rt_emit(", pool=");
    rt_decbuf(g_cur_live, line, &L); rt_emit(line);
    rt_emit("/");
    rt_decbuf(GOC_RT_POOL, line, &L); rt_emit(line);
    rt_emit("\n");
    goc_spin_unlock(&g_lock);
}

/* Print every live block (pointer, size) plus a usage summary -- the "show me
 * everything right now" entry point. */
void runtime_dump(void) {
    goc_rt_node_t *n;
    char line[64];
    int L;
    long count = 0;
    goc_spin_lock(&g_lock);
    for (n = g_live; n; n = n->next) count++;
    rt_emit("[goc-rt] ---- live allocations: ");
    rt_decbuf(count, line, &L); rt_emit(line);
    rt_emit(" blocks ----\n");
    for (n = g_live; n; n = n->next) {
        long off = 0;
        int bad = rt_check_redzones(n->real, n->ptr, n->size, &off);
        if (bad) { rt_emit_oob(n, bad, off); continue; }
        rt_emit("[goc-rt]   #");
        rt_decbuf(n->seq, line, &L); rt_emit(line);
        rt_emit(" ptr=0x");
        rt_hexbuf((unsigned long)(unsigned long long)n->ptr, line); rt_emit(line);
        rt_emit(" size=");
        rt_decbuf(n->size, line, &L); rt_emit(line);
        rt_emit("\n");
    }
    goc_spin_unlock(&g_lock);
    runtime_print();
}

/* ---- exit report (clean exit, stdio is safe) --------------------------- */
void __goc_rt_report(void) {
    goc_rt_node_t *n;
    long leaks = 0, oob = 0;
    char line[64];
    int L;
    /* Report through the crash-safe writer, NOT fprintf: this keeps the whole
     * stdio/printf machine out of -rtdiag builds that never otherwise use it
     * (a plain print() program would otherwise grow by ~45KB). */
    rt_emit("[goc-rt] allocs: total=");
    rt_decbuf(g_total_alloc, line, &L); rt_emit(line);
    rt_emit(" freed=");  rt_decbuf(g_total_free, line, &L); rt_emit(line);
    rt_emit(" live=");   rt_decbuf(g_cur_live, line, &L); rt_emit(line);
    rt_emit(" peak=");   rt_decbuf(g_peak_live, line, &L); rt_emit(line);
    rt_emit(" realloc=");rt_decbuf(g_total_realloc, line, &L); rt_emit(line);
    rt_emit(" missed="); rt_decbuf(g_missed, line, &L); rt_emit(line);
    rt_emit("\n");
    for (n = g_live; n; n = n->next) leaks++;
    if (leaks) {
        rt_emit("[goc-rt] LEAKS (");
        rt_decbuf(leaks, line, &L); rt_emit(line);
        rt_emit(" blocks):\n");
        for (n = g_live; n; n = n->next) {
            long off = 0;
            int bad = rt_check_redzones(n->real, n->ptr, n->size, &off);
            if (bad) { oob++; rt_emit_oob(n, bad, off); }
            else {
                rt_emit("[goc-rt]   #");
                rt_decbuf(n->seq, line, &L); rt_emit(line);
                rt_emit(" ptr=0x");
                rt_hexbuf((unsigned long)(unsigned long long)n->ptr, line); rt_emit(line);
                rt_emit(" size=");
                rt_decbuf(n->size, line, &L); rt_emit(line);
                rt_emit("\n");
            }
        }
        if (oob) {
            rt_emit("[goc-rt] WARNING: ");
            rt_decbuf(oob, line, &L); rt_emit(line);
            rt_emit(" leaked block(s) have corrupted redzones -- a buffer over/underflow happened earlier and was never caught.\n");
        }
    }
}

/* ---- crash dump (Windows vectored handler, no stdio) ------------------- */
#ifdef _WIN32
static long goc_rt_veh(void *ep) {
    goc_rt_node_t *n;
    long leaks = 0;
    char line[64];
    int L;
    (void)ep;
    rt_emit("[goc-rt] *** CRASH -- live allocations at fault ***\n");
    for (n = g_live; n; n = n->next) {
        long off = 0;
        int bad = rt_check_redzones(n->real, n->ptr, n->size, &off);
        if (bad) { leaks++; rt_emit_oob(n, bad, off); }
    }
    if (leaks == 0) {
        /* no redzone corruption found among live blocks; still dump them. */
        for (n = g_live; n; n = n->next) leaks++;
    }
    rt_emit("[goc-rt] live=");
    rt_decbuf(leaks, line, &L);
    rt_emit(line);
    rt_emit("\n");
    for (n = g_live; n; n = n->next) {
        rt_emit("[goc-rt]   #");
        rt_decbuf(n->seq, line, &L);
        rt_emit(line);
        rt_emit(" ptr=0x");
        rt_hexbuf((unsigned long)(unsigned long long)n->ptr, line);
        rt_emit(line);
        rt_emit(" size=");
        rt_decbuf(n->size, line, &L);
        rt_emit(line);
        rt_emit("\n");
    }
    return 0; /* EXCEPTION_CONTINUE_SEARCH: let the OS terminate the process */
}
#endif

#endif /* GOC_RTDIAG */

/* =============================================================================
 * No -rt: the public API still exists (so the same source compiles and runs
 * unchanged) but does nothing. runtime_stats zeroes the snapshot -- callers
 * read zeros instead of uninitialised memory, which keeps "-rt off" a
 * well-defined, silent no-op rather than a source-level #ifdef at every call
 * site. These stubs are the only code a non-rt build adds; the tracker above
 * is compiled out entirely.
 * ========================================================================== */
#ifndef GOC_RTDIAG
void runtime_stats(goc_rt_stats_t *out) {
    if (out == 0) return;
    out->total_alloc   = 0;
    out->total_free    = 0;
    out->live_blocks   = 0;
    out->live_bytes    = 0;
    out->peak_blocks   = 0;
    out->peak_bytes    = 0;
    out->total_realloc = 0;
    out->missed        = 0;
    out->pool_used     = 0;
    out->pool_total    = 0;
    out->oob_blocks    = 0;
}
void runtime_print(void) { }
void runtime_dump(void)  { }
long runtime_scan(void)  { return 0; }
#endif /* !GOC_RTDIAG */
