/* C23 feature: bool / true / false as keywords
 * Clause:     C23 6.2.5 bool; 6.4.1 keywords; <stdbool.h> now just keywords
 * Strategy:   bool variables/arrays/struct members, sizeof(bool), mixing with
 *             _Bool, true/false as integer expressions, <stdbool.h> compat.
 * Status:     PENDING
 * EXPECT: PASS
 */
#include <stdio.h>
#include <stdbool.h>

struct BHolder {
    bool flag;
    bool arr[3];
};

int main(void) {
    int passed = 0, total = 0;

    ++total;
    bool a = true;
    bool b = false;
    printf("case%d: a=%d b=%d\n", total, (int)a, (int)b);
    if (a == 1 && b == 0) passed++;

    ++total;
    bool arr[4] = { true, false, true, false };
    int s = 0;
    for (int i = 0; i < 4; i++) s += (int)arr[i];
    printf("case%d: array sum=%d\n", total, s);
    if (s == 2) passed++;

    ++total;
    struct BHolder h;
    h.flag = true;
    h.arr[0] = true;
    h.arr[1] = false;
    h.arr[2] = true;
    printf("case%d: holder flag=%d e0=%d e1=%d e2=%d\n", total,
           (int)h.flag, (int)h.arr[0], (int)h.arr[1], (int)h.arr[2]);
    if (h.flag == 1 && h.arr[0] == 1 && h.arr[1] == 0 && h.arr[2] == 1) passed++;

    ++total;
    int sz = (int)sizeof(bool);
    printf("case%d: sizeof(bool)=%d\n", total, sz);
    if (sz == 1) passed++;

    ++total;
    _Bool bb1 = true;
    bool bb2 = false;
    int mix = 0;
    if (bb1) mix += 1;
    if (!bb2) mix += 2;
    if (sizeof(_Bool) == sizeof(bool)) mix += 4;
    printf("case%d: mix score=%d\n", total, mix);
    if (mix == 7) passed++;

    ++total;
    int tru = true;
    int fal = false;
    int sum = tru + fal;
    printf("case%d: true as int=%d false as int=%d sum=%d\n", total, tru, fal, sum);
    if (tru == 1 && fal == 0 && sum == 1) passed++;

    ++total;
    bool viaInt = (1 > 0);
    bool viaEq = (5 == 5);
    printf("case%d: viaInt=%d viaEq=%d\n", total, (int)viaInt, (int)viaEq);
    if (viaInt && viaEq) passed++;

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
