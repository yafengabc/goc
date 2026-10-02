/* ============================================================
   c99_compound_file.c - file-scope compound literals (C99 6.5.2.5)
   Standard   : ISO/IEC 9899:1999 (C99) 6.5.2.5
   Strategy   : 3 file-scope compound literals (const, mutable, static
                const). gcc -std=c99 accepts; goc explicitly rejects
                (block scope only).
   Status     : UNSUPPORTED (verified 2026-10-02, goc vs gcc -std=c99)
   ============================================================ */
const int *p1 = (const int[]){1, 2, 3};
int *p2 = (int[]){4, 5, 6};
static const int *q = (const int[]){7, 8, 9};
int main(void) {
    return p1[0] + p2[1] + q[2];
}