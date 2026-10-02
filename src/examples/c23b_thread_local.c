#include <stdio.h>

/* thread_local / _Thread_local: goc has no native TLS, so these are accepted
 * with a warning and treated as plain (process-wide) statics. The behavior is
 * deterministic across the regression legs (only a stderr warning differs). */
thread_local int tls_a = 10;
_Thread_local static int tls_b = 20;

int main(void) {
    printf("tls_a=%d tls_b=%d\n", tls_a, tls_b);
    return 0;
}
