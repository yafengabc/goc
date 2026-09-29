/* Thin integer printing: int_print / long_print are weightless printf("%d")
 * replacements (digits-to-buffer + one write, no format interpreter). The
 * names are the UFCS spellings, so the scalar method syntax x.print() works
 * on any int / long lvalue or rvalue.
 */
#include <stdio.h>

int main() {
    int a = 42;
    long big = -1234567890;

    a.print();
    print();
    big.print();
    print();
    (a + 1).print();
    print();
    (0).print(); /* bare 0.print() would lex as the float 0. -- parenthesise */
    print();

    /* direct calls: sign edge cases via unsigned negation */
    int_print(-2147483647 - 1);
    print();
    long_print(9223372036854775807);
    print();
    long_print(-9223372036854775807 - 1);
    print();
    return 0;
}
