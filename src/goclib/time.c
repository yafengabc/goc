#include "goclib.h"
#include <time.h>

/* =============================================================================
 * time.c -- wall-clock time, calendar conversion and formatting.
 *
 * Two layers. The bottom one is the only platform-dependent part: it reads
 * the clock and reports the local UTC offset. On Windows that means
 * GetSystemTimeAsFileTime (for the epoch seconds) plus the difference
 * between GetSystemTime and GetLocalTime (for the offset, which the OS has
 * already resolved against the zone and DST). On Linux it is clock_gettime,
 * and the offset comes from the TZ environment variable because there is no
 * tzfile parser here -- see tz_offset() for the accepted spellings.
 *
 * Everything above that is portable: the calendar is the proleptic Gregorian
 * calendar (extended backwards, no Julian switch-over), converted with
 * Howard Hinnant's days_from_civil / civil_from_days pair, which are exact
 * integer algorithms needing no table and no iteration.
 *
 * The Win32 structs below are respelled with private names. A program that
 * includes <windows.h> already has SYSTEMTIME and FILETIME, and redeclaring
 * those names here would collide; the API only cares about the layout.
 * ========================================================================== */

/* ---- platform clock ---------------------------------------------------- */

/* Needed by systime_to_unix() below, which sits in the platform section
 * while this is defined with the other calendar helpers further down. */
static long days_from_civil(long y, long m, long d);

#if defined(_WIN32)

typedef struct {
    unsigned short year, month, dayofweek, day, hour, minute, second, millis;
} GSystemTime;

typedef struct {
    unsigned int lo, hi;
} GFileTime;

extern void GetSystemTimeAsFileTime(void *ft);
extern void GetSystemTime(void *st);
extern void GetLocalTime(void *st);
extern unsigned int GetTickCount(void);

/* FILETIME counts 100ns ticks from 1601-01-01; this is the 369 years
 * between that and the Unix epoch, in seconds. */
#define TICKS_PER_SEC   10000000ULL
#define SECS_1601_1970  11644473600ULL

static time_t clock_utc(void) {
    GFileTime ft;
    unsigned long long t;
    GetSystemTimeAsFileTime(&ft);
    t = ((unsigned long long)ft.hi << 32) | (unsigned long long)ft.lo;
    return (time_t)(t / TICKS_PER_SEC - SECS_1601_1970);
}

/*
 * Wall time in CLOCKS_PER_SEC units. GetTickCount is milliseconds since
 * boot, so this is not CPU time the way Linux reports it -- it is the
 * cheapest thing available without importing the performance counter, and
 * timing a section with it is still correct as long as nothing else is
 * spinning. Documented in time.h; the Linux side reports real CPU time.
 */
static clock_t clock_proc(void) {
    return (clock_t)((unsigned long long)GetTickCount() * 1000UL);
}

static long systime_to_unix(GSystemTime *st) {
    long days = days_from_civil((long)st->year, (long)st->month, (long)st->day);
    return days * 86400L + (long)st->hour * 3600L
           + (long)st->minute * 60L + (long)st->second;
}

/*
 * Local minus UTC in seconds, measured rather than looked up: converting
 * "now" both ways and subtracting gives the offset in force at this instant,
 * DST included, with no zone database of our own. Two system calls per
 * query is the price; recomputing on every call is what makes a process
 * that runs across a DST transition stay correct.
 */
static long tz_offset(void) {
    GSystemTime utc;
    GSystemTime loc;
    GetSystemTime(&utc);
    GetLocalTime(&loc);
    return systime_to_unix(&loc) - systime_to_unix(&utc);
}

#elif defined(__linux__)

typedef struct {
    long sec;
    long nsec;
} GTimeSpec;

/* clock_gettime: goa's syscall stub under goc, syscall(2) under a host compiler
 * (the vDSO is not used, so both hosts take the same route to the kernel). */
#include <syscall.h>

#define CLOCK_REALTIME            0
#define CLOCK_PROCESS_CPUTIME_ID  2

static time_t clock_utc(void) {
    GTimeSpec ts;
    if (__goc_clock_gettime(CLOCK_REALTIME, &ts) != 0) return (time_t)-1;
    return (time_t)ts.sec;
}

