/* C23 feature: #warning "message"
 * Clause:     C23 6.10.5 "Error directive" (#warning)
 * Strategy:   emit #warning with a message; compilation must still succeed and the
 *             program must run. The warning text itself is captured on each
 *             compiler's stderr (see status/group_B.md for the verbatim strings).
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

#warning "c23_warning probe: this preprocessor warning must not stop the build"

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: #warning emitted but build+run succeeded\n", total);
    passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
