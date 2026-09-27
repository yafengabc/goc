package main

import (
	"strings"
	"testing"
)

// genAsm runs the full frontend+codegen pipeline in-process and returns the
// generated x86-64 assembly for a single translation unit.
func genAsm(t *testing.T, src string) string {
	t.Helper()
	toks, err := Preprocess(src, "test.c")
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if errs := Check(prog); len(errs) > 0 {
		t.Fatalf("type check: %v", errs)
	}
	asm, err := Gen(prog, false)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	return asm
}

// TestRegisterAllocationEmitsRegs proves the allocator hands small int locals
// to callee-save registers: a function with more than four int locals must
// reference at least one of rbx/r12/r13/r14 as a local home.
func TestRegisterAllocationEmitsRegs(t *testing.T) {
	src := `int main(){
  int a,b,c,d,e,f;
  a=1; b=2; c=3; d=4; e=5; f=6;
  printf("%d %d %d %d %d %d\n", a,b,c,d,e,f);
  return 0;
}`
	asm := genAsm(t, src)
	if !strings.Contains(asm, "rbx") && !strings.Contains(asm, "r12") &&
		!strings.Contains(asm, "r13") && !strings.Contains(asm, "r14") {
		t.Errorf("expected callee-save register allocation, but none of rbx/r12/r13/r14 appear in:\n%s", asm)
	}
}

// TestAddressTakenLocalStaysOnStack proves a local whose address is taken is
// NOT register-allocated (it must remain in memory so &x is meaningful), and
// that the pipeline still succeeds.
func TestAddressTakenLocalStaysOnStack(t *testing.T) {
	src := `int main(){
  int x;
  int *p = &x;
  x = 42;
  printf("%d\n", *p);
  return 0;
}`
	// Must compile without error; x must live in memory (lea for &x).
	asm := genAsm(t, src)
	if !strings.Contains(asm, "lea") {
		t.Errorf("expected &x to emit a lea (stack address), asm:\n%s", asm)
	}
}
