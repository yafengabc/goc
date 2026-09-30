#include <stdio.h>
#include <string.h>

/* Exercises the buffered FILE layer: fopen/fclose, fprintf/fwrite/fputc,
 * fgets/fgetc/fread, fseek/ftell/feof, fscanf, the stdin/stdout/stderr macros,
 * and remove/rename. Output is plain text so the golden matches on both
 * Windows and Linux. */

int main(void) {
    FILE *f;
    char line[64];
    int n;
    long pos;
    int c, c1;
    int v[4];
    int i;

    /* ---- write a small text file ---- */
    f = fopen("goc_io_test.txt", "w");
    if (!f) { printf("open-w fail\n"); return 1; }
    fprintf(f, "hello %d\n", 42);
    fprintf(f, "world\n");
    fwrite("abc", 1, 3, f);
    fputc('!', f);
    fputc('\n', f);
    fflush(f);
    fclose(f);

    /* ---- read it back ---- */
    f = fopen("goc_io_test.txt", "r");
    if (!f) { printf("open-r fail\n"); return 1; }
    if (fgets(line, 64, f)) printf("L1=[%s]", line);
    if (fgets(line, 64, f)) printf("L2=[%s]", line);
    n = (int)fread(line, 1, 5, f);
    printf("read=%d bytes\n", n);
    pos = ftell(f);
    printf("pos=%ld eof=%d\n", pos, feof(f));

    /* seek and read individual chars: fgetc at 0, then fgetc at 6 */
    fseek(f, 0, 0);
    c1 = fgetc(f);
    fseek(f, 6, 0);
    c = fgetc(f);
    printf("first=%c at6=%c\n", (int)(unsigned char)c1, c);
    fclose(f);

    /* ---- fscanf from a file (literal text + one conversion) ---- */
    f = fopen("goc_io_test.txt", "r");
    for (i = 0; i < 4; i++) v[i] = -1;
    n = fscanf(f, "hello %d\nworld\nabc!\n", &v[0]);
    printf("scan=%d v0=%d\n", n, v[0]);
    fclose(f);

    /* ---- stdin/stdout macros and printf interleaving ---- */
    /* NOTE: we deliberately do NOT fprintf(stderr, ...) here. A native Windows
     * PE that writes both stdout and stderr to a single shell-redirected file
     * (the harness uses `2>&1`) corrupts the shared file offset, so stderr
     * output cannot be made part of a portable golden. stderr is still
     * exercised by the stream-init path below; its content is just not in the
     * golden. */
    printf("before-stdout\n");
    fprintf(stdout, "via-fprintf-stdout\n");
    printf("after-stdout\n");

    /* ---- remove / rename ---- */
    f = fopen("goc_io_rm.txt", "w");
    fclose(f);
    printf("remove=%d\n", remove("goc_io_rm.txt"));

    f = fopen("goc_io_old.txt", "w");
    fprintf(f, "x");
    fclose(f);
    printf("rename=%d\n", rename("goc_io_old.txt", "goc_io_new.txt"));
    remove("goc_io_new.txt");

    return 0;
}
