target datalayout = "e-m:w-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-w64-windows-gnu"

@counter = global i32 0, align 4
@msg = internal constant [15 x i8] c"hello from obj\00", align 1
; A minimal in-module print, standing in for goclib: writes the string to stdout.
@stdout_handle = global i32 0, align 4

declare i32 @GetStdHandle(i32)
declare i32 @WriteFile(i32, ptr, i32, ptr, ptr)

define void @print(ptr %s) {
entry:
  %h = call i32 @GetStdHandle(i32 -11)
  %p1 = getelementptr i8, ptr %s, i64 1
  %p2 = getelementptr i8, ptr %p1, i64 1
  %p3 = getelementptr i8, ptr %p2, i64 1
  %p4 = getelementptr i8, ptr %p3, i64 1
  %p5 = getelementptr i8, ptr %p4, i64 1
  %p6 = getelementptr i8, ptr %p5, i64 1
  %p7 = getelementptr i8, ptr %p6, i64 1
  %p8 = getelementptr i8, ptr %p7, i64 1
  %p9 = getelementptr i8, ptr %p8, i64 1
  %pA = getelementptr i8, ptr %p9, i64 1
  %pB = getelementptr i8, ptr %pA, i64 1
  %pC = getelementptr i8, ptr %pB, i64 1
  %pD = getelementptr i8, ptr %pC, i64 1
  %pE = getelementptr i8, ptr %pD, i64 1
  %len64 = ptrtoint ptr %pE to i64
  %len = trunc i64 %len64 to i32
  %w = call i32 @WriteFile(i32 %h, ptr %s, i32 %len, ptr null, ptr null)
  ret void
}

define i32 @fib(i32 %n) {
entry:
  %c = icmp slt i32 %n, 2
  br i1 %c, label %base, label %rec
base:
  ret i32 %n
rec:
  %n1 = sub i32 %n, 1
  %n2 = sub i32 %n, 2
  %a = call i32 @fib(i32 %n1)
  %b = call i32 @fib(i32 %n2)
  %s = add i32 %a, %b
  ret i32 %s
}

define void @bsort(ptr %a, i32 %n) {
entry:
  %z = icmp slt i32 %n, 2
  br i1 %z, label %done, label %outer
outer:
  %i = phi i32 [ 0, %entry ], [ %inext, %outer.latch ]
  %last = sub i32 %n, 1
  %oc = icmp slt i32 %i, %last
  br i1 %oc, label %inner.init, label %done
inner.init:
  br label %inner
inner:
  %j = phi i32 [ 0, %inner.init ], [ %jnext, %inner.latch ]
  %p = getelementptr i32, ptr %a, i32 %j
  %pn1 = getelementptr i32, ptr %p, i32 1
  %x = load i32, ptr %p, align 4
  %y = load i32, ptr %pn1, align 4
  %gt = icmp sgt i32 %x, %y
  br i1 %gt, label %swap, label %inner.latch
swap:
  store i32 %y, ptr %p, align 4
  store i32 %x, ptr %pn1, align 4
  br label %inner.latch
inner.latch:
  %jnext = add i32 %j, 1
  %ic = icmp slt i32 %jnext, %last
  br i1 %ic, label %inner, label %outer.latch
outer.latch:
  %inext = add i32 %i, 1
  br label %outer
done:
  ret void
}

@arr = internal global [8 x i32] [i32 8, i32 7, i32 6, i32 5, i32 4, i32 3, i32 2, i32 1], align 16

define i32 @main() {
entry:
  call void @print(ptr @msg)
  %f = call i32 @fib(i32 20)
  store i32 %f, ptr @counter, align 4
  %ap = getelementptr inbounds [8 x i32], ptr @arr, i64 0, i64 0
  call void @bsort(ptr %ap, i32 8)
  %first = load i32, ptr %ap, align 4
  ret i32 %first
}
