#ifndef GOC_TIME_H
#define GOC_TIME_H

#include <stddef.h>

/* goc time.h -- wall-clock time and calendar conversion.
 *
 * The C89 set (time/difftime/gmtime/localtime/mktime/asctime/ctime/clock)
 * plus strftime. time_t is seconds since the Unix epoch, as everywhere else
 * on a hosted C implementation; the library gets there from the platform's
 * own clock (GetSystemTimeAsFileTime on Windows, clock_gettime on Linux)
 * and does the calendar arithmetic itself.
 *
 * Two things are deliberately simpler than a hosted library's:
 *
 *   - Time zones. Windows gets the real local zone, because GetLocalTime
 *     hands it over already converted. Linux has no tzfile parser here, so
 *     localtime() reads the TZ environment variable in the "UTC+8" / "-5"
 *     hour-offset form and falls back to UTC when TZ is unset. Set TZ to
 *     get local time on Linux.
 *
 *   - calendar time is the proleptic Gregorian calendar extended backwards,
 *     which is what the days_from_civil conversion used here produces.
 *     There is no Julian/Gregorian switch-over date.
 *
 * gmtime()/localtime() return a pointer to a single static struct tm that
 * the next call overwrites, and asctime()/ctime() share one static string
 * buffer -- both exactly as the standard allows, and both therefore not
 * thread-safe and not reentrant.
 */

/* Seconds since 1970-01-01 00:00:00 UTC. */
typedef long time_t;
/* Processor time in 1/CLOCKS_PER_SEC units. */
typedef long clock_t;
#define CLOCKS_PER_SEC 1000000L

/* Broken-down calendar time. tm_year is years since 1900, tm_mon is 0-11,
 * tm_wday is 0-6 with Sunday 0, tm_yday is 0-365; tm_isdst is negative when
 * the library cannot tell. */
struct tm {
    int tm_sec;    /* 0-60 (60 allows a leap second) */
    int tm_min;    /* 0-59 */
    int tm_hour;   /* 0-23 */
    int tm_mday;   /* 1-31 */
    int tm_mon;    /* 0-11 */
    int tm_year;   /* years since 1900 */
    int tm_wday;   /* 0-6, Sunday = 0 */
    int tm_yday;   /* 0-365 */
    int tm_isdst;  /* >0 in DST, 0 not, <0 unknown */
};

/* Current calendar time; also stored through `out` when it is not null.
 * (time_t)-1 when the clock cannot be read. */
time_t time(time_t *out);
/* end - beginning, in seconds, as a double. */
double difftime(time_t end, time_t beginning);
/* Processor time since the start of the program, in CLOCKS_PER_SEC units;
 * (clock_t)-1 when unavailable. Windows reports wall time (GetTickCount is
 * all this port reaches for); Linux reports real CPU time. */
clock_t clock(void);

/* UTC / local broken-down time. Both use one static struct tm. */
struct tm *gmtime(const time_t *tp);
struct tm *localtime(const time_t *tp);
/* Inverse of localtime(): interprets the struct as local time, normalises
 * out-of-range fields (mktime of tm_mday 32 rolls into the next month) and
 * fills in tm_wday/tm_yday. Returns (time_t)-1 if it cannot be represented. */
time_t mktime(struct tm *tp);

/* Formatted calendar text. fmt takes %Y %m %d %e %H %I %M %S %y %p %a %A
 * %b %B %j %w %F %D %T %R %Z %%, plus ordinary characters. Writes at most
 * `max` bytes including the NUL and returns the length a large enough
 * buffer would have needed -- so a return of 0 or >= max means truncated. */
size_t strftime(char *s, size_t max, const char *fmt, const struct tm *tp);
/* "Sun Sep  9 01:02:03 2026\n" -- always 26 bytes including the newline. */
char *asctime(const struct tm *tp);
/* asctime(localtime(tp)) */
char *ctime(const time_t *tp);

/* ---- POSIX reentrant variants ------------------------------------------ */
/* Like gmtime/localtime but write into the caller's struct tm, so they are
 * safe to use concurrently and do not share the single static buffer. */
struct tm *gmtime_r(const time_t *tp, struct tm *result);
struct tm *localtime_r(const time_t *tp, struct tm *result);
/* Like asctime/ctime but write the 26-byte string into the caller's buffer. */
char *asctime_r(const struct tm *tp, char *buf);
char *ctime_r(const time_t *tp, char *buf);

/* ---- C11 sub-second time ----------------------------------------------- */
/* Seconds + nanoseconds since the epoch. */
struct timespec {
    time_t tv_sec;
    long   tv_nsec;
};
/* The only base timespec_get understands; returns 0 (and writes nothing)
 * for any other base, as the standard requires. */
#define TIME_UTC 1
/* Fill *ts with the current UTC time; returns `base` (TIME_UTC) on success
 * or 0 on failure. */
int timespec_get(struct timespec *ts, int base);

/* ---- POSIX clock / zone ------------------------------------------------ */
/* Real-time clock id for clock_gettime (CLOCK_REALTIME). */
#define CLOCK_REALTIME 0
/* Wall-clock time with nanosecond resolution. Returns 0 on success, -1 on
 * failure. goc has no clockid_t, so the id is a plain long. */
int clock_gettime(long clk, struct timespec *ts);
/* Populate tzname[] and timezone from the TZ environment variable (or UTC).
 * Called automatically before the first local-time lookup, so it is safe to
 * ignore; provided for source compatibility. The three globals below are
 * part of <time.h> (C/POSIX) and must be declared here so callers can read
 * them after tzset(). */
extern char *tzname[2];
extern long timezone;
extern int daylight;
void tzset(void);

#endif /* GOC_TIME_H */
