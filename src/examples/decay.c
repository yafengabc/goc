#include <stdio.h>

/* Array-to-pointer decay: an array used as an rvalue becomes a pointer to its
 * first element, except in the & and sizeof contexts. These cases all rely on
 * that rule. */

void take_ptr(int *q) { printf("%d\n", q[2]); }   /* argument decays to int*   */
void take_arr(int b[]) { printf("%d\n", b[1]); }  /* param as T[] decays to T* */
void take_str(char s[]) { printf("%s\n", s); }    /* char[] param decays to char* */

int main() {
    int a[5];
    a[0] = 0; a[1] = 10; a[2] = 20; a[3] = 30; a[4] = 40;

    int *p = a;                       /* decay in initializer          */
    int *q;
    q = a;                            /* decay in assignment           */
    printf("%d\n", *p);               /* 0                            */
    printf("%d\n", p[3]);             /* 30                           */
    take_ptr(a);                      /* 20  (call argument decays)    */
    if (q == a) printf("eq1\n"); else printf("ne1\n");   /* eq1 (comparison decays) */
    printf("%d\n", sizeof(a));        /* 20  (sizeof does NOT decay)   */

    int (*pa)[5] = &a;                /* &array -> pointer to array    */
    printf("%d\n", (*pa)[4]);         /* 40                           */

    char s[3];
    s[0] = 'h'; s[1] = 'i'; s[2] = 0;
    take_str(s);                      /* hi  (char array decays)       */
    take_arr(a);                      /* 10  (array argument decays)   */

    int *pp = a + 2;                  /* pointer arithmetic on decayed array */
    printf("%d\n", *pp);              /* 20                           */
    if (&a[0] == a) printf("same\n"); else printf("diff\n");  /* same */

    int m[2][3];
    m[0][0] = 1; m[0][1] = 2; m[0][2] = 3;
    m[1][0] = 4; m[1][1] = 5; m[1][2] = 6;
    int (*pm)[3] = m;                 /* 2-D array decays to pointer to row */
    printf("%d %d\n", pm[1][2], (*pm)[2]);  /* 6 3 */

    return 0;
}
