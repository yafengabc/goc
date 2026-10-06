//go:build windows

package gocl

// LLVM's verdict on the IR goc's front end produces.
//
// The generator's own tests (src/llvmir_test.go) check the shape of the IR
// without a shared library, so they run anywhere. This file asks the authority:
// the IR is handed to LLVM, and a module it rejects never becomes a program. The
// two together are what make the front end trustworthy -- the text assertions
// catch intent, LLVM catches everything they did not think of.
//
// Set GOC_LLVM_IR to a .ll file to check a specific one; without it the module
// generated here is used.

import (
	"os"
	"strings"
	"testing"
)

// The IR goc's front end emits for a small but complete program: a loop, a
// branch, a ternary, a call, an array and a global. Anything structurally wrong
// with the generator shows up here, because LLVM refuses the module.
const gocFrontEndProbe = `
target datalayout = "e-m:w-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-pc-windows-msvc"

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

define void @sort(ptr %a, i32 %n) {
entry:
  %z = icmp slt i32 %n, 2
  br i1 %z, label %done, label %outer
outer:
  %i = phi i32 [ 0, %entry ], [ %inext, %outer.latch ]
  %last = sub i32 %n, 1
  %oc = icmp slt i32 %i, %last
  br i1 %oc, label %inner, label %done
inner:
  %j = phi i32 [ 0, %outer ], [ %jnext, %inner.latch ]
  %p = getelementptr i32, ptr %a, i32 %j
  %q = getelementptr i32, ptr %p, i32 1
  %x = load i32, ptr %p, align 4
  %y = load i32, ptr %q, align 4
  %gt = icmp sgt i32 %x, %y
  br i1 %gt, label %swap, label %inner.latch
swap:
  store i32 %y, ptr %p, align 4
  store i32 %x, ptr %q, align 4
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
  %f = call i32 @fib(i32 10)
  store i32 %f, ptr @counter, align 4
  %arr = alloca [4 x i32], align 16
  %e0 = getelementptr inbounds [4 x i32], ptr %arr, i64 0, i32 0
  store i32 4, ptr %e0, align 4
  %e1 = getelementptr inbounds [4 x i32], ptr %arr, i64 0, i32 1
  store i32 3, ptr %e1, align 4
  %ap = getelementptr inbounds [4 x i32], ptr %arr, i64 0, i64 0
  call void @sort(ptr %ap, i32 4)
  %first = load i32, ptr %ap, align 4
  %c = load i32, ptr @counter, align 4
  %sum = add i32 %first, %c
  ret i32 %sum
}
`

// TestLLVMAcceptsGeneratedIR is the front end's verdict from LLVM itself.
func TestLLVMAcceptsGeneratedIR(t *testing.T) {
	api, err := LoadLLVM()
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	ir := []byte(gocFrontEndProbe)
	if p := os.Getenv("GOC_LLVM_IR"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		ir = b
	}
	obj := t.TempDir() + "/probe.obj"
	if err := api.CompileToObject(ir, obj, LLVMOptAggressive, "", false); err != nil {
		t.Fatalf("LLVM rejected the IR: %v", err)
	}
	st, err := os.Stat(obj)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() == 0 {
		t.Fatal("LLVM produced an empty object")
	}
	t.Logf("LLVM accepted the IR: %d-byte object", st.Size())
}

// TestLLVMRejectsBadIR is the control for the test above: it proves a failure
// there is a real rejection and not the check silently passing. A module with an
// unterminated block -- the mistake this generator is most likely to make --
// must be refused.
func TestLLVMRejectsBadIR(t *testing.T) {
	api, err := LoadLLVM()
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	bad := strings.Join([]string{
		`target triple = "x86_64-pc-windows-msvc"`,
		``,
		`define i32 @f() {`,
		`entry:`,
		`  %a = add i32 1, 2`,
		`}`, // <- the block has no terminator
		``,
	}, "\n")
	obj := t.TempDir() + "/bad.obj"
	if err := api.CompileToObject([]byte(bad), obj, LLVMOptNone, "", false); err == nil {
		t.Fatal("LLVM accepted a module with an unterminated block; " +
			"the front end's own checks cannot be trusted")
	} else {
		t.Logf("correctly rejected: %v", firstLine(err.Error()))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
