// long minimum literal: -9223372036854775808L (and the hex form
// -0x8000000000000000L) must survive the lexer as the int64 minimum.
// 9223372036854775808 alone (no minus) is 2^63, which cannot be spelled
// as a signed constant; its bit pattern is kept, so it prints as the
// minimum. Only values beyond 2^64-1 are not representable.

long g_dec = -9223372036854775808L;
long g_hex = -0x8000000000000000L;
long g_max = -9223372036854775807L;

int main(void) {
    long y = -9223372036854775808L;
    long z = -0x8000000000000000L;
    long x = -9223372036854775807L - 1;
    print(y);
    print(z);
    print(x);
    print(g_dec);
    print(g_hex);
    print(g_max);
    return 0;
}
