#!/usr/bin/env bash
# Build the whole toolchain: goc (compiler), goa (assembler) and the two test
# tools. Safe to run from any directory; paths resolve relative to this script.
#
#   bash build.sh
#
# Windows note: goc looks for goa.exe next to itself, so goa is copied up here.

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

echo "== goc =="
go build -trimpath -ldflags="-s -w" -o "goc$EXE" .

echo "== goa =="
(cd goa && go build -trimpath -ldflags="-s -w" -o "goa$EXE" .)
if [ -n "$EXE" ]; then
    cp "goa/goa$EXE" "./goa$EXE"
fi

echo "== tools =="
(cd goa && go build -o "tools/elfcheck$EXE"   ./tools/elfcheck)
(cd goa && go build -o "tools/msgboxcheck$EXE" ./tools/msgboxcheck)

echo "done: goc$EXE, goa/goa$EXE, goa/tools/elfcheck$EXE, goa/tools/msgboxcheck$EXE"
