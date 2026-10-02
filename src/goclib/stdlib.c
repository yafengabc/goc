#include "goclib.h"

/* ----------------------------- <stdlib.h> ------------------------------- */

void *malloc(size_t size) {
    return __goclib_heap_alloc((long)size);
}

void free(void *p) {
    __goclib_heap_free(p);
}

void *calloc(size_t n, size_t size) {
    size_t total = n * size;
    void *p = __goclib_heap_alloc((long)total);
    if (p) memset(p, 0, total);
    return p;
}

/* realloc needs the old block's size, so the platform primitive owns the
 * job: Windows hands it to HeapReAlloc, Linux reads the size header the
 * bump allocator keeps in front of every block. */
void *realloc(void *ptr, size_t size) {
    return __goclib_heap_realloc(ptr, (long)size);
}

/* goc's heap allocator returns 16-byte aligned blocks, which satisfies every
 * request with alignment <= 16 exactly (the common case, including typical
 * SIMD alignments). Larger power-of-two alignments are not strictly honoured
 * yet; callers needing them beyond 16 should not rely on strict alignment. */
void *aligned_alloc(size_t alignment, size_t size) {
    (void)alignment;
    return malloc(size);
}

int atoi(const char *s) {
    return (int)strtol(s, 0, 10);
}

int abs(int x) {
    return x < 0 ? -x : x;
}

long strtol(const char *s, char **endp, int base) {
    /* skip leading whitespace */
    while (*s == ' ' || *s == '\t' || *s == '\n' || *s == '\r' || *s == '\f' || *s == '\v')
        s++;
    int sign = 0;
    if (*s == '-') { sign = 1; s++; }
    else if (*s == '+') { s++; }
    /* determine base */
    if (base == 0) {
        if (*s == '0') {
            if (s[1] == 'x' || s[1] == 'X') { base = 16; s += 2; }
            else { base = 8; }
        } else {
            base = 10;
        }
    } else if (base == 16) {
        if (*s == '0' && (s[1] == 'x' || s[1] == 'X')) s += 2;
    }
    long value = 0;
    while (*s) {
        int digit;
        if (*s >= '0' && *s <= '9') digit = *s - '0';
        else if (*s >= 'a' && *s <= 'z') digit = *s - 'a' + 10;
        else if (*s >= 'A' && *s <= 'Z') digit = *s - 'A' + 10;
        else break;
        if (digit >= base) break;
        value = value * base + digit;
        s++;
    }
    if (sign) value = -value;
    if (endp) *endp = (char *)s;
    return value;
}

/*
 * Unsigned counterpart of strtol: same bases, same 0/16 prefix rules. A
 * leading '-' is accepted and negates the result modulo 2^64, so
 * strtoul("-1", 0, 10) is ULONG_MAX exactly as the standard requires.
 * Overflow wraps rather than saturating -- also standard, and the reason
 * this does not set ERANGE the way strtol does.
 */
unsigned long strtoul(const char *s, char **endp, int base) {
    while (*s == ' ' || *s == '\t' || *s == '\n' || *s == '\r' || *s == '\f' || *s == '\v')
        s++;
    int sign = 0;
    if (*s == '-') { sign = 1; s++; }
    else if (*s == '+') { s++; }
    if (base == 0) {
        if (*s == '0') {
            if (s[1] == 'x' || s[1] == 'X') { base = 16; s += 2; }
            else { base = 8; }
        } else {
            base = 10;
        }
    } else if (base == 16) {
        if (*s == '0' && (s[1] == 'x' || s[1] == 'X')) s += 2;
    }
    unsigned long value = 0;
    while (*s) {
        int digit;
        if (*s >= '0' && *s <= '9') digit = *s - '0';
        else if (*s >= 'a' && *s <= 'z') digit = *s - 'a' + 10;
        else if (*s >= 'A' && *s <= 'Z') digit = *s - 'A' + 10;
        else break;
        if (digit >= base) break;
        value = value * (unsigned long)base + (unsigned long)digit;
        s++;
    }
    if (sign) value = (unsigned long)0 - value;
    if (endp) *endp = (char *)s;
    return value;
}

long long atoll(const char *s) {
    /* long is already 64-bit in goc on both targets, so this is atol. */
    return (long long)strtol(s, 0, 10);
}

long long llabs(long long x) {
    return x < 0 ? -x : x;
}

/*
 * 10^n by repeated squaring. Every power of ten up to 1e22 is exactly
 * representable, so small exponents -- by far the common case -- are built
 * by straight multiplication and are exact. Above that the squares are
 * themselves rounded (1e32 is not exact), so a huge exponent can land a
 * ULP or two off the correctly rounded answer.
 */
