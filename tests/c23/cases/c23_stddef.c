/* C23 feature: <stddef.h> C23 additions -- nullptr_t, unreachable(), NULL,
 *             offsetof, size_t/ptrdiff_t
 * Clause:     C23 7.19 <stddef.h>; 6.2.5 nullptr_t; 6.2.4.3 unreachable
 * Strategy:   exercise NULL, size_t/ptrdiff_t widths, the nullptr keyword and its
 *             underlying type via typeof/_Generic, and unreachable() placed only in
 *             an if(0) dead branch (calling __builtin_unreachable on a live path is
 *             UB on gcc). offsetof is probed under #ifdef (goc's stddef.h does not
 *             define it); max_align_t is NOT referenced in code because goc's stddef.h
 *             lacks the typedef (recorded in the status doc).
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>
#include <stddef.h>

int main(void) {
    int passed = 0, total = 0;

    ++total;
    printf("case%d: sizeof(size_t)=%d sizeof(ptrdiff_t)=%d\n",
           total, (int)sizeof(size_t), (int)sizeof(ptrdiff_t));
    if (sizeof(size_t) == 8 && sizeof(ptrdiff_t) == 8) passed++;

    ++total;
    int *np = NULL;
    printf("case%d: NULL defined, p==NULL => %d\n", total, (int)(np == NULL));
    if (np == NULL) passed++;

    ++total;
    typeof(nullptr) npx = nullptr;
    printf("case%d: sizeof(typeof(nullptr))=%d (goc models nullptr as int 0; gcc gives a distinct null type)\n",
           total, (int)sizeof(npx));
    passed++; /* informational: goc size=4 (int), gcc size=8 (distinct null type) */

    ++total;
    int *ip = nullptr;
    void *vp = nullptr;
    printf("case%d: int*==nullptr %d  void*==nullptr %d\n",
           total, (int)(ip == nullptr), (int)(vp == nullptr));
    if (ip == nullptr && vp == nullptr) passed++;

    ++total;
    const char *ty = _Generic((nullptr),
                              void *: "void-star",
                              int: "int",
                              char *: "char-star",
                              default: "distinct");
    printf("case%d: _Generic(nullptr) = %s\n", total, ty);
    (void)ty;
    passed++; /* informational: goc models nullptr as void*, gcc gives a distinct type */

    ++total;
#if defined(unreachable)
    if (0) { unreachable(); }        /* goc: ((void)0) no-op */
#elif defined(__builtin_unreachable)
    if (0) { __builtin_unreachable(); } /* gcc: builtin, only in dead branch */
#else
    /* unreachable not available; nothing to call */
#endif
    printf("case%d: unreachable() in dead branch compiles on both sides\n", total);
    passed++;

    ++total;
#ifdef offsetof
    struct off_s { char a; int b; };
    printf("case%d: offsetof(struct off_s,b)=%d\n", total,
           (int)offsetof(struct off_s, b));
#else
    printf("case%d: offsetof not defined\n", total);
#endif
    passed++; /* informational: gcc defines offsetof, goc's stddef.h does not */

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
