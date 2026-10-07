#include "goclib.h"
#include <stdarg.h>
#include <errno.h>
#include <wchar.h>   /* %ls / %lc take wchar_t */

/* ----------------------------- <stdio.h> --------------------------------- */
/*
 * Shared formatter. Writes the expansion of `fmt` (with `ap`) into `out`,
 * stopping after `limit` characters (limit < 0 means "no limit", used by
 * sprintf). Returns the number of characters written.
 */
/* The float formatting machine, all defined well below vfmt but reached from
 * it. They need declaring here, at the first use, because ISO C forbids
 * calling an undeclared function (C99 6.5.2.2): goc tolerates the implicit
 * declaration, gcc/clang reject it, and rejecting it is what keeps this file
 * compilable outside goc. Same reason __goclib_double_to_hex above needs its
 * forward declaration. */
int __goclib_double_to_hex(char *buf, double x, int prec, int hasPrec, int upper, int alt);
int __goclib_double_to_buf(char *buf, double x, int prec);
int __goclib_double_strip_g(char *buf, int n);
int __goclib_double_to_exp(char *buf, double x, int prec, int upper, int strip);
static double fmt_split_dec(double x, int *e10);
static int      fmt_g_exp(double x);

static int vfmt(char *out, long limit, const char *fmt, va_list ap) {
    long n = 0;
    const char *p = fmt;
    while (*p) {
        if (*p != '%') {
            if (limit < 0 || n < limit) out[n] = *p;
            n++;
            p++;
            continue;
        }
        p++;
        if (*p == '%') {
            if (limit < 0 || n < limit) out[n] = '%';
            n++;
            p++;
            continue;
        }
        /* flags: '-' left-justify, '0' zero-pad (numbers only), '+' force a
         * sign for non-negative numbers, ' ' reserve its column, '#' forces the
         * base prefix (0x / 0X for %x/%X, 0 for %o) and the decimal point for
         * %a/%A. (C99 7.19.6.1) */
        int left = 0, zero = 0, alt = 0, plus = 0, blank = 0;
        while (*p == '-' || *p == '0' || *p == '+' || *p == ' ' || *p == '#') {
            if (*p == '-') left = 1;
            else if (*p == '0') zero = 1;
            else if (*p == '+') plus = 1;
            else if (*p == ' ') blank = 1;
            else if (*p == '#') alt = 1;
            p++;
        }
        /* field width: a run of digits, or '*' to read it from the args. */
        int width = 0;
        if (*p == '*') { width = va_arg(ap, int); p++; }
        else { while (*p >= '0' && *p <= '9') { width = width * 10 + (*p - '0'); p++; } }
        /* optional precision: ".NN" digits, a bare "." for zero, or ".*" to
         * read it from the args. Only %f consumes it (fractional digit count);
         * default 6. %a uses the distinction between "no precision" (exact
         * value) and "explicit .N" (rounded fraction), so hasPrec records
         * whether '.' was seen. %s also consumes it, as a maximum count. */
        int prec = 6;
        int hasPrec = 0;
        if (*p == '.') {
            hasPrec = 1;
            p++;
            prec = 0;
            if (*p == '*') { prec = va_arg(ap, int); p++; }
            else { while (*p >= '0' && *p <= '9') { prec = prec * 10 + (*p - '0'); p++; } }
        }
        /* Length modifiers. `wide` (a single `l`) only reinterprets %s/%c as
         * %ls/%lc; the integer conversions need the whole run, because the run
         * is what says how wide the argument is: `l` selects long, `ll`
         * selects long long, and `z`/`j`/`t` select size_t / intmax_t /
         * ptrdiff_t. h/hh select short/char, but the default argument
         * promotions have already widened those to int by the time the
         * argument is stored, so they are read back as int. */
        int wide = 0, longs = 0, sizeT = 0, imax = 0;
        while (*p == 'l' || *p == 'h' || *p == 'L' ||
               *p == 'z' || *p == 'j' || *p == 't') {
            if (*p == 'l') { longs++; wide = 1; }
            else if (*p == 'z' || *p == 't') sizeT = 1;
            else if (*p == 'j') imax = 1;
            p++;
        }
        char spec = *p++;
        /* The converted text lives in `field` (length `fl`); %s keeps its own
         * pointer `s` because it may exceed the static buffer. When a
         * precision caps it, the characters are copied into `field` instead
         * and `s` is cleared, so the padding code can just test `s != 0`. */
        char field[512];
        int fl = 0;
        const char *s = 0;
        if (spec == 's' && wide) {
            /* %ls: a wchar_t array (UTF-16LE on Windows, UTF-32 elsewhere).
             * Only the low byte of each unit is emitted, which is exact for
             * ASCII and lossy beyond it -- documented in <stdio.h>. Emitting
             * a wide unit verbatim would interleave NULs on Windows. */
            const wchar_t *ws = va_arg(ap, const wchar_t *);
            int i;
            if (!ws) { ws = (const wchar_t *)(const void *)"(null)"; }
            for (i = 0; ws[i] != 0; i++) {
                if (hasPrec && i >= prec) break;
                if (fl < (int)sizeof field - 1) field[fl++] = (char)(ws[i] & 0xff);
            }
        } else if (spec == 'c' && wide) {
            field[fl++] = (char)(va_arg(ap, int) & 0xff);
        } else if (spec == 's') {
            s = va_arg(ap, const char *);
            if (!s) s = "(null)";
            if (hasPrec) {
                /* ".P" on %s caps the character count (C99 7.19.6.1p8). Copy
                 * rather than NUL-terminate in place: the caller's string is
                 * const and may live in read-only storage. */
                int i = 0;
                while (i < prec && s[i] && i < (int)sizeof field - 1) {
                    field[fl++] = s[i];
                    i++;
                }
                s = 0;
            }
        } else if (spec == 'c') {
            int c = va_arg(ap, int);
            field[fl++] = (char)c;
        } else if (spec == 'd' || spec == 'i' || spec == 'u' ||
                   spec == 'o' || spec == 'x' || spec == 'X') {
            /* The width of the read follows the length modifier. It cannot be
             * one width for every conversion, because the two ends of the
             * range disagree about how wide a slot is:
             *
             *   %d   -- the default argument promotions deliver an int. On
             *           x86-64 that int arrives in an eight-byte slot, so
             *           reading eight bytes looked harmless, but on a 32-bit
             *           target the slot is four bytes and the upper half came
             *           from whatever was next to it: printf("%d", 0) printed
             *           4294967296 there.
             *   %lld -- the argument really is eight bytes, so reading four
             *           keeps only the low half: printf("%lld", 1LL << 52)
             *           printed 0.
             *
             * Both are the same bug seen from opposite ends -- one hard-coded
             * width for conversions whose widths differ -- so the fix has to
             * be a per-conversion choice, not a different single width. */
            unsigned long long v;
            if (spec == 'd' || spec == 'i') {
                long long sv;
                if (longs >= 2 || imax)       sv = va_arg(ap, long long);
                else if (longs == 1 || sizeT) sv = va_arg(ap, long);
                else                          sv = va_arg(ap, int);
                if (sv < 0) {
                    field[fl++] = '-';
                    /* -(LLONG_MIN) overflows, so the magnitude goes through
                     * -(sv + 1) + 1 rather than through -sv. */
                    v = (unsigned long long)(-(sv + 1)) + 1ull;
                } else {
                    v = (unsigned long long)sv;
                }
            } else {
                if (longs >= 2 || imax)       v = va_arg(ap, unsigned long long);
                else if (longs == 1 || sizeT) v = va_arg(ap, unsigned long);
                else                          v = va_arg(ap, unsigned int);
            }
        /* convert in the chosen base */
        int base = 10;
        if (spec == 'o') base = 8;
        else if (spec == 'x' || spec == 'X') base = 16;
        char tmp[32];
        int t = 0;
        if (v == 0) { tmp[t++] = '0'; }
        while (v > 0) {
            int d = (int)(v % base);
            v /= base;
            if (d < 10) tmp[t++] = (char)('0' + d);
            else tmp[t++] = (char)((spec == 'X' ? 'A' : 'a') + (d - 10));
        }
        while (t-- > 0) field[fl++] = tmp[t];
    } else if (spec == 'a' || spec == 'A') {
            /* %a/%A: hexadecimal floating point, "0x1.9p+3" (C99 7.19.6.1).
             * Default precision = exact value (trailing zeros stripped);
             * explicit .N rounds the fraction to N hex digits. */
            double x = va_arg(ap, double);
            fl = __goclib_double_to_hex(field, x, prec, hasPrec, spec == 'A', alt);
    } else if (spec == 'f' || spec == 'F' || spec == 'e' || spec == 'E' ||
                   spec == 'g' || spec == 'G') {
            /* %f: <prec> fractional digits, rounded half to even, no
             * exponent. %e: the same digits in scientific notation.
             * %g: <prec> SIGNIFICANT digits (C, not fractional), rendered
             * as %f while the decimal exponent stays in [-4, prec) and as
             * %e outside it, with trailing zeros stripped either way.
             * All three share __goclib_double_to_buf with the array
             * printers, so the conversion lives in exactly one place. */
            double x = va_arg(ap, double);
            int upper = (spec == 'E' || spec == 'G' || spec == 'F');
            if (spec == 'f' || spec == 'F') {
                fl = __goclib_double_to_buf(field, x, prec);
            } else if (spec == 'e' || spec == 'E') {
                fl = __goclib_double_to_exp(field, x, prec, upper, 0);
            } else {
                int sig = prec;
                int e10 = fmt_g_exp(x);
                if (sig == 0) sig = 1;          /* "%.0g" means one digit */
                if (e10 < -4 || e10 >= sig) {
                    fl = __goclib_double_to_exp(field, x, sig - 1, upper, 1);
                } else {
                    fl = __goclib_double_to_buf(field, x, sig - 1 - e10);
                    fl = __goclib_double_strip_g(field, fl);
                }
            }
        } else if (spec == 'p') {
            /* %p: "0x" followed by 16 hex digits (full 64-bit address). */
            void *pv = va_arg(ap, void *);
            unsigned long v = (unsigned long)pv;
            field[fl++] = '0';
            field[fl++] = 'x';
            int shift;
            for (shift = 60; shift >= 0; shift -= 4) {
                int d = (int)((v >> shift) & 0xf);
                field[fl++] = (char)(d < 10 ? '0' + d : 'a' + d - 10);
            }
        } else {
            /* unknown specifier: emit it verbatim */
            field[fl++] = spec;
        }
        /* ---- field-width padding ----
         *
         * The converted text is in `field[0..fl)`, except for an uncapped %s
         * whose characters still live at `s`. `sign` is the leading '-', '+'
         * or ' ' that must stay to the left of any zero padding, and `prefix`
         * is the "0x"/"0X" (or "0" for %o) base marker that '#' asks for. Both
         * are computed here rather than baked into `field`, because zero
         * padding has to go *between* them and the digits.
         */
        {
            int numeric = (spec == 'd' || spec == 'i' || spec == 'u' ||
                           spec == 'o' || spec == 'x' || spec == 'X' ||
                           spec == 'f' || spec == 'F' || spec == 'e' ||
                           spec == 'E' || spec == 'g' || spec == 'G' ||
                           spec == 'a' || spec == 'A');
            int signedish = (spec == 'd' || spec == 'i');
            /* the sign character, if any, and where it is in `field` */
            char sign = 0;
            int signlen = 0;
            if (signedish && (plus || blank) && fl > 0 && field[0] != '-') {
                /* "+"/" " only applies to non-negative values; a literal '-'
                 * from the conversion wins over both. */
                sign = plus ? '+' : ' ';
                signlen = 1;
            }
            /* the '#' base marker, if any */
            char pfx[2];
            int pfxlen = 0;
            if (alt) {
                if (spec == 'x') { pfx[0] = '0'; pfx[1] = 'x'; pfxlen = 2; }
                else if (spec == 'X') { pfx[0] = '0'; pfx[1] = 'X'; pfxlen = 2; }
                else if (spec == 'o') { pfx[0] = '0'; pfxlen = 1; }
            }
            /* digits start after any existing '-', so zero padding never
             * lands between the minus and the digits */
            int digoff = (fl > 0 && field[0] == '-') ? 1 : 0;
            int clen = s ? (int)strlen(s) : fl;
            int emit_text = s ? 1 : 0;
            int body = emit_text ? 0 : fl - digoff;   /* digits to write */
            int prelen = (digoff ? 1 : 0) + signlen + pfxlen;

            /* advance() writes one byte honouring `limit` and always counts */
            int k;
#define ADV(ch) do { if (limit < 0 || n < limit) out[n] = (char)(ch); n++; } while (0)

            if (clen >= width || width <= 0) {
                if (signlen) ADV(sign);
                if (pfxlen) { ADV(pfx[0]); if (pfxlen == 2) ADV(pfx[1]); }
                if (emit_text) { while (*s) { ADV(*s); s++; } }
                else { for (k = 0; k < fl; k++) ADV(field[k]); }
            } else {
                int pad = width - clen;
                if (left) {
                    if (signlen) ADV(sign);
                    if (pfxlen) { ADV(pfx[0]); if (pfxlen == 2) ADV(pfx[1]); }
                    if (emit_text) { while (*s) { ADV(*s); s++; } }
                    else { for (k = 0; k < fl; k++) ADV(field[k]); }
                    for (k = 0; k < pad; k++) ADV(' ');
                } else if (zero && numeric) {
                    /* "0" flag: pad with zeros, but only after sign/prefix */
                    char pc = (zero && numeric) ? '0' : ' ';
                    if (digoff) ADV(field[0]);
                    if (signlen) ADV(sign);
                    if (pfxlen) { ADV(pfx[0]); if (pfxlen == 2) ADV(pfx[1]); }
                    for (k = 0; k < pad; k++) ADV(pc);
                    if (emit_text) { while (*s) { ADV(*s); s++; } }
                    else { for (k = 0; k < body; k++) ADV(field[digoff + k]); }
                } else {
                    for (k = 0; k < pad; k++) ADV(' ');
                    if (signlen) ADV(sign);
                    if (pfxlen) { ADV(pfx[0]); if (pfxlen == 2) ADV(pfx[1]); }
                    if (emit_text) { while (*s) { ADV(*s); s++; } }
                    else { for (k = 0; k < body; k++) ADV(field[digoff + k]); }
                }
            }
#undef ADV
        }
    }
    return n;
}

