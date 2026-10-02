/* C23 feature: attribute [[fallthrough]] used OUTSIDE any switch (negative test)
 * Clause:     C23 6.7.13 (the fallthrough attribute is only valid inside a switch)
 * Strategy:   place [[fallthrough]]; at a point that is not a case clause of a
 *             switch. A conforming C23 compiler must reject it. gcc -std=c2x does
 *             ("error: invalid use of attribute 'fallthrough'"). This file records
 *             whether goc also rejects the misplaced attribute.
 * Status:     FAIL (goc leniently accepts fallthrough outside switch)
 * EXPECT: REJECT
 */
void outside(int x) {
    [[fallthrough]];
    (void)x;
}

int main(void) {
    return 0;
}
