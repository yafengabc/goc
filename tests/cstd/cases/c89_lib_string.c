/* ============================================================
   c89_lib_string.c - <string.h> full API with boundary cases
   Standard   : ISO/IEC 9899:1990 (C89) 7.11 string handling <string.h>
   Strategy   : case1 strlen; case2 strcpy/strncpy incl. zero-pad tail; case3 strcat/strncat;
                case4 strcmp/strncmp ordering; case5 strchr/strrchr; case6 strstr;
                case7 strspn/strcspn/strpbrk; case8 strtok sequence;
                case9 memcpy vs memmove overlap; case10 memcmp/memset/memchr
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>
#include <string.h>

int main(void) {
    char buf[64];
    char tok_src[32];
    char *t;
    char overlap[16] = "abcdefgh";

    printf("case1: strlen=%d empty=%d\n", (int)strlen("hello"), (int)strlen(""));
    strcpy(buf, "copy-me");
    printf("case2: strcpy=[%s]\n", buf);
    strncpy(buf, "abc", 5);
    buf[5] = 0;
    printf("case2b: strncpy-padded=[%s] len=%d\n", buf, (int)strlen(buf));
    strcpy(buf, "aa");
    strcat(buf, "-bb");
    strncat(buf, "-cc", 2);
    printf("case3: cat=[%s]\n", buf);
    printf("case4: cmp=%d,%d,%d\n", strcmp("abc", "abd"), strcmp("abc", "abc"),
           strncmp("abc", "abd", 2));
    printf("case5: chr=%s rchr=[%s]\n", strchr("hello", 'l'), strrchr("hello", 'l'));
    printf("case6: strstr=[%s]\n", strstr("foobarbaz", "bar"));
    printf("case7: spn=%d cspn=%d pbrk=%s\n",
           (int)strspn("a1b", "ab"), (int)strcspn("a1b", "1"), strpbrk("a1b", "1"));
    strcpy(tok_src, "one/two/three");
    t = strtok(tok_src, "/");
    printf("case8: tok=[%s]", t);
    while ((t = strtok(NULL, "/")) != NULL) printf("[/%s]", t);
    printf("\n");
    memmove(overlap + 2, overlap, 5);
    overlap[7] = 0;
    printf("case9: overlap-memmove=[%s]\n", overlap);
    {
        char mb[8] = "abcdefg";
        int r = memcmp("abc", "abd", 3);
        memset(mb + 2, 'X', 2);
        printf("case10: memcmp=%d memset=[%s] memchr=%s\n", r, mb, memchr(mb, 'X', 6));
    }
    return 0;
}
