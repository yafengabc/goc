/* headers.c -- prove the shipped standard headers can be #include'd by user
 * code with their standard signatures: typedefs (size_t, ptrdiff_t), NULL as
 * ((void *)0), const-qualified parameters, size_t returns, and variadic
 * prototypes (printf). Deterministic output, no stdin, safe for the headless
 * test harness on both the Windows and Linux (elfcheck) targets.
 */
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main() {
    char buf[32];

    /* size_t from <stddef.h>, strlen returns size_t */
    size_t n = strlen("hello");
    printf("strlen=%d size_t=%d\n", (int)n, (int)n);

    /* NULL is ((void *)0) and compares equal to 0 */
    printf("NULL is %d\n", NULL == 0);

    /* const-qualified params, char* return */
    char *p = strcpy(buf, "abc");
    printf("strcpy=%s p==buf:%d\n", buf, p == buf);

    printf("strcmp=%d strncmp=%d memcmp=%d\n",
           strcmp("ab", "ac"), strncmp("ab", "ab", 2), memcmp("ab", "ab", 2));

    char *q = strchr("abcabc", 'b');
    printf("strchr found=%d\n", q != 0);

    /* <stdlib.h>: atoi returns int, abs, malloc/free */
    printf("atoi=%d abs=%d\n", atoi("-99"), abs(-7));
    void *m = malloc(16);
    printf("malloc ok=%d\n", m != 0);
    free(m);

    /* printf's variadic prototype takes any number of trailing args */
    printf("varargs: %d %d %d %d %d\n", 1, 2, 3, 4, 5);

    printf("done\n");
    return 0;
}
