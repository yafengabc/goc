/* C23 feature: #if controlling expression -- division by zero (ill-formed)
 * Clause:     C23 6.10.1 "Conditional inclusion" (evaluating 1/0 in a #if is
 *             not permitted; a conforming compiler must reject the translation unit)
 * Strategy:   place `1 / 0` directly in a #if condition. gcc -std=c2x must reject
 *             it; goc's stance is recorded verbatim in status/group_B.md.
 * Status:     PENDING
 * EXPECT: REJECT
 */
#include <stdio.h>

#if 1 / 0
int main(void) { return 0; }
#else
int main(void) { return 0; }
#endif