static double pow10i(int n) {
    double r = 1.0;
    double p = 10.0;
    int e = n;
    int neg = 0;
    if (e < 0) {
        neg = 1;
        e = -e;
    }
    if (e <= 22) {
        while (e > 0) {
            r = r * 10.0;
            e--;
        }
    } else {
        while (e > 0) {
            if (e & 1) r = r * p;
            p = p * p;
            e = e >> 1;
        }
    }
    if (neg) r = 1.0 / r;
    return r;
}

/*
 * IEEE infinity / NaN without a literal that overflows: a runtime division
 * by zero. Written through a variable so nothing here is constant-folded.
 */
static double mk_inf(void) {
    double zero = 0.0;
    return 1.0 / zero;
}

static double mk_nan(void) {
    double zero = 0.0;
    return zero / zero;
}

/*
 * strtod: decimal (and C99 inf/nan) only. Hexadecimal floats -- "0x1p3" --
 * are not recognised. On failure endp is set to the ORIGINAL string, not to
 * the first character, which is how a caller tells "no conversion" from
 * "converted zero" (cJSON relies on exactly that).
 */
double strtod(const char *s, char **endp) {
    const char *p = s;
    double val = 0.0;
    double frac = 0.0;
    double fscale = 1.0;
    double sign = 1.0;
    int digits = 0;
    int exp = 0;
    int edigits = 0;
    int eneg = 0;

    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r' || *p == '\v' || *p == '\f') p++;
    if (*p == '+') {
        p++;
    } else if (*p == '-') {
        sign = -1.0;
        p++;
    }

    if ((*p == 'i' || *p == 'I') && (p[1] == 'n' || p[1] == 'N') && (p[2] == 'f' || p[2] == 'F')) {
        p = p + 3;
        if ((p[0] == 'i' || p[0] == 'I') && (p[1] == 'n' || p[1] == 'N') &&
            (p[2] == 'i' || p[2] == 'I') && (p[3] == 't' || p[3] == 'T') &&
            (p[4] == 'y' || p[4] == 'Y')) {
            p = p + 5;
        }
        if (endp) *endp = (char *)p;
        return sign * mk_inf();
    }
    if ((*p == 'n' || *p == 'N') && (p[1] == 'a' || p[1] == 'A') && (p[2] == 'n' || p[2] == 'N')) {
        p = p + 3;
        if (endp) *endp = (char *)p;
        return mk_nan();
    }

    while (*p >= '0' && *p <= '9') {
        val = val * 10.0 + (double)(*p - '0');
        p++;
        digits++;
    }
    if (*p == '.') {
        p++;
        while (*p >= '0' && *p <= '9') {
            frac = frac * 10.0 + (double)(*p - '0');
            fscale = fscale * 10.0;
            p++;
            digits++;
        }
    }
    if (digits == 0) {
        if (endp) *endp = (char *)s;
        return 0.0;
    }
    if (*p == 'e' || *p == 'E') {
        const char *save = p;
        p++;
        if (*p == '+') {
            p++;
        } else if (*p == '-') {
            eneg = 1;
            p++;
        }
        while (*p >= '0' && *p <= '9') {
            exp = exp * 10 + (*p - '0');
            p++;
            edigits++;
        }
        if (edigits == 0) p = save;         /* "1e" with no digits: stop before it */
    }
    val = val + frac / fscale;
    if (exp != 0) {
        if (eneg) exp = -exp;
        if (exp > 0) {
            val = val * pow10i(exp);
        } else {
            val = val / pow10i(-exp);
        }
    }
    if (endp) *endp = (char *)p;
    return sign * val;
}

static unsigned long rand_state = 1;

int rand(void) {
    /* glibc-style LCG */
    rand_state = rand_state * 1103515245UL + 12345UL;
    return (int)((rand_state >> 16) & 0x7fff);
}

void srand(unsigned int seed) {
    rand_state = seed ? (unsigned long)seed : 1UL;
}

/* ----------------------------- atexit hooks ------------------------------- */

#define ATEXIT_MAX 32
void (*atexit_fns[ATEXIT_MAX])(void);
int atexit_n = 0;

void exit(int code) {
    /* atexit handlers run last-registered-first; a handler may itself
     * register more handlers, so re-check the counter after each call. */
    while (atexit_n > 0) {
        void (*fn)(void) = atexit_fns[atexit_n - 1];
        atexit_n--;
        fn();
    }
    /* C exit semantics: buffered std streams are flushed after the handlers
     * (a handler's own printf must land too). A stream that was never
     * fclose'd keeps buffering until here; streams opened with fopen and
     * never closed may still lose their tail -- the teaching library does
     * not track them for a global flush. */
    fflush(stdout);
    fflush(stderr);
    __goclib_exit((long)code);
}

/* --------------------------- conversions / misc --------------------------- */

