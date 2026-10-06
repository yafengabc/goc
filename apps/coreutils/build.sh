#!/usr/bin/env bash
# Build the coreutils with both goc back ends.
#
#   bash build.sh            # Windows PE: ls_goc.exe (goa back end) + ls_gocl.exe (LLVM back end)
#   bash build.sh linux      # also Linux ELF: ls (gocl) + ls_goc (goc -target linux)
#
# Every binary is emitted into apps/coreutils/bin/, which is gitignored, so a
# fresh checkout rebuilds them with this script rather than carrying binaries.
#
# The point of the dual build is the design goal stated in ls.c: the output must
# be byte-identical whether ls was compiled by goc or gocl, and whether it runs
# as a Windows PE or a Linux ELF. tools/portability/ and the ls cross-run in
# run_tests.sh are what actually assert that; this script just produces the
# artifacts.
set -euo pipefail

cd "$(dirname "$0")/../.."   # repo root

GOCL=bin/gocl.exe
GOC=bin/goc.exe
SRC=apps/coreutils/ls.c
OUT=apps/coreutils/bin
mkdir -p "$OUT"

# When run as a file the sandbox trims HOME/TMP, which goc needs for its work
# dir; mirror build.sh's guard so a `bash build.sh` from a checked-out tree works.
export HOME="${HOME:-/tmp}"
[ -n "${TMP:-}" ]    || export TMP=/tmp
[ -n "${TEMP:-}" ]   || export TEMP=/tmp
[ -n "${TMPDIR:-}" ] || export TMPDIR=/tmp
export GOTMPDIR="$TMP"
[ -n "${GOCACHE:-}" ] || export GOCACHE="$HOME/.cache/go-build"

echo "== Windows PE =="
"$GOC"  "$SRC" -o "$OUT/ls_goc.exe"
"$GOCL" "$SRC" -o "$OUT/ls_gocl.exe"

if [ "${1:-}" = "linux" ]; then
    echo "== Linux ELF =="
    "$GOCL" -target linux "$SRC" -o "$OUT/ls"
    "$GOC"  -target linux "$SRC" -o "$OUT/ls_goc"
fi

echo "built:"
ls -la "$OUT"
