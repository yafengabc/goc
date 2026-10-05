package main

import (
	"strings"
	"testing"
)

// PEXT: `*(p ± K)` must fold into a single lea with the element-scaled
// displacement, instead of running the generic pointer-arithmetic recipe
// (materialise K, sign-extend, imul by the element width, add).

func pextBody(t *testing.T, src string, opt int) string {
	t.Helper()
	return funcBody(genAsmOpt(t, src, opt), "f")
}

// TestPextLoadAdd: *(p+1) -> lea r10, [reg+4]; load.
func TestPextLoadAdd(t *testing.T) {
	body := pextBody(t, `int f(int *p) { return *(p + 1); }
int main(void) { int x[4]; return f(x); }`, 3)
	if !strings.Contains(body, "lea r10, [rax+4]") {
		t.Fatalf("*(p+1) must fold into 'lea r10, [rax+4]';\\n%s", body)
	}
	if strings.Contains(body, "imul") {
		t.Fatalf("*(p+1) must not scale the constant with imul;\\n%s", body)
	}
}

// TestPextLoadSub: *(p-1) -> lea r10, [reg-4].
func TestPextLoadSub(t *testing.T) {
	body := pextBody(t, `int f(int *p) { return *(p - 1); }
int main(void) { int x[4]; return f(x + 1); }`, 3)
	if !strings.Contains(body, "lea r10, [rax-4]") {
		t.Fatalf("*(p-1) must fold into 'lea r10, [rax-4]';\\n%s", body)
	}
}

// TestPextLoadConstPlusPtr: 1 + p is the same displacement (pointer + int is
// commutative in C).
func TestPextLoadConstPlusPtr(t *testing.T) {
	body := pextBody(t, `int f(int *p) { return *(1 + p); }
int main(void) { int x[4]; return f(x); }`, 3)
	if !strings.Contains(body, "lea r10, [rax+4]") {
		t.Fatalf("*(1+p) must fold into 'lea r10, [rax+4]';\\n%s", body)
	}
}

// TestPextStore: the store path goes through genLValue, not genUnary, and must
// fold there too.
func TestPextStore(t *testing.T) {
	body := pextBody(t, `void f(int *p) { *(p + 1) = 7; }
int main(void) { int x[4]; f(x); return 0; }`, 3)
	if !strings.Contains(body, "lea r10, [rax+4]") {
		t.Fatalf("*(p+1)=7 must fold the store address;\\n%s", body)
	}
	if !strings.Contains(body, "mov dword [r10], eax") {
		t.Fatalf("store must go through the folded address;\\n%s", body)
	}
}

// TestPextRegisterCachedPointer: a register-cached pointer needs no evaluation
// at all -- the lea reads the callee-save directly.
func TestPextRegisterCachedPointer(t *testing.T) {
	body := pextBody(t, `int f(int *q) { int *p = q; return *(p + 3); }
int main(void) { int x[8]; return f(x); }`, 3)
	if !strings.Contains(body, "lea r10, [rbx+12]") {
		t.Fatalf("register-cached p must fold to 'lea r10, [rbx+12]';\\n%s", body)
	}
}

// TestPextCharPointerStride: the displacement scales by the pointee width, so a
// char* steps 1 byte per element, not 8.
func TestPextCharPointerStride(t *testing.T) {
	body := pextBody(t, `int f(char *p) { return *(p + 2); }
int main(void) { return f("abc"); }`, 3)
	if !strings.Contains(body, "lea r10, [rax+2]") {
		t.Fatalf("char* must stride 1 byte;\\n%s", body)
	}
}

// TestPextVariableOffsetUnchanged: a non-literal offset is NOT foldable and
// must keep the generic recipe -- folding it would be wrong.
func TestPextVariableOffsetUnchanged(t *testing.T) {
	body := pextBody(t, `int f(int *p, int k) { return *(p + k); }
int main(void) { int x[4]; return f(x, 1); }`, 3)
	if strings.Contains(body, "lea r10, [rax") && !strings.Contains(body, "imul") {
		t.Fatalf("variable offset must not fold;\\n%s", body)
	}
	if !strings.Contains(body, "imul") {
		t.Fatalf("variable offset must keep the imul scaling;\\n%s", body)
	}
}

// TestPextPointerDifferenceUnchanged: `p - q` is a byte difference, not an
// indexed access; the fold must not touch it.
func TestPextPointerDifferenceUnchanged(t *testing.T) {
	body := pextBody(t, `int f(int *p, int *q) { return (int)(p - q); }
int main(void) { int x[4]; return f(x, x + 2); }`, 3)
	if !strings.Contains(body, "idiv") {
		t.Fatalf("pointer difference must keep dividing by the element width;\\n%s", body)
	}
}

// TestPextO0Guardrail: -O0 output must be byte-identical to the pre-PEXT
// pipeline, so the fold is compiled out entirely there.
func TestPextO0Guardrail(t *testing.T) {
	body := pextBody(t, `int f(int *p) { return *(p + 1); }
int main(void) { int x[4]; return f(x); }`, 0)
	if strings.Contains(body, "lea r10, [rax+4]") {
		t.Fatalf("-O0 must not fold (byte-identical guardrail);\\n%s", body)
	}
	if !strings.Contains(body, "imul") {
		t.Fatalf("-O0 must keep the generic pointer-arithmetic recipe;\\n%s", body)
	}
}
