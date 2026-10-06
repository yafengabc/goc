// Exercises the full goclib stdio path under Linux: buffered stdout writes
// that must be flushed at exit, a formatted conversion, and a global that
// lives in .data rather than .text.
#include <stdio.h>

int counter = 40;

static const char *const banner = "gocl linux stdio\n";

int main(void) {
    int answer = counter + 2;
    fputs(banner, stdout);
    fprintf(stdout, "answer=%d\n", answer);
    printf("counter=%d answer=%d\n", counter, answer);
    return 0;
}