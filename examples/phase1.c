// Phase-1 feature test for goc's stage-5 C subset.
//
// Exercises: char literals, for / break / continue, ternary, shifts,
// bitwise AND, prefix & postfix ++/--, casts, const, typedef, extern, and
// global / static variables. The non-variadic goclib-style helpers are written
// in C (no variadic macros yet) and exercised from main. Output is compared
// against expected/phase1.txt on both the Windows-native and Linux(elfcheck)
// targets, so the golden must be identical across both backends.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* size_t now comes from the shipped <stddef.h> (pulled in by the headers
 * above); keep uchar here to exercise `typedef` itself. */
typedef unsigned char uchar;

const int kAnswer = 42;

int g_counter = 7;                    // global in .data
static unsigned long rand_state = 1;  // global static (LCG state)

// ---- C implementations of a few goclib functions (non-variadic subset) ----

size_t my_strlen(int s) {
    char* p = (char*)s;
    size_t n = 0;
    while (p[n] != 0) {
        n = n + 1;
    }
    return n;
}

int my_strcmp(int a, int b) {
    char* pa = (char*)a;
    char* pb = (char*)b;
    int i = 0;
    while (1) {
        char ca = pa[i];
        char cb = pb[i];
        if (ca != cb) {
            if (ca < cb) return -1; else return 1;
        }
        if (ca == 0) return 0;
        i = i + 1;
    }
}

int my_abs(int x) {
    return x < 0 ? -x : x;            // ternary
}

int my_popcount(int x) {
    int c = 0;
    for (int i = 0; i < 32; i++) {    // for loop
        if (x & 1) {                  // bitwise AND
            c = c + 1;
        }
        x = x >> 1;                   // signed right shift (sar)
    }
    return c;
}

unsigned long my_rand() {
    rand_state = rand_state * 1103515245 + 12345; // 64-bit wraparound mul
    return rand_state >> 16;           // unsigned right shift (shr)
}

void my_srand(unsigned long seed) {
    rand_state = seed;
}

// print a signed int as decimal into a local char buffer, then puts() it.
int print_int(int v) {
    char buf[16];
    char* p = (char*)buf;
    int neg = v < 0 ? 1 : 0;
    if (neg) v = -v;
    int i = 0;
    if (v == 0) {
        p[0] = '0';
        i = 1;
    } else {
        while (v > 0) {
            p[i] = '0' + (v % 10);    // char literal '0'
            v = v / 10;
            i = i + 1;
        }
    }
    if (neg) {
        p[i] = '-';
        i = i + 1;
    }
    int lo = 0;
    int hi = i - 1;
    while (lo < hi) {                 // reverse in place
        char t = p[lo];
        p[lo] = p[hi];
        p[hi] = t;
        lo = lo + 1;
        hi = hi - 1;
    }
    p[i] = 0;                         // null terminate
    puts((int)p);
    return i;
}

int main() {
    print_int(kAnswer);
    print_int(g_counter);
    g_counter = g_counter + 1;         // global read + write
    print_int(g_counter);

    print_int((int)my_strlen("hello"));
    print_int((int)my_strlen(""));

    print_int(my_strcmp("abc", "abd"));
    print_int(my_strcmp("abc", "abc"));

    print_int(my_abs(-123));
    print_int(my_abs(123));

    print_int(my_popcount(0xff));
    print_int(my_popcount(7));

    my_srand(42);
    print_int((int)my_rand());
    print_int((int)my_rand());

    print_int('\n');                  // char literal as int (== 10)

    int sum = 0;
    for (int k = 0; k < 10; k++) {    // for + continue + break
        if (k == 3) continue;
        if (k == 8) break;
        sum = sum + k;
    }
    print_int(sum);

    return 0;
}
