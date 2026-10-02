/* ============================================================
   c11_uchar.c - <uchar.h>, char16_t/char32_t, u"" / U"" literals
   Standard   : ISO/IEC 9899:2011 (C11) 7.28
   Strategy   : UNSUPPORTED in goc: uchar.h is skipped (note on stderr),
               char16_t/char32_t undefined, u"" / U"" prefix not lexed.
               gcc -std=c11 compiles and returns 0.
   Status     : UNSUPPORTED (verified 2026-10-02, goc vs gcc -std=c11)
   ============================================================ */
#include <uchar.h>

static const char16_t s16[] = u"ab";
static const char32_t s32[] = U"XYZ";

int main(void) {
    char16_t c16 = u'a';
    char32_t c32 = U'Z';
    return (c16 == 0x61 && c32 == 0x5a &&
            s16[0] == 0x61 && s16[1] == 0x62 && s16[2] == 0 &&
            s32[0] == 0x58 && s32[1] == 0x59 && s32[2] == 0x5a && s32[3] == 0)
        ? 0 : 1;
}