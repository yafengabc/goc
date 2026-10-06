/* Probe: what does goclib's bump allocator see when it asks the kernel for the
 * current break, and does a subsequent brk() to grow it succeed? Printed with
 * goclib's own vprintf so the output does not depend on the allocator. */
extern long brk(long);
extern int vprintf(const char *fmt, __builtin_va_list ap);
extern int printf(const char *fmt, ...);

int main(void) {
    long a = brk(0);
    printf("brk(0)        = 0x%lx\n", a);
    printf("brk failed?   = %d\n", a == -1);
    if (a != -1) {
        long r = brk(a + 65536);
        printf("brk(+64K)     = 0x%lx\n", r);
        printf("grew ok?      = %d\n", r == a + 65536);
    }
    return 0;
}