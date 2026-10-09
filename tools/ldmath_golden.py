"""Golden values for the long double math family (tests/portability_cases/
longdouble_math.c).

The reference here is deliberately computed by DIFFERENT algorithms from the
ones in src/goclib/mathl.c -- a shared reduction step would hide a shared bug:

  mathl.c                          here
  -------------------------------- ------------------------------------------
  atan: halve the angle twice,     atan: subtract atan(1/2) once via
        then series                      atan(x) = atan(c) + atan((x-c)/(1+xc))
  log:  atanh series on (m-1)/(m+1)      Decimal.ln()
  exp:  Taylor after k*ln2 split         Decimal.exp()
  pow:  repeated squaring (int y)        exp(y*ln x)
  hypot: scaled sqrt(1+t^2)              plain sqrt(x^2+y^2)

Everything is exact-rational in, 60-digit Decimal through the middle, so a
30-digit golden is 30 digits of truth: an ulp of binary128 (1e-34) lands well
past the last digit printed.
"""
from decimal import Decimal, getcontext
from fractions import Fraction

getcontext().prec = 60

PI = Decimal("3.14159265358979323846264338327950288419716939937510582097494459"
             "2307816406286208998628034825342117067982148086513282306647093844"
             "60955058223172535940812848111")
LN2 = Decimal("0.69314718055994530941723212145817656807550013436025525412068000"
              "9493393621969694715605863326996418687542001481020570685733686502"
              "3730882171539223703054395852841")


def to_binary128(s):
    """The binary128 the literal `s` names, as an exact Fraction."""
    v = Fraction(Decimal(s))
    sign = 1
    if v < 0:
        sign, v = -1, -v
    e = 0
    while Fraction(2) ** (e + 1) <= v:
        e += 1
    while Fraction(2) ** e > v:
        e -= 1
    m = v / Fraction(2) ** e
    scaled = m * 2 ** 112
    M = scaled.numerator // scaled.denominator
    rem = scaled - M
    if rem > Fraction(1, 2) or (rem == Fraction(1, 2) and M % 2 == 1):
        M += 1
    if M == 2 ** 113:
        M //= 2
        e += 1
    val = Fraction(M, 2 ** 112) * Fraction(2) ** e
    return sign * val


def D(v):
    """Exact Fraction -> Decimal at the working precision."""
    return Decimal(v.numerator) / Decimal(v.denominator)


def atan_ref(x):
    """atan for |x| anywhere, by subtracting atan(1/2) -- not by halving."""
    if x < 0:
        return -atan_ref(-x)
    if x > 1:
        return PI / 2 - atan_ref(1 / x)
    c = Decimal("0.5")
    atan_c = series_atan(c)
    r = (x - c) / (1 + x * c)
    return atan_c + series_atan(r)


def series_atan(z):
    """atan(z) for |z| <= 0.5, Taylor to the working precision."""
    z2 = z * z
    term = z
    total = z
    k = 1
    while True:
        term = -term * z2
        add = term / (2 * k + 1)
        total += add
        if abs(add) < Decimal(10) ** -58:
            break
        k += 1
        if k > 4000:
            break
    return total


def sin_ref(x):
    x = x % (2 * PI)
    if x > PI:
        x -= 2 * PI
    term = x
    total = x
    k = 1
    while True:
        term = -term * x * x / ((2 * k) * (2 * k + 1))
        total += term
        if abs(term) < Decimal(10) ** -58:
            break
        k += 1
    return total


def cos_ref(x):
    x = abs(x) % (2 * PI)
    term = Decimal(1)
    total = Decimal(1)
    k = 1
    while True:
        term = -term * x * x / ((2 * k - 1) * (2 * k))
        total += term
        if abs(term) < Decimal(10) ** -58:
            break
        k += 1
    return total


def fmt(v):
    """30 significant digits, the way printf("%.29Le") renders them.

    NOT "%.29e" % v: %-formatting coerces its argument through float(), which
    throws away all but 17 digits -- the first version of this file shipped
    goldens that were only double-accurate and looked plausible.
    """
    s = format(v, ".29e").replace("E", "e")
    mant, exp = s.split("e")
    sign = ""
    if exp[0] in "+-":
        sign, exp = exp[0], exp[1:]
    # printf always writes at least two exponent digits ("e+00"); Python
    # writes one.
    if len(exp) < 2:
        exp = "0" + exp
    return mant + "e" + sign + exp


CASES = []


def add(name, value):
    CASES.append((name, fmt(value)))


def one(name, argstr, fn):
    """fn takes the exact value of the literal `argstr` and returns a Decimal."""
    add(name, fn(D(to_binary128(argstr))))


def two(name, a, b, fn):
    add(name, fn(D(to_binary128(a)), D(to_binary128(b))))


# ---- one-argument functions -------------------------------------------------
one("sqrtl(2)", "2", lambda x: x.sqrt())
one("sqrtl(1e-40)", "1e-40", lambda x: x.sqrt())
one("sqrtl(1e300)", "1e300", lambda x: x.sqrt())
one("cbrtl(27)", "27", lambda x: (lambda r: r - (r * r * r - x) / (3 * r * r))(
    (lambda r: r - (r * r * r - x) / (3 * r * r))((x.ln() / 3).exp())))