static clock_t clock_proc(void) {
    GTimeSpec ts;
    if (__goc_clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &ts) != 0) return (clock_t)-1;
    return (clock_t)(ts.sec * 1000000L + ts.nsec / 1000L);
}

/*
 * Local minus UTC in seconds, from the TZ environment variable. Accepted:
 * "UTC" (0), "UTC+8" / "UTC-5", and the bare "+8" / "-5". Offsets are whole
 * hours and the sign is the intuitive one -- UTC+8 means local time is ahead
 * of UTC, which is the opposite of POSIX's own TZ sign convention.
 *
 * Unset TZ means UTC. There is no /etc/localtime or tzfile parsing here, so
 * a Linux process that wants local time has to say so.
 */
static long tz_offset(void) {
    char *tz = getenv("TZ");
    long h = 0;
    long sign = 1;
    if (tz == 0) return 0;
    if (tz[0] == 'U' && tz[1] == 'T' && tz[2] == 'C') tz = tz + 3;
    if (tz[0] == '\0') return 0;
    if (tz[0] == '+') {
        tz++;
    } else if (tz[0] == '-') {
        sign = -1;
        tz++;
    }
    while (*tz >= '0' && *tz <= '9') {
        h = h * 10 + (*tz - '0');
        tz++;
    }
    return sign * h * 3600L;
}

#endif /* platform */

/* ---- calendar ---------------------------------------------------------- */

static int is_leap(long y) {
    return (y % 4 == 0 && y % 100 != 0) || y % 400 == 0;
}

static int days_in_month(long y, long m) {
    static int len[12] = {31,28,31,30,31,30,31,31,30,31,30,31};
    if (m < 1 || m > 12) return 0;
    if (m == 2 && is_leap(y)) return 29;
    return len[m-1];
}

/*
 * Days since 1970-01-01 for a Gregorian date (Hinnant's algorithm). The
 * shift of the year at m <= 2 makes March the start of the year, so leap
 * days always land at the end of the counted span and February needs no
 * special case. Unsigned-free on purpose: goc's signed 64-bit right shift
 * is fine but its left shift is not, and none of this needs shifting.
 */
static long days_from_civil(long y, long m, long d) {
    long era;
    long yoe;
    long doy;
    long doe;
    y = y - (m <= 2 ? 1 : 0);
    era = (y >= 0 ? y : y - 399) / 400;
    yoe = y - era * 400;
    doy = (153 * (m + (m > 2 ? -3 : 9)) + 2) / 5 + d - 1;
    doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    return era * 146097 + doe - 719468;
}

/* Inverse of the above: days since the epoch -> year / month (1-12) / day. */
static void civil_from_days(long z, long *yy, long *mm, long *dd) {
    long era;
    long doe;
    long yoe;
    long doy;
    long mp;
    z = z + 719468;
    era = (z >= 0 ? z : z - 146096) / 146097;
    doe = z - era * 146097;
    yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    *yy = yoe + era * 400;
    doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    mp = (5 * doy + 2) / 153;
    *dd = doy - (153 * mp + 2) / 5 + 1;
    *mm = mp + (mp < 10 ? 3 : -9);
    if (*mm <= 2) *yy = *yy + 1;
}

/* ---- public interface -------------------------------------------------- */

time_t time(time_t *out) {
    time_t t = clock_utc();
    if (out) *out = t;
    return t;
}

double difftime(time_t end, time_t beginning) {
    return (double)(end - beginning);
}

clock_t clock(void) {
    return clock_proc();
}

static struct tm tm_buf;

/*
 * Shared by gmtime()/localtime() (offset 0 / platform) and the reentrant
 * _r variants. Writes into `out` rather than a fixed static, so the _r
 * functions can target the caller's struct tm. gmtime/localtime pass the
 * single static buffer, which the standard allows them to share.
 */
