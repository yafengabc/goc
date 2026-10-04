#include <stdio.h>
int main(void) {
    char buf[64];
    int r = _snprintf_s(buf, sizeof(buf), _TRUNCATE, "%d.%d", 12, 34);
    printf("r=%d str=%s\n", r, buf);          /* expect r=5 str=12.34 */
    char b2[16];
    int r2 = _snprintf_s(b2, sizeof(b2), _TRUNCATE, "hello %s", "world");
    printf("r2=%d str2=%s\n", r2, b2);         /* expect r2=11 str2=hello world */
    return 0;
}
