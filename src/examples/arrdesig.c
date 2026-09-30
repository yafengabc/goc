// Array designators ("[i] =") in brace initialisers. Exercises positional and
// designated array init, incomplete-length inference from a designator index,
// char arrays, a designator inside a nested aggregate, and the global/.data
// form. Mixing positional and designated elements is rejected by the checker,
// just like struct member designators already are.
#include <stdio.h>

int g[5] = {[1] = 10, [3] = 30};          // g = {0,10,0,30,0}
int ginf[] = {[2] = 9, [4] = 1};          // length inferred to 5
char gs[8] = {[0] = 'h', [7] = '!'};      // "h\0\0\0\0\0\0!"

struct wrap { int id; int vals[3]; };
struct wrap gw = {.id = 1, .vals = {[1] = 7}};  // vals = {0,7,0}

int main() {
    int a[5] = {[1] = 10, [3] = 30};
    int inf[] = {[2] = 9, [4] = 1};
    char s[8] = {[0] = 'h', [7] = '!'};
    struct wrap w = {.id = 1, .vals = {[1] = 7}};

    printf("a: %d %d %d %d %d\n", a[0], a[1], a[2], a[3], a[4]);
    printf("inf: %d %d %d %d %d (len=%d)\n",
           inf[0], inf[1], inf[2], inf[3], inf[4], (int)sizeof(inf) / (int)sizeof(int));
    printf("s: %c..%c len=%d\n", s[0], s[7], (int)sizeof(s));
    printf("w: id=%d vals=%d %d %d\n", w.id, w.vals[0], w.vals[1], w.vals[2]);

    printf("== global ==\n");
    printf("g: %d %d %d %d %d\n", g[0], g[1], g[2], g[3], g[4]);
    printf("ginf: %d %d %d %d %d (len=%d)\n",
           ginf[0], ginf[1], ginf[2], ginf[3], ginf[4],
           (int)sizeof(ginf) / (int)sizeof(int));
    printf("gs: %c..%c len=%d\n", gs[0], gs[7], (int)sizeof(gs));
    printf("gw: id=%d vals=%d %d %d\n", gw.id, gw.vals[0], gw.vals[1], gw.vals[2]);
    return 0;
}
