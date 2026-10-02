/* ============================================================
   c89_lib_limits.c - limits.h and float.h macro values plus sizeof summary
   Standard   : ISO/IEC 9899:1990 (C89) 5.2.4.2.1 limits, 5.2.4.2.2 float
   Strategy   : case1 char and signed char and unsigned char limits;
                case2 short and int and long limits.  goc is LP64 (long 8 bytes)
                while Windows gcc is LLP64 (long 4 bytes), so long min max and
                unsigned long max and sizeof(long) differ by platform ABI.
                case3 float.h FLT and DBL limits; case4 sizeof summary.
                each case printf distinct, gcc -std=c89 diff
   Status     : PARTIAL: goc LP64 (long 8) vs gcc LLP64 (long 4); rest matches (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>
#include <limits.h>
#include <float.h>

int main(void) {
    printf("case1: CHAR_BIT=%d SCHAR_MIN=%d SCHAR_MAX=%d UCHAR_MAX=%d\n",
           CHAR_BIT, SCHAR_MIN, SCHAR_MAX, UCHAR_MAX);
    printf("case1b: CHAR_MIN=%d CHAR_MAX=%d\n", CHAR_MIN, CHAR_MAX);
    printf("case2: SHRT=%d/%d USHRT=%d INT=%d/%d UINT=%u\n",
           SHRT_MIN, SHRT_MAX, USHRT_MAX, INT_MIN, INT_MAX, UINT_MAX);
    printf("case2b: LONG=%ld/%ld ULONG=%lu\n", LONG_MIN, LONG_MAX, ULONG_MAX);
    printf("case3: FLT_MAX=%e FLT_MIN=%e FLT_EPS=%e FLT_DIG=%d FLT_MANT=%d\n",
           FLT_MAX, FLT_MIN, FLT_EPSILON, FLT_DIG, FLT_MANT_DIG);
    printf("case3b: DBL_MAX=%e DBL_MIN=%e DBL_EPS=%e DBL_DIG=%d DBL_MANT=%d\n",
           DBL_MAX, DBL_MIN, DBL_EPSILON, DBL_DIG, DBL_MANT_DIG);
    printf("case4: sizes char=%d short=%d int=%d long=%d ptr=%d float=%d double=%d\n",
           (int)sizeof(char), (int)sizeof(short), (int)sizeof(int),
           (int)sizeof(long), (int)sizeof(char *),
           (int)sizeof(float), (int)sizeof(double));
    return 0;
}
