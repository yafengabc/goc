/*
 * cstd_lib_time2.c -- goclib <time.h> coverage, round 2.
 *
 * timespec_get (C11 7.27.2.5) and tzset (POSIX, also in the C standard
 * library via <time.h>) are the two time functions not exercised by the
 * earlier time test. timespec_get writes the current time when base is
 * TIME_UTC and returns that base; a null pointer or a non-UTC base yields 0
 * and writes nothing. tzset has no observable error mode -- it must simply
 * not crash and must leave tzname populated.
 *
 * Only the structural properties (return value, nonzero fields, no crash)
 * are compared; the actual clock value is not, because it differs between
 * runs.
 */
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <time.h>

int main(void) {
    struct timespec ts;
    int r = timespec_get(&ts, TIME_UTC);
    printf("case1: ret=%d nonzero=%d\n", r,
           (ts.tv_sec > 0 || ts.tv_nsec > 0) ? 1 : 0);

    /* null pointer and unknown base are rejected cleanly */
    int r2 = timespec_get(0, TIME_UTC);
    int r3 = timespec_get(&ts, 999);
    printf("case1b: null-ret=%d badbase-ret=%d\n", r2, r3);

    tzset();
    /* tzname[] is populated (non-null pointers) on every toolchain, but the
     * *spelling* of tzname[0] is OS/zone-database specific (e.g. "UTC" vs a
     * localized zone name), so it is not cross-checked byte for byte. */
    printf("case2: tzset-ok=%d tzname0_nn=%d tzname1_nn=%d\n", 1,
           tzname[0] != 0 ? 1 : 0, tzname[1] != 0 ? 1 : 0);
    return 0;
}
