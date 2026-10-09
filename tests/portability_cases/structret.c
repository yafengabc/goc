/* Struct-returning calls consumed IN EXPRESSION POSITION (goc native).
 *
 * Taking a member of a struct-returning call without naming it first --
 * "h ^= f(a).hi", "sink(f(a).hi)", "if (f(a).hi)" -- used to corrupt the
 * statements that FOLLOW it: the member's load left the call's result buffer
 * claim set, genCompoundAssign rolled tmpDepth back to its entry depth
 * without releasing it, and the statement boundary's unconditional release
 * subtracted the buffer's slots a second time -- driving the temporary slot
 * index negative. Negative slot indices are addresses inside live locals, so
 * the aggregate assignment after the trigger had its result buffer land on r
 * itself and printf's argument parks wrote format pointers into r's bytes:
 * every half of r printed an address inside the PE image (original repro:
 * tests/fp128/goc_structret_bug.c). Fixed by releasing the buffer at the
 * scalar member load itself, where it actually dies.
 *
 * main() below keeps the exact shape that reproduced the corruption: no
 * struct-returning call before the trigger, the declaration order of the
 * original repro, and a printf/sprintf of the assigned struct's halves. The
 * position checks after it cover the other consumption sites (argument,
 * condition, plain assignment, double members, aggregate members copied
 * onward, array members through the decayed pointer) and guard that the fix
 * did not over-release -- those paths still hand the buffer's address onward.
 *
 * Prints "OK" on the last line and exits 0 iff every check passed, so it
 * drops straight into build_run_win_ok() in tools/portability/win_regress.sh.
 *
 *   gcc -DUSE_LOCAL -o sr structret.c && ./sr    # cross-check under gcc
 */
#include <stdio.h>
#include <string.h>

typedef struct { unsigned long long lo, hi; } T128;
typedef struct { int x, y; } P;
typedef struct { P inner; double d; int arr[3]; } S;

#ifdef USE_LOCAL
/* Same-shape helpers compiled locally, so the file also runs under gcc/clang
 * as a cross-check. Deterministic, value-independent of the goclib runtime's
 * semantics: the checks compare against references computed in-source. */
T128 tfneg(T128 a) { T128 r; r.lo = a.lo; r.hi = a.hi ^ 0x8000000000000000ull; return r; }
T128 tfdbl(unsigned long long x) {
    T128 r;
    r.lo = x ^ 0x9e3779b97f4a7c15ull;
    r.hi = x ^ 0x6384f69a01f39b0cull;
    return r;
}
#else
/* The goclib runtime the bug was found with (declared, defined by goc's
 * embedded library). */
T128 goc_tf_neg(T128 a);
T128 goc_tf_from_double(unsigned long long bits);
#define tfneg goc_tf_neg
#define tfdbl goc_tf_from_double
#endif

P mkp(int x, int y) { P p; p.x = x; p.y = y; return p; }
S mks(int base) {
    S s;
    s.inner = mkp(base, base + 1);
    s.d = 2.5;
    s.arr[0] = base + 41; s.arr[1] = base + 42; s.arr[2] = base + 43;
    return s;
}
int sink(int v) { return v; }

static int fails = 0;
#define CHECK(cond, name) do { \
    if (!(cond)) { printf("FAIL %s\n", name); fails++; } \
} while (0)

int main(void) {
    /* Shape of the original repro: the trigger is the FIRST struct-returning
     * call of the function. */
    unsigned long long h = 1;
    T128 a;
    a.lo = 0x2ce32d23193c6871ull;
    a.hi = 0xca18dd5a29dc83e3ull;
    T128 flipped;
    flipped.lo = a.lo;
    flipped.hi = a.hi ^ 0x8000000000000000ull;
    T128 want, r;
    char buf[64], wantbuf[64];
    /* Canary guards, declared LAST: the locals layout hands the deepest
     * frame slots to them, and the corrupted (negative) temporary slot
     * indices climb upward from exactly there -- tmpSlot(-1) IS g7's
     * address. Whatever the frame layout, a negative-depth park lands in a
     * guard, not in unchecked padding. */
    unsigned long long g0, g1, g2, g3, g4, g5, g6, g7;
    g0 = 0x1111111111111111ull; g1 = 0x2222222222222222ull;
    g2 = 0x3333333333333333ull; g3 = 0x4444444444444444ull;
    g4 = 0x5555555555555555ull; g5 = 0x6666666666666666ull;
    g6 = 0x7777777777777777ull; g7 = 0x8888888888888888ull;

    /* Reference for the post-trigger aggregate assignment, computed while the
     * slot state is still clean, so symmetric corruption cannot fool it. */
    want = tfdbl(a.lo);
    sprintf(wantbuf, "%016llx.%016llx", want.hi, want.lo);

    /* --- the trigger: member of a struct-returning call in a compound --- */
    h ^= tfneg(a).hi;
    CHECK(h == (1ull ^ flipped.hi), "compound");

    /* --- the damage: aggregate assignment + printf parks after it --- */
    r = tfdbl(a.lo);
    sprintf(buf, "%016llx.%016llx", r.hi, r.lo);
    CHECK(strcmp(buf, wantbuf) == 0, "printf-shape");
    CHECK(g0 == 0x1111111111111111ull, "guard0");
    CHECK(g1 == 0x2222222222222222ull, "guard1");
    CHECK(g2 == 0x3333333333333333ull, "guard2");
    CHECK(g3 == 0x4444444444444444ull, "guard3");
    CHECK(g4 == 0x5555555555555555ull, "guard4");
    CHECK(g5 == 0x6666666666666666ull, "guard5");
    CHECK(g6 == 0x7777777777777777ull, "guard6");
    CHECK(g7 == 0x8888888888888888ull, "guard7");
    CHECK(a.lo == 0x2ce32d23193c6871ull && a.hi == 0xca18dd5a29dc83e3ull, "a-intact");
    CHECK(flipped.hi == (a.hi ^ 0x8000000000000000ull), "flipped-intact");

    /* --- other consumption sites for a member of a call --- */

    /* Argument position: marshalling parks temporaries. */
    CHECK(sink(tfneg(a).hi == flipped.hi ? 1 : 0) == 1, "arg");

    /* Condition position. */
    CHECK(tfneg(a).hi != 0, "cond");
    CHECK(!(tfneg(a).lo == 0xdeadbeefull), "cond-false");

    /* Plain assignment. */
    unsigned long long t = tfneg(a).hi;
    CHECK(t == flipped.hi, "assign");

    /* Double member (compound with a double member on the right). */
    double d = 1.0;
    d += mks(0).d;
    CHECK(d == 3.5, "double-member");

    /* Aggregate member copied onward: this path hands the buffer's ADDRESS
     * to the copy, so it must keep working (no over-release). */
    P p = mks(0).inner;
    CHECK(p.x == 0 && p.y == 1, "agg-member");

    /* Array member subscripted through the decayed pointer: the pointer
     * points INTO the result buffer, so the buffer must survive the load. */
    CHECK(mks(0).arr[1] == 42, "arr-member");

    if (fails == 0) {
        printf("OK\n");
        return 0;
    }
    printf("%d checks failed\n", fails);
    return 1;
}
