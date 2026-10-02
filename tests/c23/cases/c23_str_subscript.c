/* C23 (and C89+): direct subscript of a string literal
 * Clause:     C11 6.4.5 "String literals" -- a string literal is a char[N]
 *             array, so "hello"[0] is the first character, not a wide load.
 * Strategy:   subscript a string literal directly (index 0 and 1), compare
 *             against the same bytes read through a const char* control, and
 *             check a byte >= 0x80 sign-extends (goc's char is signed).
 * Status:     PASS (verified 2026-10-02; P0.8 fixed - elemWidthOf/elemSignedOf
 *             now type a string literal as char[], so the stride and load are
 *             1 byte and signed; was garbage quadword read)
 * EXPECT: PASS
 */
#include <stdio.h>

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int d0 = "hello"[0];
    printf("case%d: direct[0] = %d\n", total, d0);
    if (d0 == 'h') passed++;

    ++total;
    int d1 = "hello"[1];
    printf("case%d: direct[1] = %d\n", total, d1);
    if (d1 == 'e') passed++;

    ++total;
    const char *p = "hello";
    printf("case%d: ptr[0] = %d\n", total, p[0]);
    if (p[0] == d0) passed++;

    ++total;
    int hi = "\xE4"[0]; /* 0xE4 = -28 in signed char */
    printf("case%d: high-byte = %d\n", total, hi);
    if (hi == -28) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
