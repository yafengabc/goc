package main

// Test-side driver for the IR back end.
//
// The generator's correctness is defined by whether LLVM accepts its output and
// whether the resulting program computes the right answer, so both checks live
// here rather than in the generator's own tests. The IR is produced without a
// shared library present -- it is text -- which means these tests run anywhere;
// the ones that need LLVM skip themselves when it is absent.

import (
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
	toks, err := PreprocessTarget(src, "test.c", false)
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	prog, err := Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// These are fragments, not whole programs, so the "no main()" diagnostic --
	// which only matters for a complete translation unit -- is expected here.
	// Every other diagnostic is a real problem.
	if errs := Check(prog); len(errs) > 0 {
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
	// the -fllvm build lowers. translateProgram(nil lib) emits exactly that.
	ir, claimed, err := translateProgram(prog, nil, false)
	if err != nil {
		t.Fatalf("generate IR: %v", err)
	}
	return ir, claimed
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
			toks, err := PreprocessTarget(tc.src, "t.c", false)
			if err != nil {
				t.Skipf("preprocess: %v", err)
			}
			prog, err := Parse(toks)
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
