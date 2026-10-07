/* goc stdbit.c -- C23 7.18 <stdbit.h> bit utilities.
 *
 * Pure-C implementation (goclib is compiled by goc, so no host __builtin_*
 * and no lzcnt/tzcnt/popcnt instructions). Widths follow goc's ABI:
 * unsigned char=8, unsigned short=16, unsigned int=32,
 * unsigned long = unsigned long long = 64.
 *
 * The stdc_first_leading_* / stdc_first_trailing_* families use the C23
 * "most significant / least significant index, plus one" rule: the index is
 * 0-based and stdc_first_leading_one counts *down* from the top bit, so
 * stdc_first_leading_one(0x00FF00FFu) is 9 rather than 24. Zero (no such
 * bit) yields 0.
 */
#include <stdbit.h>
#include <stddef.h>

static unsigned sbw8(unsigned char x){ unsigned c=0; while(x){x>>=1u;c++;} return c; }
static unsigned spc8(unsigned char x){ unsigned c=0; while(x){c+=(unsigned)(x&1u);x>>=1u;} return c; }
static unsigned stz8(unsigned char x){ if(x==0) return 8; unsigned c=0; while(!(x&1u)){x>>=1u;c++;} return c; }

static unsigned sbw16(unsigned short x){ unsigned c=0; while(x){x>>=1u;c++;} return c; }
static unsigned spc16(unsigned short x){ unsigned c=0; while(x){c+=(unsigned)(x&1u);x>>=1u;} return c; }
static unsigned stz16(unsigned short x){ if(x==0) return 16; unsigned c=0; while(!(x&1u)){x>>=1u;c++;} return c; }

static unsigned sbw32(unsigned int x){ unsigned c=0; while(x){x>>=1u;c++;} return c; }
static unsigned spc32(unsigned int x){ unsigned c=0; while(x){c+=(unsigned)(x&1u);x>>=1u;} return c; }
static unsigned stz32(unsigned int x){ if(x==0) return 32; unsigned c=0; while(!(x&1u)){x>>=1u;c++;} return c; }

static unsigned sbw64(unsigned long x){ unsigned c=0; while(x){x>>=1u;c++;} return c; }
static unsigned spc64(unsigned long x){ unsigned c=0; while(x){c+=(unsigned)(x&1u);x>>=1u;} return c; }
static unsigned stz64(unsigned long x){ if(x==0) return 64; unsigned c=0; while(!(x&1u)){x>>=1u;c++;} return c; }

unsigned int stdc_leading_zeros_uc(unsigned char x){ return 8u - sbw8(x); }
unsigned int stdc_leading_ones_uc(unsigned char x){ return 8u - sbw8((unsigned char)(~x)); }
unsigned int stdc_trailing_zeros_uc(unsigned char x){ return stz8(x); }
unsigned int stdc_trailing_ones_uc(unsigned char x){ return stz8((unsigned char)(~x)); }
unsigned int stdc_count_zeros_uc(unsigned char x){ return 8u - spc8(x); }
unsigned int stdc_count_ones_uc(unsigned char x){ return spc8(x); }
unsigned int stdc_first_leading_zero_uc(unsigned char x){ { unsigned char y = (unsigned char)(~x); if (y == 0) return 0u; return 8u - sbw8(y) + 1u; } }
unsigned int stdc_first_leading_one_uc(unsigned char x){ if (x == 0) return 0u; return 8u - sbw8(x) + 1u; }
unsigned int stdc_first_trailing_zero_uc(unsigned char x){ { unsigned char y = (unsigned char)(~x); if (y == 0) return 0u; return stz8(y) + 1u; } }
unsigned int stdc_first_trailing_one_uc(unsigned char x){ if (x == 0) return 0u; return stz8(x) + 1u; }
bool stdc_has_single_bit_uc(unsigned char x){ return x != 0 && (x & (x - 1u)) == 0; }
unsigned int stdc_bit_width_uc(unsigned char x){ return sbw8(x); }
unsigned char stdc_bit_floor_uc(unsigned char x){ if (x == 0) return 0; return (unsigned char)((unsigned char)1 << (sbw8(x) - 1u)); }
unsigned char stdc_bit_ceil_uc(unsigned char x){ if (x <= 1u) return (unsigned char)1; return (unsigned char)((unsigned char)1 << sbw8((unsigned char)(x - 1u))); }

