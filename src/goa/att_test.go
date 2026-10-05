package goa

import (
	"fmt"
	"strings"
	"testing"
)

// The AT&T front end's whole job is to turn GAS syntax into goa's own without
// changing the machine code. The strongest available check is therefore an
// equivalence test: assemble the same program written both ways and require the
// emitted bytes to be identical.
//
// The Intel spelling in each case is written out by hand on purpose. Deriving
// it from the front end under test would make the test a tautology -- it would
// pass no matter what the translation did, since both sides would move
// together. Spelling it by hand is what makes a byte diff mean "the front end
// is wrong" rather than "the two copies of the front end agree".

// attEquiv assembles one program twice -- once in goa syntax, once in AT&T --
// and compares the bytes of the .text section.
func attEquiv(t *testing.T, name, goaSrc, attSrc string) {
	t.Helper()

	goaAsm := NewAssembler()
	if err := goaAsm.Assemble(goaSrc); err != nil {
		t.Fatalf("%s: goa syntax failed: %v\n--- goa source ---\n%s", name, err, goaSrc)
	}
	attAsm := NewAssembler()
	if err := attAssemble(attAsm, attSrc); err != nil {
		t.Fatalf("%s: AT&T syntax failed: %v\n--- att source ---\n%s", name, err, attSrc)
	}

	g := goaAsm.sectionByName(".text")
	s := attAsm.sectionByName(".text")
	if g == nil || s == nil {
		t.Fatalf("%s: missing .text (goa=%v att=%v)", name, g != nil, s != nil)
	}
	if hex(g.Data) != hex(s.Data) {
		t.Errorf("%s: encodings differ\n  goa: %s\n  att: %s\n--- att source ---\n%s",
			name, hex(g.Data), hex(s.Data), attSrc)
	}
}

// attCase is one instruction written both ways.
type attCase struct {
	name string
	att  string // GAS spelling
	goa  string // goa/Intel spelling, hand-written
}

// attEquivEach runs the equivalence check over a batch of single-instruction
// cases, wrapping each in a minimal labelled function.
func attEquivEach(t *testing.T, cases []attCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			goaSrc := "section .text\nf:\n\t" + c.goa + "\n"
			attSrc := "\t.text\n\t.globl f\nf:\n" + c.att + "\n"
			attEquiv(t, c.name, goaSrc, attSrc)
		})
	}
}

func hex(bs []byte) string {
	var b strings.Builder
	for i, c := range bs {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%02x", c)
	}
	return b.String()
}

func TestATTMoveForms(t *testing.T) {
	attEquivEach(t, []attCase{
		// AT&T reads source-first, so `movq %rax, %rcx` stores rax into rcx --
		// which in Intel syntax is `mov rcx, rax`. Getting this backwards is
		// the single most damaging bug this front end could have, so every
		// case below states the destination explicitly.
		{"mov reg,reg 64", "\tmovq\t%rax, %rcx", "mov rcx, rax"},
		{"mov reg,reg 32", "\tmovl\t%eax, %ecx", "mov ecx, eax"},
		{"mov reg,reg 16", "\tmovw\t%ax, %cx", "mov cx, ax"},
		{"mov reg,reg 8", "\tmovb\t%al, %cl", "mov cl, al"},
		{"mov imm32->reg", "\tmovl\t$-11, %ecx", "mov ecx, -11"},
		{"movabs imm64", "\tmovabsq\t$8589934593, %rax", "mov rax, 8589934593"},
		{"mov mem->reg", "\tmovq\t-8(%rbp), %rax", "mov rax, [rbp-8]"},
		{"mov reg->mem", "\tmovq\t%rax, -8(%rbp)", "mov [rbp-8], rax"},
		{"mov rip sym", "\tmovq\tG_g_dec(%rip), %rcx", "mov rcx, [rip+G_g_dec]"},
		{"movzx byte", "\tmovzbl\t%al, %eax", "movzx eax, al"},
		{"movsx 64<-32", "\tmovslq\t%eax, %rax", "movsxd rax, eax"},
		{"lea disp", "\tleaq\t-8(%rbp), %rax", "lea rax, [rbp-8]"},
		{"lea indexed", "\tleaq\t40(%rsp,%rax,4), %r9", "lea r9, [rsp+rax*4+40]"},
		{"lea sym rip", "\tleaq\tG_v(%rip), %rax", "lea rax, [rip+G_v]"},
	})
}

