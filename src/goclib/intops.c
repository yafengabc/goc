/* Software 64-bit integer multiply and divide.
 *
 * These are the compiler-rt helpers LLVM names when it lowers an operation the
 * target has no instruction for. RISC-V is the reason they are here: the
 * baseline ISA has no divide at all, and the M extension, when it is present,
 * still leaves a 64-bit divide to the library because the quotient needs a
 * 128-bit intermediate. A target that does have the instruction -- x86-64 --
 * never names them, so nothing here is linked into a program built for it.
 *
 * Nothing in this file uses floating point, and nothing in it calls anything
 * outside it except through the two helpers below. That matters more than it
 * sounds: an implementation that reached for `/` to implement `/` would
 * resolve back to itself and recurse until the stack ran out.
 */

static unsigned long long goc_udivmod(unsigned long long n, unsigned long long d,
                                      unsigned long long *rem) {
    unsigned long long q = 0, r = 0;
    int i;
    /* Division by zero is undefined in C and traps on real hardware. Rather
     * than iterate 64 times for nothing, return a quotient no caller can
     * confuse with a result. */
    if (d == 0) {
        if (rem) *rem = n;
        return ~0ull;
    }
    /* Restoring division: bring one bit of the dividend down at a time and
     * subtract whenever the remainder has grown past the divisor. Sixty-four
     * steps, and the only arithmetic involved is a shift, a compare and a
     * subtract -- which is exactly why it is the form that bottoms out. */
    for (i = 63; i >= 0; i--) {
        r = (r << 1) | ((n >> i) & 1ull);
        if (r >= d) {
            r -= d;
            q |= 1ull << i;
        }
    }
    if (rem) *rem = r;
    return q;
}

unsigned long long __udivdi3(unsigned long long a, unsigned long long b) {
    return goc_udivmod(a, b, 0);
}

unsigned long long __umoddi3(unsigned long long a, unsigned long long b) {
    unsigned long long r = 0;
    goc_udivmod(a, b, &r);
    return r;
}

/* -(LLONG_MIN) overflows, so the magnitude of a negative goes through
 * -(... + 1) rather than through -(...). */
static unsigned long long goc_neg_magnitude(long long a) {
    return (unsigned long long)(-(a + 1)) + 1ull;
}

long long __divdi3(long long a, long long b) {
    int neg = 0;
    unsigned long long ua, ub, q;
    if (a < 0) {
        ua = goc_neg_magnitude(a);
        neg = !neg;
    } else {
        ua = (unsigned long long)a;
    }
    if (b < 0) {
        ub = goc_neg_magnitude(b);
        neg = !neg;
    } else {
        ub = (unsigned long long)b;
    }
    q = goc_udivmod(ua, ub, 0);
    if (neg) return -(long long)q;
    return (long long)q;
}

long long __moddi3(long long a, long long b) {
    int neg = 0;
    unsigned long long ua, ub, r = 0;
    if (a < 0) {
        ua = goc_neg_magnitude(a);
        neg = 1;
    } else {
        ua = (unsigned long long)a;
    }
    if (b < 0) {
        ub = goc_neg_magnitude(b);
    } else {
        ub = (unsigned long long)b;
    }
    goc_udivmod(ua, ub, &r);
    if (neg) return -(long long)r;
    return (long long)r;
}

/* 32-bit division, for a target whose ISA has no divide instruction at all.
 *
 * These forward to the 64-bit helpers above rather than dividing: RV32 has no
 * `div` in the baseline ISA, so a `/` here would be lowered to a call to the
 * very function being defined -- the recursion that took the whole stack out
 * from under __muldi3. Sign-extending to 64 bits and asking goc_udivmod costs
 * sixty-four shift-compare-subtract steps for what is really a 32-bit division,
 * which is slow; it is also the only form that cannot resolve back to a
 * libcall, and that is the property that matters. */
int __divsi3(int a, int b) {
    return (int)__divdi3((long long)a, (long long)b);
}

int __modsi3(int a, int b) {
    return (int)__moddi3((long long)a, (long long)b);
}

unsigned int __udivsi3(unsigned int a, unsigned int b) {
    return (unsigned int)__udivdi3((unsigned long long)a, (unsigned long long)b);
}

unsigned int __umodsi3(unsigned int a, unsigned int b) {
    return (unsigned int)__umoddi3((unsigned long long)a, (unsigned long long)b);
}

/* 64x64 keeping the low 64 bits, from shift and add alone.
 *
 * Not `a * b`, and not even four 32x32 products: on a target with no 64-bit
 * multiply the compiler lowers a `mul i64` right back into a call to this
 * function, so the obvious implementation recurses until the stack is gone.
 * That is not a slow path, it is a crash -- and a confusing one, because the
 * fault is a store below the stack from a helper nothing in the source named.
 *
 * Shift-and-add is slow and unconditionally correct. Shifts, adds and
 * compares are expanded inline by every back end (only mul, div and rem become
 * library calls), so nothing in this loop can resolve back to a libcall -- the
 * property the whole file exists to have.
 *
 * The loop walks the multiplier and stops when the remaining bits are all
 * zero, so a small operand costs a few steps rather than sixty-four. */
long long __muldi3(long long a, long long b) {
    unsigned long long ua = (unsigned long long)a;
    unsigned long long ub = (unsigned long long)b;
    unsigned long long r = 0;
    int i = 0;
    while (ub) {
        if (ub & 1ull) r += ua << i;
        ub >>= 1;
        i++;
    }
    return (long long)r;
}
