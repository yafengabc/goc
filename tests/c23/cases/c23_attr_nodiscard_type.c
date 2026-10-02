/* C23 feature: attribute [[nodiscard]] applied to a struct / enum TYPE
 * Clause:     C23 6.7.13 (nodiscard attribute on an enumeration or struct/union)
 * Strategy:   declare a struct and an enum carrying [[nodiscard]], discard a
 *             prvalue returned from a function of that type -> gcc warns
 *             (-Wunused-result). This isolates the type-level placement, which
 *             goc's parser currently rejects; the function-level nodiscard tests
 *             live in c23_attr_nodiscard.c. gcc -std=c2x must compile this file.
 * Status:     FAIL (goc parse gap: attribute-on-type unsupported)
 * EXPECT: PASS
 */
#include <stdio.h>

struct [[nodiscard]] Node { int v; };
struct Node make_node(int x) { struct Node n = { x }; return n; }

enum [[nodiscard]] E_OK { E_yes, E_no };
enum E_OK get_ok(void) { return E_yes; }

int main(void) {
    int passed = 0, total = 0;

    /* case1: struct [[nodiscard]] prvalue discarded -> gcc warns */
    ++total;
    make_node(7);
    printf("case1: discarded nodiscard-struct prvalue (gcc warns)\n");
    passed++;

    /* case2: enum [[nodiscard]] prvalue discarded -> gcc warns */
    ++total;
    get_ok();
    printf("case2: discarded nodiscard-enum prvalue (gcc warns)\n");
    passed++;

    /* case3: struct nodiscard result assigned -> gcc clean */
    ++total;
    struct Node n = make_node(8);
    printf("case3: assigned node.v=%d\n", n.v);
    if (n.v == 8) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
