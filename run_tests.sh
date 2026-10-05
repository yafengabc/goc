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
# The six example legs, the goa suite and the three unit-test modules are
# independent of each other (disjoint output dirs and /tmp log names), so by
# default they run IN PARALLEL: each leg logs to /tmp/leg_<id>.log and the
# driver waits for all of them, then prints every leg's report and the summed
# pass/fail counts. Runs SEQUENTIAL by default now (parallel caused flaky
# failures under AV/IO contention); set GOC_PARALLEL=1 to re-enable it.
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
# ./cmd/goc, not "." -- src/goc is a library package (package compiler) since the
# split, and `go build -o bin/goc.exe .` would leave a Go archive there. The
# failure looks nothing like the cause: every example then fails as if the
# compiler itself were broken.
(cd src/goc && go build -trimpath -ldflags="-s -w" -o ../../bin/goc.exe ./cmd/goc) || { echo "BUILD FAILED"; exit 1; }

echo "== building goa =="
(cd src/goa && go build -trimpath -ldflags="-s -w" -o ../../bin/goa.exe ./cmd/goa) || { echo "GOA BUILD FAILED"; exit 1; }

# elfcheck verifies the ELF *structure* (headers, segments, entry). It no longer
# decides whether the program's output is right -- the Linux binaries below are
# executed by ucrun.exe, which wraps QEMU's CPU core (TCG). See find_ucrun().
echo "== building elfcheck =="
(cd tools && go build -o ../bin/elfcheck.exe ./elfcheck) || { echo "ELFCHECK BUILD FAILED"; exit 1; }

# Find ucrun.exe (standalone unicorn ELF emulator, tools/ucrun/ucrun.c).
# Unicorn exposes QEMU's TCG x86-64 core, so the Linux binaries below get real
# instruction semantics (flags, SSE2, addressing) instead of a hand-written
# guess at them -- which matters because an interpreter that agrees with our
# own codegen is only agreeing with itself.
find_ucrun() {
    if [ -n "${UCRUN:-}" ] && [ -f "$UCRUN" ]; then echo "$UCRUN"; return 0; fi
    for c in bin/ucrun.exe ../../bin/ucrun.exe ../bin/ucrun.exe; do
        if [ -f "$c" ]; then echo "$c"; return 0; fi
    done
    command -v ucrun.exe >/dev/null 2>&1 && { command -v ucrun.exe; return 0; }
    return 1
}
UCRUN=""
if UCRUN="$(find_ucrun)"; then
    echo "== linux runner: $UCRUN (unicorn/QEMU) =="
else
    echo "== WARNING: ucrun.exe not found; the Linux"
    echo "==          leg will be SKIPPED. Build it: gcc tools/ucrun/ucrun.c -I<d>/include -L<d>/lib -lunicorn -o bin/ucrun.exe =="
fi

# Fresh output dirs: goc -o <dir> writes every .exe/ELF there. The -o1/-os
# dirs hold the optimised legs' products; all are rebuilt from scratch.
rm -rf bin/goc-out bin/goc-out-o1 bin/goc-out-os
mkdir -p bin/goc-out

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
#
# Counting: each leg tracks its own counts and ends its log with a single
# "LEGSTATS pass=N fail=M" line the driver aggregates after wait.

run_win_leg() {  # <dirSuffix> <label> [extra goc flags...]
    local dir="$1" label="$2"; shift 2
    local out="bin/goc-out$dir"
    local leg_pass=0 leg_fail=0
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
                    leg_fail=$((leg_fail + 1))
                else
                    printf "ok    ${label}%-10s compile-only\n" "$name"
                    leg_pass=$((leg_pass + 1))
                fi
            else
                echo "SKIP  ${label}$name  (no src/expected/$name.txt)"
            fi
            continue
        fi

        if ! ./bin/goc.exe -c "$@" -o "$out" "$src" >/dev/null 2>"$log.err"; then
            echo "FAIL  ${label}$name  (compile): $(cat "$log.err")"
            leg_fail=$((leg_fail + 1))
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
        if [ "$rc" -eq 126 ] || [ "$rc" -eq 127 ]; then
            # A freshly written exe can be momentarily locked by the real-time
            # antivirus scan; exec then fails with 126/127. Retry once before
            # calling it a failure.
            sleep 1
            "$out/$name.exe" > "$log.raw" 2>&1
            rc=$?
        fi
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
            leg_fail=$((leg_fail + 1))
        else
            printf "ok    ${label}%-10s %6d bytes\n" "$name" "$(stat -c%s "$out/$name.exe")"
            leg_pass=$((leg_pass + 1))
        fi
    done
    echo "LEGSTATS pass=$leg_pass fail=$leg_fail"
}