// TestATTMovQDisambiguation pins the one genuinely ambiguous case. `movq` is a
// 64-bit GPR move in `movq %rsi, %rcx` but an SSE quadword move in
// `movq %xmm0, %rax`; the front end must decide from the operands, not the
// name, or one of the two silently mis-assembles.
func TestATTMovQDisambiguation(t *testing.T) {
	t.Run("gpr pair", func(t *testing.T) {
		// `movq %rsi, %rcx` stores rsi into rcx.
		attEquiv(t, "movq gpr", "section .text\nf:\n\tmov rcx, rsi\n",
			"\t.text\nf:\n\tmovq\t%rsi, %rcx\n")
	})
	t.Run("xmm to gpr", func(t *testing.T) {
		attEquiv(t, "movq xmm", "section .text\nf:\n\tmovq rax, xmm0\n",
			"\t.text\nf:\n\tmovq\t%xmm0, %rax\n")
	})
}

func TestATTArithAndFlags(t *testing.T) {
	attEquivEach(t, []attCase{
		{"add imm", "\taddq\t$8, %rax", "add rax, 8"},
		{"sub reg", "\tsubq\t%rax, %rcx", "sub rcx, rax"},
		{"and imm", "\tandl\t$255, %eax", "and eax, 255"},
		{"xor self", "\txorl\t%eax, %eax", "xor eax, eax"},
		{"cmp imm", "\tcmpl\t$5, %eax", "cmp eax, 5"},
		{"test pair", "\ttestq\t%rax, %rax", "test rax, rax"},
		{"inc reg", "\tincl\t%eax", "inc eax"},
		{"dec mem", "\tdecl\t-4(%rbp)", "dec dword [rbp-4]"},
		{"neg reg", "\tnegq\t%rcx", "neg rcx"},
		{"not reg", "\tnotl\t%edx", "not edx"},
		{"imul2 reg", "\timulq\t%rdi, %rsi", "imul rsi, rdi"},
		{"idiv", "\tidivq\t%rdi", "idiv rdi"},
		{"mul", "\tmulq\t%rdi", "mul rdi"},
		{"sar imm", "\tsarq\t$3, %rax", "sar rax, 3"},
		{"shr imm", "\tshrl\t$2, %ecx", "shr ecx, 2"},
		{"shl imm", "\tshlq\t$1, %rdx", "shl rdx, 1"},
		{"xchg", "\txchgq\t%rax, %rcx", "xchg rcx, rax"},
		{"bswap", "\tbswapq\t%rax", "bswap rax"},
		{"or reg", "\torq\t%r10, %rax", "or rax, r10"},
	})
}

// TestATTImulThreeOperand is the case that a naive "flip every two-operand
// instruction" rule gets wrong: the three-operand imul carries its
// destination last in *both* syntaxes, so flipping it computes a different
// product entirely.
func TestATTImulThreeOperand(t *testing.T) {
	attEquiv(t, "imul 3-operand",
		"section .text\nf:\n\timul r9, r8, 1374389535\n",
		"\t.text\nf:\n\timulq\t$1374389535, %r8, %r9\n")
}

func TestATTControlFlow(t *testing.T) {
	// Each case needs its branch targets to exist in both spellings.
	cases := []attCase{
		{"ret", "\tretq", "ret"},
		{"leave", "\tleave", "leave"},
		{"push", "\tpushq\t%rbp", "push rbp"},
		{"pop", "\tpopq\t%rbp", "pop rbp"},
		{"call sym", "\tcallq\tmain", "call main"},
		{"jmp", "\tjmp\t.LB2", "jmp .LB2"},
		{"je", "\tje\t.LB3", "je .LB3"},
		{"jne", "\tjne\t.LB4", "jne .LB4"},
		{"jle", "\tjle\t.LB5", "jle .LB5"},
		{"jrcxz", "\tjrcxz\t.LB6", "jrcxz .LB6"},
		{"sete", "\tsete\t%al", "sete al"},
		{"setne", "\tsetne\t%r10b", "setne r10b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			goaSrc := "section .text\nf:\n\t" + c.goa + "\n.LB2:\n.LB3:\n.LB4:\n.LB5:\n.LB6:\n"
			attSrc := "\t.text\nf:\n" + c.att + "\n.LB2:\n.LB3:\n.LB4:\n.LB5:\n.LB6:\n"
			attEquiv(t, c.name, goaSrc, attSrc)
		})
	}
}

func TestATTIndirectBranch(t *testing.T) {
	attEquivEach(t, []attCase{
		{"call reg", "\tcallq\t*%rax", "call rax"},
		{"jmp reg", "\tjmpq\t*%r10", "jmp r10"},
		{"call mem", "\tcallq\t*8(%rbp)", "call [rbp+8]"},
	})
}

