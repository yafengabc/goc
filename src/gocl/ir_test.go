package gocl

// Test-side driver for the IR back end.
//
// The generator's correctness is defined by whether LLVM accepts its output and
// whether the resulting program computes the right answer, so both checks live
// here rather than in the generator's own tests. The IR is produced without a
// shared library present -- it is text -- which means these tests run anywhere;
// the ones that need LLVM skip themselves when it is absent.

import (
	"goc/common"
	"goc/frontend"
	"os"
	"strings"
	"testing"
)

// irFromSource runs the whole front end over C source and returns the IR.
//
// It drives the production emitter, translateProgram, rather than a separate
// test-only one: the unit assertions guard the exact IR the -fllvm build
// lowers, so they must track the real code path. The runtime is not linked in
// here (lib is nil), so these fragments are lowered on their own; the full
// program path is covered by the e2e -fllvm leg.
func irFromSource(t *testing.T, src string) (string, map[string]bool) {
	t.Helper()
	toks, err := common.PreprocessTarget(src, "test.c", false)
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	prog, err := frontend.Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// These are fragments, not whole programs, so the "no main()" diagnostic --
	// which only matters for a complete translation unit -- is expected here.
	// Every other diagnostic is a real problem.
	if errs := frontend.Check(prog); len(errs) > 0 {
		var b strings.Builder
		for _, e := range errs {
			if strings.Contains(e.Error(), "program has no main()") {
				continue
			}
			b.WriteString("\n  " + e.Error())
		}
		if b.Len() > 0 {
			t.Fatalf("type errors:%s", b.String())
		}
	}
	// No runtime is linked here: the fragments guard only the user-code surface
	// this compiler lowers. translateProgram(nil lib) emits exactly that.
	ir, claimed, _, err := translateProgram(prog, nil, false, 1, "")
	if err != nil {
		t.Fatalf("generate IR: %v", err)
	}
	return ir, claimed
}

// irFromSourceTarget is irFromSource for a named target, for the assertions
// that are about one architecture's ABI rather than about the emitter in
// general. A variadic call is the clearest case: what the caller pushes and
// what the callee's va_arg reads have to agree, and the answer differs between
// x86-64 SysV, AAPCS and AAPCS64.
func irFromSourceTarget(t *testing.T, src, arch string, linux bool) string {
	t.Helper()
	toks, err := common.PreprocessTarget(src, "test.c", linux)
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	prog, err := frontend.Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if errs := frontend.Check(prog); len(errs) > 0 {
		var b strings.Builder
		for _, e := range errs {
			if strings.Contains(e.Error(), "program has no main()") {
				continue
			}
			b.WriteString("\n  " + e.Error())
		}
		if b.Len() > 0 {
			t.Fatalf("type errors:%s", b.String())
		}
	}
	ir, _, _, err := translateProgram(prog, nil, linux, 1, arch)
	if err != nil {
		t.Fatalf("generate IR for %s: %v", arch, err)
	}
	return ir
}

