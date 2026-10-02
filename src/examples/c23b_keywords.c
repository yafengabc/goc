#include <stdio.h>

/* C23 keyword aliases: alignas/alignof, noreturn, char8_t, inline, attributes */
alignas(16) int g = 7;

noreturn void no_return_proto(void);
[[unsequenced]] int proto(void);

inline int cube(int x) { return x * x * x; }

int main(void) {
    /* u8 string / char prefix -> UTF-8 (no-op for goc, already UTF-8) */
    char8_t c = u8'A';
    const char *s = u8"hi";
    printf("c=%c s=%s\n", c, s);

    /* alignas / alignof keyword aliases (alignof folds to a constant) */
    printf("alignof(int)=%d\n", (int)alignof(int));
    printf("alignof(double)=%d\n", (int)alignof(double));
    alignas(32) int av;
    (void)av;
    printf("alignof(av)=%d\n", (int)alignof(av));

    /* standard attributes on a local (silently accepted) */
    [[maybe_unused]] int unused = 5;
    [[fallthrough]] (void)unused;

    /* inline function */
    printf("cube(3)=%d\n", cube(3));

    printf("g=%d\n", g);
    return 0;
}
