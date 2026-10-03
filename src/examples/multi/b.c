/* Multi-file build, unit B: same static names as unit A (different values and
 * bodies), plus the externals the two units share. */
#include <stdio.h>

static int base[3] = {1, 2, 3};
static int scale(int x) { return x + 100; }

extern int shared_total;
int a_part(int i);
void a_report(void);

int main(void) {
    shared_total = 7;
    printf("b: %d %d\n", scale(5), base[2]);
    a_report();
    printf("shared: %d\n", a_part(0) + shared_total);
    return 0;
}
