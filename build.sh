#!/usr/bin/env bash
# Build the whole toolchain: c0 (compiler), a0 (assembler) and the two test
# tools. Safe to run from any directory; paths resolve relative to this script.
#
#   bash build.sh
#
# Windows note: c0 looks for a0.exe next to itself, so a0 is copied up here.

set -euo pipefail

cd "$(dirname "$0")"

# When this script is run as a *file*, the sandbox trims TMP/TEMP/HOME, which
# breaks go build (no work dir, no build cache). Set them explicitly.
export HOME="${HOME:-/tmp}"
[ -n "${TMP:-}" ]    || export TMP=/tmp
[ -n "${TEMP:-}" ]   || export TEMP=/tmp
[ -n "${TMPDIR:-}" ] || export TMPDIR=/tmp
export GOTMPDIR="$TMP"
[ -n "${GOCACHE:-}" ] || export GOCACHE="$HOME/.cache/go-build"

EXE="$(go env GOEXE)"   # ".exe" on Windows, "" elsewhere

echo "== c0 =="
go build -trimpath -ldflags="-s -w" -o "c0$EXE" .

echo "== a0 =="
(cd asm && go build -trimpath -ldflags="-s -w" -o "a0$EXE" .)
if [ -n "$EXE" ]; then
    cp "asm/a0$EXE" "./a0$EXE"
fi

echo "== tools =="
(cd asm && go build -o "tools/elfcheck$EXE"   ./tools/elfcheck)
(cd asm && go build -o "tools/msgboxcheck$EXE" ./tools/msgboxcheck)

echo "done: c0$EXE, asm/a0$EXE, asm/tools/elfcheck$EXE, asm/tools/msgboxcheck$EXE"
