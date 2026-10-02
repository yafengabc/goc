/* ============================================================
   c89_enum.c - C89 enumeration constants, typedef, arithmetic, switch
   Standard   : ISO/IEC 9899:1990 (C89) 6.5.2.2 enumeration specifiers
   Strategy   : 5 subcases: enumerator values are int constants, enum
                arithmetic, sizeof(enum) (implementation-defined, record vs
                gcc), switch dispatch over an enum, enumerator used as array
                bound. Trailing comma in the enumerator list is C89-illegal
                but accepted by gcc -std=c89 as an extension and by goc;
                both accept it (recorded).
   Status     : PASS (verified 2026-10-02, goc vs gcc -std=c89)
   ============================================================ */
#include <stdio.h>

typedef enum { RED = 1, GREEN, BLUE = 5, TRAIL = 6, } Color;  /* trailing comma */

int main(void) {
    Color c;
    int i;
    int arr[BLUE];

    /* case1: enumerator values: explicit and continued */
    printf("case1: %d %d %d %d\n", RED, GREEN, BLUE, TRAIL);

    /* case2: enum participates in integer arithmetic */
    c = GREEN;
    i = c + BLUE;
    printf("case2: %d\n", i);

    /* case3: sizeof(enum) is implementation-defined (record) */
    printf("case3: %d\n", (int)sizeof(Color));

    /* case4: switch over an enum value */
    c = BLUE;
    switch (c) {
        case RED:   printf("case4: red\n"); break;
        case GREEN: printf("case4: green\n"); break;
        case BLUE:  printf("case4: blue\n"); break;
        default:    printf("case4: other\n"); break;
    }

    /* case5: enumerator used as array bound; count via sizeof */
    arr[0] = RED; arr[1] = GREEN; arr[2] = 0; arr[3] = 0; arr[4] = 0;
    printf("case5: %d %d\n", arr[0], (int)(sizeof(arr) / sizeof(arr[0])));

    return 0;
}
