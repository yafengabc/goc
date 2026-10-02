"""Independent exact-integer Chudnovsky reference for pi * 10^DIG.

Same series as the C program, but computed with Python's arbitrary-precision
integers (no _BitInt, no fixed-width wrap) -- validates the C pipeline's
arithmetic end to end. Algorithm cross-checked against Machin at DIG=12.
"""
import sys
sys.set_int_max_str_digits(2_000_000)

DIG = int(sys.argv[1]) if len(sys.argv) > 1 else 100010
GUARD = 25
OUT = sys.argv[2] if len(sys.argv) > 2 else None

N = (DIG + GUARD) // 14 + 2          # ~14.18 digits per term
C = 640320
C3 = C**3 // 24

def bs(a, b):
    """returns (P, Q, T) for terms a..b-1; term k = (-1)^k * (6k)!/(k!^3) * (A+Bk) / C3^(k)"""
    if b - a == 1:
        k = a
        if k == 0:
            P = Q = 1
        else:
            P = (6*k - 5) * (2*k - 1) * (6*k - 1)
            Q = k**3 * C3
        T = P * (13591409 + 545140134*k)
        if k & 1:
            T = -T
        return P, Q, T
    m = (a + b) // 2
    Pl, Ql, Tl = bs(a, m)
    Pr, Qr, Tr = bs(m, b)
    return Pl*Pr, Ql*Qr, Tl*Qr + Pl*Tr

P, Q, T = bs(0, N)
# pi = (426880*sqrt(10005)*Q) / T  ->  pi*10^DIG via integer sqrt
one = 10**(DIG + GUARD)
# sqrt(10005) scaled by 10^(DIG+GUARD) via Newton on integers
def isqrt(n):
    x = 1 << ((n.bit_length() + 1) // 2)
    while True:
        y = (x + n // x) // 2
        if y >= x:
            return x
        x = y

s = isqrt(10005 * one * one)
pi_scaled = (426880 * s * Q) // T
# strip guard digits
val = str(pi_scaled // (10**GUARD))
if OUT:
    open(OUT, 'w').write(val + '\n')
else:
    print(val)
