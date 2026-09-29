/* Thin integer printing: int_print / long_print are weightless printf("%d\n")
 * replacements (digits-to-buffer + one write, no format interpreter). The
 * names are the UFCS spellings, so the scalar method syntax x.print() works
 * on any int / long lvalue or rvalue and prints one line, matching the
 * print(...) builtin's semantics. Direct calls, the print builtin and the
 * scalar method all land on these two functions.
 */
#include <stdio.h>

int main() {
    int a = 42;
    long big = -1234567890;

    a.print();
    big.print();
    (a + 1).print();
    (0).print(); /* bare 0.print() would lex as the float 0. -- parenthesise */

    /* direct calls: sign edge cases via unsigned negation */
    int_print(-2147483647 - 1);
    long_print(9223372036854775807);
    long_print(-9223372036854775807 - 1);

    /* the print builtin dispatches here too */
    print(100);
    print(100L);
    return 0;
}
