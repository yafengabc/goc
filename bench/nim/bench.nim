## The workload for the Nim -> C benchmark.
##
## Every kernel is deterministic and prints a checksum. That is the point: a
## compiler that miscompiles something is then visible as a *different number*
## rather than merely as a fast run. The kernels cover the things a C backend
## actually has to get right -- integer loops, recursion, heap allocation,
## Seq/string growth (which is the allocator and memcpy under it) and floating
## point.
##
## No sleeping, no I/O in the timed region, no output-dependent iteration
## counts: the same source compiled by gcc, goc and gocl runs exactly the same
## instructions' worth of work, so the only thing that differs is how well each
## compiler lowered them.

import std/strutils

const
  SieveN = 300_000
  FibN = 28
  IntIters = 5_000_000
  StrIters = 20_000
  FloatIters = 2_000_000

proc sieve(n: int): int =
  var flags = newSeq[bool](n + 1)
  for i in 2 .. n:
    flags[i] = true
  var i = 2
  while i * i <= n:
    if flags[i]:
      var j = i * i
      while j <= n:
        flags[j] = false
        j += i
    inc i
  var count = 0
  for k in 2 .. n:
    if flags[k]:
      inc count
  count

proc fib(n: int): int =
  if n < 2: n else: fib(n - 1) + fib(n - 2)

proc intLoop(): int64 =
  var sum: int64 = 0
  var i: int64 = 0
  while i < IntIters:
    sum += i * 3 - 1
    inc i
  sum

proc strBuild(): int =
  var s = ""
  for i in 0 ..< StrIters:
    s.add $(i mod 10)
  s.len

proc floatLoop(): float =
  var x = 1.0
  for i in 1 .. FloatIters:
    x = x * 1.0000001 + 0.5
  x

proc main() =
  echo "sieve    ", sieve(SieveN)
  echo "fib      ", fib(FibN)
  echo "intloop  ", intLoop()
  echo "strbuild ", strBuild()
  echo "float    ", formatFloat(floatLoop(), ffDefault, 6)

main()