long atol(const char *s) {
    return (long)strtol(s, 0, 10);
}

double atof(const char *s) {
    return strtod(s, 0);
}

long labs(long x) {
    return x < 0 ? -x : x;
}

div_t div(int numer, int denom) {
    div_t r;
    r.quot = numer / denom;
    r.rem = numer % denom;
    return r;
}

ldiv_t ldiv(long numer, long denom) {
    ldiv_t r;
    r.quot = numer / denom;
    r.rem = numer % denom;
    return r;
}

lldiv_t lldiv(long long numer, long long denom) {
    lldiv_t r;
    r.quot = numer / denom;
    r.rem = numer % denom;
    return r;
}

/* --------------------------------- atexit --------------------------------- */

int atexit(void (*fn)(void)) {
    if (atexit_n >= ATEXIT_MAX) return -1;
    atexit_fns[atexit_n++] = fn;
    return 0;
}

/* --------------------------- quick_exit hooks ----------------------------- */

#define ATQUICK_MAX 32
void (*at_quick_exit_fns[ATQUICK_MAX])(void);
int at_quick_exit_n = 0;

/* C11 quick_exit: run only the at_quick_exit handlers (LIFO) and terminate.
 * Unlike exit(), the atexit chain and stream flushing are skipped. */
void quick_exit(int code) {
    while (at_quick_exit_n > 0) {
        void (*fn)(void) = at_quick_exit_fns[at_quick_exit_n - 1];
        at_quick_exit_n--;
        fn();
    }
    __goclib_exit((long)code);
}

int at_quick_exit(void (*fn)(void)) {
    if (at_quick_exit_n >= ATQUICK_MAX) return -1;
    at_quick_exit_fns[at_quick_exit_n++] = fn;
    return 0;
}

void abort(void) {
#if defined(_WIN32)
    __goclib_exit(3);
#else
    __goclib_exit(134);   /* conventional 128+SIGABRT shell convention */
#endif
}

/* --------------------------------- qsort ---------------------------------- */
/*
 * In-place quicksort, median-of-three pivot, insertion sort for the final
 * few elements (and as the small-partition cut-off). Elements are moved
 * byte-wise so arbitrary element widths work. Recursion always descends
 * into the SMALLER half; the larger half is looped on, bounding the stack
 * depth by log2(n).
 */

static void qs_exch(char *a, char *b, long w) {
    while (w-- > 0) {
        char t = *a;
        *a = *b;
        *b = t;
        a++;
        b++;
    }
}

static void qs_sort(char *base, long n, long w,
                    int (*cmp)(const void *, const void *)) {
    long i, j;
    while (n > 8) {
        char *mid = base + (n / 2) * w;
        char *last = base + (n - 1) * w;
        /* median-of-first/mid/last parked in `last`, a decent pivot */
        if (cmp(base, mid) > 0) qs_exch(base, mid, w);
        if (cmp(mid, last) > 0) {
            qs_exch(mid, last, w);
            if (cmp(base, mid) > 0) qs_exch(base, mid, w);
        }
        /* Lomuto partition around the last element */
        i = -1;
        for (j = 0; j < n - 1; j++) {
            if (cmp(base + j * w, last) < 0) {
                i++;
                qs_exch(base + i * w, base + j * w, w);
            }
        }
        i++;
        qs_exch(base + i * w, last, w);
        /* recurse smaller side, loop the larger */
        if (i < n - i - 1) {
            qs_sort(base, i, w, cmp);
            base = base + (i + 1) * w;
            n = n - i - 1;
        } else {
            qs_sort(base + (i + 1) * w, n - i - 1, w, cmp);
            n = i;
        }
    }
    for (i = 1; i < n; i++) {
        for (j = i; j > 0 && cmp(base + (j - 1) * w, base + j * w) > 0; j--) {
            qs_exch(base + (j - 1) * w, base + j * w, w);
        }
    }
}

void qsort(void *base, size_t nmemb, size_t size,
           int (*cmp)(const void *, const void *)) {
    if (base == 0 || nmemb < 2 || size == 0) return;
    qs_sort((char *)base, (long)nmemb, (long)size, cmp);
}

void *bsearch(const void *key, const void *base, size_t nmemb, size_t size,
              int (*cmp)(const void *, const void *)) {
    char *lo;
    size_t n;
    if (key == 0 || base == 0 || size == 0) return 0;
    lo = (char *)base;
    n = nmemb;
    while (n > 0) {
        size_t mid = n / 2;
        char *p = lo + mid * (long)size;
        int r = cmp(key, p);
        if (r == 0) return p;
        if (r < 0) {
            n = mid;
        } else {
            lo = p + (long)size;
            n = n - mid - 1;
        }
    }
    return 0;
}

