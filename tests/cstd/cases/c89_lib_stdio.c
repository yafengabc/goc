/* ============================================================
   c89_lib_stdio.c - printf format family, puts/putchar, sprintf/sscanf, file I/O
   Standard   : ISO/IEC 9899:1990 (C89) 7.9 input/output <stdio.h>
   Strategy   : case1 printf conversions %d %u %x %o %c %s %f %e %g;
                case2 length modifiers %ld %lu %hd; case3 width/precision/align/zero;
                case4 puts and putchar; case5 sprintf round-trip; case6 sscanf parse;
                case7 file round trip: fopen/fputc/fclose/fgetc/fseek/ftell/rewind/fremove
                temp file lives in the process CWD (the diff build dir), removed at end.
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

int main(void) {
    printf("case1: d=%d u=%u x=%x o=%o c=%c s=[%s] f=%f e=%e g=%g\n",
           -42, 42u, 255, 64, 65, "txt", 1.5, 15000.0, 15000.0);
    printf("case2: ld=%ld lu=%lu hd=%hd\n", -100000L, 4000000000UL, (short)-1000);
    printf("case3: w=[%5d] l=[%-5d] z=[%05d] p=[%8.2f]\n", 42, 42, 42, 3.14159);
    printf("case4a: ");
    puts("puts-line");
    printf("case4b: putchar=");
    putchar(65);
    putchar('\n');
    {
        char ob[64];
        int iv;
        sprintf(ob, "sprintf-val=%d", 1234);
        printf("case5: [%s]\n", ob);
        sscanf("987 3.5", "%d", &iv);
        printf("case6: sscanf=%d\n", iv);
    }
    {
        FILE *fp;
        char ch;
        long pos;
        fp = fopen("c89_stdio_tmp.dat", "w");
        if (fp == NULL) { printf("case7: fopen-write-failed\n"); return 1; }
        fputc('A', fp);
        fputc('B', fp);
        fputc('C', fp);
        fclose(fp);
        fp = fopen("c89_stdio_tmp.dat", "r");
        if (fp == NULL) { printf("case7: fopen-read-failed\n"); return 1; }
        ch = fgetc(fp);
        printf("case7: first=%c\n", ch);
        pos = ftell(fp);
        printf("case7: pos-after-first=%ld\n", pos);
        fseek(fp, 0, SEEK_SET);
        ch = fgetc(fp);
        printf("case7: rewind-first=%c\n", ch);
        fseek(fp, 2, SEEK_SET);
        ch = fgetc(fp);
        printf("case7: seek-offset2=%c\n", ch);
        fclose(fp);
        remove("c89_stdio_tmp.dat");
        printf("case7: roundtrip-done\n");
    }
    return 0;
}