/* ============================ vfmt_lite ====================================
 * Minimal formatters for size-constrained targets.
 *
 * Why they exist: goc prunes the embedded C library by call graph at FUNCTION
 * granularity, so any single printf that reaches the full vfmt drags in the
 * whole float formatting machine -- __goclib_double_to_exp, _to_hex,
 * _strip_g, fmt_pow10, fmt_g_exp, fmt_split_dec and, behind those,
 * frexp/log/log10/fmod. On a host that is noise; on an embedded target it is
 * the whole flash budget.
 *
 * There are two, and the split is the point:
 *
 *   vfmt_i / __goclib_printf_lite     integers, chars, strings, %%. Holds NO
 *                                    reference to a float helper, so a binary
 *                                    that never prints a double links none of
 *                                    double_to_buf, floor, fmod, signbit, fabs,
 *                                    trunc, trunc_to_zero, fmt_int_part.
 *   vfmt_f / __goclib_printf_lite_f   the same plus %f / %F at 6 fractional
 *                                    digits. %f is cheap -- it needs only
 *                                    double_to_buf (floor/fmod/signbit behind
 *                                    it). %e and %g need the exponent estimator
 *                                    (fmt_split_dec -> frexp/log/log10) plus
 *                                    strip_g, so those stay out of both.
 *
 * codegen picks between them by scanning the literal (scanLiteFormat), so a
 * program pays only for the conversions it actually writes.
 *
 * The two bodies below are literal copies differing only in the %f branch.
 * That duplication is deliberate and load-bearing: it is what lets goc's
 * function-granular call-graph pruning drop the float chain from the integer
 * build. It is generated from one template rather than hand-maintained, and
 * goc's preprocessor cannot express it -- it does not constant-fold `0 && x`
 * (so a parameterised macro keeps the branch) and a macro that expands to
 * nothing mid-if-chain leaves a dangling `else`. If you change one, change
 * both, or better: change the template and regenerate.
 *
 * NOT supported, by design: field width, precision, the '-', '0', '+', ' '
 * and '#' flags, length modifiers, %p, and the %e/%g/%a family. A format
 * needing any of those is rejected at compile time and printed by the full
 * vfmt instead, so behaviour is never wrong -- only the code path differs.
 * The compile-time check is what makes this worthwhile: a run-time "try lite,
 * else fall back" probe would keep vfmt reachable and the saving would vanish.
 *
 * Output goes straight to the OS handle via WriteFile/write, bypassing the
 * FILE layer. That drops another ~15KB (fwrite, the stream objects, the
 * seek/write_at machinery) and removes buffering, which is what an embedded
 * console wants. The trade-off: printf_lite output is not interleaved with
 * buffered stdout writes, so mixing printf_lite and printf on one stream can
 * reorder if the latter is still buffered.
 *
 * Both format in two passes (measure, then write) so a long %s gets an exactly
 * sized heap buffer instead of a silent truncation.
 */

