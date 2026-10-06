#!/bin/sh
# Build phase of the Linux ELF regression. Runs on Windows, because gocl.exe is
# a Windows program: hand it /mnt/d/... paths and it reports "the system cannot
# find the path specified", since those are Linux paths and it resolves them
# against the Windows filesystem. So the compile happens here and only the run
# happens in the alpine container (see lin_run.sh).
#
# The cases that call write(2) directly live in this group and nowhere else --
# that is the syscall that made them look broken when they were listed under
# Windows. See win_regress.sh.
set -u
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
SRC=$ROOT/tests/portability_cases
DST=$ROOT/tmp/portability-out
GOCL=$ROOT/bin/gocl.exe
mkdir -p "$DST"

# case <name> <source>   -- compiled only; lin_run.sh checks the output.
build_one() {
    t=$1
    src=$2
    rm -f "$DST/$t"
    if "$GOCL" -target linux "$src" -o "$DST/$t" > "$DST/$t.build" 2>&1 && [ -f "$DST/$t" ]; then
        echo "build ok   $t"
    else
        echo "BUILD FAIL $t"
        head -3 "$DST/$t.build" | cut -c1-200 | sed 's/^/     /'
    fi
}

build_one mini "$SRC/mini_linux.c"
build_one slin "$SRC/stdio_linux.c"
for t in p1 p2 p5 p6 p7 p8 p9 pb pe pf pg vt4 tcp tcp2; do
    build_one "$t" "$SRC/$t.c"
done