run_linux_leg() {  # <dirSuffix> <label> [extra goc flags...]
    local dir="$1" label="$2"; shift 2
    local out="bin/goc-out$dir"
    local skipped=0
    local leg_pass=0 leg_fail=0
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

        if [ -z "$UCRUN" ]; then
            skipped=$((skipped + 1))
            continue
        fi

        if ! ./bin/goc.exe -c "$@" -target linux -o "$out" "$src" >/dev/null 2>"/tmp/gocl${dir}_$name.err"; then
            echo "FAIL  ${label}linux/$name  (compile): $(cat "/tmp/gocl${dir}_$name.err")"
            leg_fail=$((leg_fail + 1))
            continue
        fi

        # ELF structure still gets checked by elfcheck (headers/segments only); the
        # *output* comes from the QEMU run.
        ./bin/elfcheck.exe --structure-only "$out/$name" >/dev/null 2>&1

        "${UCRUN:?}" "$out/$name" 2>"/tmp/gocl${dir}_$name.err" | tr -d '\r' >"/tmp/gocl${dir}_$name.out"
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
            leg_fail=$((leg_fail + 1))
        else
            printf "ok    ${label}linux/%-10s %6d bytes\n" "$name" "$(stat -c%s "$out/$name")"
            leg_pass=$((leg_pass + 1))
        fi
    done
    [ "$skipped" -gt 0 ] && echo "SKIP  ${label}linux/*  ($skipped examples need a Python with unicorn)"
    echo "LEGSTATS pass=$leg_pass fail=$leg_fail"
    return 0
}

# run_unit <mod> <logfile>: unit tests for one Go module. -v once, counted
# from the same log (the old harness ran the suite twice for the count).
run_unit() {  # <mod> <logfile>
    local mod="$1" logf="$2"
    if (cd "$mod" && go test -count=1 -v ./... >"$logf" 2>&1); then
        printf "ok    unit: %-10s (%s tests)\n" "$mod" "$(grep -c '^--- PASS' "$logf")"
        echo "LEGSTATS pass=1 fail=0"
    else
        echo "FAIL  unit: $mod"
        tail -15 "$logf"
        echo "LEGSTATS pass=0 fail=1"
    fi
}

# run_goa: the goa assembler example suite (goa/run_tests.sh) covers the
# Windows examples natively, the GUI one through msgboxcheck, and the Linux
# ones under QEMU. Run it rather than growing a second copy here -- it already
# reports exit codes separately from stdout, which catches a crash that
# happens to print the right prefix.
run_goa() {
    if UCRUN="${UCRUN:-}" bash goa/run_tests.sh; then
        echo "ok    goa examples suite"
        echo "LEGSTATS pass=1 fail=0"
    else
        echo "FAIL  goa examples suite"
        echo "LEGSTATS pass=0 fail=1"
    fi
}

pass=0
fail=0
aggregate() {  # <logfile>...: sum every leg's LEGSTATS line
    local f p f2
    for f in "$@"; do
        p="$(sed -n 's/^LEGSTATS pass=\([0-9]*\) fail=[0-9]*$/\1/p' "$f" | awk '{s+=$1} END{print s+0}')"
        f2="$(sed -n 's/^LEGSTATS pass=[0-9]* fail=\([0-9]*\)$/\1/p' "$f" | awk '{s+=$1} END{print s+0}')"
        pass=$((pass + p))
        fail=$((fail + f2))
    done
}

