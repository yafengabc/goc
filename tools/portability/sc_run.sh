#!/bin/sh
# Build, link and run the syscall(2)-route test.
#
# The subject is the eight wrappers syscall.h routes through syscall(2) rather
# than libc -- stat, fstat, access, rename, mkdir, rmdir, getcwd, chmod. They
# cannot be mapped to the libc names of the same spelling, because goclib itself
# defines functions with those names (dir.c's `int stat(...)` among them) and the
# definition is what the name resolves to: gcc reports that arrangement as
# -Winfinite-recursion, and at runtime it is a stack that never comes back. So
# the route is syscall(2), which is also what goa emits on the goc side, and
# which lands the same bytes goclib's <sys/stat.h> layout was written against.
set -u
cd /tmp/gl || exit 1

CC=${CC:-gcc}
CFLAGS="-std=c2x -nostdinc -Igoclib -O2 -Wall"

echo "=== 编译 sc.c ==="
$CC $CFLAGS -c sc.c -o obj/sc.o || exit 1

echo "=== 链接 ==="
$CC -o sc obj/sc.o libgoclib.a -lm 2>&1 | head -20
if [ ! -x sc ]; then
    echo "链接失败"
    exit 1
fi

echo "=== 运行 ==="
./sc
rc=$?
echo "=== 退出码: $rc ==="
exit $rc
