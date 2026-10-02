#include <stdio.h>

/* C23 #embed: splice a file's bytes into an initializer list. The embedded file
 * (embed_data.txt) holds "ABC\n" == {65, 66, 67, 10}; limit(4) keeps all of it. */
unsigned char blob[] = {
#embed "embed_data.txt" limit(4)
};

int main(void) {
    int n = sizeof(blob) / sizeof(blob[0]);
    printf("len=%d\n", n);
    for (int i = 0; i < n; i++) {
        if (i) printf(" ");
        printf("%d", blob[i]);
    }
    printf("\n");
    return 0;
}