one("cbrtl(2)", "2", lambda x: (lambda r: r - (r * r * r - x) / (3 * r * r))(
    (lambda r: r - (r * r * r - x) / (3 * r * r))((x.ln() / 3).exp())))
one("expl(1)", "1", lambda x: x.exp())
one("expl(-0.5)", "-0.5", lambda x: x.exp())
one("expl(10)", "10", lambda x: x.exp())
one("expl(0.001)", "0.001", lambda x: x.exp())
one("expm1l(1e-20)", "1e-20", lambda x: x.exp() - 1)
one("expm1l(0.5)", "0.5", lambda x: x.exp() - 1)
one("exp2l(3)", "3", lambda x: (x * LN2).exp())
one("exp2l(0.5)", "0.5", lambda x: (x * LN2).exp())
one("exp10l(2)", "2", lambda x: (x * Decimal(10).ln()).exp())
one("exp10l(-1)", "-1", lambda x: (x * Decimal(10).ln()).exp())
one("logl(2)", "2", lambda x: x.ln())
one("logl(10)", "10", lambda x: x.ln())
one("logl(1.5)", "1.5", lambda x: x.ln())
one("logl(1e-30)", "1e-30", lambda x: x.ln())
one("log1pl(1e-20)", "1e-20", lambda x: (1 + x).ln())
one("log1pl(0.5)", "0.5", lambda x: (1 + x).ln())
one("log2l(8)", "8", lambda x: x.ln() / LN2)
one("log2l(3)", "3", lambda x: x.ln() / LN2)
one("log10l(2)", "2", lambda x: x.log10())
one("log10l(1000)", "1000", lambda x: x.log10())
one("sinl(1)", "1", sin_ref)
one("sinl(0.5)", "0.5", sin_ref)
one("sinl(-2)", "-2", sin_ref)
one("cosl(1)", "1", cos_ref)
one("cosl(0.5)", "0.5", cos_ref)
one("tanl(0.5)", "0.5", lambda x: sin_ref(x) / cos_ref(x))
one("tanl(1)", "1", lambda x: sin_ref(x) / cos_ref(x))
one("atanl(1)", "1", atan_ref)
one("atanl(0.5)", "0.5", atan_ref)
one("atanl(10)", "10", atan_ref)
one("asinl(0.5)", "0.5", lambda x: atan_ref(x / (1 - x * x).sqrt()))
one("asinl(-0.3)", "-0.3", lambda x: atan_ref(x / (1 - x * x).sqrt()))
one("acosl(0.5)", "0.5", lambda x: PI / 2 - atan_ref(x / (1 - x * x).sqrt()))
one("acosl(-0.9)", "-0.9", lambda x: PI / 2 - atan_ref(x / (1 - x * x).sqrt()))
one("sinhl(1)", "1", lambda x: (x.exp() - (-x).exp()) / 2)
one("sinhl(2)", "2", lambda x: (x.exp() - (-x).exp()) / 2)
one("coshl(1)", "1", lambda x: (x.exp() + (-x).exp()) / 2)
one("coshl(2)", "2", lambda x: (x.exp() + (-x).exp()) / 2)
one("tanhl(1)", "1", lambda x: (x.exp() - (-x).exp()) / (x.exp() + (-x).exp()))
one("tanhl(0.5)", "0.5", lambda x: (x.exp() - (-x).exp()) / (x.exp() + (-x).exp()))

# ---- two-argument functions -------------------------------------------------
two("powl(2,0.5)", "2", "0.5", lambda x, y: (y * x.ln()).exp())
two("powl(3,3)", "3", "3", lambda x, y: (y * x.ln()).exp())
two("powl(1.5,2.5)", "1.5", "2.5", lambda x, y: (y * x.ln()).exp())
two("powl(10,-3)", "10", "-3", lambda x, y: (y * x.ln()).exp())
two("hypotl(3,4)", "3", "4", lambda x, y: (x * x + y * y).sqrt())
two("hypotl(1e-200,1e-200)", "1e-200", "1e-200",
    lambda x, y: (x * x + y * y).sqrt())
two("atan2l(1,2)", "1", "2", lambda y, x: atan_ref(y / x))
two("atan2l(-1,-1)", "-1", "-1",
    lambda y, x: atan_ref(y / x) - PI if x < 0 and y < 0 else atan_ref(y / x))

def lit(s):
    """The C spelling of a literal: it MUST carry the L suffix.

    "1e-40" without it is a double, and widening a double to binary128 lands
    on a different value than rounding the decimal straight to binary128 --
    up to 2^-53 away, which is visible in the 30th digit these goldens are
    checked at.
    """
    if "." in s or "e" in s:
        return s + "L"
    return s + ".0L"


if __name__ == "__main__":
    print("    /* generated by tools/ldmath_golden.py -- decimal reference,")
    print("       60 digits of working precision, 30 printed. */")
    for name, want in CASES:
        fname, rest = name.split("(", 1)
        args = rest.rstrip(")").split(",")
        print('    expect_f("%s", %s(%s), "%s");'
              % (name, fname, ", ".join(lit(a) for a in args), want))