// TestIRVaArgMatchesTargetABI pins the va_list shape to the target's own ABI.
//
// The three shapes are genuinely different, and reading one as another is
// silent: the program links, then loads its first variadic argument from
// whatever the cursor's bits happen to address. So each target is checked for
// the construct that distinguishes it rather than for "it produced something".
func TestIRVaArgMatchesTargetABI(t *testing.T) {
	const src = `
int f(int n, ...){
  va_list ap; va_start(ap, n);
  int a = va_arg(ap, int);
  va_end(ap);
  return a;
}
`
	// AAPCS (32-bit ARM) and AAPCS64 (AArch64) both pass every variadic
	// argument on the stack and use a bare `char *` cursor -- so their va_arg is
	// a load at the cursor with no tag to walk. x86-64 SysV is the outlier: its
	// va_list is a 24-byte __va_list_tag whose gp_offset decides whether the
	// argument comes from the register save area or the overflow area.
	sysv := irFromSourceTarget(t, src, "x86_64", true)
	if !strings.Contains(sysv, "load i32, ptr") && !strings.Contains(sysv, "load i32, i32") {
		t.Errorf("x86-64 SysV va_arg did not load the argument value:\n%s", sysv)
	}

	for _, tc := range []struct{ arch, name string }{
		{"arm", "AAPCS"},
		{"aarch64", "AAPCS64"},
		// RISC-V joins them for the same reason ARM did, and the failure it
		// guards against is worth naming: llvm.va_start is target-defined, so
		// on RISC-V the object it fills is NOT the x86-64
		// {gp_offset, fp_offset, overflow, reg_save} tag. Reading that tag out
		// of it compared a pointer's low half against 48, decided every
		// argument was past the register save area, and loaded from address 0.
		{"riscv64", "RISC-V LP64"},
		{"riscv32", "RISC-V ILP32"},
	} {
		ir := irFromSourceTarget(t, src, tc.arch, true)
		// A flat cursor: read the pointer, load through it, then bump it.
		if !strings.Contains(ir, "getelementptr inbounds i8") {
			t.Errorf("%s va_arg does not advance its cursor:\n%s", tc.name, ir)
		}
		// The SysV tag has two cursors and a register save area; none of that
		// should appear on a target whose va_list is a bare pointer.
		for _, bad := range []string{"48", "reg_save_area"} {
			if strings.Contains(ir, bad) && tc.arch == "arm" {
				t.Errorf("%s va_arg leaked the x86-64 SysV shape (%q):\n%s", tc.name, bad, ir)
			}
		}
	}
}

// TestIRVarargStepMatchesSaveAreaWidth pins the *reader* side of the same
// contract. The cursor advances by the slot the caller actually stored the
// argument in, which is not the C type's width: on RISC-V 64 the prologue
// spills the variadic registers with `sd`, eight bytes apart, so a cursor that
// steps four reads the low half of one argument and then the zero above it --
// addall(4,10,20,30,40) answering 30 instead of 100.
func TestIRVarargStepMatchesSaveAreaWidth(t *testing.T) {
	const src = `
int f(int n, ...){
  va_list ap; va_start(ap, n);
  int a = va_arg(ap, int);
  (void)a;
  va_end(ap);
  return 0;
}
`
	cases := []struct {
		arch string
		step string
	}{
		// 64-bit flat-cursor targets (Windows x64, RISC-V LP64): the caller
		// widens every variadic int to i64, so the slot -- and the step -- is
		// eight bytes.
		{"riscv64", "i64 8"},
		// 32-bit targets (AAPCS, RV32 psABI): a word is four bytes and the
		// save area is an array of words.
		{"riscv32", "i64 4"},
		{"arm", "i64 4"},
	}
	for _, tc := range cases {
		ir := irFromSourceTarget(t, src, tc.arch, true)
		if !strings.Contains(ir, "getelementptr inbounds i8, ptr %") {
			t.Fatalf("%s: va_arg produced no cursor advance", tc.arch)
		}
		if !strings.Contains(ir, tc.step) {
			t.Errorf("%s: va_arg cursor did not advance by %s (expected a "+
				"getelementptr inbounds i8 ... %s):\n%s", tc.arch, tc.step, tc.step, ir)
		}
	}
}

// TestIRVarargSlotWidthIsTargetSpecific checks the caller side of the same
// contract. A variadic argument is widened to fill its slot only where the slot
// is eight bytes; 32-bit ARM's slot is a word, so widening there would push
// every following argument four bytes further out than the callee's va_arg
// expects -- each read landing on the previous argument's tail.
func TestIRVarargSlotWidthIsTargetSpecific(t *testing.T) {
	const src = `
int g(int n, ...){ return n; }
void h(void){ g(2, 10, 20); }
`
	// AAPCS64 and the x86-64/Win64 shapes keep the widened i64 operand.
	for _, arch := range []string{"aarch64", "x86_64"} {
		ir := irFromSourceTarget(t, src, arch, true)
		if !strings.Contains(ir, "sext i32 10 to i64") {
			t.Errorf("%s: variadic int was not widened to its slot width:\n%s", arch, ir)
		}
	}
	// AAPCS passes the promoted int as a word, so the widening must not appear.
	arm := irFromSourceTarget(t, src, "arm", true)
	if strings.Contains(arm, "sext i32 10 to i64") {
		t.Errorf("arm: a variadic int was widened to i64, but the AAPCS slot is a word:\n%s", arm)
	}
}

