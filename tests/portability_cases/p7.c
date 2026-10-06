#include <stdio.h>
/* Does stdout survive a second write? */
int main(void) {
    fwrite("one\n", 1, 4, stdout);
    fwrite("two\n", 1, 4, stdout);
    return 0;
}
