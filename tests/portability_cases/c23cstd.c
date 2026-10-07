/* C23 standard-library additions: self-reporting regression.
 *
 * Exercises the functions added in the "fill C23 cstd gaps" pass:
 *   stdc_rotate_left / stdc_rotate_right   (<stdbit.h>)
 *   reallocarray / free_sized / free_aligned_sized (<stdlib.h>)
 *   memalignment                            (<string.h>)
 *
 * Prints "OK" on the last line and exits 0 iff every check passed, so it drops
 * straight into build_run_win_ok() in tools/portability/win_regress.sh. */
#include <stdio.h>
#include <stdlib.h>
#include <errno.h>
#include <stdbit.h>

static int fails = 0;
#define CHK(c, m) do { if (!(c)) { printf("FAIL: %s\n", m); fails++; } } while (0)

int main(void) {
    /* ---- stdc_rotate_left / stdc_rotate_right (typed + generic) ---- */
    CHK(stdc_rotate_left_ui(0x12345678u, 8) == 0x34567812u, "rotl32 by 8");
    CHK(stdc_rotate_right_ui(0x12345678u, 8) == 0x78123456u, "rotr32 by 8");
    /* Both the typed entry points and the type-generic macro. The 8-bit cases
     * go through the macro deliberately: every stdc_*_N function is reached
     * only through _Generic in real code, and gocl's reachability walk used to
     * skip the selected branch -- so the definitions were pruned and the link
     * failed with "undefined symbol: stdc_leading_zeros_uc". */
    CHK(stdc_rotate_left_uc(0x81, 1) == 0x03, "rotl8  by 1 (typed)");
    CHK(stdc_rotate_right_uc(0x81, 1) == 0xC0, "rotr8  by 1 (typed)");
    CHK(stdc_rotate_left((unsigned char)0x81, 1) == 0x03,
        "rotl8  by 1 (generic macro)");
    CHK(stdc_rotate_right((unsigned char)0x81, 1) == 0xC0,
        "rotr8  by 1 (generic macro)");
    CHK(stdc_leading_zeros((unsigned char)0x01) == 7,
        "leading_zeros (generic macro, unsigned char)");
    CHK(stdc_trailing_zeros((unsigned char)0x81) == 0,
        "trailing_zeros (generic macro, unsigned char)");
    CHK(stdc_rotate_left((unsigned int)0x12345678u, 8) == 0x34567812u,
        "rotl32 by 8 (generic macro)");
    CHK(stdc_rotate_left_ull(0x0000000000000001ull, 63) == 0x8000000000000000ull,
        "rotl64 by 63");
    CHK(stdc_rotate_right_ull(0x8000000000000000ull, 63) == 0x0000000000000001ull,
        "rotr64 by 63");
    CHK(stdc_rotate_left_us(0x8001u, 1) == 0x0003u, "rotl16 by 1");
    CHK(stdc_rotate_right_us(0x0003u, 1) == 0x8001u, "rotr16 by 1");
    /* rotating by the full width must be the identity (no UB at shift==width) */
    CHK(stdc_rotate_left_ui(0x12345678u, 32) == 0x12345678u, "rotl32 by 32 == id");
    CHK(stdc_rotate_left((unsigned short)0xABCDu, 16) == 0xABCDu, "rotl16 by 16 == id");

    /* ---- memalignment: largest power of two dividing the address ---- */
    CHK(memalignment(0) == 1, "align(NULL) == 1");
    CHK(memalignment((void *)8) == 8, "align(8) == 8");
    CHK(memalignment((void *)12) == 4, "align(12) == 4");
    CHK(memalignment((void *)0x1000) == 4096, "align(0x1000) == 4096");
    CHK(memalignment((void *)6) == 2, "align(6) == 2");

    /* ---- reallocarray: normal alloc, then overflow guard ---- */
    int *p = (int *)reallocarray(0, 10, sizeof(int));
    CHK(p != 0, "reallocarray alloc");
    if (p) { p[0] = 42; CHK(p[0] == 42, "reallocarray write"); }
    int *q = (int *)reallocarray(p, 20, sizeof(int));
    CHK(q != 0, "reallocarray grow");
    if (q) free_sized(q, 20 * sizeof(int));

    errno = 0;
    void *big = reallocarray(0, (size_t)-1, (size_t)-1);
    CHK(big == 0, "reallocarray overflow returns NULL");
    CHK(errno == ENOMEM, "reallocarray overflow sets ENOMEM");

    /* ---- free_sized / free_aligned_sized must not crash ---- */
    void *a = malloc(32);
    CHK(a != 0, "malloc 32");
    if (a) free_sized(a, 32);
    void *b = aligned_alloc(16, 32);
    CHK(b != 0, "aligned_alloc 16/32");
    if (b) free_aligned_sized(b, 16, 32);

    if (fails == 0) { printf("OK\n"); return 0; }
    printf("TOTAL FAILS=%d\n", fails);
    return 1;
}
