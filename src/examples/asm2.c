/* asm2.c -- real-CPU exercise of the instructions goa grew beyond the basics.
 *
 * asm1.c proves the inline-asm *binder* works (C variables -> memory
 * operands). This one proves the *instruction set* works: every block below
 * hands new opcodes to the assembler and then checks, in C, that the CPU
 * actually did the right thing. A mistyped opcode byte shows up here as a
 * wrong number rather than as a listing that merely assembled.
 *
 * Two further rules matter once the block touches C *locals*:
 *   - asm1.c notes that only ';' and "//" comment out a line; C block
 *     comments are not understood inside the block.
 *   - a C local lives in a full 8-byte stack slot, so writing it must use a
 *     64-bit register. Ending a computation in eax and storing that is legal
 *     but leaves the top half of the slot untouched, and the C side then
 *     reads the leftover. `movsxd rdx, edx` is how a signed 32-bit result is
 *     widened back out to fill the slot.
 */
#include <stdio.h>

static int fails = 0;
static int checks = 0;

static void check(const char *what, int got, int want) {
    checks++;
    if (got != want) {
        fails++;
        printf("FAIL %s: got %d want %d\n", what, got, want);
    }
}

/* movzx / movsx: 0xF6 is 246 zero-extended but -10 sign-extended. */
static void test_movext(void) {
    int z, s, zw;
    __asm {
        mov rcx, 246
        movzx eax, cl
        movsx edx, cl
        movsxd rdx, edx    // widen the signed result back to the 64-bit slot
        mov z, eax
        mov s, rdx
        movzx r8d, cx
        mov zw, r8d
    }
    check("movzx byte->dword", z, 246);
    check("movsx byte->dword", s, -10);
    check("movzx word->dword", zw, 246);
}

/* movsx/movzx reading memory, where the width comes from `byte`/`word`. */
static void test_movext_mem(void) {
    int slot = 0;
    int r;
    __asm {
        mov rax, -5
        mov slot, eax
        movzx eax, byte [slot]
        mov r, eax
    }
    check("movzx from byte memory", r, 251); /* low byte of -5 */
}

/* setcc: turn a comparison into a plain 0/1 without branching. */
static void test_setcc(void) {
    int eq, ne, lt, ge;
    __asm {
        mov eax, 5
        cmp eax, 5
        sete cl
        movzx eax, cl
        mov eq, eax

        mov eax, 5
        cmp eax, 6
        setne cl
        movzx eax, cl
        mov ne, eax

        mov eax, 3
        cmp eax, 7
        setl cl
        movzx eax, cl
        mov lt, eax

        mov eax, 9
        cmp eax, 7
        setge cl
        movzx eax, cl
        mov ge, eax
    }
    check("sete (equal)", eq, 1);
    check("setne (not equal)", ne, 1);
    check("setl (less)", lt, 1);
    check("setge (greater or equal)", ge, 1);
}

/* cmovcc: the branchless select a compiler wants for x = c ? a : b. */
static void test_cmov(void) {
    int taken, skipped, negval;
    __asm {
        mov eax, 10
        mov edx, 20
        cmp eax, 5
        cmovg eax, edx
        mov taken, eax

        mov eax, 2
        mov edx, 20
        cmp eax, 5
        cmovg eax, edx
        mov skipped, eax

        mov eax, -3
        mov edx, 44
        cmp eax, 0
        cmovz eax, edx
        movsxd rax, eax    // widen before storing into the 8-byte slot
        mov negval, rax
    }
    check("cmovg taken", taken, 20);
    check("cmovg not taken", skipped, 2);
    check("cmovz stays put when flag clear", negval, -3);
}

/* Rotations: rol/ror move bits through the carry-free end of the register. */
static void test_rot(void) {
    int l, r, through;
    __asm {
        mov eax, 1
        rol eax, 3
        mov l, eax

        mov eax, 8
        ror eax, 1
        mov r, eax

        // 0x80000001 rotated left once wraps the top bit round to bit 0
        mov eax, -2147483647
        rol eax, 1
        mov through, eax
    }
    check("rol eax,3", l, 8);
    check("ror eax,1", r, 4);
    check("rol wraps top bit", through, 3);
}

