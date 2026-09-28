#!/usr/bin/env bash
# End-to-end test for the goc -> goa pipeline: compile every example with goc,
# run it, and byte-compare the output against src/expected/<name>.txt.
#
# Outputs go to bin/goc-out/ (via `goc -o`), never next to the sources, so
# src/examples/ stays pristine -- only the .c files live there.
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
(cd src && go build -trimpath -ldflags="-s -w" -o ../bin/goc.exe .) || { echo "BUILD FAILED"; exit 1; }

echo "== building goa =="
(cd src/goa && go build -trimpath -ldflags="-s -w" -o ../../bin/goa.exe .) || { echo "GOA BUILD FAILED"; exit 1; }

# elfcheck verifies an ELF and then interprets it, since a Windows box cannot
# exec one. Same golden files: the Linux backend must print exactly what the
# Windows one does.
echo "== building elfcheck =="
(cd tools && go build -o ../bin/elfcheck.exe ./elfcheck) || { echo "ELFCHECK BUILD FAILED"; exit 1; }

# Fresh output dir: goc -o bin/goc-out writes every .asm/.exe/ELF here.
rm -rf bin/goc-out
mkdir -p bin/goc-out

pass=0
fail=0

# Windows-only examples import Win32 DLLs (kernel32/user32/gdi32), so the
# Linux (ELF64) leg must skip them: they cannot compile without those DLLs.
win_only=" wintest winbox winreg "
is_win_only() { case "$win_only" in *" $1 "*) return 0;; esac; return 1; }

for src in src/examples/*.c; do
    name="$(basename "$src" .c)"
    exp="src/expected/$name.txt"

    if [ ! -f "$exp" ]; then
        if is_win_only "$name"; then
            # GUI demo with no golden: prove the user32 imports compile+link.
            if ! ./bin/goc.exe -c -o bin/goc-out "$src" >/dev/null 2>"/tmp/goc_$name.err"; then
                echo "FAIL  $name  (compile): $(cat /tmp/goc_$name.err)"
                fail=$((fail + 1))
            else
                printf "ok    %-10s compile-only\n" "$name"
                pass=$((pass + 1))
            fi
        else
            echo "SKIP  $name  (no src/expected/$name.txt)"
        fi
        continue
    fi

    if ! ./bin/goc.exe -c -o bin/goc-out "$src" >/dev/null 2>"/tmp/goc_$name.err"; then
        echo "FAIL  $name  (compile): $(cat /tmp/goc_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    ./bin/goc-out/"$name".exe 2>&1 | tr -d '\r' > "/tmp/goc_$name.out"

    if diff -u "$exp" "/tmp/goc_$name.out" > "/tmp/goc_$name.diff"; then
        printf "ok    %-10s %6d bytes\n" "$name" "$(stat -c%s bin/goc-out/"$name".exe)"
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
for src in src/examples/*.c; do
    name="$(basename "$src" .c)"
    exp="src/expected/$name.txt"

    if [ ! -f "$exp" ]; then
        echo "SKIP  linux/$name  (no src/expected/$name.txt)"
        continue
    fi

    if is_win_only "$name"; then
        echo "SKIP  linux/$name  (imports Windows DLLs)"
        continue
    fi

    if ! ./bin/goc.exe -c -target linux -o bin/goc-out "$src" >/dev/null 2>"/tmp/gocl_$name.err"; then
        echo "FAIL  linux/$name  (compile): $(cat /tmp/gocl_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    ./bin/elfcheck.exe "bin/goc-out/$name" >"/tmp/gocl_$name.out" 2>"/tmp/gocl_$name.err"
    rc=$?

    if [ "$rc" -ne 0 ] || ! diff -u "$exp" "/tmp/gocl_$name.out" >"/tmp/gocl_$name.diff"; then
        echo "FAIL  linux/$name  (exit=$rc)"
        sed -n '1,12p' "/tmp/gocl_$name.diff"
        head -3 "/tmp/gocl_$name.err"
        fail=$((fail + 1))
    else
        printf "ok    linux/%-10s %6d bytes\n" "$name" "$(stat -c%s bin/goc-out/"$name")"
        pass=$((pass + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ] || exit 1
