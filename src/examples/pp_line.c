/* pp_line.c — #line directive and character constants inside #if.
 *
 * C89 residue covered here:
 *   - #line N ["file"] rebases __LINE__ / __FILE__ from the next line on
 *   - GNU form "# N" is accepted too (not exercised in the golden output)
 *   - character literals are legal operands of #if / #elif
 */
#include <stdio.h>

#if 'A' == 65 && '\n' == 10 && '0' == 48 && '\t' == 9
#define CHARS_OK 1
#else
#define CHARS_OK 0
#endif

int main(void) {
    printf("chars=%d\n", CHARS_OK);
#line 100
    printf("a=%d\n", __LINE__);          /* logically 101 */
#line 300 "virtual.c"
    printf("b=%d %s\n", __LINE__, __FILE__);  /* 301 virtual.c */
#line 500
    printf("c=%d\n", __LINE__);          /* logically 501 */
    return 0;
}