/* vfmt_i: no floating point at all. */
static int vfmt_i(char *out, long limit, const char *fmt, va_list ap) {
    long n = 0;
    const char *p = fmt;
    while (*p) {
        if (*p != '%') {
            if (limit < 0 || n < limit) out[n] = *p;
            n++;
            p++;
            continue;
        }
        p++;
        if (*p == '%') {
            if (limit < 0 || n < limit) out[n] = '%';
            n++;
            p++;
            continue;
        }
        /* No flags, width or precision are parsed: scanLiteFormat guarantees
         * none is present, so anything landing here is a bare conversion. */
        char spec = *p++;
        char field[512];
        int fl = 0;
        const char *s = 0;
        if (spec == 's') {
            s = va_arg(ap, const char *);
            if (!s) s = "(null)";
        } else if (spec == 'c') {
            int c = va_arg(ap, int);
            field[fl++] = (char)c;
        } else if (spec == 'd' || spec == 'i' || spec == 'u' ||
                   spec == 'o' || spec == 'x' || spec == 'X') {
            unsigned long v;
            if (spec == 'd' || spec == 'i') {
                int sv = va_arg(ap, int);
                if (sv < 0 && spec != 'u') {
                    field[fl++] = '-';
                    v = (unsigned long)(-(long)sv);
                } else {
                    v = (unsigned long)(unsigned int)sv;
                }
                if (spec == 'u') v = (unsigned long)(unsigned int)sv;
            } else {
                v = va_arg(ap, unsigned int);
            }
            int base = 10;
            if (spec == 'o') base = 8;
            else if (spec == 'x' || spec == 'X') base = 16;
            char tmp[32];
            int t = 0;
            if (v == 0) { tmp[t++] = '0'; }
            while (v > 0) {
                int d = (int)(v % base);
                v /= base;
                if (d < 10) tmp[t++] = (char)('0' + d);
                else tmp[t++] = (char)((spec == 'X' ? 'A' : 'a') + (d - 10));
            }
            while (t-- > 0) field[fl++] = tmp[t];
        } else {
            /* Unreachable: scanLiteFormat rejects anything else. Emitting the
             * specifier verbatim matches vfmt's unknown-specifier behaviour. */
            field[fl++] = spec;
        }
        if (spec == 's') {
            while (*s) { if (limit < 0 || n < limit) out[n] = *s; n++; s++; }
        } else {
            int k;
            for (k = 0; k < fl; k++) {
                if (limit < 0 || n < limit) out[n] = field[k];
                n++;
            }
        }
    }
    return n;
}

/* vfmt_f: identical, plus %f / %F. */
static int vfmt_f(char *out, long limit, const char *fmt, va_list ap) {
    long n = 0;
    const char *p = fmt;
    while (*p) {
        if (*p != '%') {
            if (limit < 0 || n < limit) out[n] = *p;
            n++;
            p++;
            continue;
        }
        p++;
        if (*p == '%') {
            if (limit < 0 || n < limit) out[n] = '%';
            n++;
            p++;
            continue;
        }
        /* No flags, width or precision are parsed: scanLiteFormat guarantees
         * none is present, so anything landing here is a bare conversion. */
        char spec = *p++;
        char field[512];
        int fl = 0;
        const char *s = 0;
        if (spec == 's') {
            s = va_arg(ap, const char *);
            if (!s) s = "(null)";
        } else if (spec == 'c') {
            int c = va_arg(ap, int);
            field[fl++] = (char)c;
        } else if (spec == 'd' || spec == 'i' || spec == 'u' ||
                   spec == 'o' || spec == 'x' || spec == 'X') {
            unsigned long v;
            if (spec == 'd' || spec == 'i') {
                int sv = va_arg(ap, int);
                if (sv < 0 && spec != 'u') {
                    field[fl++] = '-';
                    v = (unsigned long)(-(long)sv);
                } else {
                    v = (unsigned long)(unsigned int)sv;
                }
                if (spec == 'u') v = (unsigned long)(unsigned int)sv;
            } else {
                v = va_arg(ap, unsigned int);
            }
            int base = 10;
            if (spec == 'o') base = 8;
            else if (spec == 'x' || spec == 'X') base = 16;
            char tmp[32];
            int t = 0;
            if (v == 0) { tmp[t++] = '0'; }
            while (v > 0) {
                int d = (int)(v % base);
                v /= base;
                if (d < 10) tmp[t++] = (char)('0' + d);
                else tmp[t++] = (char)((spec == 'X' ? 'A' : 'a') + (d - 10));
            }
            while (t-- > 0) field[fl++] = tmp[t];
        }
        else if (spec == 'f' || spec == 'F') {
            double x = va_arg(ap, double);
            fl = __goclib_double_to_buf(field, x, 6);
        } else {
            /* Unreachable: scanLiteFormat rejects anything else. Emitting the
             * specifier verbatim matches vfmt's unknown-specifier behaviour. */
            field[fl++] = spec;
        }
        if (spec == 's') {
            while (*s) { if (limit < 0 || n < limit) out[n] = *s; n++; s++; }
        } else {
            int k;
            for (k = 0; k < fl; k++) {
                if (limit < 0 || n < limit) out[n] = field[k];
                n++;
            }
        }
    }
    return n;
}

/* Raw output to a standard handle, bypassing the FILE layer. Mirrors the
 * crash-safe writer in rt.c: one WriteFile per call, no stdio state. */
#ifdef _WIN32
extern void *GetStdHandle(long which);
extern long  WriteFile(void *h, const void *buf, long n, long *written, long overlapped);
#define GOC_LITE_STDOUT (-11)  /* STD_OUTPUT_HANDLE: a HANDLE, not an fd */
static int lite_write(const char *s, long n) {
    void *h = GetStdHandle(GOC_LITE_STDOUT);
    long w = 0;
    if (!h || n <= 0) return -1;
    return WriteFile(h, s, n, &w, 0) ? (int)w : -1;
}
#else
#include <syscall.h>   /* write(2), reached per host */
#define GOC_LITE_STDOUT 1      /* stdout is file descriptor 1 */
static int lite_write(const char *s, long n) {
    if (n <= 0) return -1;
    return (int)write(GOC_LITE_STDOUT, s, n);
}
#endif

/* Shared measure-then-write driver.
 *
 * The format engine consumes its va_list, so the measuring pass cannot walk the
 * caller's `ap` directly. va_copy is what makes the second pass possible: it
 * duplicates the whole va_list, leaving the original untouched (C99 7.16.1.1).
 * Note the explicit declaration -- `va_list m = ap;` is not portable C. On
 * x86-64 SysV a va_list is an array type, so initialising one from another is
 * rejected outright ("array initializer must be an initializer list") and even
 * where it compiles the copy would share one cursor instead of duplicating it.
 * va_copy is also a plain builtin in gcc/clang, which is what lets this file
 * build as an ordinary C library outside goc.
 *
 * Copy ap, never an unstarted local: va_start writes through ap, and a copy of
 * a never-started va_list is garbage (symptom: two printf_lite calls in a row
 * exit 127 and the second line is lost, while a single call happens to work). */
static int printf_lite_with(int (*fmtfn)(char *, long, const char *, va_list),
                            const char *fmt, va_list ap) {
    va_list measure;
    va_copy(measure, ap);
    long n = fmtfn(0, 0, fmt, measure);      /* pass 1: length only */
    va_end(measure);
    if (n <= 0) return (int)n;
    if (n <= 512) {
        char buf[512];
        fmtfn(buf, n, fmt, ap);
        int w = lite_write(buf, n);
        return w < 0 ? (int)n : w;
    }
    char *big = (char *)malloc(n + 1);
    if (big != 0) {
        fmtfn(big, n, fmt, ap);
        int w = lite_write(big, n);
        free(big);
        return w < 0 ? (int)n : w;
    }
    char buf[4096];                          /* out of memory: never crash */
    fmtfn(buf, 4096, fmt, ap);
    int w = lite_write(buf, 4096);
    return w < 0 ? 4096 : w;
}

int __goclib_printf_lite(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = printf_lite_with(vfmt_i, fmt, ap);
    va_end(ap);
    return n;
}

int __goclib_printf_lite_f(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = printf_lite_with(vfmt_f, fmt, ap);
    va_end(ap);
    return n;
}

