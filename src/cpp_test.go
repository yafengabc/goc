package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spell renders a token the way a human would read it, for assertions.
func spell(tok Token) string {
	switch tok.Kind {
	case TStr:
		return "\"" + string(tok.Str) + "\""
	default:
		return tok.Text
	}
}

// pptext preprocesses src (as if from file "test.c") and returns the space-
// joined spellings of every token up to TEOF. This isolates the preprocessor
// from the rest of the compiler, so it can be tested without assembling.
func pptext(t *testing.T, src string) string {
	t.Helper()
	toks, err := Preprocess(src, "test.c")
	if err != nil {
		t.Fatalf("Preprocess failed: %v", err)
	}
	var parts []string
	for _, tok := range toks {
		if tok.Kind == TEOF {
			break
		}
		parts = append(parts, spell(tok))
	}
	return strings.Join(parts, " ")
}

func TestObjectMacro(t *testing.T) {
	got := pptext(t, "#define N 10\nint x = N + N;\n")
	want := "int x = 10 + 10 ;"
	if got != want {
		t.Fatalf("object macro:\n got %q\nwant %q", got, want)
	}
}

func TestFunctionMacro(t *testing.T) {
	got := pptext(t, "#define ADD(a, b) ((a) + (b))\nint x = ADD(1, 2);\n")
	want := "int x = ( ( 1 ) + ( 2 ) ) ;"
	if got != want {
		t.Fatalf("function macro:\n got %q\nwant %q", got, want)
	}
}

func TestStringize(t *testing.T) {
	got := pptext(t, "#define STR(x) #x\nSTR(hello)\n")
	if !strings.Contains(got, `"hello"`) {
		t.Fatalf("stringize: got %q, want it to contain \"hello\"", got)
	}
}

func TestPaste(t *testing.T) {
	got := pptext(t, "#define CAT(a, b) a##b\nCAT(foo, bar)\n")
	if !strings.Contains(got, "foobar") {
		t.Fatalf("paste: got %q, want it to contain foobar", got)
	}
}

func TestConditionalIfdef(t *testing.T) {
	src := "#define FOO\n#ifdef FOO\nint a;\n#else\nint b;\n#endif\n"
	got := pptext(t, src)
	if !strings.Contains(got, "int a ;") || strings.Contains(got, "int b ;") {
		t.Fatalf("#ifdef taken branch wrong: %q", got)
	}

	src2 := "#ifndef FOO\nint c;\n#else\nint d;\n#endif\n"
	got2 := pptext(t, src2)
	if !strings.Contains(got2, "int c ;") {
		t.Fatalf("#ifndef taken branch wrong: %q", got2)
	}
}

func TestConditionalIfExpr(t *testing.T) {
	src := "#define N 5\n#if N > 3\nint big;\n#else\nint small;\n#endif\n"
	got := pptext(t, src)
	if !strings.Contains(got, "int big ;") {
		t.Fatalf("#if expression branch wrong: %q", got)
	}
}

func TestDefinedOperator(t *testing.T) {
	src := "#if defined(FOO) && 1\nint yes;\n#else\nint no;\n#endif\n"
	got := pptext(t, src)
	if !strings.Contains(got, "int no ;") {
		t.Fatalf("defined() should be false here: %q", got)
	}
}

func TestUndef(t *testing.T) {
	src := "#define N 1\n#undef N\n#ifdef N\nint def;\n#else\nint undef;\n#endif\n"
	got := pptext(t, src)
	if !strings.Contains(got, "int undef ;") {
		t.Fatalf("#undef did not remove macro: %q", got)
	}
}

func TestRecursionGuard(t *testing.T) {
	// F references itself inside its body; without the recursion guard this
	// would loop forever.
	src := "#define F(x) x F(x)\nF(1)\n"
	got := pptext(t, src)
	if !strings.Contains(got, "F") {
		t.Fatalf("recursion guard: expected an unexpanded F to remain, got %q", got)
	}
}

