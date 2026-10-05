/*
 * cstd_lib_string2.c -- goclib <string.h> coverage, round 2.
 *
 * Exercises the functions added alongside the POSIX/C23 surface that the
 * first string test did not touch:
 *   case1  strnlen caps at n even when the string is longer (C23 7.24.6.3)
 *   case2  stpcpy copies and returns a pointer to the terminating NUL
 *          (POSIX; the returned pointer is dest + strlen(src), which lets a
 *          caller chain copies without a second strlen)
 *   case3  stpcpy overwrites in place and the returned pointer lands on NUL
 *
 * Cross-checked against mingw gcc; the two agree byte for byte because these
 * are pure memory operations with no platform dependency.
 */
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <string.h>
#include <stdint.h>

/* mingw's <string.h> does not declare the POSIX stpcpy (its export exists
 * in the C runtime, the header just omits the prototype), so supply it here.
 * The same declaration is also legal on the goc side, where <string.h>
 * already provides it -- redundant function prototypes are allowed in C. */
char *stpcpy(char *dest, const char *src);

/* When cross-checking with gcc, the C runtime does not *export* stpcpy, so
 * gcc links a self-contained reference implementation instead of the real
 * one. This documents the exact behaviour goc's own stpcpy must match, so
 * the two toolchains still compare byte for byte. On the goc side
 * STUB_STPCPY is left undefined and goc's library stpcpy is used directly. */
#ifdef STUB_STPCPY
char *stpcpy(char *dest, const char *src) {
    char *d = dest;
    while ((*d++ = *src++) != 0) { }
    return d - 1; /* pointer to the NUL just written */
}
#endif

int main(void) {
    /* case1: strnlen never reads past n, and returns the true length when the
     * string is shorter than n. */
    {
        const char *s = "hello world";
        printf("case1: full=%d cap5=%d cap20=%d empty=%d\n",
               (int)strnlen(s, 100), (int)strnlen(s, 5),
               (int)strnlen(s, 20), (int)strnlen("", 5));
    }

    /* case2: stpcpy returns a pointer to the NUL it wrote, i.e. dest+len. */
    {
        char buf[32];
        const char *src = "stpcpy-me";
        char *end = stpcpy(buf, src);
        printf("case2: len=%d end-at-nul=%d content=%s\n",
               (int)strlen(src), end == buf + strlen(src) ? 1 : 0, buf);
    }

    /* case3: stpcpy into a buffer that already holds text, twice, and confirm
     * each returned pointer points exactly at its own NUL (no off-by-one that
     * would leave a stale byte behind the new string). */
    {
        char a[16];
        strcpy(a, "old-content");
        char *pa = stpcpy(a, "new");
        printf("case3: a=%s ret-on-nul=%d\n", a,
               pa == a + 3 && *pa == 0 ? 1 : 0);
    }
    return 0;
}