static struct tm *break_time(time_t t, long offset, struct tm *out) {
    long sec = (long)t + offset;
    long days;
    long rem;
    long y;
    long m;
    long d;
    days = sec / 86400;
    rem = sec - days * 86400;
    if (rem < 0) {                      /* dates before the epoch */
        rem = rem + 86400;
        days = days - 1;
    }
    civil_from_days(days, &y, &m, &d);
    out->tm_sec = (int)(rem % 60);
    out->tm_min = (int)((rem / 60) % 60);
    out->tm_hour = (int)(rem / 3600);
    out->tm_mday = (int)d;
    out->tm_mon = (int)(m - 1);
    out->tm_year = (int)(y - 1900);
    out->tm_wday = (int)((days + 4) % 7);      /* 1970-01-01 was a Thursday */
    if (out->tm_wday < 0) out->tm_wday = out->tm_wday + 7;
    out->tm_yday = (int)(days - days_from_civil(y, 1, 1));
    out->tm_isdst = -1;                        /* unknown: no zone rules */
    return out;
}

struct tm *gmtime(const time_t *tp) {
    time_t t = tp ? *tp : clock_utc();
    return break_time(t, 0, &tm_buf);
}

struct tm *localtime(const time_t *tp) {
    time_t t = tp ? *tp : clock_utc();
    return break_time(t, tz_offset(), &tm_buf);
}

struct tm *gmtime_r(const time_t *tp, struct tm *result) {
    time_t t = tp ? *tp : clock_utc();
    return result ? break_time(t, 0, result) : 0;
}

struct tm *localtime_r(const time_t *tp, struct tm *result) {
    time_t t = tp ? *tp : clock_utc();
    return result ? break_time(t, tz_offset(), result) : 0;
}

/*
 * Push out-of-range fields up into the next unit. mday needs the length of
 * the actual month, so it cannot be folded the way the others can -- it is
 * a loop rather than a division.
 */
static void tm_normalize(struct tm *tp) {
    int dim;
    while (tp->tm_sec >= 60) { tp->tm_sec -= 60; tp->tm_min++; }
    while (tp->tm_sec < 0)   { tp->tm_sec += 60; tp->tm_min--; }
    while (tp->tm_min >= 60) { tp->tm_min -= 60; tp->tm_hour++; }
    while (tp->tm_min < 0)   { tp->tm_min += 60; tp->tm_hour--; }
    while (tp->tm_hour >= 24) { tp->tm_hour -= 24; tp->tm_mday++; }
    while (tp->tm_hour < 0)   { tp->tm_hour += 24; tp->tm_mday--; }
    while (tp->tm_mon >= 12) { tp->tm_mon -= 12; tp->tm_year++; }
    while (tp->tm_mon < 0)   { tp->tm_mon += 12; tp->tm_year--; }
    for (;;) {
        dim = days_in_month((long)tp->tm_year + 1900, (long)tp->tm_mon + 1);
        if (tp->tm_mday > dim) {
            tp->tm_mday -= dim;
            tp->tm_mon++;
            if (tp->tm_mon >= 12) { tp->tm_mon -= 12; tp->tm_year++; }
        } else if (tp->tm_mday < 1) {
            tp->tm_mon--;
            if (tp->tm_mon < 0) { tp->tm_mon += 12; tp->tm_year--; }
            tp->tm_mday += days_in_month((long)tp->tm_year + 1900,
                                         (long)tp->tm_mon + 1);
        } else {
            break;
        }
    }
}

time_t mktime(struct tm *tp) {
    long days;
    long sec;
    long y;
    long m;
    long d;
    if (tp == 0) return (time_t)-1;
    tm_normalize(tp);
    y = (long)tp->tm_year + 1900;
    m = (long)tp->tm_mon + 1;
    d = (long)tp->tm_mday;
    days = days_from_civil(y, m, d);
    sec = days * 86400L + (long)tp->tm_hour * 3600L
          + (long)tp->tm_min * 60L + (long)tp->tm_sec;
    /* Interpreted as local time, so the offset is subtracted to get UTC. */
    tp->tm_wday = (int)((days + 4) % 7);
    if (tp->tm_wday < 0) tp->tm_wday += 7;
    tp->tm_yday = (int)(days - days_from_civil(y, 1, 1));
    return (time_t)(sec - tz_offset());
}

/* ---- formatting -------------------------------------------------------- */

static char *wday_abbr(int i) {
    switch (i) {
    case 0: return "Sun";
    case 1: return "Mon";
    case 2: return "Tue";
    case 3: return "Wed";
    case 4: return "Thu";
    case 5: return "Fri";
    default: return "Sat";
    }
}

