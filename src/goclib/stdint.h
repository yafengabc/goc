#ifndef GOC_STDINT_H
#define GOC_STDINT_H

/* goc stdint.h -- fixed-width integer types (C99 7.18).
 *
 * goc is LP64: long is 8 bytes, so int64_t/uint64_t/intptr_t fall on long,
 * consistent with size_t and the pointer model. The MSYS2 gcc used for
 * comparison is LLP64 (long=4) and puts them on long long -- the PRI/SCN
 * spellings live in inttypes.h and keep printf/scanf output identical on
 * either side, so portable code should format through those macros.
 */

typedef signed char            int8_t;
typedef short                  int16_t;
typedef int                    int32_t;
typedef long                   int64_t;
typedef unsigned char          uint8_t;
typedef unsigned short         uint16_t;
typedef unsigned int           uint32_t;
typedef unsigned long          uint64_t;

typedef long                   intptr_t;
typedef unsigned long          uintptr_t;

typedef long long              intmax_t;
typedef unsigned long long     uintmax_t;

#define INT8_MIN  (-128)
#define INT8_MAX  127
#define UINT8_MAX 255
#define INT16_MIN (-32767 - 1)
#define INT16_MAX 32767
#define UINT16_MAX 65535
#define INT32_MIN (-2147483647 - 1)
#define INT32_MAX 2147483647
#define UINT32_MAX 4294967295U
#define INT64_MIN (-9223372036854775807L - 1)
#define INT64_MAX 9223372036854775807L
#define UINT64_MAX 18446744073709551615UL
#define INTMAX_MIN (-9223372036854775807LL - 1)
#define INTMAX_MAX 9223372036854775807LL
#define UINTMAX_MAX 18446744073709551615ULL
#define INTPTR_MIN (-9223372036854775807L - 1)
#define INTPTR_MAX 9223372036854775807L
#define UINTPTR_MAX 18446744073709551615UL

#define INT8_C(c)   (c)
#define INT16_C(c)  (c)
#define INT32_C(c)  (c)
#define INT64_C(c)  (c##L)
#define UINT8_C(c)  (c)
#define UINT16_C(c) (c)
#define UINT32_C(c) (c##U)
#define UINT64_C(c) (c##UL)
#define INTMAX_C(c) (c##LL)
#define UINTMAX_C(c)(c##ULL)

#endif /* GOC_STDINT_H */