/* ----------------- shared floating-point conversion -----------------------
 * The %f/%g machinery of vfmt, extracted so the array printers can reuse it:
 * a floating-point array is printed element by element with the very same
 * digits printf("%g") would emit. __goclib_double_to_buf is the %f form
 * (<prec> fractional digits, rounded half to even, no exponent);
 * __goclib_double_strip_g drops trailing fractional zeros (the %g form);
 * __goclib_double_g is the default-%g shorthand the array printers use.
 * Rounding runs first, so "0.999999" becomes "1". */
/*
 * Emit the integer part of a finite non-negative double in decimal.
 *
 * The 10^9 chunking is the whole point: a double past 2^63 has no exact
 * integer cast, so `(long)1e19` is undefined and came back as 0, which is
 * what made printf("%g", 1e19) print nothing at all. Every chunk handed to
 * the `unsigned long long` conversion here is below 10^9, so each one is
 * exact, and the recursion stitches the rest of the number together.
 */
static void fmt_int_part(char *buf, int *n, double v) {
    char tmp[24];
    unsigned long long u;
    int t = 0;
    int k;
    if (v >= 1e9) {
        double hi = floor(v / 1e9);
        double lo = v - hi * 1e9;
        fmt_int_part(buf, n, hi);
        u = (unsigned long long)lo;
        for (k = 8; k >= 0; k--) { tmp[k] = (char)('0' + (u % 10)); u /= 10; }
        for (k = 0; k < 9; k++) buf[(*n)++] = tmp[k];
        return;
    }
    u = (unsigned long long)v;
    if (u == 0) { buf[(*n)++] = '0'; return; }
    while (u > 0) { tmp[t++] = (char)('0' + (u % 10)); u /= 10; }
    while (t-- > 0) buf[(*n)++] = tmp[t];
}

int __goclib_double_to_buf(char *buf, double x, int prec) {
    int n = 0;
    int neg = 0;
    double whole;                      /* integer part, kept as a double */
    double frac;
    char dig[20];                      /* prec+1 digits to round on */
    int k;
    /* Infinity and NaN have no digits to convert; spell them out instead of
     * letting floor() propagate them into nonsense. */
    if (x != x) {
        buf[0] = 'n'; buf[1] = 'a'; buf[2] = 'n';
        return 3;
    }
    if (x > 1.7976931348623157e308 || x < -1.7976931348623157e308) {
        if (x < 0) { buf[0] = '-'; n = 1; }
        buf[n] = 'i'; buf[n+1] = 'n'; buf[n+2] = 'f';
        return n + 3;
    }
    /* signbit, not `x < 0`: -0.0 is negative to print even though it is not
     * less than zero, and it is reachable (copysign makes one). */
    if (signbit(x)) { neg = 1; x = -x; }
    if (prec > 17) prec = 17;          /* past double's precision */
    /* A negative precision is reachable, not hypothetical: the %g path calls
     * this with `sig - 1 - e10', which is negative for every value whose
     * decimal exponent is at or past its significant count -- "%.3g" of 1e100
     * arrives as -98. Without a floor the loop below never runs, so dig stays
     * uninitialized, and the rounding step then both reads and increments
     * dig[prec] and dig[prec-1] with a negative index. Clamping to 0 makes the
     * whole tail well-defined: the value prints with no fractional digits,
     * which is the only answer a negative digit count can have. */
    if (prec < 0) prec = 0;
    whole = floor(x);
    frac = x - whole;
    for (k = 0; k <= prec; k++) {
        frac *= 10.0;
        int d = (int)frac;
        dig[k] = (char)d;
        frac -= (double)d;
    }
    /* round half to even on the prec-th digit */
    {
        /* Whatever is left in `frac` -- however far out -- means the digits
         * are not an exact tie. The old test looked at the single next digit
         * only, so 0.125001 rounded down to 0.12: its (prec+2)-th digit is a
         * 0 and the 1 behind it went unseen. The digits of a binary-friendly
         * value (0.125, 2.5) come out exact, so a genuine tie still leaves a
         * clean 0.0 here and takes the half-to-even path. */
        int tail = (frac > 0.0);
        int carry = 0;
        if (prec == 0) {
            int d0 = dig[0];
            /* Parity of the integer part decides the tie: fmod is exact
             * because whole is a whole number below 2^53 or a multiple of
             * a large power of two (always even). */
            int odd = (fmod(whole, 2.0) != 0.0);
            if (d0 > 5 || (d0 == 5 && (tail != 0 || odd))) carry = 1;
        } else {
            int dp = dig[prec];
            if (dp > 5 || (dp == 5 && (tail != 0 || (dig[prec-1] & 1) != 0))) {
                int j = prec - 1;
                dig[j]++;
                while (j > 0 && dig[j] > 9) { dig[j] = 0; dig[--j]++; }
                if (dig[0] > 9) { dig[0] = 0; carry = 1; }
            }
        }
        if (carry) whole = whole + 1.0;
    }
    if (neg) {
        buf[n] = '-';
        n++;
    }
    fmt_int_part(buf, &n, whole);
    if (prec > 0) {
        buf[n] = '.';
        n++;
        int k2;
        for (k2 = 0; k2 < prec; k2++) {
            buf[n] = (char)('0' + dig[k2]);
            n++;
        }
    }
    return n;
}

/* Strips trailing fractional zeros after the '.' in buf[0..n): the "%g"
 * shape. Integer tails survive ("10.000000" -> "10") and a lone point goes
 * too ("1.000000" -> "1"). */
int __goclib_double_strip_g(char *buf, int n) {
    int i;
    for (i = 0; i < n; i++) {
        if (buf[i] == '.') {
            int last = n - 1;
            while (last > i && buf[last] == '0') last--;
            if (last == i) last--; /* nothing but zeros after the point */
            return last + 1;
        }
    }
    return n; /* no '.', nothing to strip */
}

/* -------------------- scientific notation (%e / %g) ------------------------
 * %e needs the decimal exponent of the value; %g needs it to decide which of
 * the two shapes to print. Neither can be left to a plain (int)log10: the
 * estimate is off by one often enough to matter, so it is only used to get
 * close and the real value of x settles it.
 */
/* 10^k, in groups of eight so the accumulated rounding error stays within a
 * couple of ulps. Clamped to 1e-308 .. 1e308 so the result is never 0 or inf
 * -- the renormalising loop in fmt_split_dec undoes the clamp, which is what
 * makes printf("%e", 5e-324) print 4.94066e-324 instead of spinning. */
static double fmt_pow10(int k) {
    int neg = 0;
    double r = 1.0;
    if (k > 308) k = 308;
    if (k < -308) k = -308;
    if (k < 0) { neg = 1; k = -k; }
    while (k >= 8) { r = r * 1e8; k = k - 8; }
    while (k > 0) { r = r * 10.0; k = k - 1; }
    if (neg) r = 1.0 / r;
    return r;
}

/* Splits a finite non-negative x into a mantissa in [1, 10) and the matching
 * decimal exponent (floor(log10 x), 0 for x == 0). */
static double fmt_split_dec(double x, int *e10) {
    int e;
    double m;
    if (x == 0.0) { *e10 = 0; return 0.0; }
    e = (int)floor(log10(x));
    /* Clamp here as well as inside fmt_pow10: below 1e-308 the power
     * underflows, so `e` has to agree with what was actually divided by --
     * otherwise the walk-back loop below subtracts the clamp distance a
     * second time and 5e-324 comes out as e-340. */
    if (e > 308) e = 308;
    if (e < -308) e = -308;
    m = x / fmt_pow10(e);
    while (m >= 10.0) { m = m / 10.0; e = e + 1; }
    while (m > 0.0 && m < 1.0) { m = m * 10.0; e = e - 1; }
    *e10 = e;
    return m;
}

/* floor(log10|x|) for the %g decision; inf/nan give 0 because their digits
 * are spelled out before anyone asks. */
static int fmt_g_exp(double x) {
    int e = 0;
    if (x != x) return 0;
    if (x > 1.7976931348623157e308 || x < -1.7976931348623157e308) return 0;
    if (x < 0) x = -x;
    fmt_split_dec(x, &e);
    return e;
}

/* Scientific notation: [-]d.dddd e(+|-)XX with `prec` digits after the point
 * and at least two exponent digits -- printf's %e. `strip` drops trailing
 * fractional zeros, which is the %g shape ("1.5e+10"). */
