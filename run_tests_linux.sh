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
# From src/ with -tags goc: that is where the entry point and the embedded C
# library live now. The old src/goc/cmd/goc is gone.
(cd src && go build -trimpath -ldflags="-s -w" -o ../bin/goc -tags goc -buildvcs=false .) || { echo "BUILD FAILED"; exit 1; }

echo "== building goa =="
(cd src/goa && go build -trimpath -ldflags="-s -w" -o ../../bin/goa -buildvcs=false ./cmd/goa) || { echo "GOA BUILD FAILED"; exit 1; }

# gocl is the LLVM back end, loaded at run time against libLLVM through purego
# (see src/gocl/llvm_linux.go). It is built best-effort: because the build pulls
# an external module (github.com/ebitengine/purego), we build it offline
# (GOPROXY=off) so a box whose module cache has no purego degrades to a SKIP
# instead of hanging on an unreachable module proxy. If it still cannot build
# (no purego cached and offline), GOCL stays empty and the gocl leg SKIPs --
# exactly the way the Windows leg SKIPs without libLLVM.
echo "== building gocl (LLVM back end; leg SKIPs without it) =="
GOCL=""
if (cd src && GOPROXY=off go build -trimpath -ldflags="-s -w" -o ../bin/gocl -tags gocl -buildvcs=false .) 2>/tmp/gocl_build.err; then
    GOCL="./bin/gocl"
    echo "== gocl: $GOCL =="
else
    echo "== gocl not built: $(head -1 /tmp/gocl_build.err) (gocl leg will SKIP) =="
fi

# gocl (the LLVM back end) is far less mature than the native back end. The two
# lists below keep the gocl leg green while still catching NEW regressions: an
# example listed here is allowed to fail; one that is NOT listed and fails is a
# real regression and fails the leg.
#
#   gocl_unsupported: clean "the LLVM front end does not model this yet"
#     rejections (inline asm, bit-fields, _BitInt, C11 threads) -- SKIP.
#   gocl_xfail: gocl compiles it but miscompiles (wrong output, a crash, a
#     hang) or emits IR libLLVM rejects -- XFAIL, a tracked bug.
# Seeded 2026-10-20 by running every example under gocl -target linux on the
# real kernel (WSL Arch): 60 clean, 5 unsupported, 16 miscompile/hang. Trim as
# gocl improves; an example that starts matching is reported ok, so the list can
# only shrink.
#
# Trimmed 2026-10-20: bitor, enums, test_c89, brace and fnptrs now match, so
# they left the list. test_c89 and brace were both crashes rooted in the same
# defect -- varInfo.op was never assigned, so with a function-local static
# taking uid 0 every local use minted its own alloca -- plus two follow-ons
# (extern locals, and array decay through an Index rvalue). fnptrs needed
# unary() to stop treating "&func" as the address of an lvalue and "*fp" on a
# function pointer as a load.
gocl_unsupported=" asm1 asm2 bitint test_bitfield c11_threads_basic "
# gocl_xfail is now EMPTY: every example matches its golden on the real kernel.
# The last two (mathlib, mathlib2) were rooted in LLVM's SimplifyLibCalls
# recognising goclib's libm-named functions (pow/sqrt/sin/... and the special
# functions erf/erfc/tgamma/lgamma/ilogb/logb/nan) as the C library and folding
# them to hardware-accurate values -- wrong ULP shifts, pow(-2,0.5) returning a
# finite instead of NaN, and erf(-1) looping forever through the misrecognised
# libcall. Renaming those symbols to a goclib_ prefix (see irFuncSym /
# goclibLibmRenames in src/gocl/module.go) keeps LLVM's vocabulary away from them
# and restores goclib's software output, which is what the goldens record.
gocl_xfail=" "
gocl_is_unsupported() { case "$gocl_unsupported" in *" $1 "*) return 0;; esac; return 1; }
gocl_is_xfail() { case "$gocl_xfail" in *" $1 "*) return 0;; esac; return 1; }

