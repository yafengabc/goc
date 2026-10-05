#!/usr/bin/env bash
# Build the whole toolchain: goc (compiler), goa (assembler) and the two test
# tools. Safe to run from any directory; paths resolve relative to this script.
#
#   bash build.sh
#
# Layout: the C front end is src/frontend (module goc/frontend), the native
# code generator is src/goc (module goc), the assembler is src/goa (module
# goa), and the C library is src/goclib; every binary is emitted into
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

echo "== gocl (LLVM back end) =="
# The second compiler: same front end, LLVM as the code generator. It needs
# libLLVM at run time, so it cannot be part of the ordinary build the way goc is
# -- the binary builds fine without the library and reports the missing library
# when a program is compiled, which is where the failure is actually meaningful.
(cd src/gocl && go build -trimpath -ldflags="-s -w" -o "../../bin/gocl$EXE" ./cmd/gocl)

echo "== goc (self-contained) =="
# The same compiler with the C library embedded: no goclib/ needed beside the
# binary. goc itself reads the library from disk, which is what lets a
# developer edit it without rebuilding the compiler.
(cd src && go build -trimpath -ldflags="-s -w" -o "../bin/goc-standalone$EXE" .)

echo "== goc/frontend =="
# The front end is a library; building it is just a compile check, which is
# worth doing on its own so a front-end error is not reported as a goc build
# failure. The separate module also means goc and (later) gocl can require it
# without depending on each other.
(cd src/frontend && go build ./...)

echo "== goc =="
(cd src/goc && go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "../../bin/goc$EXE" ./cmd/goc)

echo "== goa =="
(cd src/goa && go build -trimpath -ldflags="-s -w" -o "../../bin/goa$EXE" ./cmd/goa)

echo "== tools =="
(cd tools && go build -o "../bin/elfcheck$EXE"    ./elfcheck)
(cd tools && go build -o "../bin/msgboxcheck$EXE" ./msgboxcheck)

# cc.exe: a gcc/clang-compatible alias of goc. Build scripts can invoke it as a
# drop-in C compiler; it honours the gcc flag conventions accepted in main.go
# and, when named cc, behaves like gcc (compile to an executable, no auto-run).
cp -f "bin/goc$EXE" "bin/cc$EXE"

echo "done: bin/goc$EXE, bin/gocl$EXE, bin/goc-standalone$EXE, bin/cc$EXE, bin/goa$EXE,"
echo "      bin/elfcheck$EXE, bin/msgboxcheck$EXE"