int __goclib_double_to_exp(char *buf, double x, int prec, int upper, int strip) {
    int n = 0, neg = 0, e, i, ml;
    double m;
    char mant[64];
    if (x != x) {
        buf[0] = 'n'; buf[1] = 'a'; buf[2] = 'n';
        return 3;
    }
    if (x > 1.7976931348623157e308 || x < -1.7976931348623157e308) {
        if (x < 0) { buf[0] = '-'; n = 1; }
        buf[n] = 'i'; buf[n+1] = 'n'; buf[n+2] = 'f';
        return n + 3;
    }
    if (prec < 0) prec = 0;
    if (prec > 17) prec = 17;
    if (signbit(x)) { neg = 1; x = -x; }
    m = fmt_split_dec(x, &e);
    ml = __goclib_double_to_buf(mant, m, prec);
    /* Rounding can carry a 9.999... mantissa up to 10; renormalise rather
     * than print a leading "10.". */
    {
        int ip = 0;
        while (ip < ml && mant[ip] != '.') ip++;
        if (ip > 1) {
            m = m / 10.0;
            e = e + 1;
            ml = __goclib_double_to_buf(mant, m, prec);
        }
    }
    if (strip) ml = __goclib_double_strip_g(mant, ml);
    if (neg) { buf[n] = '-'; n++; }
    for (i = 0; i < ml; i++) { buf[n] = mant[i]; n++; }
    buf[n] = upper ? 'E' : 'e';
    n++;
    {
        int ae = e;
        if (ae < 0) { buf[n] = '-'; ae = -ae; } else { buf[n] = '+'; }
        n++;
        /* Two exponent digits minimum, three only when needed -- C asks for
         * "at least two", and this is what both glibc and mingw print. */
        if (ae >= 100) {
            buf[n] = (char)('0' + ae / 100); n++;
            buf[n] = (char)('0' + (ae / 10) % 10); n++;
            buf[n] = (char)('0' + ae % 10); n++;
        } else {
            buf[n] = (char)('0' + ae / 10); n++;
            buf[n] = (char)('0' + ae % 10); n++;
        }
    }
    return n;
}

/* __goclib_double_g: printf("%g") for a double -- six significant digits,
 * the %e shape once the exponent leaves [-4, 6), trailing zeros dropped.
 * The array printers share it, so a float array reads the same way. */
int __goclib_double_g(char *buf, double x) {
    int e10 = fmt_g_exp(x);
    if (e10 < -4 || e10 >= 6) return __goclib_double_to_exp(buf, x, 5, 0, 1);
    return __goclib_double_strip_g(buf, __goclib_double_to_buf(buf, x, 5 - e10));
}

/* __goclib_double_to_hex -- C99 %a/%A conversion ("0x1.9p+3").
 *
 * Reads the IEEE-754 bit pattern directly, so the digits are exact and match
 * gcc (ucrt) byte for byte: a missing precision prints all 13 hex fraction
 * digits (trailing zeros included); an explicit .N rounds the fraction to N
 * hex digits (round half to even, carry propagates into the leading digit,
 * and N beyond 13 pads with zeros). Zero prints "0x0.0000000000000p+0";
 * subnormals print as "0x0.HHH...p-1022"; inf as "inf"/"INF"; NaN as
 * "nan(ind)"/"NAN(IND)" with its sign. Returns the length written to buf. */
int __goclib_double_to_hex(char *buf, double x, int prec, int hasPrec, int upper, int alt) {
    union { double d; unsigned long long u; } cv;
    cv.d = x;
    unsigned long long bits = cv.u;
    int neg = (int)((bits >> 63) & 1);
    int ebits = (int)((bits >> 52) & 0x7ff);
    unsigned long long frac = bits & 0xfffffffffffffULL;
    int n = 0;
    int k;
    if (ebits == 0x7ff) {
        if (frac != 0) { /* NaN: the ucrt spelling is "nan(ind)" */
            if (neg) buf[n++] = '-';
            if (upper) { buf[n++]='N'; buf[n++]='A'; buf[n++]='N'; buf[n++]='(';
                         buf[n++]='I'; buf[n++]='N'; buf[n++]='D'; buf[n++]=')'; }
            else       { buf[n++]='n'; buf[n++]='a'; buf[n++]='n'; buf[n++]='(';
                         buf[n++]='i'; buf[n++]='n'; buf[n++]='d'; buf[n++]=')'; }
            return n;
        }
        if (neg) buf[n++] = '-';
        if (upper) { buf[n++]='I'; buf[n++]='N'; buf[n++]='F'; }
        else       { buf[n++]='i'; buf[n++]='n'; buf[n++]='f'; }
        return n;
    }
    if (neg) buf[n++] = '-';
    int e2, H;
    if (frac == 0 && ebits == 0) { e2 = 0; H = 0; }   /* +-0.0: "p+0" */
    else if (ebits == 0) { e2 = -1022; H = 0; }       /* subnormal */
    else { e2 = ebits - 1023; H = 1; }                /* normal */
    /* 13 hex digits of the fraction (bits 51..0), most significant first */
    char d[13];
    for (k = 0; k < 13; k++) d[k] = (char)((frac >> (48 - 4*k)) & 0xf);
    int nd = hasPrec ? prec : 13;
    if (hasPrec && prec < 13) {
        /* round at digit prec: half to even */
        int drop = d[prec];
        int up = 0;
        if (drop > 8) up = 1;
        else if (drop == 8) {
            int any = 0;
            for (k = prec + 1; k < 13; k++) if (d[k] != 0) { any = 1; break; }
            if (any) up = 1;
            else if (prec > 0 && (d[prec-1] & 1) != 0) up = 1;
            else if (prec == 0 && (H & 1) != 0) up = 1; /* tie -> even */
        }
        if (up) {
            for (k = prec - 1; k >= 0; k--) {
                d[k]++;
                if (d[k] < 16) break;
                d[k] = 0;
            }
            if (k < 0) H++;      /* carry out of the fraction */
        }
    }
    buf[n++] = '0';
    buf[n++] = upper ? 'X' : 'x';
    buf[n++] = (char)('0' + H);
    if (nd > 0) {
        buf[n++] = '.';
        for (k = 0; k < nd && k < 13; k++) buf[n++] = (char)(d[k] < 10 ? '0' + d[k] : (upper ? 'A' : 'a') + d[k] - 10);
        for (; k < nd; k++) buf[n++] = '0';   /* precision beyond 13 digits */
    } else if (alt) {
        buf[n++] = '.';   /* %#a forces the point even when the fraction is empty */
    }
    buf[n++] = upper ? 'P' : 'p';
    /* signed decimal exponent */
    char et[16];
    int etn = 0;
    int ev = e2;
    if (ev < 0) { et[etn++] = '-'; ev = -ev; }
    else et[etn++] = '+';
    char ed[12]; int edn = 0;
    if (ev == 0) ed[edn++] = '0';
    while (ev > 0) { ed[edn++] = (char)('0' + (ev % 10)); ev /= 10; }
    while (edn > 0) et[etn++] = ed[--edn];
    for (k = 0; k < etn; k++) buf[n++] = et[k];
    return n;
}

int sprintf(char *buf, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = vfmt(buf, -1, fmt, ap);
    buf[n] = 0;
    va_end(ap);
    return n;
}

/* vfprintf formats the text and writes it to `stream`.
 *
 * The old implementation always formatted into a 4096-byte stack buffer, so
 * anything longer was silently truncated -- and a large %s (say a 100,000
 * digit number from _BitInt) was cut to 4094 characters. Formatting is now
 * measured first (limit 0 writes nothing but still counts), and only then
 * written: short text stays on the stack, anything larger gets an exactly
 * sized heap buffer. */
int vfprintf(FILE *stream, const char *fmt, va_list ap) {
    va_list measure;
    va_copy(measure, ap);                /* the caller's list survives intact */
    long n = vfmt(0, 0, fmt, measure);   /* pass 1: length only */
    va_end(measure);
    if (n <= 0) return (int)n;
    if (n <= 512) {
        char buf[512];
        vfmt(buf, n, fmt, ap);
        fwrite(buf, 1, n, stream);
        return (int)n;
    }
    char *big = (char *)malloc(n + 1);
    if (big != 0) {
        vfmt(big, n, fmt, ap);
        fwrite(big, 1, n, stream);
        free(big);
        return (int)n;
    }
    char buf[4096];                    /* out of memory: never crash the caller */
    vfmt(buf, 4096, fmt, ap);
    fwrite(buf, 1, 4096, stream);
    return 4096;
}

int fprintf(FILE *stream, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = vfprintf(stream, fmt, ap);
    va_end(ap);
    return n;
}

int printf(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int n = vfprintf(__goclib_stdout(), fmt, ap);
    va_end(ap);
    return n;
}

int vprintf(const char *fmt, va_list ap) {
    return vfprintf(__goclib_stdout(), fmt, ap);
}

