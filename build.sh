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

echo "== goc (own x86-64 back end, -tags goc) =="
# The whole toolchain in one executable: front end, code generator, assembler,
# and the C library it compiles against -- the library is embedded, so the
# binary needs nothing beside it. The tag picks which code generator src/ wires
# in: goc's own emitter here, gocl's LLVM back end below. Both share the
# embedded library (src/libembed.go) and the drivers live in package goc /
# package gocl respectively.
(cd src && go build -trimpath -ldflags="-s -w" -o "../bin/goc$EXE" -tags goc .)

echo "== gocl (LLVM back end, -tags gocl) =="
# gocl the same way: the identical driver (gocl.Main) reached from the second
# entry point. It still needs libLLVM.dll beside the binary, which the release
# copy step keeps there -- the binary builds fine without it and reports the
# missing library when a program is actually compiled.
#
# Only gocl needs the stamp: goc's driver derives its version from git at run
# time, while gocl's lives in package gocl, so -X is what fills it in.
(cd src && go build -trimpath -ldflags="-s -w -X gocl.version=$VERSION" -o "../bin/gocl$EXE" -tags gocl .)

echo "== gocld (linker) =="
# Both halves matter. `go build ./...` is a compile check on the library, which
# goa depends on -- a gocld that fails to build is a goa that fails to build,
# and the error surfaces one package earlier this way. The cmd build is the
# product: a standalone linker you can run on .o files directly, the way cc
# would invoke one.
(cd src/gocld && go build ./...)
(cd src/gocld && go build -trimpath -ldflags="-s -w" -o "../../bin/gocld$EXE" ./cmd/gocld)

echo "== goc/frontend =="
# The front end is a library; building it is just a compile check, which is
# worth doing on its own so a front-end error is not reported as a goc build
# failure. The separate module also means goc and (later) gocl can require it
# without depending on each other.
(cd src/frontend && go build ./...)

echo "== goa =="
(cd src/goa && go build -trimpath -ldflags="-s -w" -o "../../bin/goa$EXE" ./cmd/goa)

echo "== tools =="
(cd tools && go build -o "../bin/elfcheck$EXE"    ./elfcheck)
(cd tools && go build -o "../bin/msgboxcheck$EXE" ./msgboxcheck)

# cc.exe: a gcc/clang-compatible alias of goc. Build scripts can invoke it as a
# drop-in C compiler; it honours the gcc flag conventions accepted in goc.go
# and, when named cc, behaves like gcc (compile to an executable, no auto-run).
cp -f "bin/goc$EXE" "bin/cc$EXE"

echo "done: bin/goc$EXE, bin/gocl$EXE, bin/gocld$EXE, bin/cc$EXE,"
echo "      bin/goa$EXE, bin/elfcheck$EXE, bin/msgboxcheck$EXE"
