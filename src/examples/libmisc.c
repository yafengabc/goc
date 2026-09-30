/* libmisc.c -- exercises the batch-1 library additions:
 *   qsort / bsearch, snprintf, atol/atof/labs/div/ldiv, strerror+errno,
 *   getenv (unset lookups only -- environment values differ per host),
 *   atexit ordering, and freopen: stdout is redirected into a file, the
 *   file is read back through a fresh handle, and the final line goes
 *   through the print builtin (os.c direct write to fd 1) so it reaches
 *   the original stdout regardless of the freopen above.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>

/* The handlers print through the print builtin (os.c direct write to the
 * original fd 1), because stdout may have been freopen'd to a file above --
 * a FILE-layer printf would land there, not on the terminal. */
static void bye1(void) { print("bye1"); }
static void bye2(void) { print("bye2"); }

static int cmp_int(const void *a, const void *b) {
    int x = *(const int *)a;
    int y = *(const int *)b;
    if (x < y) return -1;
    if (x > y) return 1;
    return 0;
}

int main() {
    /* ---- qsort + bsearch ---- */
    int a[10];
    int i;
    int key;
    int *found;
    a[0] = 42; a[1] = 7; a[2] = 19; a[3] = 3; a[4] = 88;
    a[5] = 1;  a[6] = 56; a[7] = 23; a[8] = 7; a[9] = 90;
    qsort(a, 10, sizeof(int), cmp_int);
    print(a);
    key = 23;
    found = (int *)bsearch(&key, a, 10, sizeof(int), cmp_int);
    printf("found=%d at=%ld\n", *found, found - a);
    key = 100;
    found = (int *)bsearch(&key, a, 10, sizeof(int), cmp_int);
    printf("missing=%d\n", found == 0);

    /* ---- snprintf truncation contract ---- */
    {
        char buf[8];
        int n = snprintf(buf, 8, "%s %d", "truncate", 12345);
        printf("sn=[%s] need=%d\n", buf, n);
    }

    /* ---- conversions ---- */
    printf("atol=%ld atof=%.2f labs=%ld\n",
           atol("  -99xyz"), atof("3.5e2"), labs(-7));
    {
        div_t d = div(-17, 5);
        ldiv_t ld = ldiv(-17L, 5L);
        printf("div=%d,%d ldiv=%ld,%ld\n", d.quot, d.rem, ld.quot, ld.rem);
    }

    /* ---- strerror / errno ---- */
    errno = 2;
    printf("err=[%s]\n", strerror(errno));

    /* ---- getenv: unset / null / empty all yield 0 on every host ---- */
    printf("unset=%d nullname=%d empty=%d\n",
           getenv("GOC_NO_SUCH_VAR_42") == 0,
           getenv(0) == 0,
           getenv("") == 0);

    /* ---- freopen: redirect stdout into a file, read it back ---- */
    if (freopen("goc_freopen.txt", "w", stdout) == 0) {
        print("freopen-failed");
        return 1;
    }
    printf("inside-file\n");
    fclose(stdout);
    {
        FILE *f = fopen("goc_freopen.txt", "r");
        char line[64];
        fgets(line, 64, f);
        fclose(f);
        print(line);
    }

    /* ---- atexit: LIFO order at exit() ---- */
    atexit(bye1);
    atexit(bye2);
    return 0;
}
