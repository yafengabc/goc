/* C23 feature: digit separator ' - ILLEGAL positions (negative test)
 * Clause:     C23 6.4.4.1 / 6.4.4.2 (where the apostrophe may NOT appear)
 * Strategy:   constructs that a conforming C23 compiler must reject. Verified
 *             against gcc -std=c2x ground truth (each line below produces a
 *             "digit separator ..." or "missing terminating ' character" error).
 *             goc's reaction is recorded in group_D1.md.
 * Status:     PENDING
 * EXPECT: REJECT
 */
int main(void) {
    int a = 1''000;      /* adjacent separators */
    int b = 0x'FFFF';    /* separator right after the base indicator */
    double c = 1'.2;     /* separator immediately before the decimal point */
    double d = 1.'5;     /* separator immediately after the decimal point */
    double e = 0x1'p0;   /* separator immediately adjacent to the exponent p */
    int f = 123';        /* trailing separator with no suffix */
    return a + (int)b + (int)c + (int)d + (int)e + f;
}
