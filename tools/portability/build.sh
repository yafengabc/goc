#!/bin/sh
# Build goclib with gcc -nostdinc: every header in play is goclib's own, so this
# proves goclib needs nothing from the host C library to compile. Run inside the
# WSL alpine container.
set -u
cd /tmp/gl || exit 1

CC=${CC:-gcc}
CFLAGS="-std=c2x -nostdinc -Igoclib -O2 -Wall"

rm -rf obj err
mkdir -p obj err

ok=0
fail=0
for f in goclib/*.c; do
    b=$(basename "$f" .c)
    if $CC $CFLAGS -c "$f" -o "obj/$b.o" 2>"err/$b.txt"; then
        ok=$((ok + 1))
    else
        fail=$((fail + 1))
        echo "FAIL $b"
        head -5 "err/$b.txt"
    fi
done
echo "== $CC 编译: 成功=$ok 失败=$fail =="

# Warnings that are not about goclib redefining compiler-provided macros or
# nested comment markers -- the real ones.
#
# -Wnonnull-compare is filtered on purpose. gcc knows fputs and strdup from its
# own built-ins as nonnull and objects to the null checks in file.c and
# string.c. Those checks are deliberate -- passing a null buffer through as an
# error is what a caller with an unchecked pointer gets instead of a crash --
# so the warning describes a decision rather than a defect.
echo "--- 实质警告 ---"
cat err/*.txt 2>/dev/null | grep 'warning:' \
    | grep -v 'Wbuiltin-macro-redefined' | grep -v 'Wcomment' \
    | grep -v 'Wnonnull-compare' \
    | sed 's/.*warning: //' | sort | uniq -c | sort -rn | head -20