unsigned int stdc_leading_zeros_us(unsigned short x){ return 16u - sbw16(x); }
unsigned int stdc_leading_ones_us(unsigned short x){ return 16u - sbw16((unsigned short)(~x)); }
unsigned int stdc_trailing_zeros_us(unsigned short x){ return stz16(x); }
unsigned int stdc_trailing_ones_us(unsigned short x){ return stz16((unsigned short)(~x)); }
unsigned int stdc_count_zeros_us(unsigned short x){ return 16u - spc16(x); }
unsigned int stdc_count_ones_us(unsigned short x){ return spc16(x); }
unsigned int stdc_first_leading_zero_us(unsigned short x){ { unsigned short y = (unsigned short)(~x); if (y == 0) return 0u; return 16u - sbw16(y) + 1u; } }
unsigned int stdc_first_leading_one_us(unsigned short x){ if (x == 0) return 0u; return 16u - sbw16(x) + 1u; }
unsigned int stdc_first_trailing_zero_us(unsigned short x){ { unsigned short y = (unsigned short)(~x); if (y == 0) return 0u; return stz16(y) + 1u; } }
unsigned int stdc_first_trailing_one_us(unsigned short x){ if (x == 0) return 0u; return stz16(x) + 1u; }
bool stdc_has_single_bit_us(unsigned short x){ return x != 0 && (x & (x - 1u)) == 0; }
unsigned int stdc_bit_width_us(unsigned short x){ return sbw16(x); }
unsigned short stdc_bit_floor_us(unsigned short x){ if (x == 0) return 0; return (unsigned short)((unsigned short)1 << (sbw16(x) - 1u)); }
unsigned short stdc_bit_ceil_us(unsigned short x){ if (x <= 1u) return (unsigned short)1; return (unsigned short)((unsigned short)1 << sbw16((unsigned short)(x - 1u))); }

unsigned int stdc_leading_zeros_ui(unsigned int x){ return 32u - sbw32(x); }
unsigned int stdc_leading_ones_ui(unsigned int x){ return 32u - sbw32((unsigned int)(~x)); }
unsigned int stdc_trailing_zeros_ui(unsigned int x){ return stz32(x); }
unsigned int stdc_trailing_ones_ui(unsigned int x){ return stz32((unsigned int)(~x)); }
unsigned int stdc_count_zeros_ui(unsigned int x){ return 32u - spc32(x); }
unsigned int stdc_count_ones_ui(unsigned int x){ return spc32(x); }
unsigned int stdc_first_leading_zero_ui(unsigned int x){ { unsigned int y = (unsigned int)(~x); if (y == 0) return 0u; return 32u - sbw32(y) + 1u; } }
unsigned int stdc_first_leading_one_ui(unsigned int x){ if (x == 0) return 0u; return 32u - sbw32(x) + 1u; }
unsigned int stdc_first_trailing_zero_ui(unsigned int x){ { unsigned int y = (unsigned int)(~x); if (y == 0) return 0u; return stz32(y) + 1u; } }
unsigned int stdc_first_trailing_one_ui(unsigned int x){ if (x == 0) return 0u; return stz32(x) + 1u; }
bool stdc_has_single_bit_ui(unsigned int x){ return x != 0 && (x & (x - 1u)) == 0; }
unsigned int stdc_bit_width_ui(unsigned int x){ return sbw32(x); }
unsigned int stdc_bit_floor_ui(unsigned int x){ if (x == 0) return 0; return (unsigned int)((unsigned int)1 << (sbw32(x) - 1u)); }
unsigned int stdc_bit_ceil_ui(unsigned int x){ if (x <= 1u) return (unsigned int)1; return (unsigned int)((unsigned int)1 << sbw32((unsigned int)(x - 1u))); }

unsigned int stdc_leading_zeros_ul(unsigned long x){ return 64u - sbw64(x); }
unsigned int stdc_leading_ones_ul(unsigned long x){ return 64u - sbw64((unsigned long)(~x)); }
unsigned int stdc_trailing_zeros_ul(unsigned long x){ return stz64(x); }
unsigned int stdc_trailing_ones_ul(unsigned long x){ return stz64((unsigned long)(~x)); }
unsigned int stdc_count_zeros_ul(unsigned long x){ return 64u - spc64(x); }
unsigned int stdc_count_ones_ul(unsigned long x){ return spc64(x); }
unsigned int stdc_first_leading_zero_ul(unsigned long x){ { unsigned long y = (unsigned long)(~x); if (y == 0) return 0u; return 64u - sbw64(y) + 1u; } }
unsigned int stdc_first_leading_one_ul(unsigned long x){ if (x == 0) return 0u; return 64u - sbw64(x) + 1u; }
unsigned int stdc_first_trailing_zero_ul(unsigned long x){ { unsigned long y = (unsigned long)(~x); if (y == 0) return 0u; return stz64(y) + 1u; } }
unsigned int stdc_first_trailing_one_ul(unsigned long x){ if (x == 0) return 0u; return stz64(x) + 1u; }
bool stdc_has_single_bit_ul(unsigned long x){ return x != 0 && (x & (x - 1u)) == 0; }
unsigned int stdc_bit_width_ul(unsigned long x){ return sbw64(x); }
unsigned long stdc_bit_floor_ul(unsigned long x){ if (x == 0) return 0; return (unsigned long)((unsigned long)1 << (sbw64(x) - 1u)); }
unsigned long stdc_bit_ceil_ul(unsigned long x){ if (x <= 1u) return (unsigned long)1; return (unsigned long)((unsigned long)1 << sbw64((unsigned long)(x - 1u))); }

