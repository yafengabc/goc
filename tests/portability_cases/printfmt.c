/* printf/scanf conformance: self-reporting regression.
 *
 * Pins the conversions added in the "printf/scanf matches the default C
 * library" pass, every one of which was checked byte-for-byte against the
 * ucrt reference (gcc -std=c11 on Windows) before being written here:
 *
 *   printf: integer precision (%.5d / %.0d of 0 / %#o / %#x nonzero-only),
 *           '*' width/precision including negative values, the width
 *           accounting of sign and 0x prefix (%#08x is 8 characters), %5d of
 *           a negative number (used to drop the minus), %g re-decides the
 *           style after a rounding carry (9.999999e5 -> 1e+06), '#' keeps
 *           %g trailing zeros and forces the %f/%e point, '+' and ' ' on
 *           floats, %n in all four widths, the ucrt "nan(ind)" spelling.
 *   scanf:  %hhd writes ONE byte, scansets ([a-z], [^0-9], ']' as a literal
 *           member) with their terminating NUL, %n in all widths, %p
 *           round-trips printf's output, EOF vs matching-failure return
 *           values, %c on exhausted input, hex floats ("0x10" -> 16), and
 *           the float token rule ("1.5-3" is two items; "1e+x" fails and
 *           consumes its field so "x" stays in the stream).
 *
 * Prints "OK" on the last line and exits 0 iff every check passed, so it
 * drops straight into build_run_win_ok() in tools/portability/win_regress.sh.
 *
 * Two deliberate divergences from ucrt (documented, not bugs): the sign bit
 * of a NaN is not printed -- ucrt says "-nan(ind)" for 0.0/0.0 while an
 * optimiser-folded NaN is positive, and the sign is unspecified in C -- and
 * %n stores per the standard, where ucrt's security hardening refuses to. */
#include <stdio.h>
#include <stdarg.h>
#include <string.h>
#include <wchar.h>
#include <limits.h>

static int fails = 0;
#define CHK(c, m) do { if (!(c)) { printf("FAIL: %s\n", m); fails++; } } while (0)

static char buf[256];
static char a[64], b[64];
static wchar_t wa[64];
static int i1, i2, n;
static short sh;
static char ch;
static long l1;
static long long ll1;
static unsigned u1;
static double d1, d2;
static void *pp;

/* sprintf and compare: the one-line harness for the printf half */
static int fmteq(const char *want, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(buf, sizeof buf, fmt, ap);
    va_end(ap);
    return strcmp(buf, want) == 0;
}

