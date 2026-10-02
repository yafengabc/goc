/* C23 feature: <string.h> memccpy; <stdlib.h> qsort/bsearch const-correct comparator
 * Clause:     C23 7.24.2.3 memccpy; 7.24.5.1 qsort; 7.24.5.2 bsearch
 * Strategy:   memccpy: byte found (returns pointer just past c, copies up to+incl c),
 *             byte not found (returns NULL, copies all n), c=='\0', and n smaller than
 *             the byte position (not found). Then a const-correct comparator
 *             (C23 makes the qsort/bsearch parameters const void*) used with qsort to
 *             sort an int array and bsearch to look a value up.
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

static int cmp_int(const void *a, const void *b) {
    return *(const int *)a - *(const int *)b;
}

int main(void) {
    int passed = 0, total = 0;
    char out[32];
    void *p;

    /* case1: byte found -- copies up to and including 'X', returns past it */
    ++total;
    memset(out, 0, sizeof(out));
    p = memccpy(out, "abcXdef", 'X', 10);
    printf("case%d: found 'X' p-nonnull=%d off=%d out[0..3]='%c%c%c%c'\n",
           total, (int)(p != NULL), (int)((char *)p - out),
           out[0], out[1], out[2], out[3]);
    if (p != NULL && p == out + 4 &&
        out[0] == 'a' && out[1] == 'b' && out[2] == 'c' && out[3] == 'X') passed++;

    /* case2: byte not found -- returns NULL, copies all n bytes */
    ++total;
    memset(out, 0, sizeof(out));
    p = memccpy(out, "abcdef", 'Z', 6);
    printf("case%d: not-found 'Z' p-nonnull=%d out=\"%s\"\n",
           total, (int)(p != NULL), out);
    if (p == NULL && memcmp(out, "abcdef", 6) == 0) passed++;

    /* case3: c == '\0' (NUL terminator found); "ab" is a,b,\0 -> copies 3 bytes */
    ++total;
    memset(out, 0, sizeof(out));
    p = memccpy(out, "ab", 0, 10);
    printf("case%d: c='\\0' p-nonnull=%d off=%d out=\"%s\"\n",
           total, (int)(p != NULL), (int)((char *)p - out), out);
    if (p != NULL && p == out + 3 && out[0] == 'a' && out[1] == 'b' && out[2] == 0)
        passed++;

    /* case4: n smaller than the byte position -- not found, copies n bytes */
    ++total;
    memset(out, 0, sizeof(out));
    p = memccpy(out, "abcdef", 'f', 3);
    printf("case%d: n=3 before 'f' p-nonnull=%d out[0..2]='%c%c%c'\n",
           total, (int)(p != NULL), out[0], out[1], out[2]);
    if (p == NULL && out[0] == 'a' && out[1] == 'b' && out[2] == 'c') passed++;

    /* case5: qsort with const-correct comparator sorts ascending */
    ++total;
    {
        int arr[5] = {5, 2, 8, 1, 9};
        int ok;
        qsort(arr, (size_t)5, sizeof(int), cmp_int);
        ok = (arr[0] == 1 && arr[1] == 2 && arr[2] == 5 &&
              arr[3] == 8 && arr[4] == 9);
        printf("case%d: qsort -> %d %d %d %d %d sorted=%d\n",
               total, arr[0], arr[1], arr[2], arr[3], arr[4], ok);
        if (ok) passed++;
    }

    /* case6: bsearch finds a present value */
    ++total;
    {
        int arr[5] = {1, 2, 5, 8, 9};
        int key = 8;
        int *found = bsearch(&key, arr, (size_t)5, sizeof(int), cmp_int);
        printf("case%d: bsearch(8) found-nonnull=%d *found=%d\n",
               total, (int)(found != NULL), found ? *found : -1);
        if (found != NULL && *found == 8) passed++;
    }

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
