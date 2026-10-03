#!/usr/bin/env bash
# Build and run the goc Win32 GUI tests, then screenshot the result.
#
#   bash tests/gui/run.sh [--shot]
#
# Each test program is a GUI-subsystem binary (-mwindows): it has no console, so
# success/failure is reported through the exit code, not stdout. The runner
# therefore checks the exit code and only then takes a picture.
#
# Exit codes used by mainwin.c:
#   0  all good        10 RegisterClassExW failed   11 CreateWindowExW failed
#  20  a GDI call failed mid-paint                   21 no frame was painted
set -euo pipefail

cd "$(dirname "$0")/../.."

GOC="bin/goc.exe"
OUT="tmp/guitest"
PY="C:/Users/EKSOFT/.workbuddy/binaries/python/versions/3.13.12/python.exe"
[ -x "$GOC" ] || GOC="bin/goc"

mkdir -p "$OUT"
shot=0
[ "${1:-}" = "--shot" ] && shot=1

fail=0
run_one() {
	local src="$1" name="$2" title="$3"
	echo "== $name =="
	"$GOC" "$src" -o "$OUT/$name.exe" -mwindows >/dev/null

	# GUI binaries need a moment; --keep-open so the screenshot tool can grab
	# the window before the program's own timer tears it down.
	if [ "$shot" = 1 ]; then
		set +e
		"$PY" tools/shot_gui.py "$OUT/$name.exe" "$OUT/$name.png" \
			--wait 1.2 --title "$title" --keep-open
		shot_rc=$?
		set -e
	fi

	set +e
	"$OUT/$name.exe"
	rc=$?
	set -e

	if [ "$rc" = 0 ]; then
		echo "PASS $name (exit 0)"
	else
		echo "FAIL $name (exit $rc)"
		fail=1
	fi
	[ "$shot" = 1 ] && [ "$shot_rc" != 0 ] && fail=1
	echo
}

run_one tests/gui/mainwin.c mainwin "goc main-window test"

if [ "$fail" != 0 ]; then
	echo "GUI TESTS FAILED"
	exit 1
fi
echo "GUI TESTS PASSED"
[ "$shot" = 1 ] && echo "screenshots in $OUT/"
exit 0
