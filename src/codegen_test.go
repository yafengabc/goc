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

// fnAsm extracts the body of one top-level function from generated assembly:
// the lines from its "name:" label up to the next top-level label (a line at
// column 0 ending in ':'). Instructions are tab-indented, so any line that
// starts at column 0 and ends in ':' is a fresh symbol, not code. Used to
// scope assertions to the function under test and ignore helper glue (e.g. the
// Windows _start emits __goclib_get_args, which legitimately contains imul).
func fnAsm(asm, name string) string {
	start := strings.Index(asm, name+":")
	if start < 0 {
		return asm
	}
	rest := asm[start:]
	lines := strings.Split(rest, "\n")
	for i := 1; i < len(lines); i++ {
		ln := lines[i]
		if ln == "" {
			continue
		}
		if (ln[0] == '\t' || ln[0] == ' ') {
			continue
		}
		if strings.HasSuffix(ln, ":") {
			return strings.Join(lines[:i], "\n")
		}
	}
	return rest
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
		varEnts: map[int]varInfo{
			0: {off: -8},
			1: {off: 16},
			2: {off: -16, reg: "rbx"},
		},
		scopes: []map[string]int{
			{"x": 0, "arg": 1, "rv": 2},
		},
		declUID: map[*DeclStmt]int{},
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

// -Os is the size-first level: inlining is the only pass that grows the
// program (measured +17KB across the example set), so it is the one pass
// -Os skips. Every other cleanup pass still runs, which the -Os legs in
// run_tests.sh pin behaviourally; this test pins the level split itself.
func TestOsKeepsCalls(t *testing.T) {
	src := `int add3(int a){ return a + 3; }
int main(){
    int buf[2];
    buf[0] = add3(4);
    buf[0] = add3(5);
    return buf[0];
}`
	asm1 := genAsmOpt(t, src, 1)
	asmS := genAsmOpt(t, src, 2)
	if strings.Contains(asm1, "call add3") {
		t.Errorf("-O1 must inline add3:\n%s", asm1)
	}
	if got := strings.Count(asmS, "call add3"); got != 2 {
		t.Errorf("-Os must keep both call sites, got %d:\n%s", got, asmS)
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

// TestConstPropForward locks the forwarding behaviour: constants stored into
// slots reach later loads into ANY destination register, chains propagate,
// and a load whose destination already holds the value disappears entirely.
func TestConstPropForward(t *testing.T) {
	got := lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),  // [rbp-8] = 7
		peepIns("mov rax, 5"),        // rax overwritten
		peepIns("mov [rbp-16], rax"), // [rbp-16] = 5
		peepIns("mov rcx, [rbp-8]"),  // -> mov rcx, 7 (arg-load shape)
		peepIns("mov rdx, [rbp-16]"), // -> mov rdx, 5
		peepIns("mov rax, [rbp-8]"),  // rax holds 5, slot 7 -> mov rax, 7
	}))
	want := []string{
		"\tmov rax, 7",
		"\tmov [rbp-8], rax",
		"\tmov rax, 5",
		"\tmov [rbp-16], rax",
		"\tmov rcx, 7",
		"\tmov rdx, 5",
		"\tmov rax, 7",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}

	// The load disappears when the destination already holds the constant --
	// the adjacent store->load shape peepholeIR also handles, subsumed here.
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),
		peepIns("mov rax, [rbp-8]"),
	}))
	if len(got) != 2 {
		t.Fatalf("redundant load: got %d lines, want 2: %q", len(got), got)
	}
	if got[1] != "\tmov [rbp-8], rax" {
		t.Errorf("surviving line = %q", got[1])
	}

	// Negative immediates propagate; a chain through a second slot works
	// because registers are tracked too (rdx keeps its -3 across the store).
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, -3"),
		peepIns("mov [rbp-8], rax"),
		peepIns("mov rdx, [rbp-8]"),  // -> mov rdx, -3
		peepIns("mov [rbp-16], rdx"), // [rbp-16] = -3 via tracked rdx
		peepIns("mov rcx, [rbp-16]"), // -> mov rcx, -3
	}))
	if got[2] != "\tmov rdx, -3" {
		t.Errorf("negative constant not forwarded: %q", got[2])
	}
	if got[4] != "\tmov rcx, -3" {
		t.Errorf("chain through a tracked register broken: %q", got[4])
	}
}

