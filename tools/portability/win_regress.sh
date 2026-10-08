#!/bin/sh
# Windows PE half of the goclib portability regression: goc and gocl, both
# back ends, checked against the same expectations.
#
# The split between this and lin_build.sh / lin_run.sh is not arbitrary. The
# cases that call write(2) directly belong to the Linux group, because write is
# a Linux system call and has no meaning in a PE. Listing them here produced
# three "unknown function \"write\": not in goclib" failures that had nothing to
# do with goclib -- the program under test was asking for a syscall that does
# not exist on that target. Everything both targets can express is here.
#
# Run from a Git Bash checkout on Windows.
set -u
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
SRC=$ROOT/tests/portability_cases
OUT=$ROOT/tmp/portability-out
mkdir -p "$OUT"

pass=0
fail=0

# check <label> <expected> <actual>
check() {
    if [ "$3" = "$2" ]; then
        echo "pass $1"
        pass=$((pass + 1))
    else
        echo "FAIL $1"
        echo "     期望: $2"
        echo "     实得: $3"
        fail=$((fail + 1))
    fi
}

# build_run_win <label> <compiler> <target-flag|-> <name> <expected> <tag>
build_run_win() {
    label=$1
    comp=$2
    tgt=$3
    name=$4
    want=$5
    exe="$OUT/${name}_$6.exe"
    if [ "$tgt" = "-" ]; then
        "$comp" "$SRC/$name.c" -o "$exe" > "$OUT/$name.build" 2>&1
    else
        "$comp" $tgt "$SRC/$name.c" -o "$exe" > "$OUT/$name.build" 2>&1
    fi
    if [ ! -f "$exe" ]; then
        echo "FAIL $label (编译)"
        head -3 "$OUT/$name.build" | cut -c1-160 | sed 's/^/     /'
        fail=$((fail + 1))
        return
    fi
    got=$("$exe" 2>&1)
    check "$label" "$want" "$got"
}

# Self-reporting cases. A program that prints something the kernel chose -- the
# port in tcp.c -- cannot be compared against a fixed string, so these report
# their own verdict: exit code 0 and a final line of "OK". The failure output is
# printed whole, because which check failed is the interesting part and a
# one-line summary would hide it.
build_run_win_ok() {
    label=$1
    comp=$2
    tgt=$3
    name=$4
    exe="$OUT/${name}_$5.exe"
    if [ "$tgt" = "-" ]; then
        "$comp" "$SRC/$name.c" -o "$exe" > "$OUT/$name.build" 2>&1
    else
        "$comp" $tgt "$SRC/$name.c" -o "$exe" > "$OUT/$name.build" 2>&1
    fi
    if [ ! -f "$exe" ]; then
        echo "FAIL $label (编译)"
        head -3 "$OUT/$name.build" | cut -c1-160 | sed 's/^/     /'
        fail=$((fail + 1))
        return
    fi
    got=$("$exe" 2>&1)
    rc=$?
    last=$(printf '%s\n' "$got" | tail -1)
    if [ "$rc" -eq 0 ] && [ "$last" = "OK" ]; then
        echo "pass $label"
        pass=$((pass + 1))
    else
        echo "FAIL $label (rc=$rc)"
        printf '%s\n' "$got" | sed 's/^/     /'
        fail=$((fail + 1))
    fi
}

echo "########## Windows PE ##########"
WANT_WIN1=$(printf 't=707\ns=42 abc ff')

# The stdio-only cases, run on both back ends.
for pair in "win1|$WANT_WIN1" "p2|n=42" "p5|$(printf 'raw\nr=4')" "p7|$(printf 'one\ntwo')"; do
    n=${pair%%|*}
    w=${pair#*|}
    build_run_win "$n  [goc/win] "  "$ROOT/bin/goc.exe"  "-"             "$n" "$w" goc
    build_run_win "$n  [gocl/win]" "$ROOT/bin/gocl.exe" "-target windows" "$n" "$w" gocl
done

# The socket cases. Both need loopback, which every machine this runs on has,
# and both are the only cases here that touch the network stack -- so they are
# also the only ones that can catch an option number or an fd_set layout being
# wrong for the target.
for n in tcp tcp2; do
    build_run_win_ok "$n [goc/win] "  "$ROOT/bin/goc.exe"  "-"               "$n" goc
    build_run_win_ok "$n [gocl/win]" "$ROOT/bin/gocl.exe" "-target windows"  "$n" gocl
done

# The C23 standard-library additions: stdbit rotate (left/right, all widths),
# reallocarray / free_sized / free_aligned_sized, and memalignment. Self-
# reporting (exit 0 + final "OK"). The 8-bit rotations go through the
# type-generic macro, because that is how <stdbit.h> reaches every stdc_*_N
# entry point -- the case gocl's reachability walk used to miss.
build_run_win_ok "c23cstd [goc/win] "  "$ROOT/bin/goc.exe"  "-"               "c23cstd" goc
build_run_win_ok "c23cstd [gocl/win]" "$ROOT/bin/gocl.exe" "-target windows" "c23cstd" gocl

# printf/scanf conformance: integer precision, width accounting with sign and
# prefix, '*' widths, %g carry re-decision, %n in all widths, scansets, %hhd,
# %p, EOF vs matching failure, hex floats and the float token rule, plus
# pushback that survives into the next fscanf call. Self-reporting.
build_run_win_ok "printfmt [goc/win] "  "$ROOT/bin/goc.exe"  "-"               "printfmt" goc
build_run_win_ok "printfmt [gocl/win]" "$ROOT/bin/gocl.exe" "-target windows" "printfmt" gocl

# binary128 (long double) runtime: add/sub/mul/div with correct rounding,
# subnormals, overflow and underflow, the NaN/inf rules, the widen and narrow
# conversions and the integer conversions. The interesting part here is not
# the arithmetic -- tests/fp128/oracle.c checks that bit for bit against
# libgcc -- but that the same integer code compiled by each back end agrees:
# the battery folds ~29 thousand operations into one hash.
build_run_win_ok "fp128 [goc/win] "  "$ROOT/bin/goc.exe"  "-"               "fp128" goc
build_run_win_ok "fp128 [gocl/win]" "$ROOT/bin/gocl.exe" "-target windows" "fp128" gocl

echo ""
echo "== Windows: pass=$pass fail=$fail =="
[ "$fail" -eq 0 ]
