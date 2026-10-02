/* ============================================================
   c89_trigraph.c - trigraph translation probe
   Standard   : ISO/IEC 9899:1990 (C89) 2.1.2.1 trigraph sequences
                ??= #   ??/ backslash   ??( [   ?? ) ]   (9 of 9 trigraphs:
                ??= ??/ ??' ??( ??) ??| ??< ??> ???)
   Strategy   : arr??(2??) is arr[2]; msg??(??) is msg[]; string uses ??= ??/ ??( ??).
                gcc needs -trigraphs. goc does not translate trigraphs at all.
   Status     : UNSUPPORTED (verified 2026-10-02, goc vs gcc -std=c89 -trigraphs)
   ============================================================ */
#include <stdio.h>

int main(void) {
    int arr??(2??) = { 1, 2 };
    char msg??(??) = "eq??=mark??/nline??(tail??)";

    printf("tri: arr[0]=%d arr[1]=%d\n", arr??(0??), arr??(1??));
    printf("tri: msg=[%s]\n", msg);
    return 0;
}
