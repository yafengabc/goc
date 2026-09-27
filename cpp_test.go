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
