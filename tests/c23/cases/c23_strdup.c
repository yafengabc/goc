/* C23 feature: <string.h> strdup / strndup (C23 standardizes them here, not in stdlib.h)
 * Clause:     C23 7.24.6.5 strdup; 7.24.6.7 strndup
 * Strategy:   strdup normal + empty string; strndup with n<len, n>len, n=0. Compare
 *             copied content with strcmp, check lengths, and free each allocation.
 *             (goc's <stdlib.h> does NOT declare these; they live in <string.h> per
 *             C23 -- recorded in the status doc.)
 * Status:     PASS
 * EXPECT: PASS
 */
#include <stdio.h>
#include <string.h>
#include <stdlib.h>

int main(void) {
    int passed = 0, total = 0;
    char *d;

    ++total;
    d = strdup("hello");
    printf("case%d: strdup(\"hello\")=\"%s\" len=%d\n", total, d, (int)strlen(d));
    if (d != NULL && strcmp(d, "hello") == 0 && strlen(d) == 5) passed++;
    free(d);

    ++total;
    d = strdup("");
    printf("case%d: strdup(\"\")=\"%s\" len=%d\n", total, d, (int)strlen(d));
    if (d != NULL && strcmp(d, "") == 0 && strlen(d) == 0) passed++;
    free(d);

    ++total;
    d = strndup("hello", 2);
    printf("case%d: strndup(\"hello\",2)=\"%s\" len=%d\n", total, d, (int)strlen(d));
    if (d != NULL && strcmp(d, "he") == 0 && strlen(d) == 2) passed++;
    free(d);

    ++total;
    d = strndup("hi", 10);
    printf("case%d: strndup(\"hi\",10)=\"%s\" len=%d\n", total, d, (int)strlen(d));
    if (d != NULL && strcmp(d, "hi") == 0 && strlen(d) == 2) passed++;
    free(d);

    ++total;
    d = strndup("hello", 0);
    printf("case%d: strndup(\"hello\",0)=\"%s\" len=%d\n", total, d, (int)strlen(d));
    if (d != NULL && strcmp(d, "") == 0 && strlen(d) == 0) passed++;
    free(d);

    printf("SUMMARY: %d/%d\n", passed, total);
    return passed == total ? 0 : 1;
}
