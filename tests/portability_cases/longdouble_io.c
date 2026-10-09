/* printf/scanf with goc's binary128 long double: the %Lf/%Le/%Lg wiring.
 *
 * fp128dec.c itself is differential-tested against libquadmath elsewhere
 * (tests/fp128dec); what this case pins is the stdio layer around it: the 'L'
 * modifier reaching the binary128 converter (and not the double one), the
 * field-width/flag machinery over converter output that can dwarf the static
 * buffer, and scanf's %Lf storing all 16 bytes where %lf stores 8 -- the
 * distinction the old "fold L to l" broke.
 *
 * Every expectation is a fixed string; the decimal goldens were computed by
 * exact integer arithmetic in Python (binary128 round-half-even), the same
 * oracle the fp128dec differential test uses. Self-reporting: exit 0 and a
 * final line of "OK".
 */
#include <stdio.h>
#include <string.h>

static int fails = 0;

static void expect(const char *name, const char *got, const char *want) {
    if (strcmp(got, want) != 0) {
        printf("FAIL %s:\n  got  %s\n  want %s\n", name, got, want);
        fails++;
    }
}

int main(void) {
    char buf[5120];
    long double pi = 3.14159265358979323846264338327950288L;
    long double neg = -2.5L;
    long double big = 1e4000L;
    long double v;

    /* default precision, every conversion letter; the F/E/G uppercase forms
     * spell the exponent letter and inf/nan in upper case */
    sprintf(buf, "%Lf", pi);
    expect("%Lf", buf, "3.141593");
    sprintf(buf, "%Le", pi);
    expect("%Le", buf, "3.141593e+00");
    sprintf(buf, "%Lg", pi);
    expect("%Lg", buf, "3.14159");
    sprintf(buf, "%LG", pi);
    expect("%LG", buf, "3.14159");
    sprintf(buf, "%LF", pi);
    expect("%LF", buf, "3.141593");

    /* precision: 20 fraction digits, then the full 34-significant-digit
     * expansion of the binary128 rounding of pi */
    sprintf(buf, "%.20Lf", pi);
    expect("%.20Lf", buf, "3.14159265358979323846");
    sprintf(buf, "%.33Le", pi);
    expect("%.33Le", buf, "3.141592653589793238462643383279503e+00");

    /* flags: sign, blank, '#'-forces-point, width, left, zero fill (the
     * zero padding must land between the minus and the digits) */
    sprintf(buf, "%+.1Le", neg);
    expect("%+.1Le", buf, "-2.5e+00");
    sprintf(buf, "%.6Lf", neg);
    expect("%.6Lf", buf, "-2.500000");
    sprintf(buf, "%#.0Lf", 1.0L);
    expect("%#.0Lf", buf, "1.");
    sprintf(buf, "%10.2Lf|", pi);
    expect("%10.2Lf|", buf, "      3.14|");
    sprintf(buf, "%-10.2Lf|", pi);
    expect("%-10.2Lf|", buf, "3.14      |");
    sprintf(buf, "%025.4Lf", neg);
    expect("%025.4Lf", buf, "-0000000000000000002.5000");

    /* oversized: %Lf of 1e4000 is 4001 integer digits + point + 6 fraction
     * digits -- 4008 characters, five times the static field buffer. The
     * heap retry must produce the exact expansion (1e4000 rounds to a
     * binary128 whose decimal expansion starts "1000...0" and ends in the
     * rounding error digits; only the length and the ends are pinned). */
    {
        int n = sprintf(buf, "%Lf", big);
        if (n != 4008) { printf("FAIL big.len=%d\n", n); fails++; }
        else if (memcmp(buf, "100000000000", 12) != 0 ||
                 strcmp(buf + n - 4, "0000") != 0) {
            printf("FAIL big.head=%.12s tail=%.4s\n", buf, buf + n - 4);
            fails++;
        }
        n = sprintf(buf, "%.1000Le", 1e-4000L);
        if (n != 1008 || strcmp(buf + n - 6, "e-4000") != 0) {
            printf("FAIL tiny.len=%d tail=%.6s\n", n, n > 6 ? buf + n - 6 : "?");
            fails++;
        }
    }

    /* scanf: %Lf stores 16 bytes; the value read back is bit-identical to
     * what printf prints. The 33-digit round trip pins every bit of pi's
     * mantissa (34 significant digits over-determine the 113-bit one). */
    if (sscanf("3.14159265358979323846264338327950288", "%Lf", &v) != 1) {
        printf("FAIL scan.pi.assign\n"); fails++;
    } else {
        sprintf(buf, "%.33Le", v);
        expect("scan.pi", buf, "3.141592653589793238462643383279503e+00");
    }
    if (sscanf("-1.25e-3", "%Lf", &v) == 1) {
        sprintf(buf, "%.5Le", v);
        expect("scan.neg", buf, "-1.25000e-03");
    } else { printf("FAIL scan.neg.assign\n"); fails++; }
    /* hex float: fp128dec is decimal-only by design, so scanf widens the
     * strtod value -- 0x1.8p3 is exactly 12 either way */
    if (sscanf("0x1.8p3", "%Lf", &v) == 1) {
        sprintf(buf, "%.2Lf", v);
        expect("scan.hex", buf, "12.00");
    } else { printf("FAIL scan.hex.assign\n"); fails++; }
    /* deep underflow: exact parse to a subnormal, printed back */
    if (sscanf("1e-4900", "%Lf", &v) == 1) {
        sprintf(buf, "%Le", v);
        expect("scan.us", buf, "1.000000e-4900");
    } else { printf("FAIL scan.us.assign\n"); fails++; }
    /* %lf must still store 8 bytes -- the L-fold regression's collateral */
    {
        double d = 0;
        sscanf("2.5", "%lf", &d);
        sprintf(buf, "%.1f", d);
        expect("scan.d", buf, "2.5");
    }
    /* suppressing '*' must not consume a slot either way */
    if (sscanf("9.75 1.5", "%*Lf %Lf", &v) == 1) {
        sprintf(buf, "%.2Lf", v);
        expect("scan.star", buf, "1.50");
    } else { printf("FAIL scan.star.assign\n"); fails++; }

    printf("%s\n", fails ? "BAD" : "OK");
    return fails ? 1 : 0;
}
