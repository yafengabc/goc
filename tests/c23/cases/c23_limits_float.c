/* C23 feature: <limits.h>/<float.h> C23 width & normalization macros; long double downgrade
 * Clause:     C23 7.10.1 (widths), 7.7.1/7.7.2 (FLT_NORM_MAX, FLT_IS_IEC_60559)
 * Strategy:   probe each C23 macro with #ifdef and print either its value (small ints)
 *             or "undef". goc's goclib limits.h/float.h are known to ship only the
 *             classic C89/C99 macros (CHAR_BIT, *_MIN/_MAX, LDBL_* aliased to DBL_*)
 *             and NONE of the C23 *_WIDTH / FLT_NORM_MAX / FLT_IS_IEC_60559 macros;
 *             gcc mingw's stance is measured here. Long-double downgrade is checked via
 *             the __goc_long_double_is_double marker, LDBL_MANT_DIG/LDBL_MAX_EXP,
 *             sizeof(long double), and an exact arithmetic expression.
 * Status:     PASS (probe; per-macro defined/undefined recorded in status doc)
 * EXPECT: PASS
 */
#include <stdio.h>
#include <limits.h>
#include <float.h>

int main(void) {
    /* ---- C23 integer width macros ---- */
#ifdef CHAR_WIDTH
    printf("CHAR_WIDTH       %d\n", (int)CHAR_WIDTH);
#else
    printf("CHAR_WIDTH       undef\n");
#endif
#ifdef SCHAR_WIDTH
    printf("SCHAR_WIDTH      %d\n", (int)SCHAR_WIDTH);
#else
    printf("SCHAR_WIDTH      undef\n");
#endif
#ifdef UCHAR_WIDTH
    printf("UCHAR_WIDTH      %d\n", (int)UCHAR_WIDTH);
#else
    printf("UCHAR_WIDTH      undef\n");
#endif
#ifdef SHRT_WIDTH
    printf("SHRT_WIDTH       %d\n", (int)SHRT_WIDTH);
#else
    printf("SHRT_WIDTH       undef\n");
#endif
#ifdef USHRT_WIDTH
    printf("USHRT_WIDTH      %d\n", (int)USHRT_WIDTH);
#else
    printf("USHRT_WIDTH      undef\n");
#endif
#ifdef INT_WIDTH
    printf("INT_WIDTH        %d\n", (int)INT_WIDTH);
#else
    printf("INT_WIDTH        undef\n");
#endif
#ifdef UINT_WIDTH
    printf("UINT_WIDTH       %d\n", (int)UINT_WIDTH);
#else
    printf("UINT_WIDTH       undef\n");
#endif
#ifdef LONG_WIDTH
    printf("LONG_WIDTH       %d\n", (int)LONG_WIDTH);
#else
    printf("LONG_WIDTH       undef\n");
#endif
#ifdef ULONG_WIDTH
    printf("ULONG_WIDTH      %d\n", (int)ULONG_WIDTH);
#else
    printf("ULONG_WIDTH      undef\n");
#endif
#ifdef LLONG_WIDTH
    printf("LLONG_WIDTH      %d\n", (int)LLONG_WIDTH);
#else
    printf("LLONG_WIDTH      undef\n");
#endif
#ifdef ULLONG_WIDTH
    printf("ULLONG_WIDTH     %d\n", (int)ULLONG_WIDTH);
#else
    printf("ULLONG_WIDTH     undef\n");
#endif
#ifdef BOOL_WIDTH
    printf("BOOL_WIDTH       %d\n", (int)BOOL_WIDTH);
#else
    printf("BOOL_WIDTH       undef\n");
#endif
#ifdef BITINT_MAXWIDTH
    printf("BITINT_MAXWIDTH  %d\n", (int)BITINT_MAXWIDTH);
#else
    printf("BITINT_MAXWIDTH  undef\n");
#endif

    /* ---- C23 float normalization / IEC macros ---- */
#ifdef FLT_NORM_MAX
    printf("FLT_NORM_MAX     defined\n");
#else
    printf("FLT_NORM_MAX     undef\n");
#endif
#ifdef DBL_NORM_MAX
    printf("DBL_NORM_MAX     defined\n");
#else
    printf("DBL_NORM_MAX     undef\n");
#endif
#ifdef LDBL_NORM_MAX
    printf("LDBL_NORM_MAX    defined\n");
#else
    printf("LDBL_NORM_MAX    undef\n");
#endif
#ifdef FLT_IS_IEC_60559
    printf("FLT_IS_IEC_60559 %d\n", (int)FLT_IS_IEC_60559);
#else
    printf("FLT_IS_IEC_60559 undef\n");
#endif
#ifdef DBL_IS_IEC_60559
    printf("DBL_IS_IEC_60559 %d\n", (int)DBL_IS_IEC_60559);
#else
    printf("DBL_IS_IEC_60559 undef\n");
#endif
#ifdef LDBL_IS_IEC_60559
    printf("LDBL_IS_IEC_60559 %d\n", (int)LDBL_IS_IEC_60559);
#else
    printf("LDBL_IS_IEC_60559 undef\n");
#endif

    /* ---- long double downgrade ---- */
#ifdef __goc_long_double_is_double
    printf("goc_long_double_is_double defined\n");
#else
    printf("goc_long_double_is_double undef\n");
#endif
#ifdef LDBL_MANT_DIG
    printf("LDBL_MANT_DIG    %d\n", (int)LDBL_MANT_DIG);
#else
    printf("LDBL_MANT_DIG    undef\n");
#endif
#ifdef LDBL_MAX_EXP
    printf("LDBL_MAX_EXP     %d\n", (int)LDBL_MAX_EXP);
#else
    printf("LDBL_MAX_EXP     undef\n");
#endif
    printf("sizeof(long double) %d\n", (int)sizeof(long double));
    {
        long double a = 1.0L, b = 2.0L;
        long double c = a + b;
        printf("ld arithmetic 1.0L+2.0L==3.0L => %d\n", (int)(c == 3.0L));
    }

    return 0;
}
