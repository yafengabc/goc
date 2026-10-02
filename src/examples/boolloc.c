// Regression test for #81: narrow (_Bool/char/short) stack locals must not be
// clobbered by the byte-store zero-initialiser of an adjacent neighbour.
//
// After the fix, goa encodes `mov byte [mem], 0` as a true 1-byte store
// (opcode C6, imm8). Before the fix it emitted a 4-byte C7 store, which wrote
// bytes N, N+1, N+2, N+3 and silently zeroed the higher-address slot next to
// it -- so an uninitialised narrow local's zero-init would wipe a nearby
// _Bool that had already been stored. The mix of many _Bool locals (forcing
// callee-save register spills) plus interleaved char/short/int exposes it.

#include <stdio.h>

int main() {
    // 10 _Bool locals force stack spills (only 4 callee-save regs).
    _Bool a = 1, b = 0, c = 1, d = 0, e = 1, f = 0, g = 1, h = 0, i = 1, j = 0;
    printf("%d%d%d%d%d%d%d%d%d%d\n", a, b, c, d, e, f, g, h, i, j);

    // Uninitialised-then-assigned narrow locals + adjacent char/short/int.
    _Bool u; u = 9;        // normalises to 1
    _Bool v; v = 0;
    _Bool w; w = 3;        // normalises to 1
    char  ca = 42;  char  cb; cb = 7;
    short sa = 1000; short sb; sb = 2000;
    int   ia = 30000; int ib; ib = 40000;
    _Bool ba = 1;
    printf("%d %d %d %d %d %d %d %d %d %d %d %d\n",
           u, v, w, ca, cb, sa, sb, ia, ib, ba, (u && !v), (w ? 1 : 0));

    if (u && !v) printf("ok\n"); else printf("bad\n");
    return 0;
}
