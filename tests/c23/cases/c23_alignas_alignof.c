/* C23 feature: alignas / alignof and the _Alignas / _Alignof spellings
 * Clause:     C23 6.2.8 alignment; 6.7.5 alignment specifiers
 * Strategy:   alignof on scalars, both spellings, alignas on variables /
 *             members / arrays / struct types, and verify the actual object
 *             address (full 64-bit, via unsigned long long) is a multiple of
 *             the requested alignment.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>
#include <stddef.h>

_Alignas(16) int g16;
alignas(32) char g32[64];

struct SAligned {
    char a;
    alignas(16) int b;
    char c;
};

union UAligned {
    char c;
    int i;
};

int main(void) {
    int passed = 0, total = 0;

    ++total;
    int ai = (int)alignof(int);
    int ac = (int)_Alignof(char);
    int ad = (int)alignof(double);
    int aq = (int)_Alignof(long long);
    printf("case%d: alns int=%d char=%d double=%d llong=%d\n",
           total, ai, ac, ad, aq);
    if (ac == 1 && ai >= 4 && ad >= ai && aq >= 4) passed++;

    ++total;
    alignas(16) int l16;
    int off16 = (int)((unsigned long long)&l16 % 16ULL);
    int al16 = (int)alignof(l16);
    printf("case%d: local alignas(16) offmod16=%d alignof=%d\n", total, off16, al16);
    if (off16 == 0 && al16 >= 16) passed++;

    ++total;
    int goff = (int)((unsigned long long)&g16 % 16ULL);
    int goff32 = (int)((unsigned long long)&g32 % 32ULL);
    printf("case%d: global alignas(16) offmod16=%d  alignas(32) offmod32=%d\n",
           total, goff, goff32);
    if (goff == 0 && goff32 == 0) passed++;

    ++total;
    _Alignas(double) char dc;
    int adc = (int)alignof(dc);
    int adbl = (int)_Alignof(double);
    printf("case%d: _Alignas(double) char alignof=%d want=%d\n", total, adc, adbl);
    if (adc == adbl) passed++;

    ++total;
    struct SAligned s;
    int boff = (int)((unsigned long long)&s.b % 16ULL);
    printf("case%d: struct member alignas(16) offmod16=%d\n", total, boff);
    if (boff == 0) passed++;

    ++total;
    int aStruct = (int)alignof(struct SAligned);
    int aUnion = (int)alignof(union UAligned);
    printf("case%d: alignof(struct)=%d alignof(union)=%d\n", total, aStruct, aUnion);
    if (aStruct >= 4 && aUnion >= 4) passed++;

    ++total;
    alignas(64) char big[128];
    int bmod = (int)((unsigned long long)&big % 64ULL);
    int albig = (int)alignof(big);
    printf("case%d: array alignas(64) offmod64=%d alignof=%d\n", total, bmod, albig);
    if (bmod == 0 && albig >= 64) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
