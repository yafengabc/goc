/* ============================================================
   cstd_lib_stdio2.c - <stdio.h> part 2: v* variadic family, scanf family,
                       fputs, error/EOF flags, buffering, pushback, tmpfile
   Standard   : ISO/IEC 9899:1999 (C99) 7.19 <stdio.h>
   Strategy   : case1 vfprintf to a real file, read back; case2 vprintf /
                vsnprintf incl. truncation return value; case3 fprintf to a
                file and re-read; case4 sscanf conversion counts, width,
                suppression, no-conversion, hex, double; case5 fscanf from a
                file; case6 fputs / fputc / fwrite round trip incl. embedded
                NUL; case7 feof / ferror / clearerr state machine;
                case8 setvbuf modes and setbuf(NULL,_IONBF); case9 ungetc
                pushback and pushback-after-clear; case10 tmpfile
                write/rewind/read; case11 fseek/ftell/rewind matrix with
                SEEK_SET/SEEK_END; case12 a va_list-consuming helper feeding
                vsnprintf at several truncation sizes.
   Notes      : perror writes to stderr and is deliberately not exercised
                (the harness diffs stdout only).
   Status     : PASS (2026-10-06)
   ============================================================ */
#include <stdio.h>
#include <string.h>
#include <stdarg.h>

/* vfprintf/vprintf/vsnprintf all take a va_list, so the variadic work has to
   live in a wrapper. That wrapper is the only correct way to reach them. */
static int file_all(char *tag, const char *fmt, ...) {
    va_list ap;
    int r;
    FILE *f = fopen(tag, "wb+");
    if (f == NULL) return -1;
    va_start(ap, fmt);
    r = vfprintf(f, fmt, ap);
    va_end(ap);
    fclose(f);
    return r;
}

static int to_str(char *buf, size_t sz, const char *fmt, ...) {
    va_list ap;
    int r;
    va_start(ap, fmt);
    r = vsnprintf(buf, sz, fmt, ap);
    va_end(ap);
    return r;
}

static int logmsg(FILE *f, const char *tag, int code) {
    return fprintf(f, "%s=%d\n", tag, code);
}

static int echo(const char *fmt, ...) {
    va_list ap;
    int r;
    va_start(ap, fmt);
    r = vprintf(fmt, ap);
    va_end(ap);
    return r;
}

static int vsum(int n, ...) {
    va_list ap;
    int i, s = 0;
    va_start(ap, n);
    for (i = 0; i < n; i++) s += va_arg(ap, int);
    va_end(ap);
    return s;
}

static void slurp(const char *tag) {
    FILE *f = fopen(tag, "rb");
    char buf[128];
    size_t n;
    if (f == NULL) { printf("  (open %s failed)\n", tag); return; }
    n = fread(buf, 1, sizeof buf - 1, f);
    buf[n] = 0;
    fclose(f);
    remove(tag);
    printf("[%s]", buf);
}

