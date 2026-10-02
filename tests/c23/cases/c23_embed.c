/* C23 feature: #embed (basic splice)
 * Clause:     C23 6.10.11 "Embed"
 * Strategy:   splice cases/embed_data.txt ("ABC\n" = bytes {65,66,67,10}) into
 *             byte arrays; verify array size, per-byte values, and multiple
 *             #embed directives chained in one initializer.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

/* whole file embedded as decimal byte tokens */
unsigned char blob[] = {
#embed "embed_data.txt"
};

/* two embeds chained with a literal byte in between; the separator commas
 * must sit on their own physical lines (a comma right after the filename is
 * parsed as an #embed option) */
unsigned char twice[] = {
#embed "embed_data.txt"
    , 0x5A
    ,
#embed "embed_data.txt"
};

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: sizeof(blob)=%d\n", total, (int)sizeof(blob));
    if (sizeof(blob) == 4) passed++;

    ++total;
    int ok = (blob[0]==65 && blob[1]==66 && blob[2]==67 && blob[3]==10);
    printf("case%d: blob bytes=%d,%d,%d,%d ok=%d\n",
           total, blob[0], blob[1], blob[2], blob[3], ok);
    if (ok) passed++;

    ++total;
    printf("case%d: sizeof(twice)=%d\n", total, (int)sizeof(twice));
    if (sizeof(twice) == 9) passed++;

    ++total;
    ok = (twice[0]==65 && twice[4]==0x5A && twice[5]==65 && twice[8]==10);
    printf("case%d: twice chain boundary bytes ok=%d\n", total, ok);
    if (ok) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
