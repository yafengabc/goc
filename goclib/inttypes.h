#ifndef GOC_INTTYPES_H
#define GOC_INTTYPES_H

#include <stdint.h>

/* goc inttypes.h -- PRI/SCN macros (C99 7.8.1), LP64 spellings.
 *
 * goc is LP64 (long = 8 bytes): int64_t/uint64_t/intptr_t are long, so the
 * format spellings carry "l" (and intmax_t, long long, carries "ll"). On the
 * LLP64 gcc used for comparison they carry "ll" instead; the digit output is
 * identical either way.
 */

#define PRId8 "d"
#define PRId16 "d"
#define PRId32 "d"
#define PRId64 "ld"
#define PRIdPTR "ld"
#define PRIdMAX "lld"

#define PRIi8 "i"
#define PRIi16 "i"
#define PRIi32 "i"
#define PRIi64 "li"
#define PRIiPTR "li"
#define PRIiMAX "lli"

#define PRIu8 "u"
#define PRIu16 "u"
#define PRIu32 "u"
#define PRIu64 "lu"
#define PRIuPTR "lu"
#define PRIuMAX "llu"

#define PRIx8 "x"
#define PRIx16 "x"
#define PRIx32 "x"
#define PRIx64 "lx"
#define PRIxPTR "lx"
#define PRIxMAX "llx"

#define PRIX8 "X"
#define PRIX16 "X"
#define PRIX32 "X"
#define PRIX64 "lX"
#define PRIXPTR "lX"
#define PRIXMAX "llX"

#define PRIo8 "o"
#define PRIo16 "o"
#define PRIo32 "o"
#define PRIo64 "lo"
#define PRIoPTR "lo"
#define PRIoMAX "llo"

#define SCNd8 "hhd"
#define SCNd16 "hd"
#define SCNd32 "d"
#define SCNd64 "ld"
#define SCNdPTR "ld"
#define SCNdMAX "lld"

#define SCNi8 "hhi"
#define SCNi16 "hi"
#define SCNi32 "i"
#define SCNi64 "li"
#define SCNiPTR "li"
#define SCNiMAX "lli"

#define SCNu8 "hhu"
#define SCNu16 "hu"
#define SCNu32 "u"
#define SCNu64 "lu"
#define SCNuPTR "lu"
#define SCNuMAX "llu"

#define SCNx8 "hhx"
#define SCNx16 "hx"
#define SCNx32 "x"
#define SCNx64 "lx"
#define SCNxPTR "lx"
#define SCNxMAX "llx"

#define SCNo8 "hho"
#define SCNo16 "ho"
#define SCNo32 "o"
#define SCNo64 "lo"
#define SCNoPTR "lo"
#define SCNoMAX "llo"

#endif /* GOC_INTTYPES_H */
