/* C23 MVP item #12 (empty brace initialiser {}). Every object initialised with
 * {} is zero-initialised, including scalars, arrays, structs and floats.
 * Golden: src/expected/c23_emptyinit.txt */

#include <stdio.h>

struct Point { int x; int y; };

int main(void) {
    int a = {};
    int arr[3] = {};
    struct Point p = {};
    double d = {};
    printf("a=%d\n", a);
    printf("arr=%d %d %d\n", arr[0], arr[1], arr[2]);
    printf("p=%d %d\n", p.x, p.y);
    printf("d=%f\n", d);
    return 0;
}
