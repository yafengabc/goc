/* ---------------------------------------------------------------------------
 * Known goc native-back-end bug: taking a member of a struct-returning call
 * *in expression position* corrupts the next struct-returning call.
 *
 *     MIX(goc_tf_neg(a).hi);        <-- the trigger
 *     r = goc_tf_from_double(a.lo); <-- now returns a pointer, not a value
 *
 * Expected (gcc, gocl, and goc with the member read through a variable):
 *
 *     r = 3ece32d23193c687.1000000000000000   a.lo=2ce32d23193c6871
 *
 * goc native gives:
 *
 *     r = 0000000140014000.0000000140014000   a.lo=2ce32d23193c6871
 *
 * 0x140014000 is an address inside the PE image (base 0x140000000) and both
 * halves of the 16-byte result carry it, so the call's result location is
 * being handed back instead of its contents. The damage also outlives the
 * statement: every later struct-returning call in the function is wrong.
 *
 * Workaround until it is fixed: assign to a variable first
 * (`r = goc_tf_neg(a); ... r.hi`). tests/portability_cases/fp128.c does that
 * and says why in a comment.
 *
 * Build: goc goc_structret_bug.c -o bug.exe && ./bug.exe
 * (it links against goclib, which is what supplies the goc_tf_* functions)
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
