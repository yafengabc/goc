/* ============================================================
   vla_loop.c - a variable-length array declared inside a loop
   Standard   : ISO/IEC 9899:1999 (C99) 6.7.5.2 (VLAs) + the
                block-exit lifetime of 6.2.4
   Strategy   : gcc -std=c99 is the reference. A VLA in a loop
                body must be released at every iteration, whether
                control leaves the block normally or jumps out
                with break/continue that skips the block-end
                release. Both back ends bracket each loop with a
                stack checkpoint (goc: an rsp slot; gocl:
                llvm.stacksave/stackrestore) so the frame does not
                grow once per iteration -- an unfixed build leaks
                300000 x 4 KiB (~1.2 GB) and dies of a stack
                overflow long before the final print.
   Note       : the gocld __chkstk register-clobber bug this loop
                family once exposed is guarded directly by
                TestChkstkStubSavesVolatileRegs in src/gocld; a C
                example cannot reproduce it reliably because it
                hinges on LLVM's register-allocation choices.
   ============================================================ */
#include <stdio.h>

int main(void) {
    long sum = 0;
    int i;
    /* for-loop with a VLA, exercising both continue and break. */
    for (i = 0; i < 300000; i++) {
        long n = 4096;
        char buf[n];              /* VLA: 4 KiB per iteration */
        buf[0] = (char)i;
        buf[n - 1] = (char)(i >> 8);
        if (i % 3 == 0) { sum += buf[0]; continue; }
        if (i == 299999) { sum += buf[n - 1] * 1000; break; }
        sum += buf[0] + buf[n - 1];
    }
    /* while-loop with a smaller VLA and a continue. */
    int j = 0;
    while (j < 100000) {
        long n = 2048;
        char w[n];
        w[0] = (char)j;
        j++;
        if (j % 2 == 0) { sum += w[0]; continue; }
        sum += 1;
    }
    printf("sum=%ld\n", sum);
    return 0;
}