if [ "${GOC_PARALLEL:-0}" = "1" ]; then
    echo "== running 6 example legs + goa suite + 3 unit modules in parallel =="
    run_win_leg  ""   ""           >/tmp/leg_win0.log   2>&1 & p0=$!
    run_win_leg  "-o1" "O1/"  -O1  >/tmp/leg_wino1.log  2>&1 & p1=$!
    run_win_leg  "-os" "Os/"  -Os  >/tmp/leg_winos.log  2>&1 & p2=$!
    run_linux_leg ""   ""           >/tmp/leg_lin0.log   2>&1 & p3=$!
    run_linux_leg "-o1" "O1/"  -O1 >/tmp/leg_lino1.log  2>&1 & p4=$!
    run_linux_leg "-os" "Os/"  -Os >/tmp/leg_linos.log  2>&1 & p5=$!
    run_goa                     >/tmp/leg_goa.log    2>&1 & p6=$!
    run_unit src        /tmp/unit_src.log    >/tmp/leg_unit_src.log  2>&1 & p7=$!
    run_unit goa    /tmp/unit_goa.log    >/tmp/leg_unit_goa.log  2>&1 & p8=$!
    run_unit tools      /tmp/unit_tools.log  >/tmp/leg_unit_tools.log 2>&1 & p9=$!

    rc=0
    for pid in $p0 $p1 $p2 $p3 $p4 $p5 $p6 $p7 $p8 $p9; do
        wait "$pid" || rc=1
    done

    echo "== windows target (-O0) ==";                    cat /tmp/leg_win0.log
    echo "-----------------------------"
    echo "== windows target -O1 (IR peephole on) ==";     cat /tmp/leg_wino1.log
    echo "== windows target -Os (size first) ==";         cat /tmp/leg_winos.log
    echo "== linux target -O0 (ELF64 under QEMU/Unicorn) =="; cat /tmp/leg_lin0.log
    echo "== linux target -O1 ==";                        cat /tmp/leg_lino1.log
    echo "== linux target -Os ==";                        cat /tmp/leg_linos.log
    echo "== goa examples (via goa/run_tests.sh) =="; cat /tmp/leg_goa.log
    echo "== unit tests (all three modules) ==";          cat /tmp/leg_unit_src.log /tmp/leg_unit_goa.log /tmp/leg_unit_tools.log

    aggregate /tmp/leg_win0.log /tmp/leg_wino1.log /tmp/leg_winos.log \
              /tmp/leg_lin0.log /tmp/leg_lino1.log /tmp/leg_linos.log \
              /tmp/leg_goa.log \
              /tmp/leg_unit_src.log /tmp/leg_unit_goa.log /tmp/leg_unit_tools.log
    [ "$rc" -ne 0 ] && fail=$((fail + 1))
else
    echo "== windows target (-O0) =="
    run_win_leg "" ""       | tee /tmp/leg_win0.log
    echo "== windows target -O1 (IR peephole on) =="
    run_win_leg "-o1" "O1/" -O1 | tee /tmp/leg_wino1.log
    echo "== linux target -O0 (ELF64, executed under QEMU/Unicorn) =="
    run_linux_leg "" ""     | tee /tmp/leg_lin0.log
    echo "== linux target -O1 =="
    run_linux_leg "-o1" "O1/" -O1 | tee /tmp/leg_lino1.log
    echo "== windows target -Os (size first: no inlining, cleanup passes on) =="
    run_win_leg "-os" "Os/" -Os | tee /tmp/leg_winos.log
    echo "== linux target -Os =="
    run_linux_leg "-os" "Os/" -Os | tee /tmp/leg_linos.log
    echo "== goa examples (via goa/run_tests.sh) =="
    run_goa | tee /tmp/leg_goa.log
    echo "== unit tests (all three modules) =="
    run_unit src     /tmp/unit_src.log   | tee /tmp/leg_unit_src.log
    run_unit goa /tmp/unit_goa.log   | tee /tmp/leg_unit_goa.log
    run_unit tools   /tmp/unit_tools.log | tee /tmp/leg_unit_tools.log
    aggregate /tmp/leg_win0.log /tmp/leg_wino1.log /tmp/leg_winos.log \
              /tmp/leg_lin0.log /tmp/leg_lino1.log /tmp/leg_linos.log \
              /tmp/leg_goa.log \
              /tmp/leg_unit_src.log /tmp/leg_unit_goa.log /tmp/leg_unit_tools.log
fi

echo "-----------------------------"
echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ] || exit 1
