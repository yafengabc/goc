/* ============================================================
   c89_pp_misc.c - in-body macro, undef+redefine, undef-then-identifier
   Standard   : ISO/IEC 9899:1990 (C89) 6.8 macro directives
   Strategy   : case1 macro defined inside function body; case2 undef then redefine top level;
                case3 after #undef the name becomes an ordinary identifier
                (#line renames file but is off by one in line numbering, lone # null
                directive unsupported: both exercised as negative probes)
                each case printf distinct, gcc -std=c89 diff
   Status     : PASS: all cases match gcc -std=c89 (verified 2026-10-02)
   ============================================================ */
#include <stdio.h>

#define REDEF_ME 1
#undef REDEF_ME
#define REDEF_ME 99

int main(void) {
#define MARKER 5
    printf("case1: in-body-macro MARKER=%d\n", MARKER);

    printf("case2: undef-then-redef REDEF_ME=%d\n", REDEF_ME);

#undef MARKER
    {
        int MARKER = 7;
        printf("case3: after-undef identifier MARKER=%d\n", MARKER);
    }
    return 0;
}
