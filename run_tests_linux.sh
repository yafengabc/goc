#!/usr/bin/env bash
# Native Linux end-to-end test for the c0 -> a0 pipeline.
#
# Same idea as run_tests.sh (which runs on Windows and interprets the Linux
# ELF outputs with elfcheck), but this script runs on a real Linux box and
# EXECUTES the ELF binaries directly on the real kernel. That is the only way
# to prove the SSE2 double codegen and the ELF structure are correct on actual
# hardware, not just inside the interpreter.
#
# Usage:  bash run_tests_linux.sh     (on Linux)
# Exit:   0 if every example matches its golden file, 1 otherwise.

set -u

cd "$(dirname "$0")"

# When run as a *file*, the sandbox trims TMP/TEMP/HOME, which breaks go build.
export HOME="${HOME:-/tmp}"
[ -n "${TMP:-}" ]    || export TMP=/tmp
[ -n "${TEMP:-}" ]   || export TEMP=/tmp
[ -n "${TMPDIR:-}" ] || export TMPDIR=/tmp
export GOTMPDIR="$TMP"
[ -n "${GOCACHE:-}" ] || export GOCACHE="$HOME/.cache/go-build"

echo "== building c0 =="
go build -trimpath -ldflags="-s -w" -o c0 . || { echo "BUILD FAILED"; exit 1; }

echo "== building a0 =="
(cd asm && go build -trimpath -ldflags="-s -w" -o a0 .) || { echo "A0 BUILD FAILED"; exit 1; }
cp asm/a0 ./a0   # findA0 looks next to the c0 binary first

pass=0
fail=0

echo "== c0: linux targets, run on the real kernel =="
for src in examples/*.c; do
    name="$(basename "$src" .c)"
    exp="expected/$name.txt"
    bin="examples/$name"

    if [ ! -f "$exp" ]; then
        echo "SKIP  $name  (no expected/$name.txt)"
        continue
    fi

    if ! ./c0 -c -target linux "$src" >/dev/null 2>"/tmp/c0l_$name.err"; then
        echo "FAIL  $name  (compile): $(cat /tmp/c0l_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    # No CR stripping here: on Linux the clib writes raw bytes and the golden
    # files are LF, so the comparison is byte-for-byte.
    ./"$bin" >"/tmp/c0l_$name.out" 2>"/tmp/c0l_$name.err"
    rc=$?

    if [ "$rc" -ne 0 ] || ! diff -u "$exp" "/tmp/c0l_$name.out" >"/tmp/c0l_$name.diff"; then
        echo "FAIL  $name  (exit=$rc)"
        sed -n '1,12p' "/tmp/c0l_$name.diff"
        echo "--- actual output (od -c) ---"
        od -c "/tmp/c0l_$name.out" | head -25
        echo "--- stderr ---"
        head -3 "/tmp/c0l_$name.err"
        fail=$((fail + 1))
    else
        printf "ok    %-10s %6d bytes\n" "$name" "$(stat -c%s "$bin")"
        pass=$((pass + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"

echo "== a0: linux examples, run on the real kernel =="
for asm in asm/examples/linux/*.asm; do
    name="$(basename "$asm" .asm)"
    exp="asm/expected/linux_$name.txt"
    bin="asm/examples/linux/$name"

    if [ ! -f "$exp" ]; then
        echo "SKIP  linux/$name  (no expected/linux_$name.txt)"
        continue
    fi

    if ! ./a0 -f elf "$asm" >/dev/null 2>"/tmp/a0l_$name.err"; then
        echo "FAIL  linux/$name  (assemble): $(cat /tmp/a0l_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    ./"$bin" >"/tmp/a0l_$name.out" 2>"/tmp/a0l_$name.err"
    rc=$?

    if [ "$rc" -ne 0 ] || ! diff -u "$exp" "/tmp/a0l_$name.out" >"/tmp/a0l_$name.diff"; then
        echo "FAIL  linux/$name  (exit=$rc)"
        sed -n '1,12p' "/tmp/a0l_$name.diff"
        echo "--- actual output (od -c) ---"
        od -c "/tmp/a0l_$name.out" | head -25
        echo "--- stderr ---"
        head -3 "/tmp/a0l_$name.err"
        fail=$((fail + 1))
    else
        echo "ok    linux/$name"
        pass=$((pass + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ] || exit 1
