"""Generate pi100k.c: 100k-digit pi via Chudnovsky binary splitting on
unsigned _BitInt, with per-tree-level widths so small subtrees use narrow
types (a uniform max-width type would make every leaf mul cost a full-width
Karatsuba)."""
import math

DIG = 100010          # digits computed
NT = 7200             # series terms

# --- level widths -----------------------------------------------------------
def nlevel():
    n, j = NT, 0
    while n > 1:
        n = (n + 1) // 2
        j += 1
    return j + 1      # number of levels (level j holds nodes covering 2^j leaves)

NL = nlevel()         # 7200 -> 14 levels (0..13)
def wb(j):
    m = min(2 ** j, NT)
    bits = int(m * 88 * 1.30) + 512
    return ((bits + 63) // 64) * 64

def nw(j):
    return wb(j) // 64

def nj(j):
    n = NT
    for _ in range(j):
        n = (n + 1) // 2
    return n

# wide type for the final numerator 426880 * X * Q
QBITS = NT * 88 * 1.05
XBITS = DIG * 3.32193 + 7
WW = int((QBITS + XBITS + 64) * 1.10 / 64 + 1) * 64

out = []
w = out.append
w('#include <stdio.h>')
w('#include <stdlib.h>')
w('#include <bitint.h>')
w('')
for j in range(NL):
    w('typedef unsigned _BitInt(%d) BT%d;' % (wb(j), j))
for j in range(NL):
    w('typedef signed _BitInt(%d) BS%d;' % (wb(j), j))
w('typedef unsigned _BitInt(%d) BTW;' % WW)
w('')
# WORDS13 must match the WIDE final type's word count (what bi_str reads from
# &Num), not the top-level combine width -- otherwise bi_str over-reads stack.
w('#define WORDS13 %d' % (WW // 64))
w('#define DIG %d' % DIG)
w('')
for j in range(NL):
    w('static BT%d P%d[%d];' % (j, j, nj(j)))
    w('static BT%d Q%d[%d];' % (j, j, nj(j)))
    w('static BT%d R%d[%d];' % (j, j, nj(j)))
w('static BTW Num;')
w('static BTW S;')
w('')
w('/* r = 10^e, computed by repeated squaring; the base is never squared') 
w(' * past the last needed bit so it stays far below the type width. */')
w('static void pow10(BT%d *r, long e) {' % (NL - 1))
w('    BT%d base;' % (NL - 1))
w('    *r = 1;')
w('    base = 10;')
w('    while (e > 0) {')
w('        if (e & 1) *r = *r * base;')
w('        e = e >> 1;')
w('        if (e > 0) base = base * base;')
w('    }')
w('}')
w('')
w('int main(void) {')
w('    long i, j, k;')
# --- leaves ---
w('    for (k = 1; k <= %d; k++) {' % NT)
w('        unsigned long long kk = (unsigned long long)k;')
w('        BT0 p; BT0 q; BT0 t;')
w('        p = (2*kk - 1);')
w('        p = p * (6*kk - 1);')
w('        p = p * (6*kk - 5);')
w('        q = kk;')
w('        q = q * kk;')
w('        q = q * kk;')
w('        q = q * 10939058860032000ULL;')
w('        t = p * (13591409ULL + 545140134ULL * kk);')
w('        if (kk & 1) t = 0 - t;')
w('        P0[k-1] = p; Q0[k-1] = q; R0[k-1] = t;')
w('    }')
w('    printf("CP leaves done\\n"); fflush(stdout);')
# --- combine levels ---
for j in range(1, NL):
    n = nj(j)
    prev = nj(j - 1)
    w('    for (i = 0; i < %d; i++) {' % n)
    w('        long l = 2*i; long r = 2*i + 1;')
    w('        if (r < %d) {' % prev)
    w('            P%d[i] = (BT%d)P%d[l] * (BT%d)P%d[r];' % (j, j, j-1, j, j-1))
    w('            Q%d[i] = (BT%d)Q%d[l] * (BT%d)Q%d[r];' % (j, j, j-1, j, j-1))
    w('            R%d[i] = (BT%d)(BS%d)R%d[l] * (BT%d)Q%d[r]' % (j, j, j-1, j-1, j, j-1))
    w('                    + (BT%d)P%d[l] * (BT%d)(BS%d)R%d[r];' % (j, j-1, j, j-1, j-1))
    w('        } else {')
    w('            P%d[i] = (BT%d)P%d[l];' % (j, j, j-1))
    w('            Q%d[i] = (BT%d)Q%d[l];' % (j, j, j-1))
    w('            R%d[i] = (BT%d)(BS%d)R%d[l];' % (j, j, j-1, j-1))
    w('        }')
    w('    }')
    w('    printf("CP combine %d done\\n", %d); fflush(stdout);' % (j, j))
TOP = NL - 1
w('    /* S = A*Q + T  (T already carries its alternating sign) */')
w('    BT%d S13;' % TOP)
w('    S13 = Q%d[0] * 13591409ULL;' % TOP)
w('    S13 = S13 + R%d[0];' % TOP)
w('    /* X = floor(sqrt(10005) * 10^DIG) via Newton on V = 10005*10^(2*DIG) */')
w('    BT%d V; BT%d X; BT%d Y;' % (TOP, TOP, TOP))
w('    pow10(&V, 2*DIG);')
w('    printf("CP pow10 V done\\n"); fflush(stdout);')
w('    V = V * 10005ULL;')
w('    pow10(&X, DIG - 3);')
w('    printf("CP pow10 X done\\n"); fflush(stdout);')
w('    X = X * 100025ULL;   /* 100.025eDIG, just above sqrt(10005)eDIG */')
w('    for (;;) {')
w('        Y = V / X;')
w('        Y = Y + X;')
w('        Y = Y >> 1;')
w('        if (Y >= X) break;')
w('        X = Y;')
w('    }')
w('    printf("CP newton loop done\\n"); fflush(stdout);')
w('    while (X * X > V) X = X - 1;')
w('    {')
w('        BT%d X1;' % TOP)
w('        X1 = X + 1;')
w('        while (X1 * X1 <= V) { X = X1; X1 = X1 + 1; }')
w('    }')
w('    /* pi * 10^DIG = 426880 * X * Q / S */')
w('    BT%d XA;' % TOP)
w('    XA = X * 426880ULL;')
w('    Num = (BTW)XA * (BTW)Q%d[0];' % TOP)
w('    S = (BTW)S13;')
w('    Num = Num / S;')
w('    printf("CP final div done\\n"); fflush(stdout);')
w('    char *buf = (char *)malloc(DIG + 32);')
w('    long L = __goclib_bi_str(buf, &Num, WORDS13, 0);')
w('    printf("LEN %ld\\n", L);')
w('    printf("PI %c.%s\\n", buf[0], buf + 1);')
w('    free((void *)buf);')
w('    return 0;')
w('}')

open('tmp/pi100k.c', 'w').write('\n'.join(out) + '\n')
print('levels', NL, 'top width', wb(TOP), 'words', nw(TOP), 'wide', WW)
print('array bytes total (MB):', sum(3 * nj(j) * nw(j) * 8 for j in range(NL)) / 1e6)