static char *wday_full(int i) {
    switch (i) {
    case 0: return "Sunday";
    case 1: return "Monday";
    case 2: return "Tuesday";
    case 3: return "Wednesday";
    case 4: return "Thursday";
    case 5: return "Friday";
    default: return "Saturday";
    }
}

static char *mon_abbr(int i) {
    switch (i) {
    case 0: return "Jan";
    case 1: return "Feb";
    case 2: return "Mar";
    case 3: return "Apr";
    case 4: return "May";
    case 5: return "Jun";
    case 6: return "Jul";
    case 7: return "Aug";
    case 8: return "Sep";
    case 9: return "Oct";
    case 10: return "Nov";
    default: return "Dec";
    }
}

static char *mon_full(int i) {
    switch (i) {
    case 0: return "January";
    case 1: return "February";
    case 2: return "March";
    case 3: return "April";
    case 4: return "May";
    case 5: return "June";
    case 6: return "July";
    case 7: return "August";
    case 8: return "September";
    case 9: return "October";
    case 10: return "November";
    default: return "December";
    }
}

/* Appenders that count what the result *would* need, so strftime can report
 * a short buffer instead of silently truncating. */

static void sf_chr(char *s, size_t max, size_t *n, char c) {
    if (*n + 1 < max) s[*n] = c;
    *n = *n + 1;
}

static void sf_str(char *s, size_t max, size_t *n, const char *t) {
    while (*t) sf_chr(s, max, n, *t++);
}

/* Zero-padded (or space-padded when pad is ' ') to `width`. */
static void sf_num(char *s, size_t max, size_t *n, long v, int width, char pad) {
    char buf[24];
    int i = 0;
    long x = v;
    if (x < 0) x = -x;
    do {
        buf[i++] = (char)('0' + (int)(x % 10));
        x = x / 10;
    } while (x > 0 && i < 24);
    if (v < 0) sf_chr(s, max, n, '-');
    while (i < width) {
        sf_chr(s, max, n, pad);
        width--;
    }
    while (i > 0) sf_chr(s, max, n, buf[--i]);
}

size_t strftime(char *s, size_t max, const char *fmt, const struct tm *tp) {
    size_t n = 0;
    long hour12;
    if (s == 0 || max == 0) return 0;
    s[0] = '\0';
    while (*fmt) {
        if (*fmt != '%') {
            sf_chr(s, max, &n, *fmt++);
            continue;
        }
        fmt++;
        hour12 = tp->tm_hour % 12;
        if (hour12 == 0) hour12 = 12;
        switch (*fmt) {
        case 'Y': sf_num(s, max, &n, (long)tp->tm_year + 1900, 4, '0'); break;
        case 'y': sf_num(s, max, &n, (long)(tp->tm_year + 1900) % 100, 2, '0'); break;
        case 'm': sf_num(s, max, &n, (long)tp->tm_mon + 1, 2, '0'); break;
        case 'd': sf_num(s, max, &n, (long)tp->tm_mday, 2, '0'); break;
        case 'e': sf_num(s, max, &n, (long)tp->tm_mday, 2, ' '); break;
        case 'H': sf_num(s, max, &n, (long)tp->tm_hour, 2, '0'); break;
        case 'I': sf_num(s, max, &n, hour12, 2, '0'); break;
        case 'M': sf_num(s, max, &n, (long)tp->tm_min, 2, '0'); break;
        case 'S': sf_num(s, max, &n, (long)tp->tm_sec, 2, '0'); break;
        case 'j': sf_num(s, max, &n, (long)tp->tm_yday + 1, 3, '0'); break;
        case 'w': sf_num(s, max, &n, (long)tp->tm_wday, 1, '0'); break;
        case 'p': sf_str(s, max, &n, tp->tm_hour < 12 ? "AM" : "PM"); break;
        case 'a': sf_str(s, max, &n, wday_abbr(tp->tm_wday)); break;
        case 'A': sf_str(s, max, &n, wday_full(tp->tm_wday)); break;
        case 'b': sf_str(s, max, &n, mon_abbr(tp->tm_mon)); break;
        case 'B': sf_str(s, max, &n, mon_full(tp->tm_mon)); break;
        case 'Z': break;                 /* no zone name available */
        case '%': sf_chr(s, max, &n, '%'); break;
        case 'F':
            sf_num(s, max, &n, (long)tp->tm_year + 1900, 4, '0');
            sf_chr(s, max, &n, '-');
            sf_num(s, max, &n, (long)tp->tm_mon + 1, 2, '0');
            sf_chr(s, max, &n, '-');
            sf_num(s, max, &n, (long)tp->tm_mday, 2, '0');
            break;
        case 'T':
            sf_num(s, max, &n, (long)tp->tm_hour, 2, '0');
            sf_chr(s, max, &n, ':');
            sf_num(s, max, &n, (long)tp->tm_min, 2, '0');
            sf_chr(s, max, &n, ':');
            sf_num(s, max, &n, (long)tp->tm_sec, 2, '0');
            break;
        case 'R':
            sf_num(s, max, &n, (long)tp->tm_hour, 2, '0');
            sf_chr(s, max, &n, ':');
            sf_num(s, max, &n, (long)tp->tm_min, 2, '0');
            break;
        case 'D':
            sf_num(s, max, &n, (long)tp->tm_mon + 1, 2, '0');
            sf_chr(s, max, &n, '/');
            sf_num(s, max, &n, (long)tp->tm_mday, 2, '0');
            sf_chr(s, max, &n, '/');
            sf_num(s, max, &n, (long)(tp->tm_year + 1900) % 100, 2, '0');
            break;
        default:
            sf_chr(s, max, &n, '%');
            sf_chr(s, max, &n, *fmt);
            break;
        }
        fmt++;
    }
    if (n < max) s[n] = '\0';
    else s[max - 1] = '\0';
    return n;
}

