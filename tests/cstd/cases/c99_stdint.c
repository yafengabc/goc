/* ============================================================
   c99_stdint.c - stdint.h / inttypes.h fixed-width types (C99 7.18)
   Standard   : ISO/IEC 9899:1999 (C99) 7.18
   Strategy   : int8..int64/uint64/intptr_t, INT64_C/UINT64_C,
                PRId64/PRIu64/PRIdPTR format macros. The intptr_t demo prints
                a fixed constant: raw addresses differ between runtimes (goc
                loads low, the LLP64 gcc loads high) and cannot be compared
                byte for byte; intptr_t is still exercised as a pointer type
                through the boolean check on (intptr_t)&i8.
   Status     : PASS (fixed 2026-10-02, goc LP64 vs gcc LLP64 -std=c99)
   ============================================================ */
#include <stdio.h>
#include <stdint.h>
#include <inttypes.h>
int main(void) {
    int8_t  i8  = INT8_C(-7);
    int16_t i16 = INT16_C(12345);
    int32_t i32 = INT32_C(123456789);
    int64_t i64 = INT64_C(1234567890123);
    uint64_t u64 = UINT64_C(1234567890123);
    intptr_t ip = (intptr_t)&i8;
    printf("case1: %" PRId8 " %" PRId16 "\n", i8, i16);
    printf("case2: %" PRId32 " %" PRId64 "\n", i32, i64);
    printf("case3: %" PRIu64 " %" PRIdPTR "\n", u64, (intptr_t)0x1234567890LL);
    if (ip == 0) printf("bad intptr\n");
    return 0;
}