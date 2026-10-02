/* C23 feature: <uchar.h> Unicode characters and conversion
 * Clause:     C23 7.28 <uchar.h>
 * Strategy:   include <uchar.h> directly and exercise char16_t / char32_t and the
 *             mbrtoc16 / c16rtomb / mbrtoc32 / c32rtomb conversion round-trips on an
 *             ASCII input. mingw-w64 UCRT64 ships this header (verified: it declares
 *             char16_t, char32_t, mbrtoc16, c16rtomb, mbrtoc32, c32rtomb; it does NOT
 *             provide char8_t / mbrtoc8, so those are intentionally absent). goc does
 *             not ship <uchar.h>; it should skip the header and then fail on the types.
 * Status:     UNSUPPORTED (goc lacks <uchar.h>)
 * EXPECT: UNSUPPORTED
 */
#include <stdio.h>
#include <string.h>
#include <uchar.h>
#include <wchar.h>

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: sizeof(char16_t)=%d sizeof(char32_t)=%d\n",
           total, (int)sizeof(char16_t), (int)sizeof(char32_t));
    if (sizeof(char16_t) == 2 && sizeof(char32_t) == 4) passed++;

    ++total;
    char16_t h = 0x41;
    char32_t w = 0x41;
    printf("case%d: char16_t=0x%04x char32_t=0x%08x\n",
           total, (unsigned)h, (unsigned)w);
    if (h == 0x41 && w == 0x41u) passed++;

    /* mbrtoc16 / c16rtomb round-trip on ASCII 'A' */
    ++total;
    {
        mbstate_t ps;
        char16_t c16 = 0;
        memset(&ps, 0, sizeof(ps));
        size_t n = mbrtoc16(&c16, "A", 1, &ps);
        char back[8];
        memset(&ps, 0, sizeof(ps));
        size_t m = c16rtomb(back, c16, &ps);
        printf("case%d: mbrtoc16 n=%llu c16=0x%04x c16rtomb m=%llu back='%c'\n",
               total, (unsigned long long)n, (unsigned)c16,
               (unsigned long long)m, back[0]);
        if (n == 1 && c16 == 0x41 && m == 1 && back[0] == 'A') passed++;
    }

    /* mbrtoc32 / c32rtomb round-trip on ASCII 'A' */
    ++total;
    {
        mbstate_t ps;
        char32_t c32 = 0;
        memset(&ps, 0, sizeof(ps));
        size_t n = mbrtoc32(&c32, "A", 1, &ps);
        char back[8];
        memset(&ps, 0, sizeof(ps));
        size_t m = c32rtomb(back, c32, &ps);
        printf("case%d: mbrtoc32 n=%llu c32=0x%08x c32rtomb m=%llu back='%c'\n",
               total, (unsigned long long)n, (unsigned)c32,
               (unsigned long long)m, back[0]);
        if (n == 1 && c32 == 0x41u && m == 1 && back[0] == 'A') passed++;
    }

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
