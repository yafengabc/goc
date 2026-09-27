#!/usr/bin/env bash
# End-to-end test for the goc -> goa pipeline: compile every example with goc,
# run it, and byte-compare the output against expected/<name>.txt.
#
# Usage:  bash run_tests.sh
# Exit:   0 if every example matches, 1 otherwise.

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
go build -trimpath -ldflags="-s -w" -o goc.exe . || { echo "BUILD FAILED"; exit 1; }

echo "== building goa =="
(cd goa && go build -trimpath -ldflags="-s -w" -o goa.exe .) || { echo "GOA BUILD FAILED"; exit 1; }
cp goa/goa.exe ./goa.exe

# elfcheck verifies an ELF and then interprets it, since a Windows box cannot
# exec one. Same golden files: the Linux backend must print exactly what the
# Windows one does.
echo "== building elfcheck =="
(cd goa && go build -o tools/elfcheck.exe ./tools/elfcheck) || { echo "ELFCHECK BUILD FAILED"; exit 1; }

pass=0
fail=0

for src in examples/*.c; do
    name="$(basename "$src" .c)"
    exp="expected/$name.txt"

    if [ ! -f "$exp" ]; then
        echo "SKIP  $name  (no expected/$name.txt)"
        continue
    fi

    if ! ./goc.exe -c "$src" >/dev/null 2>"/tmp/goc_$name.err"; then
        echo "FAIL  $name  (compile): $(cat /tmp/goc_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    ./examples/"$name".exe 2>&1 | tr -d '\r' > "/tmp/goc_$name.out"

    if diff -u "$exp" "/tmp/goc_$name.out" > "/tmp/goc_$name.diff"; then
        printf "ok    %-10s %6d bytes\n" "$name" "$(stat -c%s examples/"$name".exe)"
        pass=$((pass + 1))
    else
        echo "FAIL  $name"
        sed -n '1,12p' "/tmp/goc_$name.diff"
        fail=$((fail + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"

# Linux target: same C source, ELF64 output, compared against the same golden
# files. The binaries cannot run here, so elfcheck interprets them.
echo "== linux target (ELF64) =="
for src in examples/*.c; do
    name="$(basename "$src" .c)"
    exp="expected/$name.txt"

    if [ ! -f "$exp" ]; then
        echo "SKIP  linux/$name  (no expected/$name.txt)"
        continue
    fi

    if ! ./goc.exe -c -target linux "$src" >/dev/null 2>"/tmp/gocl_$name.err"; then
        echo "FAIL  linux/$name  (compile): $(cat /tmp/gocl_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    ./goa/tools/elfcheck.exe "examples/$name" >"/tmp/gocl_$name.out" 2>"/tmp/gocl_$name.err"
    rc=$?

    if [ "$rc" -ne 0 ] || ! diff -u "$exp" "/tmp/gocl_$name.out" >"/tmp/gocl_$name.diff"; then
        echo "FAIL  linux/$name  (exit=$rc)"
        sed -n '1,12p' "/tmp/gocl_$name.diff"
        head -3 "/tmp/gocl_$name.err"
        fail=$((fail + 1))
    else
        printf "ok    linux/%-10s %6d bytes\n" "$name" "$(stat -c%s examples/"$name")"
        pass=$((pass + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ] || exit 1
