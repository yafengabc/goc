#!/usr/bin/env bash
# Native Linux end-to-end test for the goc -> goa pipeline.
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

echo "== building goc =="
mkdir -p bin
(cd src && go build -trimpath -ldflags="-s -w" -o ../bin/goc .) || { echo "BUILD FAILED"; exit 1; }

echo "== building goa =="
(cd src/goa && go build -trimpath -ldflags="-s -w" -o ../../bin/goa .) || { echo "GOA BUILD FAILED"; exit 1; }

# Fresh output dir: goc -o bin/goc-out writes every .asm/ELF here, keeping
# src/examples/ pristine (only the .c files live there).
rm -rf bin/goc-out

pass=0
fail=0

# Windows-only examples import Win32 DLLs; on a Linux box they have nothing to
# link against, so skip them here. (On Windows, run_tests.sh still compiles the
# no-golden ones, e.g. winbox, as a compile-only check.)
win_only=" wintest winbox "
is_win_only() { case "$win_only" in *" $1 "*) return 0;; esac; return 1; }

echo "== goc: linux targets, run on the real kernel =="
for src in src/examples/*.c; do
    name="$(basename "$src" .c)"
    exp="src/expected/$name.txt"
    bin="bin/goc-out/$name"

    if [ ! -f "$exp" ]; then
        echo "SKIP  $name  (no src/expected/$name.txt)"
        continue
    fi

    if is_win_only "$name"; then
        echo "SKIP  $name  (imports Windows DLLs)"
        continue
    fi

    if ! ./bin/goc -c -target linux -o bin/goc-out "$src" >/dev/null 2>"/tmp/gocl_$name.err"; then
        echo "FAIL  $name  (compile): $(cat /tmp/gocl_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    # No CR stripping here: on Linux the goclib writes raw bytes and the golden
    # files are LF, so the comparison is byte-for-byte.
    ./"$bin" >"/tmp/gocl_$name.out" 2>"/tmp/gocl_$name.err"
    rc=$?

    if [ "$rc" -ne 0 ] || ! diff -u "$exp" "/tmp/gocl_$name.out" >"/tmp/gocl_$name.diff"; then
        echo "FAIL  $name  (exit=$rc)"
        sed -n '1,12p' "/tmp/gocl_$name.diff"
        echo "--- actual output (od -c) ---"
        od -c "/tmp/gocl_$name.out" | head -25
        echo "--- stderr ---"
        head -3 "/tmp/gocl_$name.err"
        fail=$((fail + 1))
    else
        printf "ok    %-10s %6d bytes\n" "$name" "$(stat -c%s "$bin")"
        pass=$((pass + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"

echo "== goa: linux examples, run on the real kernel =="
for asm in src/goa/examples/linux/*.asm; do
    name="$(basename "$asm" .asm)"
    exp="src/goa/expected/linux_$name.txt"
    bin="src/goa/examples/linux/$name"

    if [ ! -f "$exp" ]; then
        echo "SKIP  linux/$name  (no expected/linux_$name.txt)"
        continue
    fi

    if ! ./bin/goa -f elf "$asm" >/dev/null 2>"/tmp/goal_$name.err"; then
        echo "FAIL  linux/$name  (assemble): $(cat /tmp/goal_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    ./"$bin" >"/tmp/goal_$name.out" 2>"/tmp/goal_$name.err"
    rc=$?

    if [ "$rc" -ne 0 ] || ! diff -u "$exp" "/tmp/goal_$name.out" >"/tmp/goal_$name.diff"; then
        echo "FAIL  linux/$name  (exit=$rc)"
        sed -n '1,12p' "/tmp/goal_$name.diff"
        echo "--- actual output (od -c) ---"
        od -c "/tmp/goal_$name.out" | head -25
        echo "--- stderr ---"
        head -3 "/tmp/goal_$name.err"
        fail=$((fail + 1))
    else
        echo "ok    linux/$name"
        pass=$((pass + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ] || exit 1
