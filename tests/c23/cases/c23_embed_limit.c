/* C23 feature: #embed limit() option (truncation)
 * Clause:     C23 6.10.11.3 "The limit option"
 * Strategy:   embed cases/embed_data.txt ({65,66,67,10}, 4 bytes) under limit(0),
 *             limit(2), limit(4) (=exact length) and limit(10) (over-long, clamped);
 *             verify each spliced size and first bytes.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

/* limit(0): splices zero bytes; a leading sentinel keeps the list non-empty */
unsigned char a0[1] = {
    0xFF
#embed "embed_data.txt" limit(0)
};

/* limit(2): keep only the first two bytes */
unsigned char a2[] = {
#embed "embed_data.txt" limit(2)
};

/* limit(4): exactly the whole file length */
unsigned char a4[] = {
#embed "embed_data.txt" limit(4)
};

/* limit(10): file only has 4 bytes, so the embed clamps to the file length */
unsigned char a10[] = {
#embed "embed_data.txt" limit(10)
};

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: sizeof(a0)=%d, a0[0]=%d\n", total, (int)sizeof(a0), a0[0]);
    if (sizeof(a0) == 1 && a0[0] == 0xFF) passed++;

    ++total;
    int ok = (sizeof(a2) == 2 && a2[0]==65 && a2[1]==66);
    printf("case%d: sizeof(a2)=%d bytes=%d,%d ok=%d\n",
           total, (int)sizeof(a2), a2[0], a2[1], ok);
    if (ok) passed++;

    ++total;
    ok = (sizeof(a4) == 4 && a4[0]==65 && a4[3]==10);
    printf("case%d: sizeof(a4)=%d bytes=%d,...,%d ok=%d\n",
           total, (int)sizeof(a4), a4[0], a4[3], ok);
    if (ok) passed++;

    ++total;
    ok = (sizeof(a10) == 4 && a10[0]==65 && a10[1]==66 && a10[2]==67 && a10[3]==10);
    printf("case%d: sizeof(a10)=%d (clamped) ok=%d\n",
           total, (int)sizeof(a10), ok);
    if (ok) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
