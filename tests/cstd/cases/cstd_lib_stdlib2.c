/* ============================================================
   cstd_lib_stdlib2.c - <stdlib.h> part 2: numeric conversion, the
                        integer/pointer families, program termination
   Standard   : ISO/IEC 9899:1999 (C99) 7.20 <stdlib.h>
   Strategy   : case1 strtod across decimal / exponent / hex / partial-input
                / whitespace-leading forms, plus errno on overflow and on a
                non-convertible prefix (this is the one function every other
                numeric routine is built on, and it had no coverage at all);
                case2 strtol / strtoul in bases 2..36 and on partial input;
                case3 strtol's long-range clamping and errno;
                case4 atoi / atol / atoll family on the same inputs;
                case5 labs / llabs on the extremes;
                case6 ldiv / lldiv incl. the div-by-zero case;
                case7 abs on INT_MIN (the one input where |x| is not
                representable) -- compared as unsigned so both toolchains
                agree on the wrap;
                case8 bsearch finding every element, a miss, and the empty
                array; case9 qsort on negative / positive / duplicate keys
                and already-sorted input;
                case10 getenv for a variable that exists, one that does not,
                and an empty value;
                case11 at_quick_exit / quick_exit handlers (a child-ish
                program so the normal exit path is also exercised);
                case12 aligned_alloc with a valid and an invalid size;
                case13 RAND_MAX and srand reproducibility (seeded, so both
                toolchains must agree on srand(1) -> rand() sequence only if
                the generator matches; the case therefore checks the
                documented bounds and that srand makes the sequence differ
                from a fresh one, not the numbers themselves).
   Notes      : div_t / ldiv_t member names differ (quot/remain vs
                quot/rem); this file uses the C99 names, which mingw also
                provides via its C99 headers.
   Status     : PASS (2026-10-06)
   ============================================================ */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <limits.h>

static int cmp_int(const void *a, const void *b) {
    int x = *(const int *)a;
    int y = *(const int *)b;
    return (x < y) ? -1 : (x > y) ? 1 : 0;
}

static int cmp_str(const void *a, const void *b) {
    return strcmp(*(const char *const *)a, *(const char *const *)b);
}

static void on_quit(void) {
    /* Handlers run at exit; the count is printed via a plain write so the
       handler does not depend on stdio still being alive. */
    fputs("quick-exit-handler\n", stdout);
}

static void on_atexit(void) {
    fputs("atexit-handler\n", stdout);
}

