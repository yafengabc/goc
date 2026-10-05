target datalayout = "e-m:w-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-w64-windows-gnu"

@arr = internal global [8 x i32] [i32 8, i32 7, i32 6, i32 5, i32 4, i32 3, i32 2, i32 1], align 16
@counter = global i32 0, align 4

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

define i32 @main() {
entry:
  %f = call i32 @fib(i32 20)
  store i32 %f, ptr @counter, align 4
  %ap = getelementptr inbounds [8 x i32], ptr @arr, i64 0, i64 0
  call void @bsort(ptr %ap, i32 8)
  %first = load i32, ptr %ap, align 4
  %c = load i32, ptr @counter, align 4
  %sum = add i32 %first, %c
  ret i32 %sum
}
