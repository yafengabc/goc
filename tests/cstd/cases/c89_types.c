/* ============================================================
   c89_types.c - C89 basic arithmetic types, typedef, sizeof, char signedness
   Standard   : ISO/IEC 9899:1990 (C89) 6.1.2.5 types, 6.3.3.4 sizeof
   Strategy   : 6 subcases: sizeof of char/short/int/long/float/double,
                char signedness via (char)0xFF, unsigned char range and
                promotion, typedef alias size/value, long/unsigned long
                constant widths, float-to-double promotion.
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

typedef int MyInt;
typedef unsigned char Byte;

int main(void) {
    char c;
    short s;
    int i;
    long l;
    unsigned char uc;
    unsigned short us;
    unsigned int ui;
    unsigned long ul;
    float f;
    double d;
    MyInt mi = 42;
    Byte b = (Byte)0xAB;

    /* case1: sizeof fundamental types.  goc is LP64 (long=8) while host
       gcc on Windows is LLP64 (long=4), so sizeof(long) differs and is
       deliberately NOT printed here; see report cross-findings.
       Printed: char/short/int/float/double/pointer - same on both. */
    printf("case1: %d %d %d %d %d %d\n",
        (int)sizeof(c), (int)sizeof(s), (int)sizeof(i),
        (int)sizeof(f), (int)sizeof(d), (int)sizeof(int *));

    /* case2: char signedness: (char)0xFF prints -1 if signed, 255 if unsigned */
    c = (char)0xFF;
    printf("case2: %d\n", c);

    /* case3: unsigned char range and promoted comparison */
    uc = 200;
    printf("case3: %d %d\n", (int)uc, (uc > 150) ? 1 : 0);

    /* case4: typedef alias shares size and underlying type */
    printf("case4: %d %d %d\n", mi, (int)sizeof(MyInt), (int)b);

    /* case5: long and unsigned long constant widths */
    l = 2147483647L;
    ul = 4000000000UL;
    printf("case5: %ld %lu\n", l, ul);

    /* case6: float promotes to double in varargs; float size */
    f = 1.5f;
    printf("case6: %f %d\n", f + 0.25, (int)sizeof(float));

    return 0;
}
