/* bitor.c -- exercise the bitwise-or operator (|), the compound |= and the
 * precedence relationships between |, && and ==.
 *
 * Golden file: expected/bitor.txt. Runs identically on the Windows (PE32+)
 * and Linux (ELF64) targets.
 */

int main(void) {
    int ok = 1;

    /* Basic | : disjoint bits combine. */
    if ((0x0F | 0xF0) != 0xFF) ok = 0;
    printf("or disjoint = %d\n", (0x0F | 0xF0) == 0xFF);

    /* | differs from + when bits overlap. */
    printf("or overlap = %d\n", (3 | 2) == 3);
    printf("plus overlap = %d\n", (3 + 2) == 5);

    /* Compound assignment |= desugars to lhs = lhs | rhs. */
    int x = 0x12;
    x |= 0x21;
    printf("or assign = %d\n", x == 0x33);

    /* Precedence: | binds tighter than && but looser than ==.
     * 0x0F | 0xF0 == 0xFF  parses as  0x0F | (0xF0 == 0xFF)  = 0x0F | 1 = 0x0F,
     * and  1 | 1 && 0      parses as  (1 | 1) && 0          = 0. */
    printf("prec eq = %d\n", (0x0F | (0xF0 == 0xFF)) == 0x0F);
    printf("prec and = %d\n", ((1 | 1) && 0) == 0);

    /* Bitwise-or with 0 is the identity; with all-ones it saturates. */
    printf("or zero = %d\n", (0x1234 | 0) == 0x1234);
    printf("or ones = %d\n", (0x1234 | 0xFFFF) == 0xFFFF);

    printf("fails=%d\n", !ok);
    return 0;
}
