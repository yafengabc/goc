/* C23 feature: #elifdef / #elifndef
 * Clause:     C23 6.10.1 "Conditional inclusion"
 * Strategy:   exercise #elifdef and #elifndef in chains, mixed with plain
 *             #ifdef/#ifndef, nested inside a #elifdef branch, and the #else
 *             fallback when every condition is false.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

#define DEF1 1
#define DEF3 1
/* DEF2 and DEF0 intentionally left undefined */

int main(void) {
    int passed = 0, total = 0;
    int ok;
    const char *r;

    /* case1: #elifdef chain, first defined macro wins */
    ++total;
#if defined(NEVER_TAKEN)
    r = "a";
#elifdef DEF1
    r = "d1";
#elifdef DEF2
    r = "d2";
#else
    r = "other";
#endif
    printf("case%d: elifdef-chain=%s\n", total, r);
    ok = (r[0]=='d');
    if (ok) passed++;

    /* case2: #elifndef chain, first undefined macro wins */
    ++total;
#if 0
    r = "a";
#elifndef DEF2
    r = "undef-d2";
#elifndef DEF1
    r = "undef-d1";
#else
    r = "e";
#endif
    printf("case%d: elifndef-chain=%s\n", total, r);
    ok = (r[0]=='u');
    if (ok) passed++;

    /* case3: mixed #ifdef/#ifndef/#elifdef/#elifndef in one chain */
    ++total;
#ifdef DEF0
    r = "ifdef-def0";
#elifndef DEF3
    r = "ndef-def3";
#elifdef DEF2
    r = "def-def2";
#elifndef DEF2
    r = "ndef-def2";
#else
    r = "e";
#endif
    printf("case%d: mixed-chain=%s\n", total, r);
    ok = (r[0]=='n');
    if (ok) passed++;

    /* case4: nesting a conditional inside a #elifdef branch */
    ++total;
#if 0
    r = "no";
#elifdef DEF1
#  if 0
    r = "inner-no";
#  elifdef DEF3
    r = "inner-d3";
#  else
    r = "inner-e";
#  endif
#else
    r = "outer-e";
#endif
    printf("case%d: nested=%s\n", total, r);
    ok = (r[0]=='i');
    if (ok) passed++;

    /* case5: every condition false -> #else fallback */
    ++total;
#if defined(ZZZ)
    r = "z";
#elifdef ZZZ2
    r = "z2";
#elifndef DEF1
    r = "ndef-d1";
#else
    r = "all-false";
#endif
    printf("case%d: else-fallback=%s\n", total, r);
    ok = (r[0]=='a');
    if (ok) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
