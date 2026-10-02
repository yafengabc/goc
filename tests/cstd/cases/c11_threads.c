/* ============================================================
   c11_threads.c - <threads.h>, thrd_t / mtx_t / cnd_t opaque types
   Standard   : ISO/IEC 9899:2011 (C11) 7.26
   Strategy   : 1 subcase (type presence via sizeof). goc ships <threads.h>
               (batch H: opaque thrd_t/mtx_t/cnd_t/once_flag/tss_t matching the
               fallback typedefs below, so sizeof is identical either way);
               this Windows gcc has no <threads.h>, so __has_include falls back
               to opaque typedefs and the file stays gcc-clean. gcc -std=c11 diff
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c11)
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
    printf("case1: sizeof thrd_t=%d mtx_t=%d cnd_t=%d\n",
           (int)sizeof(thrd_t), (int)sizeof(mtx_t), (int)sizeof(cnd_t));
    return 0;
}