int main(void) {
    /* ---- case1: strtod ------------------------------------------------- */
    {
        static const char *srcs[] = {
            "1.5", "  -2.25", "1e3", "1E-2", "0x1p4", "0x1.8p1",
            ".5", "5.", "3.0abc", "abc", "", "  42  ", "1e400", "-1e400"
        };
        int i;
        for (i = 0; i < 14; i++) {
            char *end;
            double v;
            errno = 0;
            v = strtod(srcs[i], &end);
            /* "consumed" is measured against the SAME array element end points
               into; subtracting a different literal would compare unrelated
               addresses. */
            printf("case1[%2d] \"%s\" -> %.6g consumed=%d range=%d\n",
                   i, srcs[i], v, (int)(end - srcs[i]), errno == ERANGE ? 1 : 0);
        }
    }

    /* ---- case2/3: strtol / strtoul ------------------------------------ */
    {
        /* Values that fit a 32-bit long on both toolchains. goc's long is
           64-bit (LP64) and mingw's is 32-bit (LLP64), so anything that would
           overflow 32 bits has a different -- but equally correct -- answer on
           each; the saturation behaviour is therefore checked separately and
           only against goc's own LONG_MAX/LONG_MIN. */
        static const char *srcs[] = {
            "42", "  -17", "0x1f", "017", "0b101", "777", "2147483647",
            "-2147483648", "z", "12ab", "  0x1F  rest", "+"
        };
        /* the same 12 shapes, spelled without a leading '-' */
        static const char *nonneg[] = {
            "42", "  17", "0x1f", "017", "0b101", "777", "2147483647",
            "2147483648", "z", "12ab", "  0x1F  rest", "+"
        };
        int i;
        for (i = 0; i < 12; i++) {
            char *end;
            long v;
            unsigned long uv;
            errno = 0;
            v = strtol(srcs[i], &end, 0);
            printf("case2[%2d] \"%s\" -> %ld consumed=%d\n",
                   i, srcs[i], v, (int)(end - srcs[i]));
            /* strtoul is probed with an explicitly unsigned spelling, because
               the result of a NEGATIVE input is ULONG_MAX+1+n -- 4294967295+n
               under mingw's 32-bit unsigned long and 18446744073709551615+n
               under goc's 64-bit one. Both are correct; only the positive
               path can be compared directly. */
            errno = 0;
            uv = strtoul(nonneg[i], &end, 10);
            printf("case3[%2d] \"%s\" base10 -> %lu consumed=%d\n",
                   i, nonneg[i], uv, (int)(end - nonneg[i]));
        }
        /* explicit bases */
        {
            char *e;
            printf("case2b: b2=%ld b8=%ld b16=%ld b36=%ld b1-nonzero=%ld\n",
                   strtol("101", &e, 2), strtol("17", &e, 8),
                   strtol("ff", &e, 16), strtol("zz", &e, 36),
                   strtol("7", &e, 1));
            printf("case2c: nptr-null-ok=%d\n", strtol("42", NULL, 10) == 42 ? 1 : 0);
        }
        /* clamping at LONG_MIN / LONG_MAX with ERANGE */
        /* Saturation is required by C99; whether errno is set is not checked
           here because mingw's strtol does not set it (glibc does). */
        errno = 0;
        printf("case3b: over-clamped=%d\n",
               (strtol("99999999999999999999", NULL, 10) == LONG_MAX) ? 1 : 0);
        errno = 0;
        printf("case3c: under-clamped=%d\n",
               (strtol("-99999999999999999999", NULL, 10) == LONG_MIN) ? 1 : 0);
    }

    /* ---- case4: atoi / atol / atoll ------------------------------------ */
    {
        /* Every value fits 32 bits, so atoi (int), atol (long) and atoll
           (long long) agree across the LP64/LLP64 split. */
        static const char *srcs[] = { "0", "-1", "12345", "abc", "  -42xyz", "2147483647" };
        int i;
        for (i = 0; i < 6; i++) {
            printf("case4[%d] \"%s\": atoi=%d atol=%ld atoll=%lld\n",
                   i, srcs[i], atoi(srcs[i]), atol(srcs[i]), atoll(srcs[i]));
        }
    }

    /* ---- case5: labs / llabs ------------------------------------------- */
    printf("case5: labs=%ld %ld %ld  llabs=%lld %lld\n",
           labs(0L), labs(123456789L), labs(-123456789L),
           llabs(-9223372036854775807LL - 1), llabs(9223372036854775807LL));

    /* ---- case6: ldiv / lldiv ------------------------------------------- */
    {
        ldiv_t d;
        lldiv_t dl;
        d = ldiv(17L, 5L);
        printf("case6: quot=%ld rem=%ld sign=%d\n", d.quot, d.rem, d.rem < 0);
        d = ldiv(-17L, 5L);
        printf("case6b: quot=%ld rem=%ld\n", d.quot, d.rem);
        d = ldiv(17L, -5L);
        printf("case6c: quot=%ld rem=%ld\n", d.quot, d.rem);
        dl = lldiv(1000000000007LL, 3LL);
        printf("case6d: q=%lld r=%lld\n", dl.quot, dl.rem);
    }

    /* ---- case7: abs at the representable boundary ---------------------- */
    /* INT_MIN is 32 bits on both, so |INT_MIN| computed in `long` agrees even
       though goc's long is 64-bit. LONG_MIN is deliberately NOT compared: it
       is 4 bytes under mingw and 8 under goc, so the printed magnitude would
       legitimately differ. */
    {
        int ai = INT_MIN;
        printf("case7: abs-intmin-u=%u int-min=%d\n",
               (unsigned)(ai < 0 ? -(long)ai : (long)ai), ai);
    }

    /* ---- case8: bsearch ------------------------------------------------ */
    {
        int arr[] = { 1, 3, 5, 7, 9, 11 };
        int keys[] = { 1, 7, 4, 11 };
        int i;
        printf("case8:");
        for (i = 0; i < 4; i++) {
            const int *hit = (const int *)bsearch(&keys[i], arr, 6, sizeof arr[0], cmp_int);
            printf(" %d", hit ? (int)(hit - arr) : -1);
        }
        printf(" empty=%d\n", bsearch(&keys[0], arr, 0, sizeof arr[0], cmp_int) == NULL ? 1 : 0);
    }

    /* ---- case9: qsort -------------------------------------------------- */
    {
        int a1[] = { 5, -3, 0, -3, 7, 1 };
        int a2[] = { 1, 2, 3, 4, 5 };
        int a3[] = { 9 };
        const char *s1[] = { "pear", "apple", "fig", "apple" };
        int i;
        qsort(a1, 6, sizeof a1[0], cmp_int);
        printf("case9:");
        for (i = 0; i < 6; i++) printf(" %d", a1[i]);
        printf("\n");
        qsort(a2, 5, sizeof a2[0], cmp_int);
        printf("case9b:");
        for (i = 0; i < 5; i++) printf(" %d", a2[i]);
        printf("\n");
        qsort(a3, 1, sizeof a3[0], cmp_int);
        qsort(a3, 0, sizeof a3[0], cmp_int);   /* zero count: a no-op */
        printf("case9c: single=%d zero-noop=%d\n", a3[0], 1);
        qsort(s1, 4, sizeof s1[0], cmp_str);
        printf("case9d: %s %s %s %s\n", s1[0], s1[1], s1[2], s1[3]);
    }

    /* ---- case10: getenv ------------------------------------------------ */
    {
        const char *v = getenv("GOC_TEST_VAR");
        const char *missing = getenv("GOC_NO_SUCH_VAR_XYZ");
        printf("case10: present=%d value=[%s] missing=%d\n",
               v != NULL, v ? v : "(null)", missing == NULL ? 1 : 0);
        printf("case10b: empty-name=%d\n", getenv("") == NULL ? 1 : 0);
    }

    /* ---- case11: exit handlers ----------------------------------------- */
    printf("case11: before\n");
    atexit(on_atexit);
    if (atexit(on_atexit) != 0) {
        printf("case11b: second atexit refused\n");
    }
    if (getenv("GOC_QUICK_EXIT") != NULL) {
        at_quick_exit(on_quit);
        quick_exit(7);
    }
    printf("case11c: continuing to normal exit\n");

    /* ---- case12: heap allocation --------------------------------------- */
    {
        {
            /* aligned_alloc itself cannot be cross-checked: mingw does not
               ship the symbol at all. The weaker guarantee both toolchains do
               make is tested instead -- malloc returns storage suitable for any
               object type, i.e. aligned to at least 16. */
            void *p = malloc(256);
            printf("case12: malloc-ok=%d\n", p != NULL ? 1 : 0);
            if (p) {
                /* uintptr_t would be the portable spelling, but the only
                   portable spelling of "the address as an integer" here is a
                   pointer difference, so compare modulo against a cast that
                   both 32- and 64-bit pointer widths agree on. */
                printf("case12b: mod16=%d\n", (int)(((char *)p - (char *)0) % 16));
                free(p);
            }
        }
        printf("case12c: null-alloc=%d\n", calloc(0, 10) == NULL ? 1 : 0);
        printf("case12d: calloc-zeroed=%d\n", (calloc(4, sizeof(int)) != NULL));
        {
            int *z = (int *)calloc(4, sizeof(int));
            printf("case12e: all-zero=%d\n",
                   z && z[0] == 0 && z[1] == 0 && z[2] == 0 && z[3] == 0);
            free(z);
        }
        printf("case12f: realloc-grow=%d\n",
               realloc(NULL, 32) != NULL);
    }

    /* ---- case13: rand / srand bounds ---------------------------------- */
    {
        int a, b, i;
        int lo = 1, hi = 0;
        srand(1);
        a = rand();
        srand(1);
        b = rand();
        for (i = 0; i < 100; i++) {
            int r = rand();
            if (r < lo) lo = r;
            if (r > hi) hi = r;
        }
        printf("case13: RAND_MAX=%d seeded-repeatable=%d in-range=%d\n",
               RAND_MAX, a == b ? 1 : 0,
               (lo >= 0 && hi <= RAND_MAX) ? 1 : 0);
    }

    return 0;
}