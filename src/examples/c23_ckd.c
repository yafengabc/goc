#include <stdio.h>
#include <stdckdint.h>
#include <string.h>

/* C23 <stdckdint.h>: ckd_add/sub/mul return a _Bool that is true on overflow,
 * storing the (possibly wrapped) result in the first argument. Also exercises
 * the C23 memset_explicit, which must clear memory the implementation cannot
 * elide. */
int main(void) {
    int ri;
    _Bool o1 = ckd_add(&ri, 2147483647, 1);   /* signed int overflow */
    printf("o1=%d v1=%d\n", o1, ri);
    _Bool o2 = ckd_add(&ri, 100, 200);        /* fits */
    printf("o2=%d v2=%d\n", o2, ri);
    unsigned int ru;
    _Bool o3 = ckd_add(&ru, 0xFFFFFFFFu, 1u);  /* unsigned int overflow */
    printf("o3=%d v3=%u\n", o3, ru);

    char buf[4];
    memset_explicit(buf, 0xAB, 4);
    printf("m0=%d m3=%d\n", buf[0], buf[3]);
    return 0;
}
