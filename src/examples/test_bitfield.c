
#include <stdio.h>

struct BitFieldTest {
    unsigned int a : 3;  // 3 bits
    unsigned int b : 5;  // next 5 bits (total 8 bits so far)
    int c : 4;           // next 4 bits (total 12 bits → padded to 16)
};

// A bit-field that forces a new storage unit (does not straddle the int).
struct Straddle {
    unsigned int a : 3;
    unsigned int b : 30; // does not fit in the rest of the unit -> new unit
    unsigned int c : 5;
};

// Anonymous + zero-width bit-fields (padding control).
struct Anon {
    unsigned int a : 3;
    int : 5;             // unnamed: pads to 8 bits, no name
    unsigned int b : 2;
    unsigned int : 0;    // zero-width: forces b2 to a fresh unit
    unsigned int b2 : 2;
    unsigned char : 0;
    unsigned char b3 : 2;
};

// A bit-field beside ordinary members (the ordinary member closes the unit).
struct Mixed {
    unsigned int a : 4;
    int x;
    unsigned int b : 4;
};

// char-based bit-fields (1-byte storage units).
struct CharBits {
    unsigned char lo : 3;
    unsigned char hi : 5;
};

union UnionBits {
    unsigned int raw;
    struct {
        unsigned int lo : 8;
        unsigned int hi : 8;
    } bytes;
};

int main() {
    struct BitFieldTest t;
    t.a = 5;  // max 3 bits is 7 -> 5 ok
    t.b = 17; // max 5 bits is 31 -> 17 ok
    t.c = -3; // signed 4 bits: -8 to 7 -> -3 ok

    printf("a=%d, b=%d, c=%d\n", t.a, t.b, t.c);

    // Test that assignment is limited by bit width
    t.a = 8;  // 8 is 1000 -> truncate to 3 bits -> 0
    t.b = 40; // 40 is 101000 -> truncate to 5 bits -> 8
    printf("after truncate: a=%d, b=%d, c=%d\n", t.a, t.b, t.c);

    // Bit-fields participate in ordinary expressions.
    t.a = 3;
    t.c = -3;
    printf("expr: %d %d %d\n", t.a + t.c, t.a * 2, t.c < 0);

    // Signed extraction: a negative 4-bit field reads back negative.
    printf("signed read: c=%d (want -3)\n", t.c);

    // ++ / -- on bit-fields (wraps within the field width).
    t.a = 7; // max for 3 bits
    t.a++;
    printf("wrap: a=%d (want 0)\n", t.a);
    ++t.a;
    t.a--;
    printf("incdec: a=%d (want 0)\n", t.a);
    t.c = -1;
    ++t.c;
    printf("signed inc: c=%d (want 0)\n", t.c);

    // Straddle: b lives in its own unit, so a's bits are untouched.
    struct Straddle s;
    s.a = 5;
    s.b = 123456789;
    s.c = 17;
    printf("straddle: a=%d b=%d c=%d\n", s.a, s.b, s.c);

    // Anonymous + zero-width bit-fields.
    struct Anon an;
    an.a = 3;
    an.b = 1;
    an.b2 = 2;
    an.b3 = 3;
    printf("anon: a=%d b=%d b2=%d b3=%d\n", an.a, an.b, an.b2, an.b3);

    // Mixed: x is an ordinary int; b's unit must start after it.
    struct Mixed mx;
    mx.a = 7;
    mx.x = 1234;
    mx.b = 9;
    printf("mixed: a=%d x=%d b=%d\n", mx.a, mx.x, mx.b);

    // char storage units.
    struct CharBits cb;
    cb.lo = 5;
    cb.hi = 31;
    printf("char: lo=%d hi=%d\n", cb.lo, cb.hi);

    // Union: bit-fields and raw share storage.
    union UnionBits ub;
    ub.raw = 0;
    ub.bytes.lo = 0xAB;
    ub.bytes.hi = 0xCD;
    printf("union: raw=%d lo=%d hi=%d\n", ub.raw, ub.bytes.lo, ub.bytes.hi);

    // sizeof counts the storage units.
    printf("sizes: %d %d %d %d %d\n",
           sizeof(struct BitFieldTest),
           sizeof(struct Straddle),
           sizeof(struct Anon),
           sizeof(struct Mixed),
           sizeof(struct CharBits));

    return 0;
}
