#ifndef GOC_LIMITS_H
#define GOC_LIMITS_H

/* goc limits.h -- implementation-defined widths of the integer types.
 *
 * goc's model is fixed on both targets it emits for (Windows x64 and Linux
 * x86-64): char 8 bits, short 16, int 32, long 64, long long 64, and plain
 * char is signed. These are therefore plain constants, not the
 * <limits.h>-per-ABI values a hosted compiler would compute.
 *
 * Two spellings are worth a note:
 *   - *_MIN is written as "(-N - 1)" rather than "-N-1" because the literal
 *     N+1 does not fit the signed type being described -- folding it as a
 *     negative literal would need a wider type than the macro's own type.
 *   - ULONG_MAX does not fit goc's 64-bit constant folder, which keeps every
 *     integer constant in an int64. It folds to -1, i.e. the same 64-bit
 *     pattern, so storing it is exact; only a *comparison* against it would
 *     read as signed, which no hosted program relies on either.
 */

#define CHAR_BIT 8

#define SCHAR_MIN (-128)
#define SCHAR_MAX 127
#define UCHAR_MAX 255

/* plain char is signed in goc */
#define CHAR_MIN  (-128)
#define CHAR_MAX  127

#define SHRT_MIN  (-32768)
#define SHRT_MAX  32767
#define USHRT_MAX 65535

#define INT_MIN  (-2147483647 - 1)
#define INT_MAX  2147483647
#define UINT_MAX 4294967295U

#define LONG_MIN (-9223372036854775807L - 1)
#define LONG_MAX 9223372036854775807L
#define ULONG_MAX 18446744073709551615UL

#define LLONG_MIN (-9223372036854775807LL - 1)
#define LLONG_MAX 9223372036854775807LL

/* ---- C23 width macros (7.10.1) -----------------------------------------
 * Width = value bits. goc is LP64, so LONG_WIDTH/ULONG_WIDTH are 64 here
 * (the LLP64 gcc they are compared against reports 32 -- a documented,
 * intentional ABI difference). BOOL_WIDTH mirrors the value-bit definition
 * and BITINT_MAXWIDTH mirrors the standard's UINT_MAX upper bound; goc's
 * own _BitInt implementation supports up to 1024 bits. */
#define CHAR_WIDTH   8
#define SCHAR_WIDTH  8
#define UCHAR_WIDTH  8
#define SHRT_WIDTH   16
#define USHRT_WIDTH  16
#define INT_WIDTH    32
#define UINT_WIDTH   32
#define LONG_WIDTH   64
#define ULONG_WIDTH  64
#define LLONG_WIDTH  64
#define ULLONG_WIDTH 64
#define BOOL_WIDTH   1
#define BITINT_MAXWIDTH 65535

#endif /* GOC_LIMITS_H */