int main(void) {
    /* ---- printf: integer precision ---- */
    CHK(fmteq("00007", "%.5d", 7), "%.5d of 7");
    CHK(fmteq("",     "%.0d", 0), "%.0d of 0 prints nothing");
    CHK(fmteq("1",    "%.0d", 1), "%.0d of 1");
    CHK(fmteq("+",    "%+.0d", 0), "%+.0d of 0 keeps the sign");
    CHK(fmteq("-5",   "%+.0d", -5), "%+.0d of -5: precision 0 only silences zero");
    CHK(fmteq("0755",    "%#.4o", 0755u), "%#.4o (precision already forces the 0)");
    CHK(fmteq("0",       "%#.0o", 0u),    "%#.0o of 0 is a lone 0");
    CHK(fmteq("0",       "%#o", 0u),      "%#o of 0 has no 0x-style prefix");
    CHK(fmteq("0xff",    "%#x", 255u),    "%#x of 255");
    CHK(fmteq("0",       "%#x", 0u),      "%#x of 0 has no prefix");
    CHK(fmteq("0x005",   "%#.3x", 5u),    "%#.3x");
    /* ---- printf: width accounting (sign and prefix live inside the width) ---- */
    CHK(fmteq("  -42",   "%5d", -42),   "%5d of -42 keeps the minus");
    CHK(fmteq("-0042",  "%05d", -42),  "%05d of -42 zeroes live inside the width");
    CHK(fmteq("      -00123", "%012.5d", -123),
        "%012.5d: precision silences the 0 flag");
    CHK(fmteq("0x0000ff", "%#08x", 255u), "%#08x is 8 chars wide");
    CHK(fmteq("   0xff",  "%#7x", 255u),  "%#7x: the 0x prefix is inside the width");
    CHK(fmteq("0xff  ",   "%-#6x", 255u), "%-#6x");
    /* ---- printf: * width/precision, including negatives ---- */
    CHK(fmteq("    42", "%*d", 6, 42),    "%*d positive");
    CHK(fmteq("42    ", "%*d", -6, 42),   "%*d negative = left justify");
    CHK(fmteq("1.500000", "%.*f", -2, 1.5), "%.*f negative precision = omitted");
    CHK(fmteq("hello",   "%.*s", -1, "hello"), "%.*s negative precision");
    /* ---- printf: floats ---- */
    CHK(fmteq("1e+06", "%g", 9.999999e5), "%g re-decides after rounding carry");
    CHK(fmteq("999999", "%g", 999999.0), "%g stays decimal below the carry");
    CHK(fmteq("1.00000", "%#g", 1.0),   "%#g keeps trailing zeros");
    CHK(fmteq("1.",     "%#.0g", 1.0),  "%#.0g forces the point");
    CHK(fmteq("1.",     "%#.0f", 1.0),  "%#.0f forces the point");
    CHK(fmteq("1.e+00", "%#.0e", 1.0),  "%#.0e puts the point before e");
    CHK(fmteq("+2.5",   "%+g", 2.5),    "%+g");
    CHK(fmteq(" 2.5",   "% g", 2.5),    "% g");
    CHK(fmteq("+inf",   "%+f", 1.0 / 0.0), "%+f of inf");
    CHK(fmteq("inf",    "%f", 1.0 / 0.0),  "%f of inf");
    CHK(fmteq("nan(ind)", "%f", 0.0 / 0.0), "nan spelling (sign not printed)");
    /* ---- printf: %n in all widths ---- */
    n = -1; printf("abc%n", &n);
    CHK(n == 3, "%n counts characters");
    sh = -1; ch = -1; l1 = -1; ll1 = -1;
    printf("%hn%hhn%ln%lln", &sh, &ch, &l1, &ll1);
    /* all four %n sit at the very start: nothing has been emitted yet */
    CHK(sh == 0 && ch == 0 && l1 == 0 && ll1 == 0, "hn/hhn/ln/lln all record 0");
    /* ---- printf: wide (also pins the gocl 2-byte literal terminator) ---- */
    CHK(fmteq("wide", "%ls", L"wide"), "%ls of L\"wide\"");
    CHK(fmteq("w",    "%ls", L"w"),    "%ls of L\"w\"");
    CHK(fmteq("Z",    "%lc", L'Z'),    "%lc");

    /* ---- scanf: integers ---- */
    CHK(sscanf("-5", "%hhd", &ch) == 1 && ch == -5, "%hhd writes one byte");
    CHK(sscanf("70000", "%hd", &sh) == 1 && sh == 4464, "%hd truncates to short");
    CHK(sscanf("4294967295", "%u", &u1) == 1 && u1 == UINT_MAX, "%u");
    CHK(sscanf("12345678901234", "%lld", &ll1) == 1 && ll1 == 12345678901234ll, "%lld");
    /* "018" is not a valid octal number: the value stops at "01", the 8 stays */
    CHK(sscanf("0x1f 018", "%i %i", &i1, &i2) == 2 && i1 == 31 && i2 == 1,
        "%i autodetects hex and octal (018 -> 1)");
    CHK(sscanf("123456", "%3d%d", &i1, &i2) == 2 && i1 == 123 && i2 == 456,
        "width splits the field");
    CHK(sscanf("-123456", "%4d%d", &i1, &i2) == 2 && i1 == -123 && i2 == 456,
        "width counts the sign");
    CHK(sscanf("0x1f", "%x", &i1) == 1 && i1 == 31, "%x accepts the 0x prefix");
    CHK(sscanf("0xff", "%2x", &i1) == 0, "%2x on 0xff: field 0x converts to nothing");
    /* ---- scanf: scansets, %c, %s ---- */
    CHK(sscanf("hello world", "%[a-z]", a) == 1 && strcmp(a, "hello") == 0,
        "%[a-z]");
    CHK(sscanf("abc123", "%[^0-9]", a) == 1 && strcmp(a, "abc") == 0, "%[^0-9]");
    CHK(sscanf("]x]", "%[]x]", a) == 1 && strcmp(a, "]x]") == 0,
        "']' as a literal member");
    CHK(sscanf("a,b;c", "%[^,;],%[^;];", a, b) == 2 &&
        strcmp(a, "a") == 0 && strcmp(b, "b") == 0, "two scansets + literals");
    CHK(sscanf("abcXYZ", "%[a-cA-Z]", a) == 1 && strcmp(a, "abcXYZ") == 0,
        "range union");
    CHK(sscanf("xyz", "%[abc]", a) == 0, "empty scanset is a matching failure");
    CHK(sscanf("  hi  there", "%s %s", a, b) == 2 &&
        strcmp(a, "hi") == 0 && strcmp(b, "there") == 0, "%s skips whitespace");
    /* ---- scanf: %p and %n ---- */
    /* "0x1234abc" is 9 chars, the format's blank eats one more: n sees 10 */
    pp = 0; n = -1;
    CHK(sscanf("0x1234abc 7", "%p %n", &pp, &n) == 1 &&
        (unsigned long)pp == 0x1234abcul && n == 10, "%p round-trips printf");
    /* ---- scanf: floats, including the token rule ---- */
    CHK(sscanf("1.5 -2.25e2", "%lf %lf", &d1, &d2) == 2 &&
        d1 == 1.5 && d2 == -225.0, "two floats");
    CHK(sscanf("1.5-3", "%lf%lf", &d1, &d2) == 2 && d1 == 1.5 && d2 == -3.0,
        "1.5-3 is two items");
    CHK(sscanf("1e+x", "%lf", &d1) == 0, "1e+x: partial field is a failure");
    CHK(sscanf("1e+x tail", "%lf %s", &d1, a) == 0,
        "a failed float stops the scan: %s never runs");
    CHK(sscanf("0x10", "%lf", &d1) == 1 && d1 == 16.0, "hex float: 0x10 is 16");
    CHK(sscanf("0x1.8p1", "%lf", &d1) == 1 && d1 == 3.0, "hex float 0x1.8p1");
    CHK(sscanf("1e5x", "%lf", &d1) == 1 && d1 == 100000.0, "1e5x reads 1e5");
    /* ---- scanf: EOF vs matching failure ---- */
    i1 = -7;
    CHK(sscanf("", "%d", &i1) == -1, "empty input is EOF");
    CHK(sscanf("   ", "%d", &i1) == -1, "whitespace-only input is EOF");
    CHK(sscanf("abc", "%d", &i1) == 0, "letter where a digit belongs is 0");
    CHK(sscanf("", "%c", &ch) == -1, "%c on exhausted input does not assign");
    CHK(sscanf("12abc", "%d abc", &i1) == 1 && i1 == 12, "literal after number");

    /* ---- fscanf: the file path shares the conversions; the critical extra
     * is that pushback survives INTO the next call ---- */
    {
        FILE *f = fopen("printfmt.dat", "w");
        CHK(f != 0, "create data file");
        if (!f) { printf("%s\n", fails ? "BAD" : "OK"); return fails ? 1 : 0; }
        fprintf(f, "10 2.5 hello 0x1f\n1.5-3 1e+x end\n");
        fclose(f);
        f = fopen("printfmt.dat", "r");
        CHK(fscanf(f, "%d %lf %s %x", &i1, &d1, a, &u1) == 4 &&
            i1 == 10 && d1 == 2.5 && strcmp(a, "hello") == 0 && u1 == 0x1f,
            "fscanf mixed line");
        CHK(fscanf(f, "%lf%lf", &d1, &d2) == 2 && d1 == 1.5 && d2 == -3.0,
            "fscanf float token split");
        CHK(fscanf(f, "%lf", &d1) == 0,
            "fscanf: failed float field stays consumed");
        CHK(fscanf(f, "%s", a) == 1 && strcmp(a, "x") == 0,
            "fscanf: x survived into the next call");
        CHK(fscanf(f, "%d", &i1) == 0, "fscanf matching failure is 0");
        fclose(f);
        remove("printfmt.dat");
    }

    /* "abc" above has no newline; keep the verdict on its own line */
    printf("\n%s\n", fails ? "BAD" : "OK");
    return fails ? 1 : 0;
}
