/* C23 feature: thread_local / _Thread_local storage-class specifier
 * Clause:     C23 6.7.1 (C11 6.7.1/6.7.2 TLS); goc roadmap #118-#123 (goa native TLS)
 * Strategy:   file-scope and function-local static thread_local objects: constant
 *             initialization, read/write, sizeof, address-of, several coexisting.
 *             Single-threaded by design: per-thread independence cannot be exercised here.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

/* case 1,2,7: file-scope TLS, two spellings, coexisting */
thread_local       int tl_file_a = 11;
_Thread_local      int tl_file_b = 22;
thread_local const int tl_file_c = 33;

/* case 3: function-local static TLS that persists across calls (accumulator) */
static int tls_counter(void) {
    static thread_local int n = 0;
    n++;
    return n;
}

/* case 4: function-local static TLS with constant-expression init, mutated across calls */
static int tls_lazy(void) {
    static thread_local int lazy = 10;
    lazy *= 2;
    return lazy;
}

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int a1 = tl_file_a;
    printf("case1: file-scope thread_local = %d\n", a1);
    if (a1 == 11) passed++;

    ++total;
    tl_file_b += 3;                 /* non-const read/write */
    printf("case2: file-scope _Thread_local after write = %d\n", tl_file_b);
    if (tl_file_b == 25) passed++;

    ++total;
    int c1 = tls_counter();
    int c2 = tls_counter();
    printf("case3: function-local static tls count = %d,%d\n", c1, c2);
    if (c1 == 1 && c2 == 2) passed++;

    ++total;
    int l1 = tls_lazy();
    int l2 = tls_lazy();
    printf("case4: const-init tls mutated = %d,%d\n", l1, l2);
    if (l1 == 20 && l2 == 40) passed++;

    ++total;
    printf("case5: sizeof(tl_file_a) = %d\n", (int)sizeof(tl_file_a));
    if (sizeof(tl_file_a) == sizeof(int)) passed++;

    ++total;
    int *p = &tl_file_a;
    *p += 100;
    printf("case6: address-of tls, via pointer = %d\n", tl_file_a);
    if (tl_file_a == 111) passed++;

    ++total;
    printf("case7: three coexisting tls = %d,%d,%d\n", tl_file_a, tl_file_b, tl_file_c);
    if (tl_file_a == 111 && tl_file_b == 25 && tl_file_c == 33) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
