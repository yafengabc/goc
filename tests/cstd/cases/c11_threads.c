/* ============================================================
   c11_threads.c - <threads.h>, thrd_t / mtx_t / cnd_t complete object types
   Standard   : ISO/IEC 9899:2011 (C11) 7.26
   Strategy   : 1 subcase (completeness, not sizeof). goc now ships a real
               <threads.h> (thrd_t/mtx_t/cnd_t/once_flag/tss_t with real
               layout, backed by src/goclib/threads.c), while this Windows gcc
               has no <threads.h> at all -- so __has_include falls back to the
               opaque typedefs below and the file stays gcc-clean.
               sizeof is NOT compared: the size of these types is
               implementation-defined, and a size diff here would only be
               reporting which header got picked up, not a defect. What has to
               hold either way is that all three are complete object types:
               declarable, and with an address. gcc -std=c11 diff
   Status     : PASS (verified 2026-10-05, goc vs gcc -std=c11)
   ============================================================ */
#include <stdio.h>

#if defined(__has_include)
#  if __has_include(<threads.h>)
#    include <threads.h>
#  else
typedef struct { int opaque; } thrd_t;
typedef struct { int mtx; } mtx_t;
typedef struct { int cnd; } cnd_t;
#  endif
#else
typedef struct { int opaque; } thrd_t;
typedef struct { int mtx; } mtx_t;
typedef struct { int cnd; } cnd_t;
#endif

int main(void) {
    thrd_t t;
    mtx_t  m;
    cnd_t  c;
    /* Deliberately not sizeof: the size of these types is
     * implementation-defined, and goc now ships a real <threads.h> while this
     * gcc has none -- so goc sees the real (larger) definitions and gcc sees
     * the fallback typedefs above. Comparing sizes would only be comparing
     * which header got picked up, and would report a difference that is not a
     * defect. What has to hold either way is that all three are complete
     * object types: declarable, and with an address. */
    printf("case1: threads types complete %d\n",
           (int)((&t != 0) + (&m != 0) + (&c != 0)));
    return 0;
}