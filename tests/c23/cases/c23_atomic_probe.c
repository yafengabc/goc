/* C23 feature: _Atomic / <stdatomic.h> (C11; goc roadmap #129, deferred)
 * Clause:     C23 6.7.2.4 _Atomic; <stdatomic.h>
 * Strategy:   probe whether goc (a) accepts the raw _Atomic keyword and (b) provides
 *             <stdatomic.h>. Roadmap says syntax accepted + lock prefix (unverified) and
 *             the header is unimplemented. gcc compiles the whole thing; goc is expected
 *             to fail with the documented design-gap diagnostics.
 * Status:     UNSUPPORTED
 * EXPECT: UNSUPPORTED
 */
#include <stdio.h>
#include <stdatomic.h>

int main(void) {
    /* raw _Atomic keyword on a scalar */
    _Atomic int ax = 0;
    ax++;
    printf("ax=%d\n", ax);

    /* stdatomic.h convenience typedef and load/store */
    atomic_int a = 0;
    atomic_store(&a, 5);
    a++;
    printf("a=%d\n", (int)atomic_load(&a));
    return 0;
}
