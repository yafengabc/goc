/* C23 feature: K&R (old-style) function definitions removed in C23
 * Clause:     C23 removed legacy K&R function definition (6.7.1/6.9.1 removed)
 * Strategy:   one K&R-style definition plus a call from main. goc must REJECT this
 *             (C23 removed K&R definitions). gcc -std=c2x only emits
 *             -Wold-style-definition warning and still accepts it.
 * Status:     GOC-REJECT (PASS when goc rejects)
 * EXPECT: GOC-REJECT
 */
int add(a, b)
int a;
int b;
{
    return a + b;
}

int main(void) {
    return add(40, 2);
}