/* snprintf/vsnprintf: vfmt already returns the untruncated length while
 * writing at most `limit` bytes, which is exactly the C11 contract --
 * "return the length the text would have had", buffer capped, NUL added. */
int vsnprintf(char *buf, size_t n, const char *fmt, va_list ap) {
    long cap = (long)n - 1;
    if (cap < 0) cap = 0;
    long m = vfmt(buf, cap, fmt, ap);
    if (m < 0) m = 0;
    buf[m < cap ? m : cap] = 0;
    return (int)m;
}

int snprintf(char *buf, size_t n, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    int r = vsnprintf(buf, n, fmt, ap);
    va_end(ap);
    return r;
}

/* The _s spellings match the MSVC prototype (buffer, sizeOfBuffer, count, ...)
 * and differ from the standard bounded functions only in the return value:
 * MSVC reports a buffer-too-small as -1, whereas C99 snprintf returns the
 * length the text would have had. Callers that only check "did it fit" see the
 * same answer either way.
 *
 * sizeOfBuffer is the buffer's total element count and is the bound handed to
 * vsnprintf (so a caller that passes _TRUNCATE as `count` -- which arrives as
 * (size_t)-1 -- still gets a correctly bounded, NUL-terminated write instead of
 * the unbounded `vsnprintf(buf, (size_t)-1, ...)` the old (buf, count, size)
 * order produced). count is a secondary clamp honoured only when it is a real
 * number; _TRUNCATE fills the whole buffer. */
int _vsnprintf_s(char *buf, size_t sizeOfBuffer, size_t count, const char *fmt, va_list ap) {
    int r;
    if (buf == NULL || sizeOfBuffer == 0) return -1;
    size_t bound = (count == (size_t)-1) ? sizeOfBuffer
                                        : (count + 1 < sizeOfBuffer ? count + 1 : sizeOfBuffer);
    r = vsnprintf(buf, bound, fmt, ap);
    if ((size_t)r >= bound) {
        /* Truncated. _TRUNCATE asks to fill the buffer, so keep what fit; any
         * other count means "did not fit" and MSVC returns -1 with an empty
         * buffer. */
        if (count == (size_t)-1 && bound > 0) buf[bound - 1] = '\0';
        return -1;
    }
    return r;
}

int _snprintf_s(char *buf, size_t sizeOfBuffer, size_t count, const char *fmt, ...) {
    va_list ap;
    int r;
    va_start(ap, fmt);
    r = _vsnprintf_s(buf, sizeOfBuffer, count, fmt, ap);
    va_end(ap);
    return r;
}

void perror(const char *s) {
    FILE *e = __goclib_stderr();
    if (s && *s) {
        fputs(s, e);
        fputs(": ", e);
    }
    fputs(strerror(errno), e);
    fputc('\n', e);
}

int puts(const char *s) {
    fwrite(s, 1, (long)strlen(s), __goclib_stdout());
    fputc('\n', __goclib_stdout());
    return 0;
}

int putchar(int c) {
    return fputc(c, __goclib_stdout());
}

/* ------------------- thin integer printing ------------------------------- */
/*
 * int_print / long_print are the weightless counterparts of printf("%d\n"):
 * a digits-to-buffer conversion, one write for the digits and one for the
 * newline, no vfmt. A program that prints only integers links neither the
 * format interpreter nor the floating-point converter. Returns the number
 * of characters written, newline included.
 *
 * Both the UFCS scalar method x.print() and the print(...) builtin lower
 * here: int_print / long_print are the single source for integer output,
 * and their newline matches the print() builtin's "print a line" semantic.
 *
 * The sign is handled in unsigned arithmetic, so the most negative value
 * prints correctly: negating LONG_MIN overflows long but is exact modulo
 * 2^64, and C's unsigned arithmetic is defined modulo 2^64.
 */

/* __goclib_long_to_buf converts v to decimal digits in buf (sign first,
 * digits big-endian) and returns the character count. Shared by long_print
 * and the array printers, which supply the newline / brackets themselves. */
int __goclib_long_to_buf(char *buf, long v) {
    int n = 0;
    unsigned long u = (unsigned long)v;
    if (v < 0) {
        u = (unsigned long)0 - u;
        buf[n] = '-';
        n++;
    }
    /* digits come out least-significant first */
    do {
        buf[n] = (char)('0' + (int)(u % 10));
        u /= 10;
        n++;
    } while (u > 0);
    /* reverse the digit run (past the sign, if any) */
    {
        int lo = 0;
        int hi = n - 1;
        if (buf[0] == '-') {
            lo = 1;
        }
        while (lo < hi) {
            char t = buf[lo];
            buf[lo] = buf[hi];
            buf[hi] = t;
            lo++;
            hi--;
        }
    }
    return n;
}

int long_print(long v) {
    char buf[21]; /* sign + 20 digits (LONG_MIN's full width) */
    int n = __goclib_long_to_buf(buf, v);
    __goclib_write(buf, n);
    __goclib_write("\n", 1);
    return n + 1;
}

int int_print(int v) {
    return long_print((long)v);
}

/* Python-style array printing: "[1, 2, 3]" plus newline. The print builtin
 * passes the element count as a compile-time constant -- a C array carries
 * no length at runtime. One shared skeleton walks the array as raw bytes
 * and hands each element to a per-type converter; each public printer is a
 * thin wrapper picking the converter for its element type. Elements reuse
 * the shared digit converters (__goclib_long_to_buf for integers,
 * __goclib_double_g for floats). Adding a new element type is one converter
 * plus one wrapper; specialising a type (radix, width, ...) touches only
 * its own converter. On-demand emission links only the referenced printers
 * (and their converters, pulled in by the function-pointer reference).
 * Every function returns the number of characters written.
 *
 * The unsigned* arrays reuse the signed printers: int_array_print reads
 * ints at their true width and the int->long conversion sign-extends, so an
 * unsigned int value above INT_MAX prints negative -- the same caveat
 * printf %d has (likewise an unsigned long above LONG_MAX). Values within
 * the signed range print correctly. */

typedef int (*__goclib_array_conv)(char *buf, void *p);

/* shared skeleton: "[e1, e2, ...]" plus newline, returning chars written */
int __goclib_array_print(void *a, long n, long elem_size,
                         __goclib_array_conv conv) {
    char buf[40];
    char *p = (char *)a;
    long i;
    int total = 1; /* "[" */
    __goclib_write("[", 1);
    for (i = 0; i < n; i++) {
        int k;
        if (i > 0) {
            __goclib_write(", ", 2);
            total += 2;
        }
        k = conv(buf, p);
        __goclib_write(buf, k);
        total += k;
        p += elem_size;
    }
    __goclib_write("]\n", 2);
    return total + 2;
}

/* per-type converters: render the element at p into buf, return its length */
static int __goclib_conv_short(char *buf, void *p) {
    return __goclib_long_to_buf(buf, *(short *)p);
}
static int __goclib_conv_int(char *buf, void *p) {
    return __goclib_long_to_buf(buf, *(int *)p);
}
static int __goclib_conv_long(char *buf, void *p) {
    return __goclib_long_to_buf(buf, *(long *)p);
}
static int __goclib_conv_bool(char *buf, void *p) {
    return __goclib_long_to_buf(buf, *(_Bool *)p);
}
/* float widens to double so the %g converter serves it too. */
static int __goclib_conv_float(char *buf, void *p) {
    return __goclib_double_g(buf, (double)*(float *)p);
}
static int __goclib_conv_double(char *buf, void *p) {
    return __goclib_double_g(buf, *(double *)p);
}

int short_array_print(short *a, long n) {
    return __goclib_array_print(a, n, sizeof(short), __goclib_conv_short);
}
int int_array_print(int *a, long n) {
    return __goclib_array_print(a, n, sizeof(int), __goclib_conv_int);
}
int long_array_print(long *a, long n) {
    return __goclib_array_print(a, n, sizeof(long), __goclib_conv_long);
}
int bool_array_print(_Bool *a, long n) {
    return __goclib_array_print(a, n, sizeof(_Bool), __goclib_conv_bool);
}
int float_array_print(float *a, long n) {
    return __goclib_array_print(a, n, sizeof(float), __goclib_conv_float);
}
int double_array_print(double *a, long n) {
    return __goclib_array_print(a, n, sizeof(double), __goclib_conv_double);
}

/* Thin target for the print(...) builtin (see stdio.h): the "%s\n" case,
 * with vfmt's "(null)" guard kept. The integer case lowers straight to
 * int_print / long_print (which now carry their own newline). Everything
 * returns the total character count so the builtin keeps its
 * printf-lowering return semantics. Its name keeps the UFCS spelling
 * (T_print): str_print. */
