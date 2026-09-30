#include "goclib.h"

/* =============================================================================
 * os.c -- the five __goclib_* OS primitives.
 *
 * This is the only file in goclib that knows the OS. On Windows the calls go
 * through the win32.def import table; on Linux goa turns each extern name
 * into a "mov rax,N; syscall; ret" stub (see externLinux in codegen.go).
 * Everything else in goclib is portable C and lives in the sibling .c files,
 * which are compiled per target in name order (os.c, stdio.c, stdlib.c,
 * string.c) after the goclib.h umbrella TU.
 * ========================================================================== */
#if defined(_WIN32)

extern void *GetStdHandle(long which);
extern long  WriteFile(void *h, const void *buf, long n, long *written, long overlapped);
extern long  ReadFile(void *h, void *buf, long n, long *got, long overlapped);
extern void  ExitProcess(long code);
extern void *GetProcessHeap(void);
extern void *HeapAlloc(void *heap, long flags, long bytes);
extern long  HeapFree(void *heap, long flags, void *block);
/* HeapReAlloc resizes in place when it can and copies when it cannot, so
 * it is both smaller and faster than alloc+copy here. */
extern void *HeapReAlloc(void *heap, long flags, void *block, long bytes);

long __goclib_write(const char *buf, long len) {
    long written = 0;
    void *h = GetStdHandle(-11);            /* STD_OUTPUT_HANDLE */
    if (h == 0) return -1;
    if (len > 0) {
        if (!WriteFile(h, buf, len, &written, 0)) return -1;
    }
    return written;
}

long __goclib_read(char *buf, long len) {
    long got = 0;
    void *h = GetStdHandle(-10);            /* STD_INPUT_HANDLE */
    if (h == 0) return -1;
    if (len > 0) {
        if (!ReadFile(h, buf, len, &got, 0)) return -1;
    }
    return got;
}

void __goclib_exit(long code) {
    ExitProcess(code);
}

void *__goclib_heap_alloc(long size) {
    if (size <= 0) size = 1;
    return HeapAlloc(GetProcessHeap(), 0, size);
}

void __goclib_heap_free(void *p) {
    if (p != 0) HeapFree(GetProcessHeap(), 0, p);
}

void *__goclib_heap_realloc(void *p, long size) {
    if (p == 0) return __goclib_heap_alloc(size);
    if (size <= 0) {
        __goclib_heap_free(p);
        return 0;
    }
    return HeapReAlloc(GetProcessHeap(), 0, p, size);
}

#elif defined(__linux__)

/*
 * exit_group (231) instead of exit (60): the library itself defines a
 * function named exit, and one output cannot carry both symbols. The entry
 * stub calls the C exit, which calls __goclib_exit, which lands here.
 */
extern long write(long fd, const void *buf, long n);
extern long read(long fd, void *buf, long n);
extern void *brk(void *addr);
extern void exit_group(long code);

static char *heap_cur;                      /* brk bump-allocator cursor */

/* Every block carries this many bytes in front of the returned pointer,
 * holding the requested size. The bump allocator cannot otherwise answer
 * "how big was this block?", which realloc needs. 16 keeps the payload
 * 16-byte aligned and leaves room to grow the header later.
 */
#define HEAP_HDR 16

long __goclib_write(const char *buf, long len) {
    if (len <= 0) return 0;
    return write(1, buf, len);
}

long __goclib_read(char *buf, long len) {
    if (len <= 0) return 0;
    return read(0, buf, len);
}

void __goclib_exit(long code) {
    exit_group(code);
}

void *__goclib_heap_alloc(long size) {
    long need;
    char *next;
    char *raw;
    if (size <= 0) size = 1;
    need = ((size + HEAP_HDR) + 15) / 16 * 16;  /* header + 16-byte aligned */
    if (heap_cur == 0) {
        heap_cur = (char *)brk((void *)0);  /* query the current break */
    }
    next = heap_cur + need;
    if ((char *)brk((void *)next) != next) {
        return 0;                           /* failed: brk returns the old break */
    }
    raw = heap_cur;
    heap_cur = next;
    *((long *)raw) = size;                  /* realloc reads this back */
    return raw + HEAP_HDR;
}

void __goclib_heap_free(void *p) {
    /* bump allocator: nothing to do until the process exits. */
}

void *__goclib_heap_realloc(void *p, long size) {
    char *np;
    long old;
    long copy;
    if (p == 0) return __goclib_heap_alloc(size);
    if (size <= 0) return 0;
    old = *((long *)((char *)p - HEAP_HDR));
    np = (char *)__goclib_heap_alloc(size);
    if (np == 0) return 0;
    /* The old block is never reclaimed -- the bump allocator has no free --
     * so this trades memory for a correct copy of min(old, new) bytes. */
    copy = old;
    if (size < copy) copy = size;
    memcpy(np, p, copy);
    return np;
}

#else
#error "goclib: unknown target (need _WIN32 or __linux__)"
#endif
