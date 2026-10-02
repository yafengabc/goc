/* C23 feature: attribute [[nodiscard]] on functions (and the message form)
 * Clause:     C23 6.7.13 (nodiscard attribute)
 * Strategy:   discard a nodiscard function result -> compiler must warn (gcc
 *             -Wunused-result; goc per roadmap); assign it -> no warning. Cover
 *             the "reason" message form, and the (void)-cast suppression idiom.
 *             Type-level [[nodiscard]] (struct/enum) is a separate file because
 *             goc currently cannot parse attribute-on-type (see
 *             c23_attr_nodiscard_type.c). Warnings go to compiler stderr and are
 *             recorded in the status document; the program always compiles+runs.
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>

[[nodiscard]] int need_val(int x) { return x + 1; }

[[nodiscard("must check result")]] int need_reason(int x) { return x * 2; }

int main(void) {
    int passed = 0, total = 0;

    /* case1: discard a nodiscard function result -> warning */
    ++total;
    need_val(10);
    printf("case1: discarded nodiscard return (warning expected)\n");
    passed++;

    /* case2: assign the result -> no warning */
    ++total;
    int r = need_val(20);
    printf("case2: assigned=%d\n", r);
    if (r == 21) passed++;

    /* case3: message form, assigned -> no warning */
    ++total;
    int rr = need_reason(5);
    printf("case3: reason-form assigned=%d\n", rr);
    if (rr == 10) passed++;

    /* case4: message form, discarded -> warning carrying the reason text */
    ++total;
    need_reason(6);
    printf("case4: discarded reason-form (warning+message expected)\n");
    passed++;

    /* case5: explicit (void) cast silences the nodiscard diagnostic */
    ++total;
    (void)need_val(30);
    printf("case5: (void)-cast suppression, no warning expected\n");
    passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
