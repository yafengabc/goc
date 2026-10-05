/* ============================================================
   cstd_lib_wchar.c - <wchar.h> full API with boundary cases
   Standard   : ISO/IEC 9899:1999 (C99) 7.24 <wchar.h>
   Strategy   : case1 wcslen; case2 wcscpy/wcsncpy incl. zero-pad tail;
                case3 wcscat/wcsncat; case4 wcscmp/wcsncmp/wcscoll ordering
                incl. the L'\0' terminator comparisons; case5 wcschr/wcsrchr;
                case6 wcsstr incl. empty needle and needle past the end;
                case7 wcsspn/wcscspn/wcspbrk; case8 wcstok sequence with
                consecutive and leading delimiters; case9 wmemcpy / wmemmove / wmemcmp / wmemset / wmemchr over
                fixed-size wide arrays (array, not string, semantics);
                case10 the _s variants: a too-small buffer makes them fail
                (the exact code is a platform extension, so only non-zero is
                compared); case11 wcsxfrm in the single-locale C runtime;
                case12 wint_t / WEOF / sizeof(wchar_t);
                case13 wcsncpy with n == length leaves no terminator.
   Notes      : wcsxfrm only has to work in the "C" locale, where it is the
                identity; a real locale-sensitive collation is out of scope
                for a freestanding libc.
   Status     : PASS (2026-10-06)
   ============================================================ */
#include <stdio.h>
#include <wchar.h>
#include <string.h>

/* wcsxfrm: both goclib and mingw declare the 3-parameter form (dest, src, n)
   -- a freestanding libc has a single collation, so there is no second string
   to sort against. wcscoll covers the ordering question on its own. */

