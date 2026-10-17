/* ---------------------------------------------------------------------------
 * Regression: taking a member of a struct-returning call *in expression
 * position* used to corrupt the next struct-returning call (fixed; this file
 * stays as the byte-exact harness that caught it).
 *
 *     MIX(goc_tf_neg(a).hi);        <-- the trigger
 *     r = goc_tf_from_double(a.lo); <-- used to return a pointer, not a value
 *
 * Correct output (gcc, gocl, and goc since the structret fix):
 *
 *     r = 3ece32d23193c687.1000000000000000   a.lo=2ce32d23193c6871
 *
 * Pre-fix, goc native gave:
 *
 *     r = 0000000140014000.0000000140014000   a.lo=2ce32d23193c6871
 *
 * 0x140014000 is an address inside the PE image (base 0x140000000): the
 * member load left the call's result-buffer claim set, the compound
 * assignment rolled tmpDepth back over it, and the statement boundary's
 * release subtracted the slots twice -- negative slot indices then aliased
 * live locals, and printf's argument parks wrote a format-string pointer
 * into r's own bytes. Fixed by releasing the buffer at the scalar member
 * load itself (releaseCallResultBuffer, codegen.go); the shape that
 * reproduces it is layout-dependent, which is why this exact statement
 * sequence is pinned here byte for byte.
 *
 * Workaround (still valid): assign to a variable first
 * (`r = goc_tf_neg(a); ... r.hi`). tests/portability_cases/fp128.c does that
 * and says why in a comment. The wider position battery lives in
 * structret.c next to this file.
 *
 * ------------------------------------------------------------------------- */

#include <stdio.h>

typedef struct {
    unsigned long long lo;
    unsigned long long hi;
} goc_tf128;

goc_tf128 goc_tf_add(goc_tf128 a, goc_tf128 b);
goc_tf128 goc_tf_mul(goc_tf128 a, goc_tf128 b);
goc_tf128 goc_tf_neg(goc_tf128 a);
goc_tf128 goc_tf_from_double(unsigned long long bits);
int        goc_tf_cmp(goc_tf128 a, goc_tf128 b);

int main(void) {
    unsigned long long h = 0xCBF29CE484222325ull;
    goc_tf128 a, b, r;
    a.hi = 0xca18dd5a29dc83e3ull;
    a.lo = 0x2ce32d23193c6871ull;
    b.hi = 0xe21299d8cadc314bull;
    b.lo = 0x860f5366ff99bdb2ull;
#define MIX(v) do { h ^= (unsigned long long)(v); h *= 0x100000001B3ull; } while (0)
    r = goc_tf_add(a, b); MIX(r.hi); MIX(r.lo);
    r = goc_tf_mul(a, b); MIX(r.hi); MIX(r.lo);
    MIX(goc_tf_cmp(a, b));
    MIX(goc_tf_neg(a).hi);
#undef MIX
    r = goc_tf_from_double(a.lo);
    printf("r = %016llx.%016llx   a.lo=%016llx\n", r.hi, r.lo, a.lo);
    return 0;
}