unsigned int stdc_leading_zeros_ull(unsigned long long x){ return 64u - sbw64(x); }
unsigned int stdc_leading_ones_ull(unsigned long long x){ return 64u - sbw64((unsigned long)(~x)); }
unsigned int stdc_trailing_zeros_ull(unsigned long long x){ return stz64(x); }
unsigned int stdc_trailing_ones_ull(unsigned long long x){ return stz64((unsigned long)(~x)); }
unsigned int stdc_count_zeros_ull(unsigned long long x){ return 64u - spc64(x); }
unsigned int stdc_count_ones_ull(unsigned long long x){ return spc64(x); }
unsigned int stdc_first_leading_zero_ull(unsigned long long x){ { unsigned long y = (unsigned long)(~x); if (y == 0) return 0u; return 64u - sbw64(y) + 1u; } }
unsigned int stdc_first_leading_one_ull(unsigned long long x){ if (x == 0) return 0u; return 64u - sbw64(x) + 1u; }
unsigned int stdc_first_trailing_zero_ull(unsigned long long x){ { unsigned long y = (unsigned long)(~x); if (y == 0) return 0u; return stz64(y) + 1u; } }
unsigned int stdc_first_trailing_one_ull(unsigned long long x){ if (x == 0) return 0u; return stz64(x) + 1u; }
bool stdc_has_single_bit_ull(unsigned long long x){ return x != 0 && (x & (x - 1u)) == 0; }
unsigned int stdc_bit_width_ull(unsigned long long x){ return sbw64(x); }
unsigned long long stdc_bit_floor_ull(unsigned long long x){ if (x == 0) return 0; return (unsigned long)((unsigned long)1 << (sbw64(x) - 1u)); }
unsigned long long stdc_bit_ceil_ull(unsigned long long x){ if (x <= 1u) return (unsigned long)1; return (unsigned long)((unsigned long)1 << sbw64((unsigned long)(x - 1u))); }

/* ---- C23 rotation (7.18) -------------------------------------------------
 *
 * goc has no rotate built-in, so each width is rotated with a width-bounded
 * pair of shifts. `s` is masked into [0, width) first: when the masked shift
 * is 0 the result is the identity (correct -- rotating by the full width is a
 * no-op), and the `(width - s)` companion shift is then never 0, so no operand
 * is ever shifted by its own width (which would be undefined). Operands are
 * unsigned, so right shifts are logical. */
static unsigned char rotl8(unsigned char x, unsigned int s) {
    unsigned int m = s & 7;
    if (m == 0) return x;            /* rotate by the full width is identity */
    return (unsigned char)((x << m) | (x >> (8 - m)));
}
static unsigned char rotr8(unsigned char x, unsigned int s) {
    unsigned int m = s & 7;
    if (m == 0) return x;
    return (unsigned char)((x >> m) | (x << (8 - m)));
}
static unsigned short rotl16(unsigned short x, unsigned int s) {
    unsigned int m = s & 15;
    if (m == 0) return x;
    return (unsigned short)((x << m) | (x >> (16 - m)));
}
static unsigned short rotr16(unsigned short x, unsigned int s) {
    unsigned int m = s & 15;
    if (m == 0) return x;
    return (unsigned short)((x >> m) | (x << (16 - m)));
}
static unsigned int rotl32(unsigned int x, unsigned int s) {
    unsigned int m = s & 31;
    if (m == 0) return x;
    return (x << m) | (x >> (32 - m));
}
static unsigned int rotr32(unsigned int x, unsigned int s) {
    unsigned int m = s & 31;
    if (m == 0) return x;
    return (x >> m) | (x << (32 - m));
}
static unsigned long long rotl64(unsigned long long x, unsigned int s) {
    unsigned int m = s & 63;
    if (m == 0) return x;
    return (x << m) | (x >> (64 - m));
}
static unsigned long long rotr64(unsigned long long x, unsigned int s) {
    unsigned int m = s & 63;
    if (m == 0) return x;
    return (x >> m) | (x << (64 - m));
}

unsigned char stdc_rotate_left_uc(unsigned char x, unsigned int s) { return rotl8(x, s); }
unsigned char stdc_rotate_right_uc(unsigned char x, unsigned int s) { return rotr8(x, s); }
unsigned short stdc_rotate_left_us(unsigned short x, unsigned int s) { return rotl16(x, s); }
unsigned short stdc_rotate_right_us(unsigned short x, unsigned int s) { return rotr16(x, s); }
unsigned int stdc_rotate_left_ui(unsigned int x, unsigned int s) { return rotl32(x, s); }
unsigned int stdc_rotate_right_ui(unsigned int x, unsigned int s) { return rotr32(x, s); }
unsigned long stdc_rotate_left_ul(unsigned long x, unsigned int s) { return (unsigned long)rotl64((unsigned long long)x, s); }
unsigned long stdc_rotate_right_ul(unsigned long x, unsigned int s) { return (unsigned long)rotr64((unsigned long long)x, s); }
unsigned long long stdc_rotate_left_ull(unsigned long long x, unsigned int s) { return rotl64(x, s); }
unsigned long long stdc_rotate_right_ull(unsigned long long x, unsigned int s) { return rotr64(x, s); }