char *asctime(const struct tm *tp) {
    static char buf[26];
    strftime(buf, sizeof(buf), "%a %b %e %H:%M:%S %Y\n", tp);
    return buf;
}

char *ctime(const time_t *tp) {
    return asctime(localtime(tp));
}

/* ====================== POSIX reentrant + C11 =========================== */

char *asctime_r(const struct tm *tp, char *buf) {
    strftime(buf, 26, "%a %b %e %H:%M:%S %Y\n", tp);
    return buf;
}

char *ctime_r(const time_t *tp, char *buf) {
    return asctime_r(localtime(tp), buf);
}

/*
 * clock_gettime: wall-clock (CLOCK_REALTIME) on both platforms; the others
 * are only meaningful on Linux. Windows has no sub-second clock beyond the
 * file-time tick here, so any non-REALTIME request returns -1. The internal
 * loader is renamed __goc_clock_gettime to avoid clashing with this public
 * declaration's signature.
 */
int clock_gettime(long clk, struct timespec *ts) {
#if defined(_WIN32)
    if (clk != CLOCK_REALTIME) return -1;
    {
        GFileTime ft;
        unsigned long long t;
        GetSystemTimeAsFileTime(&ft);
        t = ((unsigned long long)ft.hi << 32) | (unsigned long long)ft.lo;
        ts->tv_sec = (long)(t / TICKS_PER_SEC - SECS_1601_1970);
        ts->tv_nsec = (long)((t % TICKS_PER_SEC) * 100ULL);
    }
    return 0;
#else
    if (clk == CLOCK_REALTIME || clk == CLOCK_PROCESS_CPUTIME_ID)
        return (int)__goc_clock_gettime(clk, ts);
    return -1;
#endif
}

int timespec_get(struct timespec *ts, int base) {
    if (ts == 0 || base != TIME_UTC) return 0;
    if (clock_gettime(CLOCK_REALTIME, ts) != 0) return 0;
    return base;
}

/* tzset: goc resolves the local offset on every call via tz_offset() (which
 * reads TZ), so there is nothing to precompute. We still expose tzname[] and
 * timezone for source compatibility; they are populated lazily. */
char *tzname[2] = { "UTC", "UTC" };
long timezone = 0;
int daylight = 0;

void tzset(void) {
    char *tz = getenv("TZ");
    if (tz == 0 || tz[0] == '\0') {
        tzname[0] = "UTC";
        tzname[1] = "UTC";
        timezone = 0;
        daylight = 0;
        return;
    }
    /* Whatever the spelling, the offset is read live by tz_offset(); here we
     * just record that a zone was requested. */
    tzname[0] = "UTC";
    tzname[1] = tz;
    timezone = -tz_offset();    /* POSIX sign: timezone = UTC - local */
    daylight = 0;
}
