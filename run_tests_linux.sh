#!/usr/bin/env bash
# Native Linux end-to-end test for the goc -> goa pipeline.
#
# Same idea as run_tests.sh (which runs on Windows and executes the Linux ELF
# outputs under Unicorn, QEMU's CPU core), but this script runs on a real Linux
# box and EXECUTES the ELF binaries directly on the real kernel. That is the
# only check that proves the SSE2 double codegen, the SysV prologues and the
# ELF structure against the actual loader rather than a model of it.
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
(cd cmd/goa && go build -trimpath -ldflags="-s -w" -o ../../bin/goa ./cmd/goa) || { echo "GOA BUILD FAILED"; exit 1; }

# Fresh output dir: goc -o bin/goc-out writes every .asm/ELF here, keeping
# src/examples/ pristine (only the .c files live there).
# The mkdir matters: goc's -o follows gcc, so a path that does not exist yet
# is taken as an output *file* name, not a directory. Without it the first
# example writes a file called bin/goc-out and every later one dies with
# "Not a directory" (exit 126) before ever reaching the kernel.
rm -rf bin/goc-out
mkdir -p bin/goc-out

pass=0
fail=0

# Windows-only examples import Win32 DLLs; on a Linux box they have nothing to
# link against, so skip them here. (On Windows, run_tests.sh still compiles the
# no-golden ones, e.g. winbox, as a compile-only check.)
win_only=" wintest winbox winreg "
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

    # Diff unconditionally: `rc != 0 || !diff` short-circuits before diff
    # runs, so the crash case -- the one you most want the output for --
    # left no file behind and printed "can't read .../diff" instead.
    diff -u "$exp" "/tmp/gocl_$name.out" >"/tmp/gocl_$name.diff"
    diffrc=$?
    if [ "$rc" -ne 0 ] || [ "$diffrc" -ne 0 ]; then
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
for asm in goa/examples/linux/*.asm; do
    name="$(basename "$asm" .asm)"
    exp="goa/expected/linux_$name.txt"
    bin="goa/examples/linux/$name"

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

    diff -u "$exp" "/tmp/goal_$name.out" >"/tmp/goal_$name.diff"
    diffrc=$?
    if [ "$rc" -ne 0 ] || [ "$diffrc" -ne 0 ]; then
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