func TestPredefinedMacros(t *testing.T) {
	got := pptext(t, "#define L __LINE__\nL\n")
	if !strings.Contains(got, "2") {
		t.Fatalf("__LINE__ should expand to a number: %q", got)
	}
	gotFile := pptext(t, "#define F __FILE__\nF\n")
	if !strings.Contains(gotFile, "test.c") {
		t.Fatalf("__FILE__ should expand to the filename: %q", gotFile)
	}
}

func TestMultilineMacro(t *testing.T) {
	src := "#define SUM(a, b) \\\n  ((a) + (b))\nint x = SUM(3, 4);\n"
	got := pptext(t, src)
	want := "int x = ( ( 3 ) + ( 4 ) ) ;"
	if got != want {
		t.Fatalf("multiline macro:\n got %q\nwant %q", got, want)
	}
}

func TestIncludeLocal(t *testing.T) {
	dir := t.TempDir()
	hdr := filepath.Join(dir, "h.h")
	if err := os.WriteFile(hdr, []byte("#define HVAL 42\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mainFile := filepath.Join(dir, "main.c")
	src := "#include \"h.h\"\nint x = HVAL;\n"
	toks, err := Preprocess(src, mainFile)
	if err != nil {
		t.Fatalf("include failed: %v", err)
	}
	var parts []string
	for _, tok := range toks {
		if tok.Kind == TEOF {
			break
		}
		parts = append(parts, spell(tok))
	}
	got := strings.Join(parts, " ")
	want := "int x = 42 ;"
	if got != want {
		t.Fatalf("include + macro:\n got %q\nwant %q", got, want)
	}
}

func TestFunctionVsObjectMacro(t *testing.T) {
	// Space between name and '(' => object-like macro whose body starts with '('.
	got := pptext(t, "#define F (10)\nint x = F + 1;\n")
	if !strings.Contains(got, "( 10 )") {
		t.Fatalf("object macro with spaced '(' should keep parens: %q", got)
	}
}

func TestIfCharConstant(t *testing.T) {
	// Character literals in #if evaluate to their byte value ('A'=65, '\n'=10).
	src := "#if 'A' == 65 && '\\n' == 10 && '0' == 48\nint yes;\n#else\nint no;\n#endif\n"
	got := pptext(t, src)
	if !strings.Contains(got, "int yes ;") || strings.Contains(got, "int no ;") {
		t.Fatalf("#if char constant branch wrong: %q", got)
	}
}

func TestLineDirective(t *testing.T) {
	// #line N makes the NEXT source line logically N+1.
	got := pptext(t, "#line 100\n__LINE__\n")
	if !strings.Contains(got, "101") {
		t.Fatalf("#line: got %q, want __LINE__ to be 101", got)
	}
	// Optional filename, which __FILE__ then reports.
	got2 := pptext(t, "#line 200 \"gen.c\"\n__FILE__ __LINE__\n")
	if !strings.Contains(got2, `"gen.c"`) || !strings.Contains(got2, "201") {
		t.Fatalf("#line with file: got %q", got2)
	}
}

func TestGnuLineDirective(t *testing.T) {
	// GNU form "# N \"file\"" (as produced by cpp / gcc -E).
	got := pptext(t, "# 42 \"x.c\"\n__LINE__ __FILE__\n")
	if !strings.Contains(got, "43") || !strings.Contains(got, `"x.c"`) {
		t.Fatalf("GNU #line: got %q", got)
	}
}

func TestLineDirectiveDoesNotLeakIntoIncludes(t *testing.T) {
	dir := t.TempDir()
	// A #line inside an included file must not renumber the includer.
	if err := os.WriteFile(filepath.Join(dir, "h.h"), []byte("#line 900\n"), 0644); err != nil {
		t.Fatal(err)
	}
	src := "#include \"h.h\"\n__LINE__\n"
	toks, err := Preprocess(src, filepath.Join(dir, "m.c"))
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	var parts []string
	for _, tok := range toks {
		if tok.Kind == TEOF {
			break
		}
		parts = append(parts, spell(tok))
	}
	got := strings.Join(parts, " ")
	// The __LINE__ in m.c sits on physical line 2 and must stay 2, not 901.
	if !strings.Contains(got, "2") || strings.Contains(got, "901") {
		t.Fatalf("#line leaked out of the include: %q", got)
	}
}
