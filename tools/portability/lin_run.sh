#!/bin/sh
# Run phase of the Linux ELF regression. Executes inside the WSL alpine
# container against the binaries lin_build.sh produced on the Windows side.
#
# Kept as a script file rather than an inline `wsl -d alpine -- sh -c` loop: the
# shell Git Bash hands to WSL loses "$t" to Windows path conversion, so every
# case in an inline loop comes out as "./: Permission denied" -- which reads like
# a linker failure and is not one.
#
# Override the checkout path if the repository is not on /mnt/d:
#   DST=/mnt/c/code/goc/tmp/portability-out sh lin_run.sh
set -u
DST=${DST:-/mnt/d/Projects/goc/tmp/portability-out}

pass=0
fail=0

# case <name> <expected>
run_one() {
    t=$1
    want=$2
    if [ ! -x "$DST/$t" ]; then
        printf 'FAIL %-5s (缺少可执行文件)\n' "$t"
        fail=$((fail + 1))
        return
    fi
    got=$(cd "$DST" && "./$t" 2>&1)
    rc=$?
    if [ "$got" = "$want" ] && [ "$rc" -eq 0 ]; then
        printf 'pass %-5s\n' "$t"
        pass=$((pass + 1))
    else
        printf 'FAIL %-5s (rc=%d)\n' "$t" "$rc"
        printf '     期望: %s\n' "$(printf '%s' "$want" | tr '\n' '|')"
        printf '     实得: %s\n' "$(printf '%s' "$got" | tr '\n' '|')"
        fail=$((fail + 1))
    fi
}

# Self-reporting case: its output contains a port the kernel chose, so it
# cannot be compared against a fixed string. It reports its own verdict as
# exit code 0 and a final line of "OK".
run_ok() {
    t=$1
    if [ ! -x "$DST/$t" ]; then
        printf 'FAIL %-5s (缺少可执行文件)\n' "$t"
        fail=$((fail + 1))
        return
    fi
    got=$(cd "$DST" && "./$t" 2>&1)
    rc=$?
    last=$(printf '%s\n' "$got" | tail -1)
    if [ "$rc" -eq 0 ] && [ "$last" = "OK" ]; then
        printf 'pass %-5s\n' "$t"
        pass=$((pass + 1))
    else
        printf 'FAIL %-5s (rc=%d)\n' "$t" "$rc"
        printf '%s\n' "$got" | sed 's/^/     /'
        fail=$((fail + 1))
    fi
}

run_one mini "$(printf 'hi from gocl/linux')"
run_one slin "$(printf 'gocl linux stdio\nanswer=42\ncounter=40 answer=42')"
run_one p1   "A"
run_one p2   "n=42"
run_one p5   "$(printf 'raw\nr=4')"
run_one p6   "$(printf 'A\nn=42')"
run_one p7   "$(printf 'one\ntwo')"
run_one p8   "only"
run_one p9   "n=42"
run_one pb   "700"
run_one pe   "plain text"
run_one pf   "42"
run_one pg   "n=42"
run_one vt4  "13013"
# The socket cases, on the target where the syscalls behind them are native.
run_ok tcp
run_ok tcp2

echo ""
echo "== Linux: pass=$pass fail=$fail =="
[ "$fail" -eq 0 ]
