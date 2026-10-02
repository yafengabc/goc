/* C23 MVP item #11 (enum with explicit underlying type: enum E : T {...}).
 * goc still models the enumerators as int, so the values are int-sized; the
 * syntax is parsed and ignored for layout. Output is identical on both targets.
 * Golden: src/expected/c23_enum.txt */

#include <stdio.h>

enum Color : int { RED, GREEN = 5, BLUE };
enum Small : unsigned char { A = 250, B, C };

int main(void) {
    enum Color c = BLUE;
    enum Small s = C;
    printf("red=%d green=%d blue=%d\n", RED, GREEN, (int)c);
    printf("A=%d B=%d C=%d\n", A, B, (int)s);
    return 0;
}
