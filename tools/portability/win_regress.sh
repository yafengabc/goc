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

echo "########## Windows PE ##########"
WANT_WIN1=$(printf 't=707\ns=42 abc ff')

# The stdio-only cases, run on both back ends.
for pair in "win1|$WANT_WIN1" "p2|n=42" "p5|$(printf 'raw\nr=4')" "p7|$(printf 'one\ntwo')"; do
    n=${pair%%|*}
    w=${pair#*|}
    build_run_win "$n  [goc/win] "  "$ROOT/bin/goc.exe"  "-"             "$n" "$w" goc
    build_run_win "$n  [gocl/win]" "$ROOT/bin/gocl.exe" "-target windows" "$n" "$w" gocl
done

echo ""
echo "== Windows: pass=$pass fail=$fail =="
[ "$fail" -eq 0 ]
