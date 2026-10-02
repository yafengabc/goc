/* C23 MVP items #4 (static_assert keyword) and #7 (_Static_assert).
 * A failing assertion aborts compilation; all of these pass, so the program
 * runs and prints its line. The warning form is exercised by the compiler
 * itself when an assert is reached.
 * Golden: src/expected/c23_static_assert.txt
 * Identical output on Windows (PE32+) and Linux (ELF64) targets. */

_Static_assert(1 > 0, "basic truth must hold");
static_assert(sizeof(int) == 4, "int must be 4 bytes");
_Static_assert(2 + 2 == 4, "arithmetic must hold");
static_assert('A' == 65, "character value must hold");

int main(void) {
    static_assert(1, "in-function assertion");
    _Static_assert(3 * 3 == 9, "in-function static assert");
    printf("static_assert ok\n");
    return 0;
}
