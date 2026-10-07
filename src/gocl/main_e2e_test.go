// End-to-end tests for the gocl compiler.
//
// These drive the built binary rather than calling into the package, because
// the thing worth testing is the whole chain: preprocess, parse, check, lower to
// IR, compile with libLLVM, lay out the image, and link. A test that calls
// TranslateProgram directly would pass while the executable it produces
// segfaults -- which is exactly what happened when the .refptr indirection was
// missing, and the only reason it was caught was a test that ran the result.
//
// The tests skip when no libLLVM is configured. A missing library is not a
// failure: gocl reports it when a program is compiled, and the point of these
// tests is the code path after that point.
//
// This lived under cmd/gocl/ while that was a main package. The entry point
// moved to src/ (build tag gocl), so the test moved here: an external test
// package needs a non-test package to sit beside, and package gocl is the one
// that now carries the driver.

package gocl_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// compilerPath finds bin/gocl.exe above the working directory, so the test
// works from any package directory without GOC_TEST_EXE.
func compilerPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GOC_TEST_GOCL"); p != "" {
		return p
	}
	name := "gocl.exe"
	if runtime.GOOS != "windows" {
		name = "gocl"
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		// Only the repository's own bin/ counts. Walking up from src/gocl
		// passes src/ itself, and a stale src/bin/gocl.exe left by an
		// earlier `go build ./...` sits closer than bin/gocl.exe -- so a
		// plain upward walk silently tests the stale copy. It fails
		// confusingly: the error names goclib.h line 51 while the file that
		// actually broke is stdarg.h, which reads like a library regression
		// rather than an expired build artifact.
		if filepath.Base(dir) != "src" {
			p := filepath.Join(dir, "bin", name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skipf("no bin/%s found above %s; run `bash build.sh` first", name, "")
	return ""
}

// llvmConfigured reports whether a libLLVM shared library is reachable. The
// command looks for libLLVM.dll beside the binary and upwards, so the search
// here mirrors that rather than reading the environment directly.
func llvmConfigured(t *testing.T) bool {
	t.Helper()
	if os.Getenv("GOC_LLVM_DLL") != "" {
		return true
	}
	lib := "libLLVM.dll"
	if runtime.GOOS != "windows" {
		lib = "libLLVM.so"
	}
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "bin", lib)); err == nil {
			return true
		}
		if _, err := os.Stat(filepath.Join(dir, lib)); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

// build compiles src with gocl and returns the executable's path.
func build(t *testing.T, name, src string) string {
	t.Helper()
	if !llvmConfigured(t) {
		t.Skip("skipping: no libLLVM configured (set GOC_LLVM_DLL)")
	}
	exe := compilerPath(t)
	dir := t.TempDir()
	cPath := filepath.Join(dir, name+".c")
	if err := os.WriteFile(cPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name+".exe")
	cmd := exec.Command(exe, cPath, "-o", out)
	// A temp directory for the compiler's own temporaries: the LLVM object
	// goes through one, and the default location is not always writable.
	cmd.Env = append(os.Environ(), "TMP="+dir, "TEMP="+dir, "TMPDIR="+dir)
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gocl failed: %v\n%s", err, o)
	}
	return out
}

func TestGoclEndToEnd(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantCode int
		wantOut  string
	}{
		{
			name:     "constant",
			src:      "int main(void) { return 42; }",
			wantCode: 42,
		},
		{
			name:     "recursion",
			src:      "int f(int n){return n<2?n:f(n-1)+f(n-2);}\nint main(void){return f(10);}",
			wantCode: 55,
		},
		{
			name: "loop over an array",
			src: "int sum(int *a, int n){int s=0;for(int i=0;i<n;i++)s+=a[i];return s;}\n" +
				"int main(void){int a[5]={1,2,3,4,5};return sum(a,5);}",
			wantCode: 15,
		},
		{
			name:     "pointer arithmetic",
			src:      "int f(int *p,int n){int s=0;while(n--)s+=*p++;return s;}\nint main(void){int a[3]={10,20,30};return f(a,3);}",
			wantCode: 60,
		},
		{
			// The library travels through the object too, so a printf here
			// proves the two halves linked rather than one being dead code.
			name:    "printf",
			src:     "#include <stdio.h>\nint main(void){printf(\"v=%d\\n\", 42);return 0;}",
			wantOut: "v=42",
		},
		{
			// A global with a value: the object defines it, and the stub must
			// not define it again.
			name:     "initialised global",
			src:      "int g = 7;\nint main(void){ g = g * 3; return g; }",
			wantCode: 21,
		},
		{
			// The case that needed the .refptr indirection. The IR front end
			// cannot put a relocation in an initialiser, so it emits
			// `@G_gp = external global` and the stub gives it storage and
			// writes the address before main runs. When the stub addressed the
			// symbol directly instead, the displacement was computed from the
			// section start and the program read the wrong bytes -- a crash
			// rather than a link error, which is why it is pinned here.
			name:     "address of a global in a static initialiser",
			src:      "int g = 7;\nint *gp = &g;\nint main(void){ return *gp; }",
			wantCode: 7,
		},
		{
			// Two address-of slots in one program, so the walk collects more
			// than one and the second must not overwrite the first.
			name:     "two address-of initialisers",
			src:      "int a = 3;\nint b = 4;\nint *pa = &a;\nint *pb = &b;\nint main(void){ return *pa * 10 + *pb; }",
			wantCode: 34,
		},
		{
			// A function's name in a value context decays to its address. The
			// stub binds the slot, and the indirect call through it is the
			// reason the binding has to be right.
			name: "function pointer in a table",
			src: "int add(int a,int b){return a+b;}\nint mul(int a,int b){return a*b;}\n" +
				"int main(void){int (*ops[2])(int,int)={add,mul};return ops[0](3,4)*100+ops[1](3,4);}",
			wantCode: 712, // add(3,4)*100 + mul(3,4) = 700 + 12
		},
		{
			name:     "ternary and logical operators",
			src:      "int f(int a,int b){return (a>b?a:b) + (a&&b?1:0);}\nint main(void){return f(5,3);}",
			wantCode: 6,
		},
		{
			name:     "struct by pointer",
			src:      "struct P{int x;int y;};\nint sum(struct P *p){return p->x+p->y;}\nint main(void){struct P p; p.x=3; p.y=4; return sum(&p);}",
			wantCode: 7,
		},
		{
			name: "string handling",
			src: "#include <stdio.h>\n#include <string.h>\n" +
				"int main(void){char b[16]; strcpy(b,\"gocl\"); printf(\"%s %d\\n\", b, (int)strlen(b)); return 0;}",
			wantOut: "gocl 4",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			exe := build(t, strings.ReplaceAll(tc.name, " ", "_"), tc.src)
			run := exec.Command(exe)
			o, _ := run.CombinedOutput()
			if got := run.ProcessState.ExitCode(); got != tc.wantCode {
				t.Errorf("exit code %d, want %d (output %q)", got, tc.wantCode, o)
			}
			if tc.wantOut != "" && !strings.Contains(string(o), tc.wantOut) {
				t.Errorf("output %q does not contain %q", o, tc.wantOut)
			}
		})
	}
}

