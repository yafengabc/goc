#!/usr/bin/env bash
# End-to-end test for the goc -> goa pipeline: compile every example with goc,
# run it, and byte-compare the output against src/expected/<name>.txt -- at
# -O0 and, with the same goldens, at -O1 and -Os (the optimisation track's
# behavioural guardrail: a pass that changes observable output fails here,
# not in prod). -Os is the size-first level: no inlining, every cleanup pass.
#
# Outputs go to bin/goc-out/ (-O0), bin/goc-out-o1/ (-O1) and bin/goc-out-os/
# (-Os), never next to the sources, so src/examples/ stays pristine -- only
# the .c files live there.
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

# Fresh output dirs: goc -o <dir> writes every .exe/ELF there. The -o1/-os
# dirs hold the optimised legs' products; all are rebuilt from scratch.
rm -rf bin/goc-out bin/goc-out-o1 bin/goc-out-os
mkdir -p bin/goc-out

pass=0
fail=0

# Windows-only examples import Win32 DLLs (kernel32/user32/gdi32), so the
# Linux (ELF64) leg must skip them: they cannot compile without those DLLs.
win_only=" wintest winbox winreg "
is_win_only() { case "$win_only" in *" $1 "*) return 0;; esac; return 1; }

# Each leg compiles every example with the same extra goc flags, runs it, and
# byte-compares stdout against src/expected/<name>.txt. The -O1 legs exist
# from day one of the optimisation track: every pass that lands from now on
# must keep these goldens true, and the harness for that should predate the
# passes rather than be bolted on after the first behaviour break.
#
# dirSuffix separates output directories and tmp files so legs cannot clobber
# each other's products; label prefixes the report ("O1/").

run_win_leg() {  # <dirSuffix> <label> [extra goc flags...]
    local dir="$1" label="$2"; shift 2
    local out="bin/goc-out$dir"
    mkdir -p "$out"
    for src in src/examples/*.c; do
        name="$(basename "$src" .c)"
        exp="src/expected/$name.txt"
        log="/tmp/goc${dir}_$name"

        if [ ! -f "$exp" ]; then
            if is_win_only "$name"; then
                # GUI demo with no golden: prove the user32 imports compile+link.
                if ! ./bin/goc.exe -c "$@" -o "$out" "$src" >/dev/null 2>"$log.err"; then
                    echo "FAIL  ${label}$name  (compile): $(cat "$log.err")"
                    fail=$((fail + 1))
                else
                    printf "ok    ${label}%-10s compile-only\n" "$name"
                    pass=$((pass + 1))
                fi
            else
                echo "SKIP  ${label}$name  (no src/expected/$name.txt)"
            fi
            continue
        fi

        if ! ./bin/goc.exe -c "$@" -o "$out" "$src" >/dev/null 2>"$log.err"; then
            echo "FAIL  ${label}$name  (compile): $(cat "$log.err")"
            fail=$((fail + 1))
            continue
        fi

        # Capture the exit code separately from stdout. Comparing output alone
        # hides a program that prints the right thing and then dies -- verified:
        # a program whose printf matches the golden and which then faults on a
        # null store exits 139 and was being reported as ok. goa's own suite hit
        # this once already ("4" then crash) and was fixed there; this leg was
        # still doing it wrong.
        "$out/$name.exe" > "$log.raw" 2>&1
        rc=$?
        # Strip CR: the Windows console layer emits CRLF.
        tr -d '\r' < "$log.raw" > "$log.out"

        # Always diff, so the diagnostic file exists even when the run crashed.
        diff -u "$exp" "$log.out" > "$log.diff"
        diffrc=$?
        if [ "$rc" -ne 0 ] || [ "$diffrc" -ne 0 ]; then
            echo "FAIL  ${label}$name (exit=$rc)"
            sed -n '1,12p' "$log.diff"
            echo "  raw bytes:"
            od -c "$log.raw" | head -8
            fail=$((fail + 1))
        else
            printf "ok    ${label}%-10s %6d bytes\n" "$name" "$(stat -c%s "$out/$name.exe")"
            pass=$((pass + 1))
        fi
    done
}

run_linux_leg() {  # <dirSuffix> <label> [extra goc flags...]
    local dir="$1" label="$2"; shift 2
    local out="bin/goc-out$dir"
    local skipped=0
    mkdir -p "$out"
    for src in src/examples/*.c; do
        name="$(basename "$src" .c)"
        exp="src/expected/$name.txt"

        if [ ! -f "$exp" ]; then
            echo "SKIP  ${label}linux/$name  (no src/expected/$name.txt)"
            continue
        fi

        if is_win_only "$name"; then
            echo "SKIP  ${label}linux/$name  (imports Windows DLLs)"
            continue
        fi

        if [ -z "$UCPY" ]; then
            skipped=$((skipped + 1))
            continue
        fi

        if ! ./bin/goc.exe -c "$@" -target linux -o "$out" "$src" >/dev/null 2>"/tmp/gocl${dir}_$name.err"; then
            echo "FAIL  ${label}linux/$name  (compile): $(cat "/tmp/gocl${dir}_$name.err")"
            fail=$((fail + 1))
            continue
        fi

        # ELF structure still gets checked by elfcheck (headers/segments only); the
        # *output* comes from the QEMU run.
        ./bin/elfcheck.exe --structure-only "$out/$name" >/dev/null 2>&1

        "$UCPY" tools/ucrun.py "$out/$name" 2>"/tmp/gocl${dir}_$name.err" | tr -d '\r' >"/tmp/gocl${dir}_$name.out"
        rc=${PIPESTATUS[0]}

        # Diff unconditionally: `rc != 0 || !diff` short-circuits before diff
        # runs, so a crash -- the case you most want the output for -- left no
        # file for the sed below and printed "can't read .../diff" instead.
        diff -u "$exp" "/tmp/gocl${dir}_$name.out" >"/tmp/gocl${dir}_$name.diff"
        diffrc=$?
        if [ "$rc" -ne 0 ] || [ "$diffrc" -ne 0 ]; then
            echo "FAIL  ${label}linux/$name  (exit=$rc)"
            sed -n '1,12p' "/tmp/gocl${dir}_$name.diff"
            head -3 "/tmp/gocl${dir}_$name.err"
            fail=$((fail + 1))
        else
            printf "ok    ${label}linux/%-10s %6d bytes\n" "$name" "$(stat -c%s "$out/$name")"
            pass=$((pass + 1))
        fi
    done
    [ "$skipped" -gt 0 ] && echo "SKIP  ${label}linux/*  ($skipped examples need a Python with unicorn)"
    return 0
}

echo "== windows target (-O0) =="
run_win_leg "" ""

echo "-----------------------------"
echo "pass=$pass fail=$fail"

echo "== windows target -O1 (IR peephole on) =="
run_win_leg "-o1" "O1/" -O1

echo "== linux target -O0 (ELF64, executed under QEMU/Unicorn) =="
run_linux_leg "" ""

echo "== linux target -O1 =="
run_linux_leg "-o1" "O1/" -O1

echo "== windows target -Os (size first: no inlining, cleanup passes on) =="
run_win_leg "-os" "Os/" -Os

echo "== linux target -Os =="
run_linux_leg "-os" "Os/" -Os

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
