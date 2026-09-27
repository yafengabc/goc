// Regression test for stack arguments past the register capacity. On SysV
// there is no shadow space, so a callee that spilled its register arguments
// into [rbp+16+8k] would clobber the caller's stack-argument area -- sum7
// used to lose its seventh argument on Linux (21 instead of 28). Register
// arguments are now spilled into the callee's own frame on both targets.

int sum7(int a, int b, int c, int d, int e, int f, int g) {
    return a + b + c + d + e + f + g;
}

int main() {
    printf("%d\n", sum7(1, 2, 3, 4, 5, 6, 7));
    printf("%d\n", sum7(10, 20, 30, 40, 50, 60, 70));
    return 0;
}
