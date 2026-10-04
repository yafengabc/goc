#!/usr/bin/env bash
# Regenerate the AT&T assembly samples goa's front-end tests run against.
#
# The four files checked into src/goa/testdata/att are enough to keep the tests
# meaningful on a fresh clone without adding half a megabyte of generated code
# to the repository. This script produces the *full* corpus instead -- one .s
# per goc example -- which is what you want when changing the front end, because
# the interesting cases (jump tables, SSE compare chains, struct-by-value
# returns) only show up in the bigger programs.
#
# The samples are LLVM's own AsmPrinter output for goc's examples, which is the
# exact input the front end has to eat in production.
#
# Usage:
#   bash tools/gen-att-samples.sh              # fill testdata/att, keep the four
#   bash tools/gen-att-samples.sh --all        # replace testdata/att entirely
#
# Requires a built bin/goc.exe (bash build.sh) and a reachable libLLVM.dll.

set -euo pipefail

cd "$(dirname "$0")/.."

GOC=./bin/goc.exe
OUT=src/goa/testdata/att
EXAMPLES=src/examples

if [ ! -x "$GOC" ]; then
	echo "error: $GOC not found -- run 'bash build.sh' first" >&2
	exit 1
fi

# The four committed samples double as the front end's smoke test on a fresh
# clone, so --all is the only mode that clears the directory.
if [ "${1:-}" = "--all" ]; then
	rm -rf "$OUT"
fi
mkdir -p "$OUT"

kept=0
skipped=0
failed=0
for c in "$EXAMPLES"/*.c; do
	base=$(basename "$c" .c)
	dest="$OUT/$base.s"

	# The committed samples are refreshed too, so a change in LLVM's output
	# format shows up here rather than as a puzzling test failure later.
	if [ "${1:-}" != "--all" ] && [ -f "$dest" ]; then
		kept=$((kept + 1))
		continue
	fi
	if ! "$GOC" -fllvm -S -O1 "$c" -o "$dest" 2>/dev/null; then
		# Examples the LLVM path rejects outright (inline assembly, bitfields,
		# _BitInt) leave nothing to sample.
		rm -f "$dest"
		failed=$((failed + 1))
		continue
	fi
	kept=$((kept + 1))
done

total=$(ls "$OUT"/*.s 2>/dev/null | wc -l | tr -d ' ')
size=$(du -sh "$OUT" | cut -f1)
echo "att samples: $total files, $size in $OUT"
if [ "$skipped" -gt 0 ]; then
	echo "  ($skipped already present and left alone)"
fi
if [ "$failed" -gt 0 ]; then
	echo "  ($failed examples are not LLVM-eligible and produced no sample)"
fi