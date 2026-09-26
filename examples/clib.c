// Exercises the whole clib: stdio, string and stdlib.
// A string literal is just a pointer, and a pointer fits in an int
// variable, so the heap functions are usable even though c0 has no
// pointer type.

int main() {
    printf("== stdio ==\n");
    printf("d=%d s=%s c=%c x=%x pct=%%\n", 42, "str", 65, 255);
    puts("puts: hello");
    putchar(74);
    putchar(10);
    printf("neg=%d zero=%d\n", -1234, 0);
    printf("four varargs: %d %d %d %d\n", 1, 2, 3, 4);
    printf("five varargs: %d %d %d %d %d\n", 1, 2, 3, 4, 5);

    printf("== string ==\n");
    printf("strlen=%d\n", strlen("hello"));
    printf("strcmp same=%d\n", strcmp("abc", "abc"));
    printf("strcmp less=%d\n", strcmp("abc", "abd"));
    printf("strcmp more=%d\n", strcmp("abd", "abc"));

    printf("== stdlib ==\n");
    printf("atoi=%d\n", atoi("-456"));
    printf("abs=%d\n", abs(-7));

    printf("== heap ==\n");
    int p = malloc(64);
    printf("malloc ok=%d\n", p > 0);
    strcpy(p, "into the heap");
    printf("copied len=%d s=%s\n", strlen(p), p);
    memset(p, 65, 5);
    printf("memset len=%d s=%s\n", strlen(p), p);
    free(p);

    int q = malloc(32);
    memcpy(q, "abcdef", 7);      // 7 = 6 chars plus the NUL: memcpy does not terminate
    printf("memcpy s=%s\n", q);
    free(q);
    printf("done\n");
    return 0;
}
