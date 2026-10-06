#!/usr/bin/env bash
set +e
K="$1"; CC="$2"; EXE="$3"
case "$CC" in
  goc)  CMD=(/d/projects/goc/bin/goc.exe -I "$NIMLIB" .nc_$K/*.c -o "$EXE") ;;
  gcc)  CMD=(gcc -O2 -I "$NIMLIB" .nc_$K/*.c -o "$EXE") ;;
  gocl) CMD=(/d/projects/goc/bin/gocl.exe -I "$NIMLIB" .nc_$K/*.c -o "$EXE") ;;
esac
"${CMD[@]}" >/dev/null 2>build.err || { echo "BUILD_FAIL:$(cat build.err|head -1)"; exit 0; }
OUT=$(./"$EXE" 2>/dev/null)
echo "OUT:$OUT"
TIMEFORMAT='%R'
best=999; sum=0
for i in $(seq 1 10); do
  t=$( { time ./"./$EXE" >/dev/null 2>&1; } 2>&1 )
  sum=$(awk "BEGIN{print $sum+$t}")
  best=$(awk "BEGIN{print ($t<$best)?$t:$best}")
done
echo "avg=$(awk "BEGIN{printf \"%.4f\",$sum/10}") best=$best"