// TestIRGeneration covers the shapes the generator has to get right, each
// checked by inspecting the emitted text. LLVM's own parser is the final
// authority and runs in the goa package, which is where the shared library is
// available; these assertions catch the common mistakes before that.
func TestIRGeneration(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
		not  []string
	}{
		{
			name: "constant return",
			src:  "int main(void) { return 42; }",
			want: []string{"define i32 @main()", "ret i32 42"},
		},
		{
			name: "arithmetic uses LLVM mnemonics",
			src:  "int f(int a, int b) { return a + b - a * b; }",
			// C says "+", LLVM says "add". Emitting the C spelling produces a
			// module LLVM rejects outright.
			want: []string{"add i32", "sub i32", "mul i32"},
			not:  []string{"+ i32", "- i32", "* i32"},
		},
		{
			// A bare function name in a value context *is* the function's
			// address. LLVM agrees -- a function symbol is its own pointer --
			// so the conversion is the bare symbol and no load. Rendering the
			// zero value instead (the "a name with no storage" fallback) put a
			// null where a function pointer belonged, and the program faulted
			// at the first indirect call.
			name: "function designator decays to its address",
			src:  "static int f(int x) { return x; }\nint main(void) { int (*p)(int) = f; return p(1); }",
			want: []string{"ptr @f", "store ptr @f"},
			not:  []string{"store ptr null"},
		},
		{
			// The same conversion on an argument, which is how goclib's
			// printf_lite_with(vfmt_i, ...) passes its formatter.
			name: "function designator as an argument",
			src: "static int g(int (*q)(int), int v) { return q(v); }\n" +
				"static int f(int x) { return x; }\n" +
				"int main(void) { return g(f, 1); }",
			want: []string{"call i32 @g(ptr @f, i32 1)"},
			not:  []string{"call i32 @g(ptr null"},
		},
		{
			name: "local variable gets a slot",
			src:  "int f(void) { int x = 5; x = x + 1; return x; }",
			want: []string{"alloca i32", "store i32 5", "load i32"},
		},
		{
			// Both arms return, so there is no join point and no phi -- only a
			// conditional branch. A phi here would be dead.
			name: "if with two returns needs no phi",
			src:  "int f(int a) { if (a > 0) return 1; return 0; }",
			want: []string{"icmp sgt", "br i1"},
			not:  []string{"phi"},
		},
		{
			// A value computed in either arm does need one.
			name: "if with a join produces a phi",
			src:  "int f(int a) { int x; if (a > 0) x = 1; else x = 2; return x; }",
			want: []string{"icmp sgt", "br i1"},
		},
		{
			name: "while loop",
			src:  "int f(int n) { int s = 0; while (n > 0) { s = s + n; n = n - 1; } return s; }",
			want: []string{"br i1", "br label"},
		},
		{
			name: "for loop has three sections",
			src:  "int f(int n) { int s = 0; for (int i = 0; i < n; i++) s = s + i; return s; }",
			want: []string{"icmp slt", "br i1"},
		},
		{
			name: "array indexing scales by element size",
			src:  "int f(int *a, int i) { return a[i]; }",
			// A byte offset would be wrong for anything wider than a char.
			want: []string{"mul i64", "getelementptr"},
		},
		{
			name: "pointer dereference",
			src:  "int f(int *p) { return *p; }",
			want: []string{"load i32"},
		},
		{
			name: "short circuit operators branch",
			src:  "int f(int a, int b) { return a && b; }",
			want: []string{"br i1"},
		},
		{
			name: "nested ternary produces phis",
			src:  "int f(int a) { return a > 0 ? 1 : a < 0 ? -1 : 0; }",
			want: []string{"phi i32"},
		},
		{
			name: "global variable",
			src:  "int g = 7;\nint f(void) { return g; }",
			want: []string{"@G_g = global i32", "load i32, ptr @G_g"},
		},
		{
			name: "function call",
			src:  "int h(int x) { return x; }\nint f(void) { return h(3); }",
			// Arguments carry their types explicitly: LLVM only infers them
			// from a declaration, and a function defined later in the same
			// module has none to infer from.
			want: []string{"call i32 @h(i32 3)"},
		},
		{
			name: "struct member access",
			src:  "struct P { int x; int y; };\nint f(struct P *p) { return p->y; }",
			want: []string{"getelementptr", "load i32"},
		},
		{
			name: "recursion needs no separate declaration",
			// A `declare` for a function the same module defines is rejected.
			src:  "int f(int n) { return n <= 0 ? 0 : f(n-1); }",
			not:  []string{"declare i32 @f"},
			want: []string{"define i32 @f"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ir, _ := irFromSource(t, tc.src)
			for _, w := range tc.want {
				if !strings.Contains(ir, w) {
					t.Errorf("IR is missing %q\n--- IR ---\n%s", w, ir)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(ir, n) {
					t.Errorf("IR unexpectedly contains %q\n--- IR ---\n%s", n, ir)
				}
			}
		})
	}
}

