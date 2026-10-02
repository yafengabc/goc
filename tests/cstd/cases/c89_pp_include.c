/* ============================================================
   c89_pp_include.c - <> vs "" include, nested include, guard idiom
   Standard   : ISO/IEC 9899:1990 (C89) 6.8.2 source file inclusion
   Strategy   : case1 <> system header (stdio.h) alive; case2 "" relative header value;
                case3 header function-like macro; case4 include-guard second shot idempotent
                (macro-expanded #include not supported by goc: negative probe)
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>
#include "c89_pp_include_inc.h"
#include "c89_pp_include_inc.h"

int main(void) {
    printf("case1: stdio-included-ok\n");
    printf("case2: local-header INC_VAL=%d\n", INC_VAL);
    printf("case3: local-header macro INC_FUNC(21)=%d\n", INC_FUNC(21));
    printf("case4: guard-idiom double-include ok\n");
    return 0;
}
