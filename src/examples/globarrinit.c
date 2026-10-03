#include <stdio.h>

// Regression test: a global/static pointer slot initialised by the address of
// another global array (the array decays to a pointer in C). goa has no data
// relocations, so goc must bind the address at startup -- otherwise the slot
// stays NULL. MDQuickViewer's highlight tables hit exactly this
// (extra_words = json_words). Covers both decay (words) and explicit address-of
// (&g) forms.
static const char *words[] = {"alpha", "beta", "gamma", NULL};
static int g = 42;
static int *pg = &g;

typedef struct {
    const char *name;
    const char *const *list;
    int n;
} Table;

static const Table def = {"t", words, 3};

int main(void) {
    if (def.list == 0) { printf("FAIL null list\n"); return 1; }
    if (pg != &g) { printf("FAIL null pg\n"); return 2; }
    printf("name=%s n=%d first=%s third=%s pg=%d\n",
           def.name, def.n, def.list[0], def.list[2], *pg);
    return 0;
}
