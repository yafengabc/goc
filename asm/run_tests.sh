#!/usr/bin/env bash
# Regression suite for a0: assemble every example, run it, and byte-compare
# the output against expected/<name>.txt.
#
# Usage:  bash run_tests.sh
# Exit:   0 if every example matches, 1 otherwise.

set -u

cd "$(dirname "$0")"

# When this script is run as a *file*, the sandbox trims TMP/TEMP/HOME, which
# breaks go build (no work dir, no build cache). Set them explicitly.
export HOME="${HOME:-/tmp}"
[ -n "${TMP:-}" ]   || export TMP=/tmp
[ -n "${TEMP:-}" ]  || export TEMP=/tmp
[ -n "${TMPDIR:-}" ] || export TMPDIR=/tmp
export GOTMPDIR="$TMP"
[ -n "${GOCACHE:-}" ] || export GOCACHE="$HOME/.cache/go-build"

echo "== building a0 =="
go build -trimpath -ldflags="-s -w" -o a0.exe . || { echo "BUILD FAILED"; exit 1; }
go build -o tools/msgboxcheck.exe ./tools/msgboxcheck || { echo "TOOL BUILD FAILED"; exit 1; }
# elfcheck verifies the ELF structure and then interprets the program, since
# a Windows box cannot actually exec an ELF binary.
go build -o tools/elfcheck.exe ./tools/elfcheck || { echo "TOOL BUILD FAILED"; exit 1; }

pass=0
fail=0

for asm in examples/*.asm; do
    name="$(basename "$asm" .asm)"
    exp="expected/$name.txt"

    if ! ./a0.exe "$asm" >/dev/null 2>"/tmp/a0_$name.err"; then
        echo "FAIL  $name  (assemble): $(cat /tmp/a0_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    if [ ! -f "$exp" ]; then
        echo "SKIP  $name  (no expected/$name.txt)"
        continue
    fi

    # Run and capture the exit code separately: comparing stdout alone would
    # hide a crash that emits the correct prefix (e.g. fmath CI: "4" then die).
    ./examples/"$name".exe > "/tmp/a0_$name.raw" 2> "/tmp/a0_$name.err"
    rc=$?
    # Strip CR: the console layer may emit CRLF on Windows.
    tr -d '\r' < "/tmp/a0_$name.raw" > "/tmp/a0_$name.out"

    if [ "$rc" -ne 0 ] || ! diff -u "$exp" "/tmp/a0_$name.out" > "/tmp/a0_$name.diff"; then
        echo "FAIL  $name (exit=$rc)"
        sed -n '1,12p' "/tmp/a0_$name.diff"
        echo "  raw bytes:"
        od -c "/tmp/a0_$name.raw" | head -8
        if [ -s "/tmp/a0_$name.err" ]; then
            echo "  stderr:"
            cat "/tmp/a0_$name.err"
        fi
        fail=$((fail + 1))
    else
        echo "ok    $name"
        pass=$((pass + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"

# Linux examples: assembled with -f elf, then run under elfcheck.
echo "== linux (ELF) =="
for asm in examples/linux/*.asm; do
    name="$(basename "$asm" .asm)"
    exp="expected/linux_$name.txt"

    if ! ./a0.exe -f elf "$asm" >/dev/null 2>"/tmp/a0_$name.err"; then
        echo "FAIL  linux/$name  (assemble): $(cat /tmp/a0_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    ./tools/elfcheck.exe "examples/linux/$name" >"/tmp/a0_$name.out" 2>"/tmp/a0_$name.err"
    rc=$?
    if [ ! -f "$exp" ]; then
        echo "SKIP  linux/$name  (no expected/linux_$name.txt)"
        continue
    fi
    if [ "$rc" -ne 0 ] || ! diff -u "$exp" "/tmp/a0_$name.out" >"/tmp/a0_$name.diff"; then
        echo "FAIL  linux/$name  (exit=$rc)"
        sed -n '1,12p' "/tmp/a0_$name.diff"
        cat "/tmp/a0_$name.err"
        fail=$((fail + 1))
    else
        echo "ok    linux/$name"
        pass=$((pass + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"

# GUI example: cannot be compared against stdout, so drive it through the
# real UI instead. Needs an interactive desktop (fails on a locked screen).
echo "== gui: examples/msgbox.asm =="
./a0.exe examples/msgbox.asm >/dev/null || { echo "FAIL  msgbox (assemble)"; fail=$((fail + 1)); }
./tools/msgboxcheck.exe examples/msgbox.exe || fail=$((fail + 1))

echo "-----------------------------"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ] || exit 1
