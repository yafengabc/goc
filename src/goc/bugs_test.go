package compiler

import (
	"goc/common"
	"goc/frontend"
	"strings"
	"testing"
)

// TestCompoundAssignEvalOnce pins C11 6.5.16.2 evaluate-once semantics for
// compound assignment. The parser used to desugar "E1 op= E2" into
// "E1 = E1 op E2", duplicating the lvalue: in "a[i++] += 10" the index
// expression ran twice and i was incremented twice (goc exited 212 where gcc
// exits 1121). The fix keeps the operator on frontend.AssignExpr and lets codegen park
// the lvalue address/value, so i++ must fire exactly once.
func TestCompoundAssignEvalOnce(t *testing.T) {
	src := `int main(){
  int a[4] = {1, 2, 3, 4};
  int i = 0;
  a[i++] += 10;
  return a[0]*100 + a[1]*10 + i;
}`
	asm := genAsm(t, src)
	if got := strings.Count(asm, "\tinc "); got != 1 {
		t.Errorf("a[i++] += 10 must evaluate i++ exactly once, found %d inc instructions:\n%s", got, asm)
	}
}

// TestCompoundAssignScalarFastPath is the simple-local smoke leg: "x += 10"
// takes the register/stack fast path in genCompoundAssign and must still
// compile into a real add plus a store back to the local's home.
func TestCompoundAssignScalarFastPath(t *testing.T) {
	src := `int main(){
  int x = 2;
  x += 10;
  return x;
}`
	asm := genAsm(t, src)
	if !strings.Contains(asm, "add") {
		t.Errorf("x += 10 must emit an add:\n%s", asm)
	}
	if !strings.Contains(asm, "mov [rbp") {
		t.Errorf("x += 10 must store the result back to the local slot:\n%s", asm)
	}
}

// TestO1InlineSkipsCalleeSavePushes guards the root cause of the -O1 inliner
// being permanently dead. extractInlineCand required the prologue shape
// "push rbp; mov rbp,rsp; sub rsp,N" to be contiguous, but codegen emits four
// unconditional callee-save pushes between "mov rbp,rsp" and "sub rsp,N", so
// every candidate failed the shape check and -O1 compiled exactly like -Os.
// The callee below is a leaf with no locals (nothing homes to a callee-save
// register, so the safety screen passes) yet still carries the full prologue
// with all four pushes -- it must now inline at -O1 while -Os keeps both call
// sites.
func TestO1InlineSkipsCalleeSavePushes(t *testing.T) {
	src := `int bump(int x, int y){ return x * y + 7; }
int main(){
  int r = 0;
  r = r + bump(2, 3);
  r = r + bump(4, 5);
  return r;
}`
	asm1 := genAsmOpt(t, src, 1)
	asmS := genAsmOpt(t, src, 2)
	if strings.Contains(asm1, "call bump") {
		t.Errorf("-O1 must inline bump despite the callee-save pushes in its prologue:\n%s", asm1)
	}
	if got := strings.Count(asmS, "call bump"); got != 2 {
		t.Errorf("-Os must keep both call sites, got %d:\n%s", got, asmS)
	}
}

// TestMFValueNotTreatedAsInput pins the -MF flag fix: the dependency-file
// name must be consumed, never appended to cfg.inputs (before the fix the
// value fell through to the input list and goc died with
// "open deps.d: file not found").
func TestMFValueNotTreatedAsInput(t *testing.T) {
	cfg, _ := parseArgs([]string{"-MF", "deps.d", "in.c"})
	if len(cfg.inputs) != 1 || cfg.inputs[0] != "in.c" {
		t.Errorf("-MF deps.d must consume deps.d, inputs = %v", cfg.inputs)
	}
	cfg, _ = parseArgs([]string{"-MF=deps.d", "in.c"})
	if len(cfg.inputs) != 1 || cfg.inputs[0] != "in.c" {
		t.Errorf("-MF=deps.d must not leak deps.d into inputs, got %v", cfg.inputs)
	}
}

// TestBOMPrefixedSource pins the UTF-8 BOM handling: spliceContinuations
// strips the EF BB BF prefix before tokenizing, so a BOM-prefixed translation
// unit preprocesses cleanly instead of failing with
// "unexpected character 'ï'".
func TestBOMPrefixedSource(t *testing.T) {
	src := string([]byte{0xEF, 0xBB, 0xBF}) + "int main(){ return 0; }"
	if _, err := common.Preprocess(src, "bom.c"); err != nil {
		t.Errorf("BOM-prefixed source must preprocess cleanly, got: %v", err)
	}
}

// TestIfDivideByZeroErrors pins the const-eval fix: a division (or remainder)
// by zero inside #if/#elif was silently yielding 0; it must now be reported.
func TestIfDivideByZeroErrors(t *testing.T) {
	if _, err := common.Preprocess("#if 1/0\nint x;\n#endif\nint main(){ return 0; }", "d.c"); err == nil ||
		!strings.Contains(err.Error(), "division by zero") {
		t.Errorf("#if 1/0 must report division by zero, got: %v", err)
	}
	if _, err := common.Preprocess("#if 0\n#elif 2/0\n#endif\nint main(){ return 0; }", "d.c"); err == nil ||
		!strings.Contains(err.Error(), "division by zero") {
		t.Errorf("#elif 2/0 must report division by zero, got: %v", err)
	}
}

// TestVarTypesResetAcrossTUs pins the per-translation-unit reset of the
// parse-time variable-type table. varTypes was not cleared in frontend.Parse (unlike
// typedefs/structs/frontend.EnumConsts), so a name declared in one TU stayed
// resolvable in the next: the built-in library TUs run before the user's, and
// a name collision made _Alignof/typeof silently pick up a stale type.
func TestVarTypesResetAcrossTUs(t *testing.T) {
	toks1, err := common.Preprocess("int v;", "a.c")
	if err != nil {
		t.Fatalf("TU1 preprocess: %v", err)
	}
	if _, err := frontend.Parse(toks1); err != nil {
		t.Fatalf("TU1 parse: %v", err)
	}
	toks2, err := common.Preprocess("int main(){ return _Alignof(v); }", "b.c")
	if err != nil {
		t.Fatalf("TU2 preprocess: %v", err)
	}
	if _, err := frontend.Parse(toks2); err == nil {
		t.Errorf("v declared only in TU1 must not resolve in TU2; _Alignof(v) should fail to parse")
	}
}
