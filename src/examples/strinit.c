// String-literal initialisation of char arrays, the one array initialiser C
// allows. Covers length inference ("char s[]"), explicit sizes with zero
// tails, the empty string, adjacent-literal concatenation, escapes,
// multi-declarator statements (a *DeclList -- genStmt used to emit nothing
// for them, leaving every initialiser unrun), and globals.
#include <stdio.h>

char g[] = "world";
char g8[8] = "hi";

int main() {
    char s[] = "hi";
    char t[8] = "abc";
    char e[] = "";
    char cat[] = "ab" "cd";
    char esc[] = "q\tw\ne";
    char a[] = "x", b[] = "yz";
    int i = 7, j = 9;
    char *lp = "lptr";

    printf("== local ==\n");
    printf("s=%s sizeof=%d\n", s, (int)sizeof(s));
    printf("t=%s t3=%d t7=%d sizeof=%d\n", t, (int)t[3], (int)t[7], (int)sizeof(t));
    printf("e_len=%d e0=%d\n", (int)sizeof(e), (int)e[0]);
    printf("cat=%s sizeof=%d\n", cat, (int)sizeof(cat));
    printf("esc=[%s] sizeof=%d\n", esc, (int)sizeof(esc));
    printf("a=%s b=%s\n", a, b);
    printf("i=%d j=%d\n", i, j);
    printf("lp=%s\n", lp);

    printf("== global ==\n");
    printf("g=%s g5=%d sizeof=%d\n", g, (int)g[5], (int)sizeof(g));
    printf("g8=%s g82=%d sizeof=%d\n", g8, (int)g8[2], (int)sizeof(g8));

    printf("== mutate ==\n");
    s[0] = 'H';
    g8[1] = 'I';
    t[3] = 'd';
    printf("s=%s t=%s g8=%s\n", s, t, g8);
    return 0;
}
