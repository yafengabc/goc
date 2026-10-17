#!/usr/bin/env bash
# Bit-compare goclib's binary128 runtime against gcc's __float128.
#
#   sh tests/fp128/run_oracle.sh
#
# HOST ONLY, and deliberately so: the oracle is __float128, which resolves to
# libgcc's own __addtf3/__subtf3/__multf3/__divtf3, and goc cannot compile
# __float128 at all. This is the "is the answer right?" test. The other half
# -- "does every back end produce the same answer?" -- is
# tests/portability_cases/fp128.c, which folds the same battery into a hash
# and runs under goc and gocl through win_regress.sh.
#
# Every operation must agree bit for bit, with one deliberate exception: a
# NaN result is checked for NaN-ness and not for its payload, because C does
# not say which NaN comes back and libgcc's is negative while ours is
# positive.
set -eu

cd "$(dirname "$0")/../.."
ROOT="$PWD"

CC="${CC:-/d/msys64/ucrt64/bin/gcc.exe}"

echo "== fp128 oracle: goclib/fp128.c vs gcc __float128 (libgcc tf3) =="
"$CC" -std=c11 -O2 -o bin/fp128_oracle.exe tests/fp128/oracle.c src/goclib/fp128.c
bin/fp128_oracle.exe
