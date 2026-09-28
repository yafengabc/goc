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

# elfcheck verifies the ELF *structure* (headers, segments, entry). It no longer
# decides whether the program's output is right -- the Linux binaries below are
# executed by Unicorn, which is QEMU's CPU core (TCG). See find_python().
echo "== building elfcheck =="
(cd tools && go build -o ../bin/elfcheck.exe ./elfcheck) || { echo "ELFCHECK BUILD FAILED"; exit 1; }

# Find a Python that can import unicorn. Unicorn exposes QEMU's TCG x86-64 core
# as a library, so ucrun.py gives us real instruction semantics (flags, SSE2,
# addressing) instead of a hand-written guess at them -- which matters because
# an interpreter that agrees with our own codegen is only agreeing with itself.
find_python() {
    if [ -n "${GOC_PYTHON:-}" ] && [ -x "$GOC_PYTHON" ]; then echo "$GOC_PYTHON"; return 0; fi
    for c in python3 python py; do
        if command -v "$c" >/dev/null 2>&1 && "$c" -c "import unicorn" >/dev/null 2>&1; then
            command -v "$c"; return 0
        fi
    done
    for p in /d/msys/ucrt64/bin/python3.exe /c/msys64/ucrt64/bin/python3.exe \
             /c/msys64/mingw64/bin/python3.exe; do
        if [ -x "$p" ]; then echo "$p"; return 0; fi
    done
    return 1
}
UCPY=""
if UCPY="$(find_python)"; then
    echo "== linux runner: $UCPY -c 'import unicorn' ok =="
else
    echo "== WARNING: no Python with the unicorn/QEMU bindings found; the Linux"
    echo "==          leg will be SKIPPED. Set GOC_PYTHON=/path/to/python to enable it. =="
fi

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
# files. The ELF cannot be exec'd on Windows, so ucrun.py loads it into a
# Unicorn (QEMU TCG) VM and runs it there: real x86-64 semantics, plus Linux
# write/brk/exit_group syscall emulation.
echo "== linux target (ELF64, executed under QEMU/Unicorn) =="
linux_skipped=0
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

    if [ -z "$UCPY" ]; then
        linux_skipped=$((linux_skipped + 1))
        continue
    fi

    if ! ./bin/goc.exe -c -target linux -o bin/goc-out "$src" >/dev/null 2>"/tmp/gocl_$name.err"; then
        echo "FAIL  linux/$name  (compile): $(cat /tmp/gocl_$name.err)"
        fail=$((fail + 1))
        continue
    fi

    # ELF structure still gets checked by elfcheck (headers/segments only); the
    # *output* comes from the QEMU run.
    ./bin/elfcheck.exe --structure-only "bin/goc-out/$name" >/dev/null 2>&1

    "$UCPY" tools/ucrun.py "bin/goc-out/$name" 2>"/tmp/gocl_$name.err" | tr -d '\r' >"/tmp/gocl_$name.out"
    rc=${PIPESTATUS[0]}

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

[ "$linux_skipped" -gt 0 ] && echo "SKIP  linux/*  ($linux_skipped examples need a Python with unicorn)"

# goa's own assembler examples have their own suite (src/goa/run_tests.sh),
# covering the Windows examples natively, the GUI one through msgboxcheck, and
# the Linux ones under QEMU. Run it rather than growing a second copy here --
# it already reports exit codes separately from stdout, which catches a crash
# that happens to print the right prefix.
echo "== goa examples (via src/goa/run_tests.sh) =="
if GOC_PYTHON="${UCPY:-}" bash src/goa/run_tests.sh; then
    pass=$((pass + 1))
else
    echo "FAIL  goa examples suite"
    fail=$((fail + 1))
fi

# Unit tests. Each of src/, src/goa/ and tools/ is its own Go module, so `go
# test ./...` run from src alone silently skips the assembler's unit tests --
# iterate all three explicitly.
echo "== unit tests (all three modules) =="
for mod in src src/goa tools; do
    if (cd "$mod" && go test -count=1 ./... >"/tmp/unit.log" 2>&1); then
        printf "ok    unit: %-10s (%s tests)\n" "$mod" "$(cd "$mod" && go test -count=1 ./... -v 2>/dev/null | grep -c '^--- PASS')"
        pass=$((pass + 1))
    else
        echo "FAIL  unit: $mod"
        tail -15 "/tmp/unit.log"
        fail=$((fail + 1))
    fi
done

echo "-----------------------------"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ] || exit 1