// TestIREveryBlockTerminated checks the invariant LLVM enforces most strictly:
// every basic block must end in exactly one terminator. A block left open is
// the single most common way this generator could produce an invalid module,
// and LLVM's complaint about it points at the block rather than the cause.
func TestIREveryBlockTerminated(t *testing.T) {
	sources := []string{
		"int f(int a) { if (a > 0) return 1; return 0; }",
		"int f(int n) { int s = 0; while (n > 0) { s += n; n--; } return s; }",
		"int f(int n) { int s = 0; for (int i = 0; i < n; i++) s += i; return s; }",
		"int f(int n) { do { n--; } while (n > 0); return n; }",
		"int f(int a) { return a > 0 ? 1 : a < 0 ? -1 : 0; }",
		"int f(int a) { int x = 0; if (a) x = 1; else x = 2; return x; }",
		"int f(int a) { switch (a) { case 1: return 1; case 2: return 2; default: return 0; } }",
		"int f(int a) { if (a) { if (a > 1) return 2; } return 0; }",
	}
	terms := []string{"ret ", "br ", "switch ", "unreachable"}
	for _, src := range sources {
		ir, _ := irFromSource(t, src)
		// Walk the body of each function, one basic block at a time.
		for _, blk := range irBlocks(ir) {
			last := ""
			for _, ln := range strings.Split(blk, "\n") {
				t := strings.TrimSpace(ln)
				// A closing brace ends the function, not a block.
				if t == "" || strings.HasSuffix(t, ":") || t == "}" {
					continue
				}
				last = t
			}
			if last == "" {
				continue // an empty block carries no instruction
			}
			ok := false
			for _, term := range terms {
				if strings.HasPrefix(last, term) {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("block does not end in a terminator (last: %q)\n%s", last, ir)
			}
		}
	}
}

// irBlocks splits a function body into basic blocks: everything from one label
// to the next. A `define ... {` line is not a label -- it opens a function, and
// the block named by the first label starts right after it.
func irBlocks(ir string) []string {
	var out []string
	var cur strings.Builder
	started := false
	for _, ln := range strings.Split(ir, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "define ") {
			started = true
			continue
		}
		if started && strings.HasSuffix(t, ":") && !strings.HasPrefix(t, "target") {
			if hasInstruction(cur.String()) {
				out = append(out, cur.String())
			}
			cur.Reset()
			cur.WriteString(ln + "\n")
			continue
		}
		if started {
			cur.WriteString(ln + "\n")
		}
	}
	if hasInstruction(cur.String()) {
		out = append(out, cur.String())
	}
	return out
}

// hasInstruction reports whether a chunk of IR contains anything other than
// labels, so an empty block is not mistaken for an unterminated one.
func hasInstruction(block string) bool {
	for _, ln := range strings.Split(block, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasSuffix(t, ":") {
			continue
		}
		return true
	}
	return false
}

