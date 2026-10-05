package main

// End-to-end tests for the -fllvm back end: a C file goes in, an executable that
// computes the right answer comes out.
//
// These run the whole pipeline rather than a stage of it, because that is the
// only way the interesting failures show up. A front end that emits valid IR for
// a module the linker cannot satisfy still produces a binary; a link step that
// quietly drops a function still produces one. Only running the result catches
// either.
//
// The shared library is optional: without it these skip, and a goc build without
// LLVM behaves exactly as it did before any of this existed.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// llvmAvailable reports whether a -fllvm build can run here.
func llvmAvailable(t *testing.T) bool {
	t.Helper()
	if os.Getenv("GOC_LLVM_DLL") == "" {
		if _, err := filepath.Abs(filepath.Join("..", "..", "bin", "libLLVM.dll")); err != nil {
			return false
		}
	}
	return true
}

// buildAndRun compiles src with the LLVM back end and returns the program's exit
// code and stdout.
func buildAndRun(t *testing.T, name, src string, wantCode int, wantOut string) {
	t.Helper()
	if !llvmAvailable(t) {
		t.Skip("skipping: no libLLVM configured (set GOC_LLVM_DLL)")
	}
	dir := t.TempDir()
	cPath := filepath.Join(dir, name+".c")
	if err := os.WriteFile(cPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, name+".exe")
	cmd := exec.Command(exePath(t), "-fllvm", cPath, "-o", exe)
	cmd.Env = append(os.Environ(), "TMP="+dir, "TEMP="+dir, "TMPDIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("goc -fllvm failed: %v\n%s", err, out)
	}
	run := exec.Command(exe)
	o, _ := run.CombinedOutput()
	if run.ProcessState.ExitCode() != wantCode {
		t.Errorf("exit code %d, want %d (stdout %q)", run.ProcessState.ExitCode(), wantCode, o)
	}
	if wantOut != "" && !containsBytes(string(o), wantOut) {
		t.Errorf("stdout %q does not contain %q", o, wantOut)
	}
}

// containsBytes reports whether hay holds needle. strings.Contains would do,
// but the output arrives as bytes from a program's stdout and this keeps the
// conversion local to the one place it is needed.
func containsBytes(hay, needle string) bool {
	return strings.Contains(hay, needle)
}

// exePath finds the compiler under test. The test binary lives in a temporary
// directory, so the binary is located relative to the source tree instead.
func exePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GOC_TEST_EXE"); p != "" {
		return p
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// wd is <repo>/src
	return filepath.Join(wd, "..", "bin", "goc.exe")
}

