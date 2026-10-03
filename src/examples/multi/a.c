/* Multi-file build, unit A: this file's statics are private to it, so its
 * base[] and scale() may share a name with unit B's without colliding --
 * each one is renamed to a per-file symbol when the units are merged. */
#include <stdio.h>

static int base[3] = {10, 20, 30};
static int scale(int x) { return x * 2; }

int shared_total = 0;

int a_part(int i) { return scale(base[i]); }

void a_report(void) { printf("a: %d %d\n", a_part(1), shared_total); }
