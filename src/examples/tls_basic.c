#include <stdio.h>

/* Global thread-local variables of different types. */
_Thread_local int    g_a;
_Thread_local int    g_b;
_Thread_local double g_d;
_Thread_local char   g_c;
_Thread_local int    g_arr[4];

int fib_tls(int n) {
    static _Thread_local int calls;   /* per-thread, initialised once */
    calls++;
    if (n < 2) return n;
    return fib_tls(n - 1) + fib_tls(n - 2);
}

int main(void) {
    g_a = 7;
    g_b = 13;
    g_d = 2.5;
    g_c = 'Z';
    g_arr[0] = 1; g_arr[1] = 2; g_arr[2] = 3; g_arr[3] = 4;
    if (g_a != 7)  return 1;
    if (g_b != 13) return 2;
    if (g_d != 2.5) return 3;
    if (g_c != 'Z') return 4;
    if (g_arr[2] != 3) return 5;

    int r = fib_tls(10);              /* fib(10) = 55 */
    if (r != 55) return 6;

    /* Write g_a again after the recursive calls to prove each TLS var has its
     * own storage independent of the stack. */
    g_a = 100;
    printf("%d %d %g %c %d %d %d %d %d\n",
           g_a, g_b, g_d, g_c, g_arr[0], g_arr[1], g_arr[2], g_arr[3], r);
    return 0;
}