int main(void) {
    char buf[128];
    char small[8];
    int r, a = 0, b = 0;
    double d = 0.0;
    unsigned u = 0u;
    FILE *f;

    /* case1: vfprintf into a real file */
    r = file_all("tmp_vio.txt", "n=%d s=%s f=%.2f", 42, "str", 1.5);
    printf("case1: vfprintf ret=%d read=", r);
    slurp("tmp_vio.txt");

    /* case2: vprintf, and vsnprintf truncation semantics */
    r = echo("case2: vprintf n=%d s=%s\n", 7, "abc");
    printf("case2: vprintf ret=%d\n", r);
    memset(small, '#', sizeof small);
    r = to_str(small, 5, "%d,%d,%d", 11, 22, 33);
    printf("case2b: vsnprintf ret=%d small=[%s]\n", r, small);
    r = to_str(buf, sizeof buf, "%08.3f|%+d|%#x", 3.14159, 42, 255);
    printf("case2c: [%s]\n", buf);

    /* case3: fprintf to a file, read back */
    f = fopen("tmp_fio.txt", "wb+");
    if (f == NULL) { printf("case3: open failed\n"); return 1; }
    logmsg(f, "alpha", 1);
    logmsg(f, "beta", 2);
    fflush(f);
    rewind(f);
    printf("case3: file=");
    {
        char line[64];
        while (fgets(line, (int)sizeof line, f) != NULL) printf("[%s]", line);
    }
    printf("\n");
    fclose(f);
    remove("tmp_fio.txt");

    /* case4: sscanf conversion counts and edges */
    {
        static const char *srcs[] = {
            "12 34", "12 abc", "0x1F 2.5e3", "   -7   ", "notanumber"
        };
        int i;
        for (i = 0; i < 5; i++) {
            int n1 = -1, n2 = -1, k;
            unsigned v1 = 0u;
            double dv = -1.0;
            const char *s = srcs[i];
            k = sscanf(s, "%d %d", &n1, &n2);
            printf("case4[%d] \"%s\": k=%d(%d,%d)\n", i, s, k, n1, n2);
            k = sscanf(s, "%x %lf", &v1, &dv);
            printf("case4[%d] \"%s\": k=%d(%u,%.2f)\n", i, s, k, v1, dv);
        }
        r = sscanf("5 6", "%d %d", &a, &b);
        printf("case4b: two=%d (%d,%d)\n", r, a, b);
        u = 0u;
        r = sscanf("ff", "%x", &u);
        printf("case4c: hex=%d val=%u\n", r, u);
        d = 0.0;
        r = sscanf("2.5", "%lf", &d);
        printf("case4d: dbl=%d val=%.3f\n", r, d);
        a = 0;
        r = sscanf("12345", "%3d", &a);
        printf("case4e: width3=%d val=%d\n", r, a);
        a = 0;
        r = sscanf("skip 42", "%*d %d", &a);
        printf("case4f: suppressed=%d val=%d\n", r, a);
        r = sscanf("xyz", "%d", &a);
        printf("case4g: none=%d\n", r);
        r = sscanf("", "%d", &a);
        printf("case4h: empty=%d\n", r);
        {
            char s1[16], s2[16];
            r = sscanf("ab cd", "%s %s", s1, s2);
            printf("case4i: strs=%d (%s|%s)\n", r, s1, s2);
        }
    }

    /* case5: fscanf from a file */
    f = fopen("tmp_scan.txt", "wb+");
    if (f == NULL) { printf("case5: open failed\n"); return 1; }
    fputs("100 200\nword 3.5\n", f);
    fflush(f);
    rewind(f);
    {
        int x = 0, y = 0;
        char w[16];
        double dd = 0.0;
        int k1 = fscanf(f, "%d %d", &x, &y);
        int k2 = fscanf(f, "%15s %lf", w, &dd);
        printf("case5: k1=%d(%d,%d) k2=%d(%s,%.2f) eof=%d\n",
               k1, x, y, k2, w, dd, feof(f) ? 1 : 0);
    }
    fclose(f);
    remove("tmp_scan.txt");

    /* case6: fputs / fputc / fwrite round trip incl. embedded NUL */
    f = fopen("tmp_fw.txt", "wb+");
    if (f == NULL) { printf("case6: open failed\n"); return 1; }
    fputs("line-one\n", f);
    fputs("", f);
    fputc('X', f);
    {
        static const char raw[] = { 'a', 'b', '\0', 'c' };
        fwrite(raw, 1, sizeof raw, f);
    }
    fflush(f);
    rewind(f);
    {
        char got[32];
        size_t n = fread(got, 1, sizeof got, f);
        int ch;
        printf("case6: read=%u has_nul=%d", (unsigned)n,
               memchr(got, '\0', n) != NULL ? 1 : 0);
        ch = fgetc(f);
        printf(" eofnext=%d\n", ch == EOF ? -1 : ch);
    }
    fclose(f);
    remove("tmp_fw.txt");

    /* case7: feof / ferror / clearerr state machine */
    f = fopen("tmp_err.txt", "wb+");
    if (f == NULL) { printf("case7: open failed\n"); return 1; }
    fputs("ab\n", f);
    fflush(f);
    rewind(f);
    printf("case7: initial eof=%d err=%d\n", feof(f) ? 1 : 0, ferror(f) ? 1 : 0);
    fgetc(f); fgetc(f); fgetc(f);
    printf("case7b: after-read eof=%d\n", feof(f) ? 1 : 0);
    clearerr(f);
    printf("case7c: after-clear eof=%d\n", feof(f) ? 1 : 0);
    fgetc(f);
    a = fgetc(f);
    printf("case7d: reread=%d eof=%d\n", a == EOF ? -1 : a, feof(f) ? 1 : 0);
    fclose(f);
    remove("tmp_err.txt");

    /* case8: setvbuf modes and setbuf */
    f = fopen("tmp_buf.txt", "wb+");
    if (f == NULL) { printf("case8: open failed\n"); return 1; }
    r = setvbuf(f, NULL, _IOFBF, 4096);
    printf("case8: setvbuf-full=%d", r);
    r = setvbuf(f, NULL, _IONBF, 0);
    printf(" setvbuf-none=%d", r);
    setbuf(f, NULL);
    fputs("buf|", f);
    fflush(f);
    rewind(f);
    {
        char g[64];
        size_t n = fread(g, 1, sizeof g, f);
        printf(" read=[%.*s]\n", (int)n, g);
    }
    fclose(f);
    remove("tmp_buf.txt");

    /* case9: ungetc pushback, and pushback after clearerr */
    f = fopen("tmp_ug.txt", "wb+");
    if (f == NULL) { printf("case9: open failed\n"); return 1; }
    fputs("xy", f);
    fflush(f);
    rewind(f);
    a = fgetc(f);
    printf("case9: read=%c", a == EOF ? '?' : a);
    r = ungetc(a, f);
    printf(" ungetc=%d", r);
    a = fgetc(f);
    printf(" reread=%c", a == EOF ? '?' : a);
    a = fgetc(f);
    printf(" next=%c", a == EOF ? '?' : a);
    a = fgetc(f);
    printf(" end=%d eof=%d\n", a == EOF ? -1 : a, feof(f) ? 1 : 0);
    clearerr(f);
    r = ungetc('Z', f);
    printf("case9b: pushback-after-clear=%d\n", r == EOF ? -1 : r);
    fclose(f);
    remove("tmp_ug.txt");

    /* case10: tmpfile write / rewind / read */
    f = tmpfile();
    if (f == NULL) {
        printf("case10: tmpfile unavailable\n");
    } else {
        fputs("temp-content", f);
        fflush(f);
        rewind(f);
        {
            char g[32];
            size_t n = fread(g, 1, sizeof g, f);
            printf("case10: [%.*s] tell=%ld\n", (int)n, g, ftell(f));
        }
        fclose(f);
    }

    /* case11: fseek / ftell / rewind matrix */
    f = fopen("tmp_seek.txt", "wb+");
    if (f == NULL) { printf("case11: open failed\n"); return 1; }
    fputs("0123456789", f);
    fflush(f);
    printf("case11: end=%ld\n", ftell(f));
    fseek(f, 3, SEEK_SET);
    a = fgetc(f);
    printf("case11b: set3=%ld c=%c\n", ftell(f), a == EOF ? '?' : a);
    fseek(f, -2, SEEK_END);
    a = fgetc(f);
    printf("case11c: end-2=%ld c=%c\n", ftell(f), a == EOF ? '?' : a);
    fseek(f, 0, SEEK_CUR);
    printf("case11d: cur=%ld\n", ftell(f));
    rewind(f);
    printf("case11e: after-rewind=%ld\n", ftell(f));
    fclose(f);
    remove("tmp_seek.txt");

    /* case12: the same va_list value fed to vsnprintf at several sizes */
    printf("case12: vsum=%d\n", vsum(4, 1, 2, 3, 4));
    {
        char t[16];
        int r1 = to_str(t, 16, "%s-%d", "abcdef", 7);
        char t2[4];
        int r2 = to_str(t2, 4, "%s-%d", "abcdef", 7);
        printf("case12b: full=%d [%s] trunc=%d [%s]\n", r1, t, r2, t2);
    }

    return 0;
}