int str_print(const char *s) {
    if (!s) s = "(null)";
    long n = 0;
    while (s[n]) n++;
    __goclib_write(s, n);
    __goclib_write("\n", 1);
    return (int)(n + 1);
}

int getchar(void) {
    return fgetc(__goclib_stdin());
}

/* ------------------------------- sscanf ---------------------------------- */
/*
 * The scanf conversions goc supports: d i u o x X (with h/l/ll lengths),
 * f e g a and friends, c, s, and "%%". Widths and "*" suppression are
 * honoured; scansets (%[...]), %p and %n are not. Floating point is parsed
 * by strtod, so the two agree by construction.
 *
 * Returns the number of items ASSIGNED (suppressed conversions do not
 * count), or -1 if the input ends before the first conversion completes.
 * That distinction is the whole reason scanf returns "assigned" rather
 * than "matched": callers test "!= 1" after asking for one item.
 */
static const char *scan_ws(const char *p) {
    while (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r' || *p == '\v' || *p == '\f') p++;
    return p;
}

/*
 * Read an unsigned integer of the given base (0 = autodetect: 0x/0X means
 * hex, a leading 0 means octal, otherwise decimal). Stops at the first
 * character that is not a digit in that base. *ok reports whether at least
 * one digit was consumed.
 */
static unsigned long scan_uint(const char **pp, int base, int width, int *ok) {
    const char *p = *pp;
    unsigned long v = 0;
    int n = 0;
    int d;
    *ok = 0;
    if (base == 0) {
        if (p[0] == '0' && (p[1] == 'x' || p[1] == 'X')) {
            base = 16;
            p = p + 2;
        } else if (p[0] == '0') {
            base = 8;
        } else {
            base = 10;
        }
    } else if (base == 16 && p[0] == '0' && (p[1] == 'x' || p[1] == 'X')) {
        /* An explicit "%x" still accepts the 0x prefix -- scanf's %x and %i
         * agree here, and "0x1f" must not be read as the integer 0. */
        p = p + 2;
    }
    while (*p != '\0' && (width <= 0 || n < width)) {
        if (*p >= '0' && *p <= '9') {
            d = *p - '0';
        } else if (*p >= 'a' && *p <= 'z') {
            d = *p - 'a' + 10;
        } else if (*p >= 'A' && *p <= 'Z') {
            d = *p - 'A' + 10;
        } else {
            break;
        }
        if (d >= base) break;
        v = v * (unsigned long)base + (unsigned long)d;
        p++;
        n++;
        *ok = 1;
    }
    *pp = p;
    return v;
}

int vsscanf(const char *s, const char *fmt, va_list ap) {
    const char *sp = s;
    const char *fp = fmt;
    int assigned = 0;
    int suppress;
    int width;
    int lmod;
    int base;
    int ok;
    int neg;
    unsigned long uv;
    char *dst;

    while (*fp != '\0') {
        /* Whitespace in the format matches any run of whitespace in the
         * input, including none -- it never causes a mismatch. */
        if (*fp == ' ' || *fp == '\t' || *fp == '\n') {
            sp = scan_ws(sp);
            fp++;
            continue;
        }
        if (*fp != '%') {
            if (*sp != *fp) break;
            sp++;
            fp++;
            continue;
        }
        fp++;
        if (*fp == '%') {
            if (*sp != '%') break;
            sp++;
            fp++;
            continue;
        }
        suppress = 0;
        if (*fp == '*') {
            suppress = 1;
            fp++;
        }
        width = 0;
        while (*fp >= '0' && *fp <= '9') {
            width = width * 10 + (*fp - '0');
            fp++;
        }
        /* goc's long is already 64 bits, so "l", "ll" and "L" all name the
         * same 8-byte target for an integer conversion. */
        lmod = 0;
        if (*fp == 'h' || *fp == 'l' || *fp == 'L') {
            lmod = *fp;
            fp++;
            if ((lmod == 'l' && *fp == 'l') || (lmod == 'h' && *fp == 'h')) fp++;
        }

        if (*fp == 'c') {
            int n = width > 0 ? width : 1;
            int i;
            if (suppress) {
                sp = sp + n;
            } else {
                dst = (char *)va_arg(ap, char *);
                for (i = 0; i < n && *sp != '\0'; i++) {
                    dst[i] = *sp;
                    sp++;
                }
                assigned++;
            }
            fp++;
            continue;
        }
        if (*fp == 's') {
            int n = 0;
            if (!suppress) dst = (char *)va_arg(ap, char *);
            sp = scan_ws(sp);
            while (*sp != '\0' && *sp != ' ' && *sp != '\t' && *sp != '\n' && *sp != '\r' &&
                   (width <= 0 || n < width)) {
                if (!suppress) dst[n] = *sp;
                sp++;
                n++;
            }
            if (n == 0) break;
            if (!suppress) {
                dst[n] = '\0';
                assigned++;
            }
            fp++;
            continue;
        }
        if (*fp == 'f' || *fp == 'F' || *fp == 'e' || *fp == 'E' ||
            *fp == 'g' || *fp == 'G' || *fp == 'a' || *fp == 'A') {
            char *endp;
            double dv;
            sp = scan_ws(sp);
            dv = strtod(sp, &endp);
            if (endp == sp) break;
            sp = endp;
            if (!suppress) {
                void *out = va_arg(ap, void *);
                if (lmod == 0) {
                    *(float *)out = (float)dv;
                } else {
                    *(double *)out = dv;
                }
                assigned++;
            }
            fp++;
            continue;
        }

        base = -1;
        if (*fp == 'd' || *fp == 'u') base = 10;
        else if (*fp == 'i') base = 0;
        else if (*fp == 'o') base = 8;
        else if (*fp == 'x' || *fp == 'X') base = 16;
        if (base < 0) break;                /* unknown conversion: stop */

        sp = scan_ws(sp);
        neg = 0;
        if (*sp == '+') {
            sp++;
        } else if (*sp == '-') {
            neg = 1;
            sp++;
        }
        uv = scan_uint(&sp, base, width, &ok);
        if (!ok) break;
        /* A single target pointer: goc now scopes block-local declarations
         * correctly (the old i16/i32/i64/f32/f64 name-mangling workaround that
         * masked the shadowing bug is gone -- see task #38). */
        if (!suppress) {
            void *out = va_arg(ap, void *);
            if (lmod == 'h') {
                *(short *)out = (short)(neg ? -(long)uv : (long)uv);
            } else if (lmod == 0) {
                *(int *)out = (int)(neg ? -(long)uv : (long)uv);
            } else {
                *(long *)out = neg ? -(long)uv : (long)uv;
            }
            assigned++;
        }
        fp++;
        continue;
    }
    /* EOF means "an input failure occurred before the first conversion could
     * complete", i.e. the input was already exhausted. A *matching* failure
     * (a digit expected, a letter found) is not an input failure: scanf returns
     * the number of items assigned, which is then 0. Telling the two apart is
     * the whole point of the return value -- callers test "!= 1" after asking
     * for one item, and would misread a matching failure as end-of-input. */
    if (assigned == 0 && sp == s && *s == '\0') return -1;
    return assigned;
}

/* sscanf is a thin wrapper over vsscanf: pull the va_list and forward. */
int sscanf(const char *s, const char *fmt, ...) {
    va_list ap;
    int r;
    va_start(ap, fmt);
    r = vsscanf(s, fmt, ap);
    va_end(ap);
    return r;
}

/* =============================================================================
 * fscanf -- formatted input from a FILE* (standard streams, disk files, ...).
 *
 * Mirrors sscanf's conversion logic but sources characters from `stream` via
 * fgetc/ungetc instead of a string pointer. ungetc provides exactly one
 * character of pushback, which is sufficient: every branch reads at most one
 * look-ahead character and pushes it back when it does not match.
 * ========================================================================== */
typedef struct { FILE *f; int pushed; int got_any; } __fscan;

static int __fs_get(__fscan *s) {
    int c;
    if (s->pushed >= 0) { c = s->pushed; s->pushed = -1; return c; }
    c = fgetc(s->f);
    if (c != -1) s->got_any = 1;
    return c;
}
static void __fs_unget(__fscan *s, int c) { s->pushed = c; }

static int __fs_ws(__fscan *s) {
    int c;
    for (;;) {
        c = __fs_get(s);
        if (c == ' ' || c == '\t' || c == '\n' || c == '\r' ||
            c == '\v' || c == '\f') continue;
        break;
    }
    if (c != -1) __fs_unget(s, c);
    return 0;
}

static int __fs_digit(int c, int base) {
    int d;
    if (c >= '0' && c <= '9') d = c - '0';
    else if (c >= 'a' && c <= 'z') d = c - 'a' + 10;
    else if (c >= 'A' && c <= 'Z') d = c - 'A' + 10;
    else return -1;
    if (d < base) return d;
    return -1;
}

int vfscanf(FILE *stream, const char *fmt, va_list ap) {
    __fscan sc;
    const char *fp = fmt;
    int assigned = 0, suppress, lmod, base, ok, neg, c, cnt;
    unsigned long uv;
    char *dst;
    long width;

    if (stream == 0) return -1;
    sc.f = stream; sc.pushed = -1; sc.got_any = 0;
    while (*fp != '\0') {
        if (*fp == ' ' || *fp == '\t' || *fp == '\n') {
            __fs_ws(&sc);
            fp++;
            continue;
        }
        if (*fp != '%') {
            c = __fs_get(&sc);
            if (c == -1) break;
            if (c != *fp) { __fs_unget(&sc, c); break; }
            fp++;
            continue;
        }
        fp++;
        if (*fp == '%') {
            c = __fs_get(&sc);
            if (c == -1) break;
            if (c != '%') { __fs_unget(&sc, c); break; }
            fp++;
            continue;
        }
        suppress = 0;
        if (*fp == '*') { suppress = 1; fp++; }
        width = 0;
        while (*fp >= '0' && *fp <= '9') { width = width * 10 + (*fp - '0'); fp++; }
        lmod = 0;
        if (*fp == 'h' || *fp == 'l' || *fp == 'L') {
            lmod = *fp; fp++;
            if ((lmod == 'l' && *fp == 'l') || (lmod == 'h' && *fp == 'h')) fp++;
        }

        if (*fp == 'c') {
            long n = (width > 0) ? width : 1;
            long i;
            if (suppress) {
                for (i = 0; i < n; i++) { c = __fs_get(&sc); if (c == -1) break; }
            } else {
                dst = (char *)va_arg(ap, char *);
                for (i = 0; i < n; i++) {
                    c = __fs_get(&sc);
                    if (c == -1) break;
                    dst[i] = (char)c;
                }
                assigned++;
            }
            fp++;
            continue;
        }
        if (*fp == 's') {
            long n = 0;
            if (!suppress) dst = (char *)va_arg(ap, char *);
            __fs_ws(&sc);
            for (;;) {
                c = __fs_get(&sc);
                if (c == -1 || c == ' ' || c == '\t' || c == '\n' || c == '\r') {
                    if (c != -1) __fs_unget(&sc, c);
                    break;
                }
                if (!suppress) dst[n] = (char)c;
                n++;
                if (width > 0 && n >= width) break;
            }
            if (!suppress) { dst[n] = 0; assigned++; }
            fp++;
            continue;
        }
        if (*fp == 'f' || *fp == 'F' || *fp == 'e' || *fp == 'E' ||
            *fp == 'g' || *fp == 'G' || *fp == 'a' || *fp == 'A') {
            double dv;
            char tb[256];
            long ti = 0;
            char *endp;
            __fs_ws(&sc);
            c = __fs_get(&sc);
            if (c == -1) break;
            tb[ti++] = (char)c;
            for (;;) {
                c = __fs_get(&sc);
                if (c == -1) break;
                if ((c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' ||
                    c == '+' || c == '-') {
                    if (ti < 255) tb[ti++] = (char)c;
                } else { __fs_unget(&sc, c); break; }
            }
            tb[ti] = 0;
            if (ti == 0) break;
            dv = strtod(tb, &endp);
            if (endp == tb) break;   /* nothing parseable */
            if (!suppress) {
                void *out = va_arg(ap, void *);
                if (lmod == 0) *(float *)out = (float)dv;
                else *(double *)out = dv;
                assigned++;
            }
            fp++;
            continue;
        }

        base = -1;
        if (*fp == 'd' || *fp == 'u') base = 10;
        else if (*fp == 'i') base = 0;
        else if (*fp == 'o') base = 8;
        else if (*fp == 'x' || *fp == 'X') base = 16;
        if (base < 0) break;         /* unknown conversion: stop */

        __fs_ws(&sc);
        c = __fs_get(&sc);
        if (c == -1) break;
        neg = 0;
        if (c == '+') { c = __fs_get(&sc); }
        else if (c == '-') { neg = 1; c = __fs_get(&sc); }
        if (c == -1) break;

        uv = 0; ok = 0; cnt = 0;
        {
            int hexpre = 0;
            if (c == '0') {
                int d2 = __fs_get(&sc);
                if (d2 == 'x' || d2 == 'X') {
                    if (base == 0 || base == 16) { hexpre = 1; base = 16; }
                    else if (d2 != -1) __fs_unget(&sc, d2);
                } else {
                    if (d2 != -1) __fs_unget(&sc, d2);
                    if (base == 0) base = 8;
                }
            }
            if (hexpre) {
                int d2 = __fs_get(&sc);
                if (d2 != -1) {
                    int d = __fs_digit(d2, 16);
                    if (d >= 0) { uv = (unsigned long)d; ok = 1; cnt = 1; }
                    else __fs_unget(&sc, d2);
                }
            } else {
                int d = __fs_digit(c, base);
                if (d >= 0) { uv = (unsigned long)d; ok = 1; cnt = 1; }
                else __fs_unget(&sc, c);
            }
            while (ok) {
                int d3 = __fs_get(&sc);
                if (d3 == -1) break;
                {
                    int d = __fs_digit(d3, base);
                    if (d < 0) { __fs_unget(&sc, d3); break; }
                    if (width > 0 && cnt >= width) { __fs_unget(&sc, d3); break; }
                    uv = uv * (unsigned long)base + (unsigned long)d;
                    cnt++;
                }
            }
        }
        if (!ok) break;
        if (!suppress) {
            void *out = va_arg(ap, void *);
            if (lmod == 'h') *(short *)out = (short)(neg ? -(long)uv : (long)uv);
            else if (lmod == 0) *(int *)out = (int)(neg ? -(long)uv : (long)uv);
            else *(long *)out = neg ? -(long)uv : (long)uv;
            assigned++;
        }
        fp++;
        continue;
    }
    if (assigned == 0 && !sc.got_any) return -1;
    return assigned;
}

int fscanf(FILE *stream, const char *fmt, ...) {
    va_list ap;
    int r;
    va_start(ap, fmt);
    r = vfscanf(stream, fmt, ap);
    va_end(ap);
    return r;
}

int vscanf(const char *fmt, va_list ap) {
    return vfscanf(__goclib_stdin(), fmt, ap);
}

int scanf(const char *fmt, ...) {
    va_list ap;
    int r;
    va_start(ap, fmt);
    r = vscanf(fmt, ap);
    va_end(ap);
    return r;
}

/* setbuf is the simple two-argument form of setvbuf. goc's streams do not
 * honour caller-supplied buffers, so this mirrors setvbuf and is effectively a
 * no-op -- but it links and is callable, as a real libc provides. */
void setbuf(FILE *stream, char *buf) {
    setvbuf(stream, buf, buf ? _IOFBF : _IONBF, BUFSIZ);
}

/* tmpnam writes a unique (not-yet-created) file name into `s`, or into an
 * internal static buffer when `s` is null, and returns it. */
char *tmpnam(char *s) {
    static char name[40];
    static long seq = 0;
    char *dst = s ? s : name;
    long i = 0, v;
    const char *pre = "goc_tmp_";
    while (pre[i]) { dst[i] = pre[i]; i++; }
    v = ++seq;
    if (v == 0) { dst[i++] = '0'; }
    else {
        char t[20]; int k = 0;
        while (v > 0) { t[k++] = (char)('0' + (v % 10)); v /= 10; }
        while (k > 0) { dst[i++] = t[--k]; }
    }
    dst[i++] = '.'; dst[i++] = 't'; dst[i++] = 'm'; dst[i++] = 'p'; dst[i] = 0;
    return dst;
}

/* _fileno / _setmode -- the io.h pair Windows programs use to switch a stream
 * between text and binary. goc writes bytes through untouched, so there is no
 * translation to turn off; the call only has to answer plausibly. The stream
 * identity is decided by address against the three standard FILEs, which is
 * the only thing callers ever ask about (_fileno(stdout) and friends at
 * startup). */
int _fileno(FILE *f) {
    if (f == stdin) { return 0; }
    if (f == stdout) { return 1; }
    if (f == stderr) { return 2; }
    return -1;
}

int _setmode(int fd, int mode) {
    if (fd < 0) { return -1; }
    return mode;
}
