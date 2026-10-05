//go:build windows

package goa

// Whole-pipeline check for the LLVM backend's link path: IR text -> COFF object
// (produced by libLLVM through the FFI layer) -> merged with the entry stub ->
// PE image -> executed, with the program's own result checked.
//
// This is the only test that proves the pieces fit together. Everything here can
// pass on its own and still yield an image that dies at load time, which is
// exactly what happened while this was being built:
//
//   - a relocation written a few bytes early corrupts the entry stub
//   - an unwind table holding section-relative offsets faults on the first call
//   - a .bss section dropped from the image aliases the unwind table
//   - an import naming "kernel32" instead of "kernel32.dll" fails to load
//
// The suite skips unless a libLLVM DLL is reachable; set GOC_LLVM_DLL to point
// at one. The .obj files are cached next to the IR so repeated runs are cheap.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// llvmOnce guards one-time DLL initialisation inside a test binary.
var llvmOnce sync.Once

// The entry stub mirrors what goc's codegen emits: capture the entry stack,
// align it, call main, hand the result to ExitProcess. The `extern` is the one
// import the stub needs; everything else comes from the object.
const llvmE2EStub = `
extern ExitProcess, kernel32
global _start
section .text
_start:
	mov r12, [rsp]
	lea r13, [rsp+8]
	and rsp, -16
	sub rsp, 48
	call main
	mov rcx, rax
	call ExitProcess
`

func TestLinkLLVMLifecycle(t *testing.T) {
	cases := []struct {
		file string
		want int
		why  string
	}{
		{"min.ll", 21, "constant folding only"},
		{"fib.ll", 6765, "recursive fib(20): call, recursion, stack growth"},
		{"pure.ll", 6766, "fib + bubble sort + global array + .bss counter"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			ll := loadLLVM(t)
			obj := compileIRToObject(t, ll, filepath.Join("testdata", tc.file))
			a := NewAssembler()
			if err := a.Assemble(llvmE2EStub); err != nil {
				t.Fatalf("assemble entry stub: %v", err)
			}
			if err := a.IngestCOFFBytes(obj); err != nil {
				t.Fatalf("merge object: %v", err)
			}
			out := filepath.Join(t.TempDir(), "a.exe")
			if err := a.BuildPE(out); err != nil {
				t.Fatalf("BuildPE: %v", err)
			}
			runAndCheckExit(t, out, tc.want, tc.why)
		})
	}
}

// TestLinkLLVMUnwindTablePresent guards the one thing whose absence is silent
// until something throws: an image whose code came from LLVM but whose exception
// directory was not carried over still links and still runs, and then cannot
// unwind a frame.
func TestLinkLLVMUnwindTablePresent(t *testing.T) {
	ll := loadLLVM(t)
	obj := compileIRToObject(t, ll, filepath.Join("testdata", "pure.ll"))
	a := NewAssembler()
	if err := a.Assemble(llvmE2EStub); err != nil {
		t.Fatal(err)
	}
	if err := a.IngestCOFFBytes(obj); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "a.exe")
	if err := a.BuildPE(out); err != nil {
		t.Fatal(err)
	}
	img, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	rva, size := peDataDir(t, img, 3) // exception directory
	if size == 0 {
		t.Fatal("exception directory is empty: the loader cannot unwind frames")
	}
	entries := size / 12
	// Each RUNTIME_FUNCTION must name a range inside .text and an unwind record
	// inside .xdata; a zero or out-of-range value here is what turns an
	// exception into STATUS_PRIVILEGED_INSTRUCTION.
	textBase, textEnd := sectionRange(t, img, ".text")
	xdata, xdataEnd := sectionRange(t, img, ".xdata")
	if xdata == 0 {
		t.Fatal("image has an exception directory but no .xdata section")
	}
	for i := 0; i < entries; i++ {
		off := fileOffsetOfRVA(t, img, rva+i*12)
		begin := rd32(img, off)
		end := rd32(img, off+4)
		unwind := rd32(img, off+8)
		if begin < textBase || end > textEnd || end <= begin {
			t.Errorf("entry %d: code range 0x%x..0x%x is not inside .text (0x%x..0x%x)",
				i, begin, end, textBase, textEnd)
		}
		if unwind < xdata || unwind >= xdataEnd {
			t.Errorf("entry %d: unwind RVA 0x%x is not inside .xdata (0x%x..0x%x)",
				i, unwind, xdata, xdataEnd)
		}
	}
	t.Logf("%d RUNTIME_FUNCTION entries, all in range", entries)
}

// TestLinkLLVMRejectsUnknownUndefined makes sure a symbol nothing defines is
// reported as a link error. Importing it on a guess builds an image that loads
// and then dies with STATUS_ENTRYPOINT_NOT_FOUND, which says nothing useful.
func TestLinkLLVMRejectsUnknownUndefined(t *testing.T) {
	ll := loadLLVM(t)
	src := `
target triple = "x86_64-w64-windows-gnu"
declare i32 @no_such_function_anywhere()
define i32 @main() {
entry:
  %v = call i32 @no_such_function_anywhere()
  ret i32 %v
}
`
	path := filepath.Join(t.TempDir(), "u.ll")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	obj := compileIRToObject(t, ll, path)
	a := NewAssembler()
	if err := a.Assemble(llvmE2EStub); err != nil {
		t.Fatal(err)
	}
	err := a.IngestCOFFBytes(obj)
	if err == nil {
		t.Fatal("expected a link error for an undefined symbol")
	}
	if !contains(err.Error(), "no_such_function_anywhere") {
		t.Errorf("the error should name the symbol; got: %v", err)
	}
}

// --- helpers ---------------------------------------------------------------

// runAndCheckExit runs a freshly linked image and compares its exit code. The
// entry stub passes main's return value to ExitProcess, so the program's own
// result is observable this way.
func runAndCheckExit(t *testing.T, exe string, want int, why string) {
	t.Helper()
	cmd := exec.Command(exe)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("%s: expected a non-zero exit status from the stub, got success (%q)", why, out)
	}
	code := cmd.ProcessState.ExitCode()
	if code != want {
		t.Errorf("%s: exit code %d, want %d", why, code, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
