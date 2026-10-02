/* ============================================================
   c89_bitfield.c - C89 bit-fields: signedness, widths, unnamed/zero-width
   Standard   : ISO/IEC 9899:1990 (C89) 6.5.2.1 bit-field specifiers
   Strategy   : 6 subcases: unsigned and signed small-width fields, plain int
                bit-field signedness (implementation-defined, record gcc vs
                goc), one-bit unsigned field, signed 3-bit range, out-of-range
                truncation, zero-width/unnamed layout and sizeof.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

struct Bf {
    int a : 3;        /* plain int bit-field: signedness impl-defined, record */
    unsigned b : 3;
    signed c : 3;
    unsigned d : 1;   /* holds 0/1 only */
    unsigned : 4;     /* unnamed padding field */
    int : 0;          /* unnamed zero width: align next field to a new unit */
    int f : 2;
};

int main(void) {
    struct Bf x;

    /* case1: unsigned 3-bit and signed 3-bit stored values */
    x.b = 7;
    x.c = 3;
    printf("case1: %u %d\n", x.b, x.c);

    /* case2: plain int :3 signedness: 4 stored -> -4 signed / 4 unsigned */
    x.a = 4;
    printf("case2: %d\n", x.a);

    /* case3: unsigned one-bit field holds only 0/1 */
    x.d = 1;
    printf("case3: %u\n", x.d);

    /* case4: signed 3-bit range is -4..3 */
    x.c = -4;
    printf("case4: %d\n", x.c);

    /* case5: out-of-range value truncates to the width */
    x.b = 8;   /* 8 modulo 8 == 0 */
    printf("case5: %u\n", x.b);

    /* case6: sizeof of bit-field struct (layout, record) */
    printf("case6: %d\n", (int)sizeof(struct Bf));

    return 0;
}
