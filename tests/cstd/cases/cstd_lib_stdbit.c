/*
 * cstd_lib_stdbit.c -- goclib <stdbit.h> coverage.
 *
 * <stdbit.h> (C23) provides the generic macros stdc_bit_floor / stdc_has_single_bit
 * that dispatch on the argument type via _Generic, plus per-width backing
 * functions (stdc_bit_floor_uc / _us / _ui / _ul / _ull and the matching
 * stdc_has_single_bit_*). goc ships the header; mingw 16.x does not, so when
 * the header is absent a reference implementation is compiled in and the two
 * toolchains still cross-check goc's bit_floor / has_single_bit logic.
 *
 *   case1  stdc_bit_floor_* : largest power of two <= x (0 -> 0)
 *   case2  stdc_has_single_bit_* : true iff x is a power of two and nonzero
 */
#include <stdio.h>
#if __has_include(<stdbit.h>)
#include <stdbit.h>
#else
/* Reference implementation (pure C) for the cross-check on toolchains without
 * <stdbit.h>. Mirrors the C23 semantics exactly. sbw counts the number of
 * significant bits (the width), so the largest power of two <= x is
 * 1 << (sbw(x) - 1). */
static unsigned sbw8 (unsigned char x){unsigned n=0;while(x){x>>=1;n++;}return n;}
static unsigned sbw16(unsigned short x){unsigned n=0;while(x){x>>=1;n++;}return n;}
static unsigned sbw32(unsigned int x){unsigned n=0;while(x){x>>=1;n++;}return n;}
static unsigned sbw64(unsigned long long x){unsigned n=0;while(x){x>>=1;n++;}return n;}
static unsigned char     stdc_bit_floor_uc(unsigned char x){return x?(unsigned char)((unsigned char)1<<(sbw8(x)-1u)):0;}
static unsigned short    stdc_bit_floor_us(unsigned short x){return x?(unsigned short)((unsigned short)1<<(sbw16(x)-1u)):0;}
static unsigned int      stdc_bit_floor_ui(unsigned int x){return x?(unsigned int)((unsigned int)1<<(sbw32(x)-1u)):0;}
static unsigned long     stdc_bit_floor_ul(unsigned long x){return x?(unsigned long)((unsigned long)1<<(sbw64(x)-1u)):0;}
static unsigned long long stdc_bit_floor_ull(unsigned long long x){return x?(unsigned long long)((unsigned long long)1<<(sbw64(x)-1u)):0;}
static int stdc_has_single_bit_uc(unsigned char x){return x&&!(x&(x-1u));}
static int stdc_has_single_bit_us(unsigned short x){return x&&!(x&(x-1u));}
static int stdc_has_single_bit_ui(unsigned int x){return x&&!(x&(x-1u));}
static int stdc_has_single_bit_ul(unsigned long x){return x&&!(x&(x-1u));}
static int stdc_has_single_bit_ull(unsigned long long x){return x&&!(x&(x-1u));}
#endif
#include <stdint.h>

int main(void) {
    /* case1: bit_floor = greatest power of two not exceeding the argument. */
    printf("case1: bf_uc=%u bf_us=%u bf_ui=%u bf_ul=%lu bf_ull=%llu\n",
           (unsigned)stdc_bit_floor_uc(0x37),          /* 0x20 */
           (unsigned)stdc_bit_floor_us(0x1234),        /* 0x1000 */
           stdc_bit_floor_ui(0x10001),                 /* 0x10000 */
           stdc_bit_floor_ul(0x80000000UL),            /* 0x80000000 */
           (unsigned long long)stdc_bit_floor_ull(0xFFFFFFFFFFFFFFFFULL)); /* 0x8000000000000000 */

    /* zero floors to zero at every width */
    printf("case1b: bf_zero_uc=%u bf_zero_ull=%llu\n",
           (unsigned)stdc_bit_floor_uc(0),
           (unsigned long long)stdc_bit_floor_ull(0ULL));

    /* case2: has_single_bit = exactly one 1 bit. */
    printf("case2: hsb_uc=%d hsb_us=%d hsb_ui=%d hsb_ul=%d hsb_ull=%d\n",
           stdc_has_single_bit_uc(0x80) ? 1 : 0,
           stdc_has_single_bit_us(0x8000) ? 1 : 0,
           stdc_has_single_bit_ui(0x80000000u) ? 1 : 0,
           stdc_has_single_bit_ul(0x80000000UL) ? 1 : 0,
           stdc_has_single_bit_ull(0x8000000000000000ULL) ? 1 : 0);

    /* not-a-power-of-two and zero are both false */
    printf("case2b: hsb_notpow=%d hsb_zero=%d\n",
           stdc_has_single_bit_uc(0x83) ? 1 : 0,
           stdc_has_single_bit_uc(0) ? 1 : 0);
    return 0;
}