# run_gocl_leg compiles every example with gocl (the LLVM back end) into an ELF
# and runs it on the real kernel, byte-comparing stdout against the same goldens
# the native leg uses. The point is coverage the native leg structurally cannot
# give: a miscompile confined to the LLVM back end -- a register clobbered
# across a stack probe, a bad alloca lowering, a mis-typed value -- shows up
# here as a diff where the native back end is blind to it. gocl links in a
# single step (unlike goc there is no separate -c object stage), so it compiles
# straight to the ELF via -target linux. Known gocl gaps are handled by the
# gocl_unsupported / gocl_xfail lists above. Each run is bounded by a timeout so
# a gocl hang (verified: mathlib2 loops forever) cannot stall the whole suite.
run_gocl_leg() {
    if [ -z "$GOCL" ]; then
        echo "SKIP  gocl/*  (gocl not built)"
        return 0
    fi
    local out="bin/gocl-out"
    local leg_pass=0 leg_fail=0 leg_xfail=0
    mkdir -p "$out"
    for src in src/examples/*.c; do
        name="$(basename "$src" .c)"
        exp="src/expected/$name.txt"
        bin="$out/$name"

        if [ ! -f "$exp" ]; then
            echo "SKIP  gocl/$name  (no src/expected/$name.txt)"
            continue
        fi
        if is_win_only "$name"; then
            echo "SKIP  gocl/$name  (win-only)"
            continue
        fi
        if gocl_is_unsupported "$name"; then
            echo "SKIP  gocl/$name  (gocl: construct not modelled)"
            continue
        fi

        if ! "$GOCL" -target linux -o "$bin" "$src" >/dev/null 2>"/tmp/gocl_$name.err"; then
            if gocl_is_xfail "$name"; then
                echo "XFAIL gocl/$name  (compile)"
                leg_xfail=$((leg_xfail + 1))
            else
                echo "FAIL  gocl/$name  (compile): $(head -1 /tmp/gocl_$name.err)"
                leg_fail=$((leg_fail + 1))
            fi
            continue
        fi

        # Run on the real kernel, bounded so a hang cannot stall the suite.
        timeout 30 "$bin" >"/tmp/gocl_$name.out" 2>"/tmp/gocl_$name.err"
        rc=$?
        if [ "$rc" -eq 124 ]; then
            if gocl_is_xfail "$name"; then
                echo "XFAIL gocl/$name  (hang)"
                leg_xfail=$((leg_xfail + 1))
            else
                echo "FAIL  gocl/$name  (hang)"
                leg_fail=$((leg_fail + 1))
            fi
            continue
        fi
        if [ "$rc" -ne 0 ]; then
            if gocl_is_xfail "$name"; then
                echo "XFAIL gocl/$name  (exit=$rc)"
                leg_xfail=$((leg_xfail + 1))
            else
                echo "FAIL  gocl/$name  (exit=$rc)"
                sed -n '1,8p' "/tmp/gocl_$name.err"
                leg_fail=$((leg_fail + 1))
            fi
            continue
        fi

        # No CR stripping: on Linux goclib writes raw LF and the goldens are LF,
        # so the comparison is byte-for-byte.
        diff -u "$exp" "/tmp/gocl_$name.out" >"/tmp/gocl_$name.diff"
        diffrc=$?
        if [ "$diffrc" -ne 0 ]; then
            if gocl_is_xfail "$name"; then
                echo "XFAIL gocl/$name  (output differs)"
                leg_xfail=$((leg_xfail + 1))
            else
                echo "FAIL  gocl/$name  (output differs)"
                sed -n '1,8p' "/tmp/gocl_$name.diff"
                leg_fail=$((leg_fail + 1))
            fi
        else
            printf "ok    gocl/%-10s %6d bytes\n" "$name" "$(stat -c%s "$bin")"
            leg_pass=$((leg_pass + 1))
        fi
    done
    echo "-----------------------------"
    echo "gocl: pass=$leg_pass xfail=$leg_xfail fail=$leg_fail"
    pass=$((pass + leg_pass))
    gocl_xfail_total=$((gocl_xfail_total + leg_xfail))
    fail=$((fail + leg_fail))
}

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
gocl_xfail_total=0

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

    if ! ./bin/goc -target linux -o "bin/goc-out/$name" "$src" >/dev/null 2>"/tmp/gocl_$name.err"; then
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
echo "== gocl: linux target, LLVM back end, run on the real kernel =="
run_gocl_leg
echo "-----------------------------"
echo "pass=$pass fail=$fail xfail=$gocl_xfail_total"
[ "$fail" -eq 0 ] || exit 1