// TestConstPropKills locks the invalidation rules: labels, calls, ALU ops,
// indirect or sized stores, and inline asm all wipe the knowledge; a lea or
// a register copy does not (they write no memory).
func TestConstPropKills(t *testing.T) {
	// label: knowledge must hold on all incoming paths -> gone
	got := lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),
		lLine("L1:"),
		peepIns("mov rcx, [rbp-8]"),
	}))
	if got[3] != "\tmov rcx, [rbp-8]" {
		t.Errorf("knowledge survived a label: %q", got[3])
	}
	// call: callee may store through pointers to any slot
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),
		peepIns("call printf"),
		peepIns("mov rcx, [rbp-8]"),
	}))
	if got[3] != "\tmov rcx, [rbp-8]" {
		t.Errorf("knowledge survived a call: %q", got[3])
	}
	// indirect store through a register: may alias any slot. The rax value
	// itself survives (the store does not write rax).
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),
		peepIns("mov [r10], rax"),
		peepIns("mov rcx, [rbp-8]"),
	}))
	if got[3] != "\tmov rcx, [rbp-8]" {
		t.Errorf("knowledge survived an indirect store: %q", got[3])
	}
	// sized store: writes only part of a slot -> invalidate, never mislead
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),
		peepIns("mov byte [rbp-3], al"),
		peepIns("mov rcx, [rbp-8]"),
	}))
	if got[3] != "\tmov rcx, [rbp-8]" {
		t.Errorf("knowledge survived a sized store: %q", got[3])
	}
	// ALU op on rax: the register no longer holds the constant, so a later
	// load of the still-known slot must materialise it again (not be dropped)
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),
		peepIns("add rax, 1"),
		peepIns("mov rax, [rbp-8]"),
	}))
	if len(got) != 4 || got[3] != "\tmov rax, 7" {
		t.Errorf("want the load rewritten after an intervening add, got %q", got)
	}
	// inline asm: closes everything, its own lines untouched
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),
		rawLine("mov rax, 9"),
		peepIns("mov rcx, [rbp-8]"),
	}))
	if got[3] != "\tmov rcx, [rbp-8]" {
		t.Errorf("knowledge survived inline asm: %q", got[3])
	}
	// lea does not write memory: slot knowledge survives across it
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 7"),
		peepIns("mov [rbp-8], rax"),
		peepIns("lea r10, [rip+G_g]"),
		peepIns("mov rcx, [rbp-8]"),
	}))
	if got[3] != "\tmov rcx, 7" {
		t.Errorf("knowledge did not survive a lea: %q", got[3])
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

// TestInlineExpands drives inlineCalls end to end on a miniature program:
// the leaf `add` gets a template, main's call site is replaced by the
// remapped body followed by the continuation label, and the prologue's
// `sub rsp` grows by the inlined frame need.
func TestInlineExpands(t *testing.T) {
	insts := []Inst{
		lLine("_start:"),
		peepIns("and rsp, -16"),
		peepIns("sub rsp, 48"),
		peepIns("call main"),
		peepIns("mov rcx, rax"),
		peepIns("call ExitProcess"),
		lLine("add:"),
		peepIns("push rbp"),
		peepIns("mov rbp, rsp"),
		peepIns("sub rsp, 304"),
		peepIns("mov [rbp-8], rcx"),
		peepIns("mov rax, [rbp-8]"),
		peepIns("shl rax, 32"),
		peepIns("mov rsp, rbp"),
		peepIns("pop rbp"),
		peepIns("ret"),
		lLine("main:"),
		peepIns("push rbp"),
		peepIns("mov rbp, rsp"),
		peepIns("sub rsp, 288"),
		peepIns("mov rcx, 3"),
		peepIns("call add"),
		peepIns("mov [rbp-16], rax"),
		peepIns("xor eax, eax"),
		peepIns("mov rsp, rbp"),
		peepIns("pop rbp"),
		peepIns("ret"),
	}
	got := lineTexts(inlineCalls(insts))
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "call add") {
		t.Fatalf("call add was not expanded:/n%s", joined)
	}
	// main's frame grows by add's need (maxSlot 8 -> 16 aligned): 288+304.
	if !strings.Contains(joined, "\tsub rsp, 304") {
		t.Errorf("main prologue frame did not grow")
	}
	// add's body lands inside main with slots remapped: main's deepest slot
	// is [rbp-16], so the inline area starts at base = 16+8 = 24 and add's
	// [rbp-8] becomes [rbp-24]; its epilogue became the continuation jump.
	want := []string{
		"\tmov rcx, 3",
		"\tmov [rbp-24], rcx",
		"\tmov rax, [rbp-24]",
		"\tshl rax, 32",
		"\tjmp .L__inl0",
		".L__inl0:",
		"\tmov [rbp-16], rax",
	}
	pos := 0
	for _, w := range want {
		found := -1
		for i := pos; i < len(got); i++ {
			if got[i] == w {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("expected %q after line %d, got:/n%s", w, pos, joined)
		}
		pos = found + 1
	}
}

// TestInlineRejects pins the safety screen: a body containing a call, an
// inline-asm line, a callee-save write, a stack-argument offset or a push
// must not become a template.
func TestInlineRejects(t *testing.T) {
	base := func(extra ...Inst) []Inst {
		insts := []Inst{
			lLine("add:"),
			peepIns("push rbp"),
			peepIns("mov rbp, rsp"),
			peepIns("sub rsp, 32"),
			peepIns("mov [rbp-8], rcx"),
			peepIns("mov rax, [rbp-8]"),
			peepIns("mov rsp, rbp"),
			peepIns("pop rbp"),
			peepIns("ret"),
			lLine("main:"),
			peepIns("push rbp"),
			peepIns("mov rbp, rsp"),
			peepIns("sub rsp, 64"),
			peepIns("call add"),
			peepIns("mov rsp, rbp"),
			peepIns("pop rbp"),
			peepIns("ret"),
		}
		// insert the suspicious instruction before add's epilogue
		return append(insts[:8], append(extra, insts[8:]...)...)
	}
	for _, tc := range []struct {
		name  string
		extra []Inst
	}{
		{"call", []Inst{peepIns("call printf")}},
		{"raw", []Inst{rawLine("\tmov eax, 5")}},
		{"callee-save", []Inst{peepIns("mov rbx, rax")}},
		{"stack-arg", []Inst{peepIns("mov rax, [rbp+16]")}},
		{"push", []Inst{peepIns("push rax")}},
		{"ret", []Inst{peepIns("ret")}},
	} {
		got := lineTexts(inlineCalls(base(tc.extra...)))
		if !contains(got, "\tcall add") {
			t.Errorf("%s: call add was expanded, want rejected", tc.name)
		}
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// TestConstPropFold locks the ALU evaluation: known operands fold the
// destination value (the instruction still emits -- flags stay put), a
// partially-unknown operand kills the destination, and the shl/sar int
// normalisation pair composes with the fold.
func TestConstPropFold(t *testing.T) {
	got := lineTexts(constProp([]Inst{
		peepIns("mov rax, 3"),
		peepIns("mov r10, 4"),
		peepIns("add rax, r10"),     // rax = 7, instruction kept
		peepIns("mov rcx, [rbp-8]"), // slot unknown: no forward
	}))
	want := []string{
		"\tmov rax, 3",
		"\tmov r10, 4",
		"\tadd rax, r10", // still emitted for its flag effects
		"\tmov rcx, [rbp-8]",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	// The folded value feeds later loads of the slot it was stored to.
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 3"),
		peepIns("mov r10, 4"),
		peepIns("add rax, r10"),
		peepIns("shl rax, 32"), // 7<<32 known
		peepIns("sar rax, 32"), // back to 7: int normalisation composes
		peepIns("mov [rbp-8], rax"),
		peepIns("mov rdx, [rbp-8]"), // -> mov rdx, 7
	}))
	if got[6] != "\tmov rdx, 7" {
		t.Errorf("folded value did not reach the later load: %q", got[6])
	}
	// An unknown second operand invalidates only the destination; cmp/test
	// write flags only and disturb nothing.
	got = lineTexts(constProp([]Inst{
		peepIns("mov rax, 3"),
		peepIns("add rax, rcx"), // rcx unknown: rax dies
		peepIns("mov r10, 5"),
		peepIns("cmp rax, 5"),
		peepIns("mov [rbp-8], r10"),
		peepIns("mov rdx, [rbp-8]"), // -> mov rdx, 5
	}))
	if got[5] != "\tmov rdx, 5" {
		t.Errorf("cmp disturbed register knowledge: %q", got[5])
	}
}

// TestDeadStores locks the coverage rule: a store covered by a later
// full-width store to the same slot disappears once nothing reads it in
// between; a read (even via lea) saves it; labels, calls, jumps, inline asm
// and any indirect/sized memory operand block the elimination entirely.
func TestDeadStores(t *testing.T) {
	// covered: the first store is unread when the second lands
	got := lineTexts(deadStores([]Inst{
		peepIns("mov [rbp-8], rax"),  // dead
		peepIns("mov rcx, 5"),        // unrelated register work
		peepIns("mov [rbp-8], rcx"),  // covers it
		peepIns("mov rdx, [rbp-16]"), // reads another slot, no interference
	}))
	if len(got) != 3 || got[0] != "\tmov rcx, 5" {
		t.Errorf("covered store survived: %q", got)
	}
	// a read in between saves the store
	got = lineTexts(deadStores([]Inst{
		peepIns("mov [rbp-8], rax"),
		peepIns("mov rdx, [rbp-8]"), // reads it
		peepIns("mov [rbp-8], rcx"),
	}))
	if len(got) != 3 {
		t.Errorf("read-backed store was dropped: %q", got)
	}
	// lea takes the address: the callee may write through it
	got = lineTexts(deadStores([]Inst{
		peepIns("mov [rbp-8], rax"),
		peepIns("lea rcx, [rbp-8]"),
		peepIns("mov [rbp-8], rcx"),
	}))
	if len(got) != 3 {
		t.Errorf("address-taken store was dropped: %q", got)
	}
	// a label before the covering store: other paths may read the slot
	got = lineTexts(deadStores([]Inst{
		peepIns("mov [rbp-8], rax"),
		lLine(".L1:"),
		peepIns("mov [rbp-8], rcx"),
	}))
	if len(got) != 3 {
		t.Errorf("store crossed by a label was dropped: %q", got)
	}
	// a sized store is no covering store (it rewrites only part of the slot)
	got = lineTexts(deadStores([]Inst{
		peepIns("mov [rbp-8], rax"),
		peepIns("mov byte [rbp-8], cl"),
	}))
	if len(got) != 2 {
		t.Errorf("sized store treated as covering: %q", got)
	}
}

// TestCastDerefWidth locks the cast-dereference load width: *(int *)p must
// load 4 bytes (mov eax, [..]) even though the cast expression's OWN type is
// the 8-byte pointer. Regression for the shared array-print skeleton, where
// __goclib_conv_int read 8 bytes and printed 51539607563 for {11,12,13}.
func TestCastDerefWidth(t *testing.T) {
	// int deref: 4-byte load with sign extension
	asm := genAsm(t, `int a[1] = {7};
int main(){
  int v;
  v = *(int *)&a[0];
  return v;
}`)
	if !strings.Contains(asm, "mov eax, [rax]") {
		t.Errorf("*(int *)p did not emit a 4-byte load: %q", asm)
	}
	if strings.Contains(asm, "mov rax, [rax]") {
		t.Errorf("*(int *)p emitted an 8-byte load: %q", asm)
	}
	// short deref: 2-byte load
	asm = genAsm(t, `short a[1] = {7};
int main(){
  int v;
  v = *(short *)&a[0];
  return v;
}`)
	if !strings.Contains(asm, "mov ax, [rax]") {
		t.Errorf("*(short *)p did not emit a 2-byte load: %q", asm)
	}
	// char deref: 1-byte load
	asm = genAsm(t, `char a[1] = {7};
int main(){
  int v;
  v = *(char *)&a[0];
  return v;
}`)
	if !strings.Contains(asm, "mov al, [rax]") {
		t.Errorf("*(char *)p did not emit a 1-byte load: %q", asm)
	}
	// double deref through a cast: must take the double path (movsd) even
	// though elemClassOf used to report TInt for cast expressions.
	asm = genAsm(t, `double a[1] = {1.5};
int main(){
  double v;
  v = *(double *)&a[0];
  return (int)v;
}`)
	if !strings.Contains(asm, "movsd xmm0, [rax]") {
		t.Errorf("*(double *)p did not emit a double load: %q", asm)
	}
	// long deref keeps the 8-byte load -- a long* cast really is 8 bytes
	asm = genAsm(t, `long a[1] = {7};
int main(){
  long v;
  v = *(long *)&a[0];
  return (int)v;
}`)
	if !strings.Contains(asm, "mov rax, [rax]") {
		t.Errorf("*(long *)p did not emit an 8-byte load: %q", asm)
	}
}

func TestSizeofStringLiteral(t *testing.T) {
	// A string literal is a char[N] array, so sizeof("abc") is 4 -- three
	// characters plus the terminator. It used to report 8 because exprType
	// decays the literal to char*, and sizeof does not use its operand as a
	// value. cJSON's strdup is "strlen(s) + sizeof("")", which then copied
	// (and later printed) seven bytes of neighbouring memory.
	asm := genAsm(t, `int main(){ return (int)sizeof("abc"); }`)
	if !strings.Contains(asm, "mov rax, 4") {
		t.Errorf("sizeof(\"abc\") did not fold to 4: %q", asm)
	}
	asm = genAsm(t, `int main(){ return (int)sizeof(""); }`)
	if !strings.Contains(asm, "mov rax, 1") {
		t.Errorf("sizeof(\"\") did not fold to 1: %q", asm)
	}
	// sizeof on the type name, and on a pointer expression, stay as they were
	asm = genAsm(t, `int main(){ return (int)sizeof(char *); }`)
	if !strings.Contains(asm, "mov rax, 8") {
		t.Errorf("sizeof(char *) did not stay 8: %q", asm)
	}
}

func TestPtrArithIndexWidth(t *testing.T) {
	// "(p + n)[i]" keeps the pointer's element width, so indexing an
	// "unsigned char *" loads one byte. This is the shape cJSON's
	// `#define at(b) ((b)->content + (b)->offset)` macro expands to; the
	// default 8-byte load made every character-class switch in the parser
	// read a whole quadword and fall through to its default label, so
	// parse_number gave up on the very first digit.
	asm := genAsm(t, `
struct B { const unsigned char *content; long length; long offset; };
int main(){
  struct B b;
  int v;
  b.content = (const unsigned char *)"123";
  b.length = 3;
  b.offset = 0;
  v = (b.content + b.offset)[0];
  return v;
}`)
	// "(p + n)[0]" must stride by 1 byte and load 1 byte out of the
	// unsigned char* -- the old code defaulted the element width to 8.
	if !strings.Contains(asm, "imul r11, 1") {
		t.Errorf("(p + n)[i] on an unsigned char* did not stride by 1: %q", asm)
	}
	if !strings.Contains(asm, "mov al, [r10]") {
		t.Errorf("(p + n)[i] on an unsigned char* did not emit a 1-byte load: %q", asm)
	}
	// An int* still strides and loads 4 bytes
	asm = genAsm(t, `
int main(){
  int a[4] = {1, 2, 3, 4};
  int v;
  v = (a + 2)[0];
  return v;
}`)
	if !strings.Contains(asm, "imul r11, 4") {
		t.Errorf("(a + 2)[0] on an int* did not stride by 4: %q", asm)
	}
	if !strings.Contains(asm, "mov eax, [r10]") {
		t.Errorf("(a + 2)[0] on an int* did not emit a 4-byte load: %q", asm)
	}
}

func TestBinaryTypePtrArith(t *testing.T) {
	// exprType had no *Binary branch, so the result of pointer arithmetic was
	// typed nil. "(p + 1) - base" then classified its left operand as an
	// integer, took genBinary's "integer - pointer" swap path and produced the
	// negated difference. cJSON's parse_string bounds-checks every escape
	// sequence with exactly this shape, so one escaped string anywhere in a
	// JSON document made the whole parse fail.
	asm := genAsm(t, `
int main(){
  const unsigned char *base = (const unsigned char *)"abcdefgh";
  const unsigned char *p = base + 3;
  long d;
  d = (long)(p + 1 - base);
  return (int)d;
}`)
	// Scope the checks to main: the Windows _start also emits
	// __goclib_get_args, whose malloc-size arithmetic legitimately uses imul,
	// and that must not trip the no-scaling assertion below.
	main := fnAsm(asm, "main")
	// The swap path reorders the operands before subtracting.
	if strings.Contains(main, "mov r11, rax\n\tmov rax, r10\n\tmov r10, r11") {
		t.Errorf("(p + 1) - base took the integer-minus-pointer swap path: %q", main)
	}
	// Pointer minus pointer: one subtraction, no element scaling (the
	// pointed-to type is unsigned char, so the stride is 1).
	if !strings.Contains(main, "sub r10, rax\n\tmov rax, r10") {
		t.Errorf("(p + 1) - base did not emit a pointer difference: %q", main)
	}
	if strings.Contains(main, "imul") {
		t.Errorf("(p + 1) - base scaled the difference by an element size: %q", main)
	}
}
