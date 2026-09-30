// Exercises goclib's printf precision support for %f. Before the fix, %.15f
// was mishandled (the handler hard-coded 6 fractional digits and did not parse
// ".NN"), so this locks the ".precision" parsing on both Win and Linux backends.
//
// Note: goclib truncates rather than rounds the last fractional digit, matching
// its original default-precision behaviour, so the golden reflects truncation.

#include <stdio.h>

int main() {
    printf("default=%f\n", 3.14159265);
    printf("p6=%.6f\n", 3.14159265);
    printf("p3=%.3f\n", 1.5);
    printf("p15=%.15f\n", 0.1);
    printf("p0=%.0f\n", 2.5);
    printf("p0b=%.0f\n", 2.0);
    printf("w2=%10.2f\n", 1.2345);   // field-width 10 now honoured: "      1.23"
    printf("neg=%.2f\n", -3.14159);
    printf("zero=%.5f\n", 0.0);
    return 0;
}
