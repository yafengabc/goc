package compiler

import (
	"goc/frontend"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spell renders a token the way a human would read it, for assertions.
func spell(tok frontend.Token) string {
	switch tok.Kind {
	case frontend.TStr:
		return "\"" + string(tok.Str) + "\""
	default:
		return tok.Text
	}
}

// pptext preprocesses src (as if from file "test.c") and returns the space-
// joined spellings of every token up to frontend.TEOF. This isolates the preprocessor
// from the rest of the compiler, so it can be tested without assembling.
func pptext(t *testing.T, src string) string {
	t.Helper()
	toks, err := Preprocess(src, "test.c")
	if err != nil {
		t.Fatalf("Preprocess failed: %v", err)
	}
	var parts []string
	for _, tok := range toks {
		if tok.Kind == frontend.TEOF {
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
		if tok.Kind == frontend.TEOF {
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
		if tok.Kind == frontend.TEOF {
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

// TestElifChainWithMacro regression (P0.6): a #elif whose condition contains
// macro expansion must still expand macros even though the preceding #if was
// false -- the enclosing frame is inactive while the #elif condition is
// evaluated. Before the fix, X stayed unexpanded, the condition evaluated to
// 0, and the chain fell through to #else.
func TestElifChainWithMacro(t *testing.T) {
	got := pptext(t, "#define X 5\n#if X == 1\nint a;\n#elif X == 5\nint b;\n#elif X == 9\nint c;\n#else\nint d;\n#endif\n")
	want := "int b ;"
	if got != want {
		t.Fatalf("elif chain with macro cond:\n got %q\nwant %q", got, want)
	}
}

// TestElifPlainConstant: bare constant in a #elif (control case, no macro).
func TestElifPlainConstant(t *testing.T) {
	got := pptext(t, "#if 0\nint a;\n#elif 1\nint b;\n#else\nint c;\n#endif\n")
	want := "int b ;"
	if got != want {
		t.Fatalf("elif plain constant:\n got %q\nwant %q", got, want)
	}
}

// TestElifDefined: #elif defined(NAME) after a false #if picks the branch.
func TestElifDefined(t *testing.T) {
	got := pptext(t, "#define X 5\n#if defined(UNDEF)\nint a;\n#elif defined(X)\nint b;\n#else\nint c;\n#endif\n")
	want := "int b ;"
	if got != want {
		t.Fatalf("elif defined:\n got %q\nwant %q", got, want)
	}
}

// TestElifAfterTaken: once an earlier branch fired, a later #elif with a true
// condition must not emit a second body.
func TestElifAfterTaken(t *testing.T) {
	got := pptext(t, "#define X 5\n#if X == 5\nint b;\n#elif X == 5\nint dup;\n#else\nint c;\n#endif\n")
	want := "int b ;"
	if got != want {
		t.Fatalf("elif after taken:\n got %q\nwant %q", got, want)
	}
}

// TestStandardPredefinedMacros (P1.7): __STDC__/__STDC_HOSTED__/__STDC_VERSION__
// are predefined object-like macros; __STDC_VERSION__ must be 202311 (C23).
func TestStandardPredefinedMacros(t *testing.T) {
	got := pptext(t, "__STDC__ __STDC_HOSTED__ __STDC_VERSION__\n")
	if got != "1 1 202311" {
		t.Fatalf("standard predefined macros:\n got %q\nwant %q", got, "1 1 202311")
	}
}

// TestDateTimeMacros (P1.7): __DATE__ expands to a C-standard "Mmm dd yyyy"
// string and __TIME__ to "hh:mm:ss"; both must survive #if defined() tests.
func TestDateTimeMacros(t *testing.T) {
	got := pptext(t, "__DATE__ __TIME__\n")
	// Both expand to quoted string tokens, e.g. "Oct  2 2026" "18:04:17".
	// __DATE__ itself contains spaces, so parse by quote boundaries, not by
	// whitespace splitting.
	if len(got) < 24 || got[0] != '"' {
		t.Fatalf("date/time macros: got %q", got)
	}
	endDate := strings.IndexByte(got[1:], '"') // closing quote of __DATE__
	if endDate < 0 {
		t.Fatalf("date/time macros: missing closing quote: %q", got)
	}
	date := got[1 : 1+endDate]
	tm := got[endDate+3:]
	if len(date) != 11 || date[3] != ' ' || date[6] != ' ' {
		t.Fatalf("__DATE__ not in \"Mmm dd yyyy\" form: %q", date)
	}
	if len(tm) != 10 || tm[0] != '"' || tm[3] != ':' || tm[6] != ':' {
		t.Fatalf("__TIME__ not in \"hh:mm:ss\" form: %q", tm)
	}
	// defined() must see them as defined macros.
	got2 := pptext(t, "#if defined(__DATE__) && defined(__TIME__)\nint ok;\n#else\nint bad;\n#endif\n")
	if got2 != "int ok ;" {
		t.Fatalf("defined(__DATE__/__TIME__): got %q", got2)
	}
}

// TestDefinedHasOperators (P2.15): defined(__has_c_attribute) and
// defined(__has_include) must evaluate to 1 (C23 6.10.10), so the portable
// guard "#if defined(__has_c_attribute) && __has_c_attribute(x)" activates.
func TestDefinedHasOperators(t *testing.T) {
	got := pptext(t, "#if defined(__has_c_attribute)\nint a;\n#else\nint b;\n#endif\n#if defined(__has_include)\nint c;\n#else\nint d;\n#endif\n")
	want := "int a ; int c ;"
	if got != want {
		t.Fatalf("defined(__has_*):\n got %q\nwant %q", got, want)
	}
}

// TestHasCAttributeGuard: the full C23 guard must select the branch when the
// attribute is supported and reject it when not.
func TestHasCAttributeGuard(t *testing.T) {
	got := pptext(t, "#if defined(__has_c_attribute) && __has_c_attribute(deprecated)\nint a;\n#else\nint b;\n#endif\n#if defined(__has_c_attribute) && __has_c_attribute(likely)\nint c;\n#else\nint d;\n#endif\n")
	want := "int a ; int d ;"
	if got != want {
		t.Fatalf("__has_c_attribute guard:\n got %q\nwant %q", got, want)
	}
}

// TestLibraryIgnoresDiskHeaders pins the isolation the C library needs.
//
// The library's own sources include <stdio.h> and friends by name. If those
// resolved against the user's include path, a program shipping its own
// stdio.h -- in the working directory or via -I -- would replace the runtime's
// header, and goclib would then fail to compile against itself. The symptom is
// badly attributed: the error names a library source ("goclib/file.c: line
// 234: expected type specifier, got \"FILE\"") and says nothing about the
// header the user shadowed.
func TestLibraryIgnoresDiskHeaders(t *testing.T) {
	dir := t.TempDir()
	// A stdio.h in the working directory, which is what resolveInclude searches
	// first for a quoted include and what -I adds to the angled list.
	shadow := "extern int not_the_real_stdio;\n"
	if err := os.WriteFile(filepath.Join(dir, "stdio.h"), []byte(shadow), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	// The library's view: no disk header may be visible.
	if _, err := PreprocessLibrary("#include <stdio.h>\nint x;\n", "goclib/probe.c", false); err != nil {
		t.Errorf("PreprocessLibrary must fall back to the built-in stdio.h, got: %v", err)
	}

	// The user's view: the shadowing header is what they asked for, so their
	// own translation unit sees it. This is the half that must NOT change --
	// "external headers first" is the documented order, and this test failing
	// on this line would mean the isolation leaked into user code.
	toks, err := Preprocess("#include <stdio.h>\nint x;\n", filepath.Join(dir, "user.c"))
	if err != nil {
		t.Fatalf("Preprocess failed: %v", err)
	}
	var sawShadow bool
	for _, tok := range toks {
		if tok.Text == "not_the_real_stdio" {
			sawShadow = true
		}
	}
	if !sawShadow {
		t.Error("user code did not pick up the header from the working " +
			"directory; external headers must take priority over the built-in ones")
	}
}