// TestATTSignExtend covers LLVM's no-operand sign-extension idioms.
//
// The AT&T names describe the *source* width, so they read backwards against
// the Intel ones: `cqto` widens eax into edx:eax and is therefore the 64-bit
// `cqo` (REX.W 99), while `cwtq`/`cltq` widen ax/eax into `cwde`/`cdqe`.
// Translating `cqto` to a 32-bit `cdq` would leave the upper half of rdx
// undefined -- a silent wrong answer, not a compile error.
func TestATTSignExtend(t *testing.T) {
	attEquivEach(t, []attCase{
		{"cqto", "\tcqto", "cqo"},
		{"cltq", "\tcltq", "cdqe"},
		{"cwtq", "\tcwtq", "cwde"},
	})
}

// TestATTOperandSplitting is the parsing test that matters most: an AT&T
// memory operand contains commas of its own, so a naive split on every comma
// would tear `-8(%rbp,%rbx,4), %eax` apart and silently lose the addressing.
func TestATTOperandSplitting(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"%rax, %rcx", []string{"%rax", "%rcx"}},
		{"-8(%rbp,%rbx,4), %eax", []string{"-8(%rbp,%rbx,4)", "%eax"}},
		{"(%rax,%rbx,8)", []string{"(%rax,%rbx,8)"}},
		{`$1, "a,b"`, []string{"$1", `"a,b"`}},
		{"$1374389535, %r8, %r9", []string{"$1374389535", "%r8", "%r9"}},
	}
	for _, c := range cases {
		got := attSplitOperands(c.in)
		if len(got) != len(c.want) {
			t.Errorf("split %q: got %d parts %q, want %d %q",
				c.in, len(got), got, len(c.want), c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("split %q part %d: got %q want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestATTMemoryRewrite(t *testing.T) {
	cases := []struct{ in, want string }{
		{"-8(%rbp)", "[rbp-8]"},
		{"(%rax)", "[rax]"},
		{"40(%rsp,%rax,4)", "[rsp+rax*4+40]"},
		{"8(%rsp,%r9,8)", "[rsp+r9*8+8]"},
		{"G_x(%rip)", "[rip+G_x]"},
		{"G_x", "G_x"},
		{"%rax", "rax"},
		{"$-42", "-42"},
		{"$0x2a", "42"},
	}
	for _, c := range cases {
		got, err := attOperand(c.in, attDefaultAliases())
		if err != nil {
			t.Errorf("operand %q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("operand %q: got %q want %q", c.in, got, c.want)
		}
	}
}

// TestATTDirectiveParsing covers the GAS directives LLVM actually emits: the
// COFF .def block, the SEH unwind block, the alignment directives, and the
// `@feat.00 = 0` module-level constant.
func TestATTDirectiveParsing(t *testing.T) {
	src := "\t.def\t@feat.00;\n" +
		"\t.scl\t3;\n" +
		"\t.type\t0;\n" +
		"\t.endef\n" +
		"\t.globl\t@feat.00\n" +
		"@feat.00 = 0\n" +
		"\t.att_syntax\n" +
		"\t.file\t\"goc.ll\"\n" +
		"\t.text\n" +
		"\t.globl\tmain\n" +
		"\t.p2align\t4\n" +
		"main:\n" +
		"\t.seh_proc main\n" +
		"\tpushq\t%rbp\n" +
		"\t.seh_pushreg %rbp\n" +
		"\tsubq\t$40, %rsp\n" +
		"\t.seh_stackalloc 40\n" +
		"\t.seh_endprologue\n" +
		"\tmovl\t$0, %eax\n" +
		"\t.seh_startepilogue\n" +
		"\taddq\t$40, %rsp\n" +
		"\tpopq\t%rbp\n" +
		"\t.seh_endepilogue\n" +
		"\tretq\n" +
		"\t.seh_endproc\n"

	a := NewAssembler()
	if err := attAssemble(a, src); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if _, ok := a.syms["main"]; !ok {
		t.Errorf("symbol main not defined (have %v)", a.syms)
	}
	if v, ok := a.consts["@feat.00"]; !ok || v != 0 {
		t.Errorf("@feat.00 constant not recorded (consts=%v)", a.consts)
	}
	text := a.sectionByName(".text")
	if text == nil || len(text.Data) == 0 {
		t.Fatal("no code emitted into .text")
	}
	// push rbp (1) ; sub rsp,40 (4) ; mov eax,0 (5) ; add rsp,40 (4) ;
	// pop rbp (1) ; ret (1)
	if len(text.Data) != 16 {
		t.Errorf("unexpected .text length %d (%s)", len(text.Data), hex(text.Data))
	}
	// The unwind record must exist: the Windows loader walks a RUNTIME_FUNCTION
	// table while dispatching an exception through this frame.
	if len(a.uwRecs) == 0 {
		t.Error("no unwind record captured for main")
	}
}

func TestATTDataDirectives(t *testing.T) {
	src := "\t.text\n" +
		"\t.globl\tf\n" +
		"f:\n" +
		"\tretq\n" +
		"\t.section\t.rdata,\"dr\"\n" +
		".Lstr:\n" +
		"\t.asciz\t\"hi\"\n" +
		"\t.long\t0\n" +
		"\t.long\t1065353216\n" +
		"\t.zero\t8\n" +
		"\t.byte\t255\n" +
		"\t.data\n" +
		"\t.align\t3\n" +
		"G_v:\n" +
		"\t.quad\t42\n" +
		"G_p:\n" +
		"\t.quad\tG_v\n"

	a := NewAssembler()
	if err := attAssemble(a, src); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	rd := a.sectionByName(".rdata")
	if rd == nil {
		t.Fatalf("no .rdata section (sections: %v)", a.sectionNames())
	}
	// "hi\0" + long 0 + 1.0f + 8 zero bytes + 0xff
	want := []byte{
		'h', 'i', 0,
		0, 0, 0, 0,
		0, 0, 0x80, 0x3f,
		0, 0, 0, 0, 0, 0, 0, 0,
		0xff,
	}
	if hex(rd.Data) != hex(want) {
		t.Errorf(".rdata:\n got %s\nwant %s", hex(rd.Data), hex(want))
	}
	if _, ok := a.syms["G_v"]; !ok {
		t.Error("G_v not defined")
	}
	if _, ok := a.syms["G_p"]; !ok {
		t.Error("G_p not defined")
	}
	// G_p's slot is a relocated pointer to G_v: dereferenced directly, so it
	// needs the absolute+wide+virtual form (full virtual address, 8 bytes).
	found := false
	for _, f := range a.fixups {
		if f.sym == "G_v" && f.absolute && f.wide {
			found = true
		}
	}
	if !found {
		t.Errorf("no absolute wide fixup for G_p -> G_v (fixups: %v)", a.fixups)
	}
}

// TestATTCommentStripping guards the string-literal case: a '#' inside .asciz
// data is data, not the start of a comment.
func TestATTCommentStripping(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\tmovq\t%rax, %rcx # a comment", "\tmovq\t%rax, %rcx "},
		{"\t.asciz\t\"a#b\"", "\t.asciz\t\"a#b\""},
		{"\t.asciz\t\"a\\\"#b\"", "\t.asciz\t\"a\\\"#b\""},
		{"\tretq", "\tretq"},
	}
	for _, c := range cases {
		if got := stripATTComment(c.in); got != c.want {
			t.Errorf("strip %q:\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestATTStringUnquoting(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"abc"`, "abc"},
		{`"a\nb"`, "a\nb"},
		{`"a\tb"`, "a\tb"},
		{`"q\"q"`, `q"q`},
		{`"a\\b"`, `a\b`},
	}
	for _, c := range cases {
		if got := attUnquote(c.in); got != c.want {
			t.Errorf("unquote %q: got %q want %q", c.in, got, c.want)
		}
	}
}

// TestATTBssSection checks that a .bss switch produces uninitialised storage:
// the section's virtual offset advances while its file bytes stay empty. That
// combination is what keeps a large zero-filled buffer from costing anything on
// disk -- the loader zero-fills it instead.
func TestATTBssSection(t *testing.T) {
	src := "\t.text\nf:\n\tretq\n\t.bss\n\t.align\t8\nG_buf:\n\t.zero\t64\n"
	a := NewAssembler()
	if err := attAssemble(a, src); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	b := a.sectionByName(".bss")
	if b == nil {
		t.Fatalf("no .bss section (sections: %v)", a.sectionNames())
	}
	if !b.Bss {
		t.Error(".bss section not marked Bss")
	}
	if len(b.Data) != 0 {
		t.Errorf(".bss has %d file bytes, want 0 (%s)", len(b.Data), hex(b.Data))
	}
	// The label sits at the start of the buffer; the reservation grows the
	// section past it.
	if got := a.syms["G_buf"]; got.off != 0 {
		t.Errorf("G_buf at offset %d, want 0", got.off)
	}
	if b.cur != 64 {
		t.Errorf(".bss virtual size %d, want 64", b.cur)
	}
}
