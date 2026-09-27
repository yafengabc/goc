/* c0lib_test.c -- end-to-end proof that c0 compiles the REAL C-version clib
 * (clib/c0lib.c) and that it runs correctly on both backends.
 *
 * We #include the canonical clib/c0lib.c verbatim; everything below is just a
 * driver that exercises it. The five __clib_* OS primitives come from the
 * hand-written assembly backend (clib/clib.asm), which c0 pulls in on demand;
 * every public function (strlen, strcpy, printf, malloc, ...) is the C version
 * from c0lib.c, which suppresses the assembly twin.
 */
#include "../clib/c0lib.c"

int main() {
    /* --- variadic printf: %s %c %d %ld %u %x %f --- */
    printf("hello %s count=%d char=%c big=%ld uns=%u hex=%x pi=%f\n",
           "world", 42, 'A', 123456789L, 255, 255, 3.141593);

    /* --- strlen --- */
    printf("strlen(hello)=%d\n", (int)strlen("hello"));

    /* --- strcpy into a malloc'd buffer (char* byte access + heap) --- */
    char *heap = malloc(32);
    if (heap) {
        char *p = strcpy(heap, "copied!");
        printf("strcpy -> %s (p==heap:%d)\n", heap, p == heap);
        free(heap);
    } else {
        printf("malloc failed\n");
    }

    /* --- strcmp --- */
    printf("strcmp(abc,abc)=%d strcmp(abc,abd)=%d\n",
           strcmp("abc", "abc"), strcmp("abc", "abd"));

    /* --- strcat --- */
    char cat[64];
    strcpy(cat, "foo");
    strcat(cat, "-bar");
    printf("strcat -> %s\n", cat);

    /* --- strchr (NULL handling) --- */
    char *q = strchr("findXme", 'X');
    printf("strchr X -> %s\n", q == 0 ? "(null)" : q);

    /* --- memcpy / memset on local char arrays (byte access) --- */
    char sa[4];
    sa[0] = 'a'; sa[1] = 'b'; sa[2] = 'c'; sa[3] = 0;
    char sb[4];
    memcpy(sb, sa, 4);
    printf("memcpy -> %s\n", sb);

    memset(sb, 0, 4);
    sb[0] = 7; sb[1] = 7;
    printf("memset z0=%d z2=%d\n", (int)sb[0], (int)sb[2]);

    /* --- memcmp --- */
    char mc[4];
    memcpy(mc, sa, 4);
    printf("memcmp eq=%d diff=%d\n", memcmp(sa, mc, 4), memcmp(sa, "xyz", 4));

    /* --- strncpy --- */
    char nc[8];
    strncpy(nc, "hi", 8);
    printf("strncpy -> %s\n", nc);

    /* --- sprintf into a malloc'd buffer --- */
    char *sbuf = malloc(64);
    int n = sprintf(sbuf, "sprintf x=%d y=%ld", 100, 200L);
    printf("sprintf(%d): %s\n", n, sbuf);
    free(sbuf);

    /* --- atoi / strtol --- */
    printf("atoi(12345)=%d strtol(0xff,0,0)=%ld strtol(-42,0,10)=%ld\n",
           atoi("12345"), strtol("0xff", 0, 0), strtol("-42", 0, 10));

    /* --- abs --- */
    printf("abs(-9)=%d abs(9)=%d\n", abs(-9), abs(9));

    /* --- srand / rand (deterministic LCG) --- */
    srand(1);
    printf("rand=%d rand=%d rand=%d\n", rand(), rand(), rand());

    /* --- puts / putchar --- */
    puts("puts-line");
    putchar('P'); putchar('Q'); putchar('\n');

    return 0;
}