// TestGoclReportsMissingLibrary checks that a missing libLLVM is a diagnostic
// rather than a silent fallback to something else. Falling back would produce a
// program that is not the one that was asked for, and the difference would show
// up later as a performance regression nobody could trace.
func TestGoclReportsMissingLibrary(t *testing.T) {
	exe := compilerPath(t)
	dir := t.TempDir()
	cPath := filepath.Join(dir, "a.c")
	if err := os.WriteFile(cPath, []byte("int main(void){return 0;}"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, cPath, "-o", filepath.Join(dir, "a.exe"))
	cmd.Env = append(os.Environ(), "GOC_LLVM_DLL="+filepath.Join(dir, "no-such-library.dll"))
	o, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("compiled with a library that does not exist")
	}
	if !strings.Contains(string(o), "LLVM") {
		t.Errorf("diagnostic does not mention LLVM: %q", o)
	}
}

// TestGoclRejectsMissingEntry checks the one diagnostic that belongs to the
// linker rather than to LLVM. A program with no main and no WinMain has nothing
// to jump to, and saying so is more useful than producing an image whose entry
// symbol is missing.
func TestGoclRejectsMissingEntry(t *testing.T) {
	if !llvmConfigured(t) {
		t.Skip("skipping: no libLLVM configured")
	}
	exe := compilerPath(t)
	dir := t.TempDir()
	cPath := filepath.Join(dir, "a.c")
	if err := os.WriteFile(cPath, []byte("int f(void){return 0;}"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, cPath, "-o", filepath.Join(dir, "a.exe"))
	o, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("compiled a program with no entry point")
	}
	// The front end catches this one, before any LLVM is involved, and says
	// "no main()" -- which is the better diagnostic: it names the language rule
	// rather than the linker symptom. Pin the message, not the phase.
	if !strings.Contains(string(o), "no main") {
		t.Errorf("diagnostic does not mention the missing entry: %q", o)
	}
}

// TestGoclMatchesGoc runs the same source through both compilers. The point is
// not that the bytes agree -- they cannot, one is LLVM's code and the other
// goc's -- but that the answer does: a program that behaves one way under goc
// has to behave the same way under gocl, or one of them is wrong.
func TestGoclMatchesGoc(t *testing.T) {
	gocPath := os.Getenv("GOC_TEST_GOC")
	if gocPath == "" {
		gocPath = filepath.Join(filepath.Dir(compilerPath(t)), "goc.exe")
		if _, err := os.Stat(gocPath); err != nil {
			t.Skipf("no goc.exe beside gocl; set GOC_TEST_GOC")
		}
	}
	if !llvmConfigured(t) {
		t.Skip("skipping: no libLLVM configured")
	}
	src := "#include <stdio.h>\n" +
		"int g = 7;\n" +
		"int *gp = &g;\n" +
		"int fib(int n){return n<2?n:fib(n-1)+fib(n-2);}\n" +
		"int main(void){printf(\"%d %d %d\\n\", fib(12), *gp, g*3); return 0;}\n"
	dir := t.TempDir()
	cPath := filepath.Join(dir, "a.c")
	if err := os.WriteFile(cPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	fromGocl := runOne(t, dir, cPath, compilerPath(t), "gocl")
	fromGoc := runOne(t, dir, cPath, gocPath, "goc")
	if fromGocl != fromGoc {
		t.Errorf("output differs:\n  gocl: %q\n  goc:  %q", fromGocl, fromGoc)
	}
	if !strings.Contains(fromGocl, "144 7 21") {
		t.Errorf("unexpected output %q", fromGocl)
	}
}

// runOne compiles src with the given compiler and returns what the program
// printed. Failures are fatal on the spot: a mismatch between the two
// compilers is only meaningful if both actually ran.
func runOne(t *testing.T, dir, src, bin, tag string) string {
	t.Helper()
	out := filepath.Join(dir, "a-"+tag+".exe")
	cmd := exec.Command(bin, src, "-o", out)
	cmd.Env = append(os.Environ(), "TMP="+dir, "TEMP="+dir, "TMPDIR="+dir)
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s failed: %v\n%s", tag, err, o)
	}
	r := exec.Command(out)
	o, _ := r.CombinedOutput()
	return string(o)
}
