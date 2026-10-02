/* C23 feature: macro expansion boundaries
 * Clause:     C23 6.10.3 "Macro replacement"
 * Strategy:   verify self-reference does not recurse infinitely (#define X X),
 *             deep nested expansion (~30 layers), that arguments are macro-expanded
 *             before substitution, # stringification, and ## pasting (new token and
 *             the empty-operand edge).
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>

/* case1: self-reference. X expands to X once and is NOT re-scanned (guarded),
 * so the preprocessor terminates; inside #if an unknown identifier folds to 0. */
#define X X
#if X
#  define SELF_V 1
#else
#  define SELF_V 0
#endif

/* case2: ~30 layers of nested macro expansion. Lk = L(k-1)+1, so L30 == 30. */
#define L1  1
#define L2  L1+1
#define L3  L2+1
#define L4  L3+1
#define L5  L4+1
#define L6  L5+1
#define L7  L6+1
#define L8  L7+1
#define L9  L8+1
#define L10 L9+1
#define L11 L10+1
#define L12 L11+1
#define L13 L12+1
#define L14 L13+1
#define L15 L14+1
#define L16 L15+1
#define L17 L16+1
#define L18 L17+1
#define L19 L18+1
#define L20 L19+1
#define L21 L20+1
#define L22 L21+1
#define L23 L22+1
#define L24 L23+1
#define L25 L24+1
#define L26 L25+1
#define L27 L26+1
#define L28 L27+1
#define L29 L28+1
#define L30 L29+1

/* case3: arguments are macro-expanded before substitution */
#define ARG 10
#define SHOW(a) a
int var42 = 7;

/* case4: # stringification */
#define STR(x) #x

/* case5/6: ## pasting */
#define GLUE(a, b) a##b
#define EMPTY
#define PASTE(x, y) x##y
#define EVAL(x, y)  PASTE(x, y)   /* expands args first (not adjacent to ##) */
int var7 = 99;
int MYVAL = 5;

int main(void) {
    int passed = 0, total = 0;
    int ok;

    ++total;
    printf("case%d: self-ref #if X folds to %d (want 0)\n", total, SELF_V);
    ok = (SELF_V == 0);
    if (ok) passed++;

    ++total;
    printf("case%d: nested L30 = %d (want 30)\n", total, L30);
    ok = (L30 == 30);
    if (ok) passed++;

    ++total;
    printf("case%d: SHOW(ARG) = %d (want 10)\n", total, SHOW(ARG));
    ok = (SHOW(ARG) == 10);
    if (ok) passed++;

    ++total;
    {
        const char *s = STR(hello);   /* stringification works via a const char* */
        printf("case%d: STR(hello) = %s\n", total, s);
        ok = (s[0]=='h' && s[1]=='e');
        if (ok) passed++;
    }

    ++total;
    {
        int v = GLUE(var, 42);   /* -> var42 */
        printf("case%d: GLUE(var,42)=%d (want 7)\n", total, v);
        ok = (v == 7);
        if (ok) passed++;
    }

    ++total;
    {
        int v = EVAL(EMPTY, MYVAL);  /* EMPTY expands empty -> paste MYVAL */
        printf("case%d: EVAL(EMPTY,MYVAL)=%d (want 5)\n", total, v);
        ok = (v == 5);
        if (ok) passed++;
    }

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
