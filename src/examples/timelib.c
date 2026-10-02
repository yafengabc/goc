/* goclib <time.h>: calendar conversion and formatting.
 *
 * Everything here is pinned to a fixed timestamp, and the two calls that
 * depend on the local zone -- localtime() and mktime() -- are only checked
 * against each other, never against a hard-coded offset. That is what makes
 * the expected output identical on the Windows leg (real local zone from
 * the OS) and the Linux leg (TZ, unset under the emulator, so UTC):
 * gmtime/strftime/asctime are zone-free, and localtime->mktime is a round
 * trip that cancels whatever offset each platform happens to be using.
 *
 * clock() is only checked for being non-negative: it is wall time on
 * Windows and CPU time on Linux, so its value is not portable. */
#include <stdio.h>
#include <time.h>

int main() {
    time_t t = 1767225600;          /* 2026-01-01 00:00:00 UTC */
    time_t leap = 1583020800;       /* 2020-03-01 00:00:00 UTC, leap year */
    struct tm *g;
    struct tm *l;
    struct tm copy;
    char buf[128];
    size_t n;

    g = gmtime(&t);
    printf("gmtime: %04d-%02d-%02d %02d:%02d:%02d\n",
           g->tm_year + 1900, g->tm_mon + 1, g->tm_mday,
           g->tm_hour, g->tm_min, g->tm_sec);
    printf("wday=%d yday=%d isdst=%d\n", g->tm_wday, g->tm_yday, g->tm_isdst);
    printf("asctime: %s", asctime(g));

    strftime(buf, sizeof(buf), "%Y-%m-%d %H:%M:%S", g);
    printf("F/T: %s\n", buf);
    strftime(buf, sizeof(buf), "%A %B %d %Y %j %w", g);
    printf("names: %s\n", buf);
    strftime(buf, sizeof(buf), "%I:%M %p %D %R", g);
    printf("12h: %s\n", buf);
    strftime(buf, sizeof(buf), "%% literal %y-%m-%e", g);
    printf("odd: %s\n", buf);

    /* Truncation is reported, not hidden: 5 bytes cannot hold this. */
    n = strftime(buf, 5, "%Y-%m-%d", g);
    printf("short: n=%d buf=%s\n", (int)n, buf);

    /* Leap year: 2020-02-29 exists and 2021-02-29 rolls into March. */
    g = gmtime(&leap);
    printf("leap: %04d-%02d-%02d yday=%d\n",
           g->tm_year + 1900, g->tm_mon + 1, g->tm_mday, g->tm_yday);
    copy = *g;
    copy.tm_mon = 1;
    copy.tm_mday = 29;
    mktime(&copy);
    printf("2020-02-29 -> %04d-%02d-%02d\n",
           copy.tm_year + 1900, copy.tm_mon + 1, copy.tm_mday);
    copy = *g;
    copy.tm_year = 121;             /* 2021 */
    copy.tm_mon = 1;
    copy.tm_mday = 29;
    mktime(&copy);
    printf("2021-02-29 -> %04d-%02d-%02d\n",
           copy.tm_year + 1900, copy.tm_mon + 1, copy.tm_mday);

    /* Out-of-range fields normalise upward across month and year. */
    copy = *g;
    copy.tm_mon = 0;
    copy.tm_mday = 32;
    copy.tm_hour = 25;
    copy.tm_min = 61;
    copy.tm_sec = 61;
    mktime(&copy);
    printf("overflow -> %04d-%02d-%02d %02d:%02d:%02d\n",
           copy.tm_year + 1900, copy.tm_mon + 1, copy.tm_mday,
           copy.tm_hour, copy.tm_min, copy.tm_sec);

    /* localtime -> mktime is the identity in whatever zone the host is in,
     * so that round trip is the only portable assertion we can make here:
     * the offset itself differs between the Windows leg (real local zone)
     * and the Linux leg (UTC), so it is never printed. */
    l = localtime(&t);
    printf("mktime(localtime(t)) == t: %d\n", mktime(l) == t ? 1 : 0);

    printf("difftime=%d\n", (int)difftime(t + 3661, t));
    printf("clock ok=%d\n", clock() >= 0 ? 1 : 0);
    printf("time now > 2026: %d\n", time(0) > t ? 1 : 0);
    return 0;
}