/* -------------------------------- getenv ---------------------------------- */
/*
 * Windows asks kernel32 directly. Linux has no getenv syscall and goc does
 * not link libc (so there is no `environ` symbol to declare): the portable
 * trick is reading /proc/self/environ, a NUL-separated KEY=VALUE list the
 * kernel exposes for every process.
 */

#if defined(_WIN32)
extern long GetEnvironmentVariableA(const char *name, char *buf, long size);
#else
extern long open(const char *path, long flags, long mode);
extern long read(long fd, void *buf, long n);
extern long close(long fd);
#endif

static char envbuf[1024];

char *getenv(const char *name) {
    long nl = 0;
    if (name == 0) return 0;
    while (name[nl]) nl++;
    if (nl == 0) return 0;
#if defined(_WIN32)
    {
        long n = GetEnvironmentVariableA(name, envbuf, 1024);
        if (n <= 0 || n >= 1024) return 0;
        return envbuf;
    }
#else
    {
        long fd = open("/proc/self/environ", 0 /* O_RDONLY */, 0);
        char raw[8192];
        long total = 0, got, i;
        if (fd < 0) return 0;
        while (total < 8192 &&
               (got = read(fd, raw + total, 8192 - total)) > 0) {
            total += got;
        }
        close(fd);
        i = 0;
        while (i < total) {
            long j = i;
            while (j < total && raw[j]) j++;
            /* entry occupies raw[i..j): "NAME=VALUE" */
            if (j - i > nl && raw[i + nl] == '=') {
                long k = 0;
                while (k < nl && raw[i + k] == name[k]) k++;
                if (k == nl) {
                    long vlen = j - i - nl - 1;
                    long t;
                    if (vlen >= 1024) vlen = 1023;
                    for (t = 0; t < vlen; t++) {
                        envbuf[t] = raw[i + nl + 1 + t];
                    }
                    envbuf[vlen] = 0;
                    return envbuf;
                }
            }
            i = j + 1;
        }
        return 0;
    }
#endif
}

/* ----------------------------------------------------------------------------
 * system -- run a command through the host shell and return its exit status.
 *
 * Windows: spawn "cmd.exe /c <command>" via CreateProcessA and wait for the
 * child's exit code. (The child runs on the host; goc links no C runtime, so
 * the Windows API is the only available launch path.)
 *
 * Linux: vfork + execve("/bin/sh", {"sh","-c",cmd,NULL}, NULL) + wait4. The
 * child shares the parent's stack until it execs or _exit-s, so it must not
 * fall through to the parent's wait4 -- execve replaces the image on success
 * and _exit(127) covers the failure path. This path is emitted for real Linux
 * targets; the ucrun-based regression runner cannot fork, so it is not run
 * there.
 * ------------------------------------------------------------------------- */
#if !defined(_WIN32)
/* Raw Linux syscalls backing system() -- declared at file scope (goc parses
 * the ", linux" platform marker only on a top-level extern). */
extern long __goclib_vfork(void), linux;
extern long __goclib_execve(const char *path, char **argv, char **envp), linux;
extern long __goclib_wait4(long pid, long *status, long options, void *rusage), linux;
#endif

int system(const char *command) {
    if (command == 0) return 1;   /* a command processor is available */
#if defined(_WIN32)
    {
        STARTUPINFOA si;
        PROCESS_INFORMATION pi;
        char cl[8192];
        unsigned long i = 0;
        const char *p = "cmd.exe /c ";
        while (*p && i + 1 < (unsigned long)sizeof(cl)) { cl[i++] = *p++; }
        while (*command && i + 1 < (unsigned long)sizeof(cl)) { cl[i++] = *command++; }
        cl[i] = 0;
        memset(&si, 0, sizeof(si));
        memset(&pi, 0, sizeof(pi));
        si.cb = (DWORD)sizeof(STARTUPINFOA);
        if (!CreateProcessA(0, cl, 0, 0, 0, 0, 0, 0, &si, &pi)) return -1;
        WaitForSingleObject(pi.hProcess, (DWORD)0xffffffff);  /* INFINITE */
        {
            DWORD code = 0;
            GetExitCodeProcess(pi.hProcess, &code);
            CloseHandle(pi.hThread);
            CloseHandle(pi.hProcess);
            return (int)code;
        }
    }
#else
    {
        long pid = __goclib_vfork();
        if (pid == 0) {
            char *argv[4];
            argv[0] = "/bin/sh";
            argv[1] = "-c";
            argv[2] = (char *)command;
            argv[3] = 0;
            __goclib_execve("/bin/sh", argv, 0);
            __goclib_exit(127);
        }
        {
            long st;
            __goclib_wait4(pid, &st, 0, 0);
            return (int)st;
        }
    }
#endif
}