func TestLLVMBackendEndToEnd(t *testing.T) {
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
			name: "loops and arrays",
			src: "int sum(int *a, int n){int s=0;for(int i=0;i<n;i++)s+=a[i];return s;}\n" +
				"int main(void){int a[5]={1,2,3,4,5};return sum(a,5);}",
			wantCode: 15,
		},
		{
			name: "nested loops and sorting",
			src: "void bsort(int a[],int n){for(int i=0;i<n-1;i++)for(int j=0;j<n-1-i;j++)\n" +
				"if(a[j]>a[j+1]){int t=a[j];a[j]=a[j+1];a[j+1]=t;}}\n" +
				"int main(void){int a[5]={5,3,1,4,2};bsort(a,5);return a[0]*100+a[4];}",
			wantCode: 105, // sorted {1,2,3,4,5}: first=1, last=5 → 1*100+5
		},
		{
			name:     "pointer arithmetic",
			src:      "int f(int *p, int n){int s=0;while(n--)s+=*p++;return s;}\nint main(void){int a[3]={10,20,30};return f(a,3);}",
			wantCode: 60,
		},
		{
			// printf is variadic, so main stays on the native path while the
			// helpers go through LLVM. The output proves both halves linked.
			name: "runtime call alongside IR code",
			src: "#include <stdio.h>\nint dbl(int x){return x*2;}\n" +
				"int main(void){printf(\"v=%d\\n\", dbl(21));return dbl(2);}",
			wantCode: 4,
			wantOut:  "v=42",
		},
		{
			name:     "global variable",
			src:      "int g = 7;\nint main(void){ g = g * 3; return g; }",
			wantCode: 21,
		},
		{
			// A bare function name in a value context is the function's
			// address. The IR front end has to recognise that rather than
			// falling through to "a name with no storage", which renders a
			// null -- and a null function pointer faults at the first
			// indirect call. goclib's own printf_lite_with(vfmt_i, ...) is
			// this same shape, which is how the gap reached a hello-world.
			name: "function pointer assigned and called",
			src: "static int twice(int x){return x*2;}\n" +
				"int main(void){int (*fp)(int) = twice; return fp(21) == 42 ? 0 : 1;}",
			wantCode: 0,
		},
		{
			name: "function pointer passed as an argument",
			src: "static int twice(int x){return x*2;}\n" +
				"static int apply(int (*fn)(int), int v){return fn(v);}\n" +
				"int main(void){return apply(twice, 21) == 42 ? 0 : 1;}",
			wantCode: 0,
		},
		{
			// The C library's own path: printf_lite_with takes the formatter
			// as a function pointer, so a printf that specialises to the lite
			// formatter goes through this too.
			name:     "printf reaching a function-pointer parameter",
			src:      "#include <stdio.h>\nint main(void){printf(\"n=%d\\n\", 42);return 0;}",
			wantCode: 0,
			wantOut:  "n=42",
		},
		{
			name:     "struct by pointer",
			src:      "struct P{int x;int y;};\nint sum(struct P *p){return p->x+p->y;}\nint main(void){struct P p; p.x=3; p.y=4; return sum(&p);}",
			wantCode: 7,
		},
		{
			name:     "ternary and logical operators",
			src:      "int f(int a,int b){return (a>b?a:b) + (a&&b?1:0);}\nint main(void){return f(5,3);}",
			wantCode: 6,
		},
		{
			name:     "do-while",
			src:      "int f(int n){int s=0;do{s+=n;n--;}while(n>0);return s;}\nint main(void){return f(5);}",
			wantCode: 15,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			buildAndRun(t, tc.name, tc.src, tc.wantCode, tc.wantOut)
		})
	}
}

// TestLLVMBackendNeedsTheLibrary checks that asking for the LLVM back end
// without the library fails loudly. Silently falling back would produce a
// different binary than the one that was asked for, and the difference would
// only show up as a performance regression nobody could explain.
func TestLLVMBackendNeedsTheLibrary(t *testing.T) {
	if !llvmAvailable(t) {
		t.Skip("skipping: no libLLVM configured")
	}
	dir := t.TempDir()
	cPath := filepath.Join(dir, "a.c")
	if err := os.WriteFile(cPath, []byte("int main(void){return 0;}"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exePath(t), "-fllvm", cPath, "-o", filepath.Join(dir, "a.exe"))
	cmd.Env = append(os.Environ(),
		"GOC_LLVM_DLL="+filepath.Join(dir, "no-such-library.dll"),
		"TMP="+dir, "TEMP="+dir, "TMPDIR="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a build with -fllvm and no library succeeded; it should have failed\n%s", out)
	}
	if !containsBytes(string(out), "LLVM") {
		t.Errorf("the error should name the missing library; got: %s", out)
	}
}

// TestLLVMBackendIsOptional guards the promise that a goc without LLVM behaves
// exactly as before: the same program, built the same way, produces the same
// result with the flag absent.
func TestLLVMBackendIsOptional(t *testing.T) {
	dir := t.TempDir()
	cPath := filepath.Join(dir, "a.c")
	src := "int f(int n){return n<2?n:f(n-1)+f(n-2);}\nint main(void){return f(10);}"
	if err := os.WriteFile(cPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "a.exe")
	cmd := exec.Command(exePath(t), cPath, "-o", exe)
	// An empty GOC_LLVM_DLL must not turn the native path off.
	cmd.Env = append(os.Environ(), "GOC_LLVM_DLL=", "TMP="+dir, "TEMP="+dir, "TMPDIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the native build failed: %v\n%s", err, out)
	}
	run := exec.Command(exe)
	run.Run()
	if c := run.ProcessState.ExitCode(); c != 55 {
		t.Errorf("native build returned %d, want 55", c)
	}
}
