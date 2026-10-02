/* C23 MVP item #13 (memccpy, plus regression for strdup / strndup). memccpy
 * copies src -> dest until it meets byte c, then returns dest+index_of_c+1, or
 * NULL if c is not found within n bytes. strdup / strndup come from earlier
 * POSIX work and are checked here so the C23 string batch stays green.
 * Golden: src/expected/c23_string.txt */

#include <stdio.h>
#include <string.h>

int main(void) {
    char src[] = "hello world";
    char d[32];
    void *r = memccpy(d, src, 'w', sizeof(src));
    printf("found=%d\n", (char *)r == d + 7);
    printf("copied=[%.7s]\n", d);

    char d2[32];
    void *r2 = memccpy(d2, src, 'z', sizeof(src));
    printf("notfound=%d\n", r2 == 0);

    char *dupe = strdup("goc");
    printf("dupe=%s\n", dupe ? dupe : "NULL");
    char *nd = strndup("abcdef", 3);
    printf("ndup=%s\n", nd ? nd : "NULL");
    return 0;
}
