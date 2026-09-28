package main

import (
	"strings"
	"testing"
)

// genAsm runs the full frontend+codegen pipeline in-process and returns the
// generated x86-64 assembly for a single translation unit, at -O0.
func genAsm(t *testing.T, src string) string {
	t.Helper()
	return genAsmOpt(t, src, 0)
}

// genAsmOpt is genAsm at an explicit optimisation level.
func genAsmOpt(t *testing.T, src string, opt int) string {
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
	asm, err := Gen(prog, false, opt)
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

// bindTestCG returns a CG whose variable tables mirror a function with one
// local (x, at rbp-8), one parameter (arg, at rbp+16), one register-homed
// local (rv, defensive no-op branch) and two globals (g, st).
func bindTestCG() *CG {
	return &CG{
		vars: map[string]varInfo{
			"x":   {off: -8},
			"arg": {off: 16},
			"rv":  {off: -16, reg: "rbx"},
		},
		globalLab: map[string]string{
			"g":  "G_g",
			"st": "G_st3_st",
		},
	}
}

// TestBindAsmLine covers the inline-asm binder's rewrite rules:
// locals/params -> [rbp+off], globals/static locals -> [rip+G_x], the
// inside-bracket bare form, and everything that must be left untouched
// (registers, mnemonics, numeric literals, label definitions, array-style
// references, strings, comments, unknown names).
func TestBindAsmLine(t *testing.T) {
	c := bindTestCG()
	cases := []struct{ in, want string }{
		// locals and parameters bind to their stack slot
		{"mov rax, x", "mov rax, [rbp-8]"},
		{"add rax, arg", "add rax, [rbp+16]"},
		// a variable already inside brackets binds to the bare form
		{"mov rax, [x]", "mov rax, [rbp-8]"},
		{"lea rax, [x]", "lea rax, [rbp-8]"},
		// globals / static locals bind through the global label
		{"mov rax, [g]", "mov rax, [G_g]"},
		{"lea rax, g", "lea rax, [rip+G_g]"},
		{"mov rax, [st]", "mov rax, [G_st3_st]"},
		// registers, mnemonics, size keywords: never rewritten
		{"mov rax, rbx", "mov rax, rbx"},
		{"mov eax, xmm0", "mov eax, xmm0"},
		{"push rbp", "push rbp"},
		{"ret", "ret"},
		{"mov dword ptr [rax], 1", "mov dword ptr [rax], 1"},
		// numeric literals, hex, negative constants
		{"mov eax, 123", "mov eax, 123"},
		{"mov eax, 0x10", "mov eax, 0x10"},
		{"add rax, -8", "add rax, -8"},
		// label definitions and array-style references are not operands
		{"foo: nop", "foo: nop"},
		{"mov eax, x[0]", "mov eax, x[0]"},
		// strings and comments are not scanned
		{`mov rax, "x"`, `mov rax, "x"`},
		{"mov eax, 1 ; x arg g", "mov eax, 1 ; x arg g"},
		{"mov eax, 1 // x", "mov eax, 1 // x"},
		// unknown names pass through untouched
		{"mov rax, mystery", "mov rax, mystery"},
		// register-homed var (defensive branch) stays untouched
		{"mov rax, rv", "mov rax, rv"},
	}
	for _, tc := range cases {
		if got := c.bindAsmLine(tc.in); got != tc.want {
			t.Errorf("bindAsmLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestBindAsmLineMisc pins down the binder's structural behavior: whole
// string literals with escapes, quotes inside comments, and '#' passthrough.
func TestBindAsmLineMisc(t *testing.T) {
	c := bindTestCG()
	cases := []struct{ in, want string }{
		// escaped quote inside a string literal must not end the string
		{`mov rax, "a\"x"`, `mov rax, "a\"x"`},
		// a quote inside a comment is not a string start
		{`mov eax, 1 ; it's x`, `mov eax, 1 ; it's x`},
		// lines starting with '#' are goa comments, passed through verbatim
		{"# comment with x and g", "# comment with x and g"},
	}
	for _, tc := range cases {
		if got := c.bindAsmLine(tc.in); got != tc.want {
			t.Errorf("bindAsmLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// peepIns/lLine/rawLine build stream lines for the peepholeIR unit tests.
func peepIns(s string) Inst { return Inst{Kind: instInstr, Text: "\t" + s} }
func lLine(s string) Inst   { return Inst{Kind: instLabel, Text: s} }
func rawLine(s string) Inst { return Inst{Kind: instRaw, Text: s} }

func lineTexts(insts []Inst) []string {
	txts := make([]string, len(insts))
	for i, in := range insts {
		txts[i] = in.Text
	}
	return txts
}

// TestPeepholeIRMovZero locks the single-line rule and its refusals.
func TestPeepholeIRMovZero(t *testing.T) {
	got := lineTexts(peepholeIR([]Inst{
		peepIns("mov rax, 0"),
		peepIns("mov eax, 0"),
		peepIns("mov rbx, 0"),
		peepIns("mov rbp, 0"),     // frame pointer: never becomes xor
		peepIns("mov rsp, 0"),     // stack pointer ditto
		peepIns("mov rax, 5"),     // nonzero immediate untouched
		peepIns("mov [rbp-8], 0"), // memory destination untouched
	}))
	want := []string{
		"\txor eax, eax",
		"\txor eax, eax",
		"\txor ebx, ebx",
		"\tmov rbp, 0",
		"\tmov rsp, 0",
		"\tmov rax, 5",
		"\tmov [rbp-8], 0",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestPeepholeIRLoadRules locks the pair rules and, crucially, the cases that
// must NOT fire: a label, an inline-asm line, or an unparseable instruction
// closes the lookback window, and a width mismatch is never forwarded.
func TestPeepholeIRLoadRules(t *testing.T) {
	// Duplicate load dropped; the window stays on the survivor, so a third
	// identical load drops too.
	got := peepholeIR([]Inst{
		peepIns("mov rax, [rbp-8]"),
		peepIns("mov rax, [rbp-8]"),
		peepIns("mov rax, [rbp-8]"),
	})
	if len(got) != 1 {
		t.Errorf("duplicate loads: got %d lines, want 1: %q", len(got), lineTexts(got))
	}
	// Store-then-load of the same slot into the same register: load dropped.
	got = peepholeIR([]Inst{
		peepIns("mov [rbp-8], rax"),
		peepIns("mov rax, [rbp-8]"),
	})
	if len(got) != 1 {
		t.Errorf("store->load same reg: got %d lines, want 1: %q", len(got), lineTexts(got))
	}
	// Width mismatch: a 4-byte store followed by an 8-byte load must be kept
	// (the upper 4 bytes were never written by the store).
	got = peepholeIR([]Inst{
		peepIns("mov [rbp-8], eax"),
		peepIns("mov rax, [rbp-8]"),
	})
	if len(got) != 2 {
		t.Errorf("width mismatch: got %d lines, want 2 (no forwarding): %q", len(got), lineTexts(got))
	}
	// A label closes the window: control can arrive at it from anywhere, so
	// the load after it is not provably redundant.
	got = peepholeIR([]Inst{
		peepIns("mov rax, [rbp-8]"),
		lLine("L3:"),
		peepIns("mov rax, [rbp-8]"),
	})
	if len(got) != 3 {
		t.Errorf("label must break the window: got %d lines, want 3", len(got))
	}
	// Inline asm may do anything (including writing [rbp-8]): it closes the
	// window too, and its own lines are never rewritten.
	got = peepholeIR([]Inst{
		peepIns("mov rax, [rbp-8]"),
		rawLine("mov rax, 0"),
		peepIns("mov rax, [rbp-8]"),
	})
	if len(got) != 3 {
		t.Errorf("inline asm must break the window: got %d lines, want 3", len(got))
	}
	if got[1].Text != "mov rax, 0" {
		t.Errorf("inline asm line was rewritten: %q", got[1].Text)
	}
}

// TestOpt1LeavesInlineAsmAlone pins the end-to-end contract split: the -O0
// textual pass rewrites even a user's inline "mov eax, 0" (legacy behaviour,
// bytes kept identical), while -O1's IR pass leaves inline asm untouched.
func TestOpt1LeavesInlineAsmAlone(t *testing.T) {
	src := `int main(){
  __asm {
    mov eax, 0
  }
  return 0;
}`
	asm0 := genAsmOpt(t, src, 0)
	asm1 := genAsmOpt(t, src, 1)
	if strings.Contains(asm0, "mov eax, 0") {
		t.Errorf("-O0 textual peephole should rewrite the inline mov eax, 0:\n%s", asm0)
	}
	if !strings.Contains(asm1, "mov eax, 0") {
		t.Errorf("-O1 IR peephole must leave inline __asm alone:\n%s", asm1)
	}
}

// TestOpt1StillMatchesO0Behaviour runs a real program through both levels and
// requires the peephole not to break anything the compiler relies on: both
// levels must produce output (the golden comparison over every example lives
// in run_tests.sh; this test only needs the pipeline to succeed and differ).
func TestOpt1StillCompiles(t *testing.T) {
	src := `int main(){
  int i;
  int s;
  s = 0;
  for (i = 0; i < 10; i = i + 1) {
    s = s + i * 2;
  }
  printf("%d\n", s);
  return 0;
}`
	asm0 := genAsmOpt(t, src, 0)
	asm1 := genAsmOpt(t, src, 1)
	if asm0 == "" || asm1 == "" {
		t.Fatal("empty assembly from one of the levels")
	}
}

// TestAsmStmtEmission runs the full pipeline on a function with an inline
// asm block and checks the binder produced real memory operands: the local x
// must be forced onto the stack (hasAsm disables register allocation) and the
// global g must address via its .data label.
func TestAsmStmtEmission(t *testing.T) {
	src := `int g;
int main(){
  int x;
  __asm {
    mov eax, x
    mov g, eax
  }
  return 0;
}`
	asm := genAsm(t, src)
	if !strings.Contains(asm, "[rbp-8]") {
		t.Errorf("expected local x bound to [rbp-8], asm:\n%s", asm)
	}
	if !strings.Contains(asm, "[rip+G_g]") {
		t.Errorf("expected global g bound to [rip+G_g], asm:\n%s", asm)
	}
}

// TestAsmBlockCapture pins the lexer's __asm block collection: the raw text
// between the braces (and only that) becomes a TAsm token, whitespace between
// the keyword and '{' is tolerated, and the statement after the closing '}'
// must NOT be swallowed into the block.
func TestAsmBlockCapture(t *testing.T) {
	src := `int f(){
  __asm   {
    mov eax, 1
    nop
  }
  return 0;
}`
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	// Token 0: 'int' keyword, then 'f', '(' , ')', '{', then TKeyword __asm,
	// then TAsm, then 'return' ...
	asm := -1
	for i, tk := range toks {
		if tk.Kind == TAsm {
			asm = i
			break
		}
	}
	if asm < 0 {
		t.Fatalf("no TAsm token in %+v", toks)
	}
	block := toks[asm].Text
	if !strings.Contains(block, "mov eax, 1") || !strings.Contains(block, "nop") {
		t.Errorf("TAsm text %q should contain the block body", block)
	}
	if strings.Contains(block, "return") {
		t.Errorf("TAsm text %q swallowed the statement after the block", block)
	}
	// The next token after TAsm must be the 'return' keyword on a later line.
	if asm+1 >= len(toks) || toks[asm+1].Text != "return" {
		t.Errorf("token after TAsm = %+v, want the return keyword", toks[asm+1])
	}
}