// TestIREligibilityIsConservative guards the routing decision. A function the
// front end cannot model must stay on the native path, where it at least
// compiles; sending it here would produce a module missing half its behaviour.
func TestIREligibilityIsConservative(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"plain function", "int f(int a) { return a + 1; }", true},
		{"recursion", "int f(int n) { return n ? f(n-1) : 0; }", true},
		{"variadic reaches LLVM", "int f(const char *s, ...) { return 0; }", true},
		{"inline asm stays native", "int f(void) { __asm { nop } return 0; }", false},
		{"_BitInt stays native", "int f(_BitInt(37) x) { return 0; }", false},
		{"bit-field stays native",
			"struct S { unsigned a : 3; unsigned b : 5; };\nint f(struct S *s) { return s->a; }", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			toks, err := common.PreprocessTarget(tc.src, "t.c", false)
			if err != nil {
				t.Skipf("preprocess: %v", err)
			}
			prog, err := frontend.Parse(toks)
			if err != nil {
				t.Skipf("parse: %v", err)
			}
			if len(prog.Funcs) == 0 {
				t.Skip("no function parsed")
			}
			got := llvmEligible(prog.Funcs[0])
			if got != tc.want {
				t.Errorf("llvmEligible = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIRRoundTripsThroughFile keeps the debug path honest: whatever the tests
// above compare against has to be reachable from outside, or the loop for
// working on this generator breaks and the breakage is found later.
func TestIRRoundTripsThroughFile(t *testing.T) {
	ir, _ := irFromSource(t, "int main(void) { return 1; }")
	out := os.Getenv("GOC_IR_OUT")
	if out == "" {
		t.Skip("set GOC_IR_OUT to write the IR to a file")
	}
	if err := os.WriteFile(out, []byte(ir), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestIRSizePipeline pins the -Os shape, and it exists because LLVM broke it
// without warning at the source level.
//
// LLVM 21 removed the `Os` pipeline: asking for it is not a pipeline that runs
// badly, it is a pipeline string the library rejects, so a build that had been
// working stopped with
//
//	The optimization level "Os" is no longer supported. Use O2 in
//	conjunction with the optsize attribute instead.
//
// The replacement is not just the pipeline name -- it is an attribute on each
// function, which is the half that is easy to leave out and produces a build
// that succeeds and is not size-optimised. So both are asserted: the pipeline
// the library is given, and the attribute the IR carries.
func TestIRSizePipeline(t *testing.T) {
	if !llvmAvailable(t) {
		t.Skip("skipping: no libLLVM configured")
	}
	if got := irPasses(2); got != "default<O2>" {
		t.Errorf("-Os pipeline = %q, want default<O2> (LLVM 21 removed the Os pipeline)", got)
	}

	// The attribute is only present for -Os.
	plain := irFrom(t, 1)
	if strings.Contains(plain, "optsize") {
		t.Error("an ordinary build carries the optsize attribute; it would trade speed for bytes nobody asked to save")
	}

	for _, sizeOpt := range []int{2} {
		ir := irFrom(t, sizeOpt)
		if !strings.Contains(ir, "attributes #0 = { optsize }") {
			t.Errorf("-Os IR has no optsize attribute:\n%s", ir)
		}
	}
}

// irFrom lowers a one-function program at the given optimisation level.
func irFrom(t *testing.T, opt int) string {
	t.Helper()
	toks, err := common.PreprocessTarget("int f(int n){return n*2;}\nint main(void){return f(21);}", "test.c", false)
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	prog, err := frontend.Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ir, _, _, err := translateProgram(prog, nil, false, opt, "")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	return ir
}

// A comparison's C result type is int whatever the operands are. When
// binaryType answered with the operand common type instead, "v += (a != b)"
// over doubles resolved its own common type as double and emitted
//
//	sitofp i64 -> double; fadd double; fptosi double -> i64
//
// for what is an integer add: three extra conversions per evaluation plus an
// integer-to-float round trip that a wide accumulator cannot always survive.
// Measured on a 1e8-iteration loop that read one volatile double per pass, it
// cost 0.46s against clang -O2 and gcc -O2's 0.20s; with the type fixed, 0.06s
// -- LLVM then sees the loop invariant it was being denied.
//
// The result is unchanged either way, so nothing that checks the computed
// answer can catch a regression here. Only the shape can.
func TestIRComparisonResultIsInt(t *testing.T) {
	ir, _ := irFromSource(t, `
long v;
double a, b;
void f(void){ v += (a != b); }
`)
	for _, bad := range []string{"sitofp", "uitofp", "fptosi", "fptoui"} {
		if strings.Contains(ir, bad) {
			t.Errorf("\"v += (a != b)\" emitted %s: an integer add was routed through floating point:\n%s", bad, ir)
		}
	}
	if !strings.Contains(ir, "add i64") {
		t.Errorf("\"v += (a != b)\" has no integer add:\n%s", ir)
	}
}

// TestIREntryStubAlignsStackOnX8664Linux pins the stub that makes the ELF
// entry point correct on x86-64.
//
// The eight bytes it corrects are invisible until something spills an SSE
// register: the kernel enters _start with rsp 16-byte aligned, while LLVM's
// prologue is written for being entered by `call`, which has pushed a return
// address and left rsp 8 mod 16. _start's own `pushq` then lands it back on
// 8 mod 16, and every call it makes from there hands the callee a stack that is
// off by eight. Nothing on the integer path notices; a variadic function's
// prologue spills the float save area with `movaps`, which faults on an
// unaligned address, so printf("%f", 1.5) died with SIGSEGV before printing
// anything.
//
// No test that runs a program and compares its output can be relied on to catch
// the stub's disappearance either -- an integer-only test suite passes
// throughout -- so the assertion is on the shape of the module.
func TestIREntryStubAlignsStackOnX8664Linux(t *testing.T) {
	const src = "int main(void) { return 0; }"
	ir := irFromSourceTarget(t, src, "x86_64", true)
	for _, want := range []string{
		`module asm ".globl __goc_entry"`,
		`module asm "__goc_entry:"`,
		`module asm "  andq $-16, %rsp"`,
		`module asm "  callq _start"`,
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("x86-64 Linux IR has no %s: the entry point is _start itself, so every\n"+
				"function in the program runs with a stack eight bytes off:\n%s", want, ir)
		}
	}
	// The `and` is what makes the stub right whether or not the kernel's
	// alignment promise holds; a fixed subtraction would depend on it.
	if strings.Contains(ir, `subq $8, %rsp`) && !strings.Contains(ir, "andq $-16, %rsp") {
		t.Errorf("entry stub subtracts a fixed 8 instead of realigning:\n%s", ir)
	}

	// Windows is entered through the PE loader with the stack already in the
	// shape the prologue expects, and AArch64/ARM/RISC-V enter a function with
	// the stack pointer the caller left -- no return address is pushed -- so
	// none of them may grow a stub that would be a no-op at best.
	for _, tc := range []struct {
		name  string
		arch  string
		linux bool
	}{
		{"Windows x86-64", "x86_64", false},
		{"Linux aarch64", "aarch64", true},
		{"Linux arm", "arm", true},
		{"Linux riscv64", "riscv64", true},
	} {
		got := irFromSourceTarget(t, src, tc.arch, tc.linux)
		if strings.Contains(got, "__goc_entry") {
			t.Errorf("%s IR has an entry realignment stub: %s does not push a return\n"+
				"address, so there is no eight-byte skew to correct:\n%s", tc.name, tc.arch, got)
		}
	}
}

// TestIRWindowsDefinesFltused pins the symbol a COFF module that touches
// floating point has to provide.
//
// LLVM names `_fltused` when it lowers such a module, and a gocl program links
// nothing but the object LLVM just wrote -- so without a definition the link
// ends at "undefined symbol(s): _fltused" for every program that formats a
// double, while the same source links fine on Linux and fine under `goc`.
// It is a global spelled in the IR because goc prefixes a C global with "G_",
// which would define G__fltused and leave the reference unresolved.
func TestIRWindowsDefinesFltused(t *testing.T) {
	const src = "int main(void) { return 0; }"
	win := irFromSourceTarget(t, src, "x86_64", false)
	if !strings.Contains(win, "@_fltused =") {
		t.Errorf("Windows IR does not define _fltused: a module that uses floating point\n"+
			"will fail to link with an undefined-symbol error:\n%s", win)
	}
	if lin := irFromSourceTarget(t, src, "x86_64", true); strings.Contains(lin, "_fltused") {
		t.Errorf("Linux IR defines _fltused: nothing names it there, so it is dead bytes:\n%s", lin)
	}
}
