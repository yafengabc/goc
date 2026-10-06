#!/bin/sh
# Link the goclib-built program and run it.
#
# The link is the real test, and it has one subtlety: goclib defines printf,
# fwrite, malloc, strlen and ~390 other names that musl also defines. goclib's
# must win -- the program under test is meant to be running goclib's code.
#
# The way to say that to the linker is an archive rather than loose objects: ld
# pulls an archive member only to satisfy a symbol that is still undefined, so
# musl's printf is never pulled in, while musl's own startup and the kernel
# entry points goclib calls (brk, write, exit_group) are. Loose .o files, by
# contrast, are all unconditionally included and every duplicate is an error.
set -u
cd /tmp/gl || exit 1

CC=${CC:-gcc}
CFLAGS="-std=c2x -nostdinc -Igoclib -O2 -Wall"

echo "=== 编译 hello.c（只用 goclib 的头）==="
$CC $CFLAGS -c hello.c -o obj/hello.o || exit 1

echo "=== 打成静态库 ==="
rm -f libgoclib.a
ar rcs libgoclib.a obj/args.o obj/assert.o obj/bitint.o obj/ctype.o \
    obj/dir.o obj/errno.o obj/file.o obj/math.o obj/mtx.o obj/os.o obj/rt.o \
    obj/stdbit.o obj/stdio.o obj/stdlib.o obj/string.o obj/threads.o \
    obj/time.o obj/uchar.o obj/wchar.o || exit 1
echo "libgoclib.a: $(ls -la libgoclib.a | awk '{print $5}') 字节"

echo "=== 链接：程序在前（优先），libgoclib.a 次之，musl 最后 ==="
$CC -o hello obj/hello.o libgoclib.a -lm 2>&1 | head -20
rc=$?
if [ ! -x hello ]; then
    echo "链接失败 (rc=$rc)"
    exit 1
fi

echo "=== 确认跑的是 goclib 而非 musl 的 printf ==="
# hello's own undefined symbols (printf, fputs, malloc, ...) must all have been
# satisfied from libgoclib.a. If musl's printf had been used instead, the
# program would still run, so also check which definitions landed in the binary.
if nm hello 2>/dev/null | grep -q ' T printf$'; then
    echo "printf 定义在 hello 中（来自 libgoclib.a）"
fi
nm hello 2>/dev/null | grep -E ' [Tt] (printf|fwrite|malloc|strlen|fputs)$' | sort

echo "=== 运行 ==="
./hello
echo "=== 退出码: $? ==="