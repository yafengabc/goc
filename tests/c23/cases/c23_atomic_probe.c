/* C23 feature: _Atomic / <stdatomic.h> (C11; goc roadmap #129)
 * Clause:     C23 6.7.2.4 _Atomic; <stdatomic.h>
 * Strategy:   minimal probe: does goc accept the raw _Atomic keyword, and does
 *             it provide <stdatomic.h> (atomic_int, atomic_store, atomic_load)?
 *             Written when goc rejected the keyword outright and had no header;
 *             both are supported now (c23_atomic.c is the full suite), so the
 *             probe compiles and runs on both toolchains.
 * Status:     PASS (was UNSUPPORTED; #129 landed 2026-10-05)
 * EXPECT: PASS */
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
