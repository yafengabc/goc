#!/usr/bin/env bash
# Build the whole toolchain: goc (compiler), goa (assembler) and the two test
# tools. Safe to run from any directory; paths resolve relative to this script.
#
#   bash build.sh
#
# Layout: compiler source lives in goc/ (module goc) with goclib/ beside
# it, and the assembler in goa/ (module goa); every binary is emitted into
# ./bin so goc and goa stay siblings (findGoa looks next to the goc binary
# first).
#
# goc/goclib has to travel with the compiler: //go:embed resolves its
# pattern relative to the package directory, so a goclib left behind in src/
# would silently drop the whole C library from the binary -- a link error
# naming printf, not a build error.

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

# Version stamp for goc --version: git describe on a tag (v0.0.1), a suffix
# past it (v0.0.1-3-ga1b2c3d), or a bare hash with no tags yet. Empty when
# git is unavailable; goc then reports "dev".
#
# The stamp target is `main.version`, NOT the module path: `go build .` in the
# package directory builds the main package under the synthetic path
# command-line-arguments, where -X silently does nothing on newer toolchains.
# `main.version` is the canonical, version-independent spelling.
VERSION=""
if command -v git >/dev/null 2>&1; then
	VERSION="$(git describe --tags --always 2>/dev/null || true)"
fi

mkdir -p bin

echo "== goc =="
(cd src/goc && go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "../../bin/goc$EXE" .)

echo "== goa =="
(cd src/goa && go build -trimpath -ldflags="-s -w" -o "../../bin/goa$EXE" ./cmd/goa)

echo "== tools =="
(cd tools && go build -o "../bin/elfcheck$EXE"    ./elfcheck)
(cd tools && go build -o "../bin/msgboxcheck$EXE" ./msgboxcheck)

# cc.exe: a gcc/clang-compatible alias of goc. Build scripts can invoke it as a
# drop-in C compiler; it honours the gcc flag conventions accepted in main.go
# and, when named cc, behaves like gcc (compile to an executable, no auto-run).
cp -f "bin/goc$EXE" "bin/cc$EXE"

echo "done: bin/goc$EXE, bin/cc$EXE, bin/goa$EXE, bin/elfcheck$EXE, bin/msgboxcheck$EXE"
