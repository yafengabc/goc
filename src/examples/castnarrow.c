#include <stdio.h>
int main(void) {
    int v = 0xFFFFFFE2;                 /* -30 as a signed int */
    unsigned char uc = (unsigned char)v;
    printf("uc=%u\n", (unsigned int)uc);       /* expect 226 (low 8 bits) */
    int y = 0x80;
    char c = (char)y;
    printf("c=%d\n", (int)c);                 /* expect -128 (sign-extended) */
    unsigned short us = (unsigned short)0xDEADBEEF;
    printf("us=%u\n", (unsigned int)us);      /* expect 48879 (0xBEEF) */
    return 0;
}