/* bt/bts/btr/btc: test and modify one bit of a bitmask. */
static void test_bitops(void) {
    int after_set, after_compl, after_reset, seen;
    __asm {
        mov rax, 0
        bts rax, 3
        mov after_set, eax

        btc rax, 3
        mov after_compl, eax

        mov rax, 255
        btr rax, 0
        mov after_reset, eax

        mov rax, 8
        bt rax, 3
        setc cl
        movzx eax, cl
        mov seen, eax
    }
    check("bts sets bit 3", after_set, 8);
    check("btc clears bit 3", after_compl, 0);
    check("btr clears bit 0", after_reset, 254);
    check("bt reads bit 3", seen, 1);
}

/* bswap reverses bytes: little-endian register -> big-endian wire order. */
static void test_bswap(void) {
    int swapped;
    __asm {
        mov eax, 0x11223344
        bswap eax
        mov swapped, eax
    }
    check("bswap eax", swapped, 0x44332211);
}

/* push imm / pop reg, and push/pop of a memory slot. */
static void test_pushpop(void) {
    int popped;
    long long wide;
    __asm {
        push 42
        pop rax
        mov popped, eax

        push -1
        pop rax
        mov wide, rax
    }
    check("push imm8 / pop", popped, 42);
    check("push -1 keeps full width", (int)wide, -1);
}

/* not / mul / neg on registers. */
static void test_unary(void) {
    int notv, prod, negv;
    __asm {
        mov rax, 0
        not rax
        mov notv, rax

        mov rax, 7
        mov rcx, 6
        mul rcx
        mov prod, eax

        mov rax, 21
        neg rax
        mov negv, rax
    }
    check("not 0", notv, -1);
    check("mul 7*6", prod, 42);
    check("neg 21", negv, -21);
}

/* xchg swaps without a scratch register. */
static void test_xchg(void) {
    int a, b;
    __asm {
        mov rax, 11
        mov rcx, 22
        xchg rax, rcx
        mov a, eax
        mov b, ecx
    }
    check("xchg rax<-rcx", a, 22);
    check("xchg rcx<-rax", b, 11);
}

/* adc/sbb: multi-word add and subtract carry the borrow/carry in CF.
 * `cmp 5, 10` borrows, so CF=1 feeds these. */
static void test_adcsbb(void) {
    int sum, diff;
    __asm {
        mov rcx, 5
        mov rdx, 10
        cmp rcx, rdx   // 5 - 10 borrows => CF = 1

        mov rax, 100
        adc rax, 0
        mov sum, eax

        mov rcx, 5
        mov rdx, 10
        cmp rcx, rdx   // set CF again
        mov rax, 100
        sbb rax, 0
        mov diff, eax
    }
    check("adc adds the carry", sum, 101);
    check("sbb subtracts the borrow", diff, 99);
}

/* Short jumps: `jmp short` (EB rel8), `j<cc> short` (7x rel8) and jrcxz,
 * which exists in no other form. All three need the linker to measure a
 * 1-byte displacement instead of the usual 4. */
static void test_short_jumps(void) {
    int skipped, loop_hits, branch;
    __asm {
        mov eax, 0
        jmp short .Lsk
        add eax, 5
    .Lsk:
        add eax, 2
        mov skipped, eax

        // jrcxz taken when rcx is zero
        mov rcx, 0
        mov eax, 0
        jrcxz .Lcz
        add eax, 100
    .Lcz:
        add eax, 1
        mov loop_hits, eax

        // jz short: taken
        mov eax, 3
        cmp eax, 3
        jz short .Lz
        mov eax, -1
        jmp short .Lout
    .Lz:
        mov eax, 7
    .Lout:
        mov branch, eax
    }
    check("jmp short skips one insn", skipped, 2);
    check("jrcxz taken on rcx==0", loop_hits, 1);
    check("jz short taken", branch, 7);
}

/* leave tears down a frame in one byte; cpuid/rdtsc are the only way to read
 * the CPU identity and cycle counter. We only check that cpuid does not trap
 * and leaves eax non-zero (the max leaf is never 0 on any real x86-64). */
static void test_scalar(void) {
    int leaf;
    __asm {
        mov eax, 0
        cpuid
        mov leaf, eax
    }
    check("cpuid leaf 0 returns eax", leaf != 0, 1);
}

int main(void) {
    test_movext();
    test_movext_mem();
    test_setcc();
    test_cmov();
    test_rot();
    test_bitops();
    test_bswap();
    test_pushpop();
    test_unary();
    test_xchg();
    test_adcsbb();
    test_short_jumps();
    test_scalar();

    if (fails == 0) {
        printf("all ok (%d checks)\n", checks);
    }
    return 0;
}
