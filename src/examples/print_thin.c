/* Thin print dispatch: single-argument print(...) calls lower to
 * str_print / int_print / long_print, so this program links none
 * of the vfmt format interpreter. Multi-argument and float prints stay on
 * printf (covered by ufcs_print.c); the point here is that every call in
 * this file takes the thin path and the binary still works end to end.
 */
#include <stdio.h>

int main() {
    print("hello world");
    print(42);
    print();
    print(-1234567890123456789);
    int x = 7;
    print(x + 1);
    print();
    long L = 9223372036854775807;
    print(L);
    print((char)65); /* char prints as its numeric value, like %d */
    _Bool b = 1;
    print(b);
    print();
    return 0;
}