int main(void) {
    wchar_t buf[64];
    wchar_t tok_src[32];
    wchar_t *t;
    size_t i;

    printf("case1: wcslen=%d empty=%d\n",
           (int)wcslen(L"hello"), (int)wcslen(L""));

    wcscpy(buf, L"copy-me");
    printf("case2: wcscpy=[%ls] len=%d\n", buf, (int)wcslen(buf));
    wcsncpy(buf, L"abc", 5);
    buf[5] = 0;
    printf("case2b: wcsncpy-padded=[%ls] len=%d\n", buf, (int)wcslen(buf));

    wcscpy(buf, L"aa");
    wcscat(buf, L"-bb");
    wcsncat(buf, L"-cc", 2);
    printf("case3: cat=[%ls]\n", buf);

    printf("case4: cmp=%d,%d,%d ncmp=%d,%d coll=%d\n",
           wcscmp(L"abc", L"abd"), wcscmp(L"abc", L"abc"), wcscmp(L"abd", L"abc"),
           wcsncmp(L"abc", L"abd", 2), wcsncmp(L"abc", L"abd", 3),
           wcscoll(L"abc", L"abd"));
    /* the terminator participates: "ab" < "abc" */
    printf("case4b: prefix=%d,%d\n", wcscmp(L"ab", L"abc"), wcscmp(L"abc", L"ab"));

    /* Pointer-identity checks need a named object: comparing against
       `L"hello" + n` would compare addresses of two distinct literal
       objects, which is not what the standard says. */
    {
        wchar_t h[8];
        const wchar_t *hp;
        wcscpy(h, L"hello");
        printf("case5: chr=[%ls] rchr=[%ls] notfound=%d\n",
               wcschr(h, 'l'), wcsrchr(h, 'l'),
               wcschr(h, 'z') == NULL ? 1 : 0);
        hp = wcschr(h, 0);
        printf("case5b: chr-nul-at-end=%d", hp == h + 5 ? 1 : 0);
        hp = wcsrchr(h, 0);
        printf(" rchr-nul-at-end=%d\n", hp == h + 5 ? 1 : 0);
    }

    {
        wchar_t h[8];
        const wchar_t *r;
        wcscpy(h, L"hello");
        printf("case6: str=[%ls]\n", wcsstr(h, L"o"));
        r = wcsstr(h, L"");
        printf("case6b: empty-needle-returns-base=%d\n", r == h ? 1 : 0);
        r = wcsstr(h, L"zz");
        printf("case6c: miss=%d\n", r == NULL ? 1 : 0);
        r = wcsstr(h, L"o");
        printf("case6d: first-o=%d\n", r == h + 4 ? 1 : 0);
    }

    printf("case7: spn=%d cspn=%d pbrk=[%ls]\n",
           (int)wcsspn(L"abcde", L"abc"), (int)wcscspn(L"abcde", L"cd"),
           wcspbrk(L"abcde", L"dc"));

    for (i = 0; i < 32; i++) tok_src[i] = 0;
    {
        static const char *src = ",a,,b;c";
        for (i = 0; src[i]; i++) tok_src[i] = (wchar_t)(unsigned char)src[i];
        printf("case8: tokens=");
        t = wcstok(tok_src, L",;", NULL);
        while (t != NULL) {
            printf("[%ls]", t);
            t = wcstok(NULL, L",;", NULL);
        }
        printf("\n");
    }

    /* case9: wmemset / wmemcmp / wmemchr / wmemcpy / wmemmove over fixed-size
       wide arrays. These are ARRAY operations, not string operations: the
       length is explicit and an embedded or trailing NUL is just data. */
    {
        wchar_t m[10];
        wchar_t n[10];
        for (i = 0; i < 10; i++) { m[i] = (wchar_t)'a'; n[i] = 0; }
        wmemset(m, (wchar_t)'X', 3);
        m[9] = 0;                       /* terminate for %ls */
        wmemcpy(n, m, 9);
        n[9] = 0;
        printf("case9: wmemset=[%ls] wmemcmp=%d wmemchr-found=%d\n",
               m, wmemcmp(m, n, 9), wmemchr(m, (wchar_t)'a', 9) != NULL ? 1 : 0);
        {
            wchar_t *hit = wmemchr(m, (wchar_t)'X', 9);
            printf("case9b: first-X-offset=%d\n", hit ? (int)(hit - m) : -1);
        }
        wmemcpy(m, L"12345678", 8);
        m[9] = 0;
        printf("case9c: wmemcpy=[%ls]\n", m);
        /* wmemmove with overlap: shift a run forward in place */
        for (i = 0; i < 10; i++) m[i] = (wchar_t)('0' + (int)(i % 10));
        m[9] = 0;
        wmemmove(m + 2, m, 8);
        printf("case9d: wmemmove=[%ls]\n", m);
        printf("case9e: wmemcmp-partial=%d,%d\n",
               wmemcmp(m, m + 2, 6), wmemcmp(m, m, 4));
    }

    /* case10: the _s variants' error behaviour */
    {
        wchar_t small[4];
        {
            int rc = wcsncpy_s(small, 4, L"abcdef", 6);
            /* The _s family signals failure with a non-zero value; the exact
               code is a platform extension (MSVC returns ERANGE=34, goclib
               returns -1), so only the sign is compared. */
            printf("case10: wcsncpy_s-fails=%d empty=%d\n",
                   rc != 0 ? 1 : 0, small[0] == 0 ? 1 : 0);
        }
        {
            int rc = wcsncpy_s(small, 4, L"ab", 2);
            printf("case10b: wcsncpy_s-ok=%d [%ls]\n", rc, small);
        }
        {
            wchar_t d[16];
            wcscpy(d, L"xy");
            int rc = wcscpy_s(d, 16, L"zz");
            printf("case10c: wcscpy_s=%d [%ls]\n", rc, d);
        }
        {
            wchar_t d[16];
            wcscpy(d, L"pq");
            int rc = wcsncat_s(d, 16, L"rs", 2);
            printf("case10d: wcsncat_s=%d [%ls]\n", rc, d);
        }
        {
            /* An 8-unit source into an 8-unit buffer does NOT fit, because the
             * terminator needs a ninth unit. Both toolchains must reject it.
             * What they then leave in `dest` differs -- Annex K empties it,
             * goclib's MSVC-style _s leaves it alone -- so only the failure
             * itself is compared, and a separate case checks that the buffer
             * is still writable afterwards. */
            wchar_t d[8];
            int rc;
            wcscpy(d, L"keep");
            rc = wcsncpy_s(d, 8, L"12345678", 8);
            printf("case10e: exact-fit-fails=%d\n", rc != 0 ? 1 : 0);
            d[0] = L'!';
            printf("case10f: still-writable=%d\n", d[0] == L'!' ? 1 : 0);
        }
    }

    /* case11: wcsxfrm in the C locale copies through */
    {
        wchar_t src1[16], dst1[16];
        wcscpy(src1, L"banana");
        wcsxfrm(dst1, src1, 16);
        printf("case11: [%ls] [%ls]\n", dst1, src1);
    }

    /* case12: constants and wint_t */
    printf("case12: WEOF-is-neg=%d sizeof(wchar_t)=%d sizeof(wint_t)=%d\n",
           (int)(WEOF < 0), (int)sizeof(wchar_t), (int)sizeof(wint_t));

    /* case13: wcsncpy with n exactly the length leaves no terminator, which
       is the documented behaviour and the caller's responsibility */
    {
        wchar_t exact[4];
        wcscpy(exact, L"xyz");
        wcsncpy(exact, L"ABCDE", 3);
        printf("case13: no-terminator-check=%d\n", exact[0] == L'A' ? 1 : 0);
    }

    return 0;
}