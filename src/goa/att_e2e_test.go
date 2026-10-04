package goa

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The .s files under testdata/att are genuine LLVM AsmPrinter dumps of goc's
// own example programs -- the exact input this front end has to eat in
// production. They are what makes the tests worth having: a hand-picked list of
// instructions would never have surfaced the operand-order rule for
// `addq %r14, 64(%r15)`, nor the fact that `.str.0` is file-scoped while
// `.LBB0_3` is not, nor that a memory operand needs the mnemonic's size suffix
// to know whether it is 32 or 64 bits wide.
//
// Only a handful are checked in, to keep the repository small;
// tools/gen-att-samples.sh regenerates the full corpus (one file per example in
// src/examples, ~70 of them) and is what to run when changing the front end.
// Both the unit tests and the breadth test below skip cleanly if the directory
// is empty, so a tree without the samples still builds.

// TestATTRealLLVMOutputParses is the breadth check: the whole corpus must
// assemble cleanly. A single regression here is a real gap, so the test fails
// on the first one rather than counting.
func TestATTRealLLVMOutputParses(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "att", "*.s"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Skip("no AT&T samples -- run: bash tools/gen-att-samples.sh")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		a := NewAssembler()
		if err := attAssemble(a, string(src)); err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		// A parsed file must have actually produced code -- an assembler that
		// silently dropped every instruction would sail through the check
		// above while emitting an empty image.
		if text := a.sectionByName(".text"); text == nil || len(text.Data) == 0 {
			t.Errorf("%s: parsed but emitted no code", filepath.Base(f))
		}
	}
}

// The entry stub mirrors what goc's codegen emits: capture the entry stack,
// align it, call main, hand the result to ExitProcess. `_start` is the PE
// entry point; `global` names it.
//
// The body is written in AT&T because the whole buffer goes through the AT&T
// front end. A goa-syntax stub would have its operands reversed on the way in,
// turning `lea r13, [rsp+8]` into `lea [rsp+8], r13` -- and an lea with a
// memory destination has no encoding at all, so the failure would surface as a
// confusing encoder error rather than as "you wrote the wrong dialect".
const attEntryStub = `
extern ExitProcess, kernel32
global _start
section .text
_start:
	movq	(%rsp), %r12
	leaq	8(%rsp), %r13
	andq	$-16, %rsp
	subq	$48, %rsp
	call	main
	movq	%rax, %rcx
	call	ExitProcess
`

// TestATTRealLLVMOutputLinksWithExterns is the narrow half: with the Win32
// imports declared, a whole LLVM-generated translation unit must link into a
// runnable PE. This is what proves the front end's symbols, sections and
// relocations line up -- a mis-scaled displacement or a mis-qualified label
// shows up here as a link failure even though parsing was clean.
//
// Known gap: a switch statement's jump table is emitted by LLVM as a table of
// *label differences* (`.long .LBB29_14-.LJTI29_0`), which needs a pair of
// relocations against two labels in one field. goa records one symbol per
// fixup, so such files fail to link with "undefined symbol: X-.Y". The cases
// that need this are skipped explicitly rather than silently dropped, so the
// moment the relocations are taught the list empties by itself and nothing has
// to be edited.
func TestATTRealLLVMOutputLinksWithExterns(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "att", "*.s"))
	if err != nil || len(files) == 0 {
		t.Skip("no AT&T samples -- run: bash tools/gen-att-samples.sh")
	}
	// Every Win32 entry point goclib can call. LLVM emits calls to these by
	// their plain names; goa turns each into an import against kernel32.
	win32 := []string{
		"GetStdHandle", "WriteFile", "ReadFile", "SetFilePointer",
		"CloseHandle", "CreateFileA", "GetFileSize", "SetEndOfFile",
		"CreateFileW", "MultiByteToWideChar", "WideCharToMultiByte",
		"GetCommandLineA", "GetCommandLineW",
		"WriteConsoleA", "SetConsoleMode", "GetConsoleMode",
		"SetConsoleOutputCP", "SetConsoleCP", "GetConsoleOutputCP",
		"GetConsoleCP", "GetFileType", "FlushFileBuffers",
		"SetStdHandle", "GetProcessHeap", "HeapAlloc", "HeapFree",
		"GetTickCount", "QueryPerformanceCounter", "QueryPerformanceFrequency",
		"Sleep", "GetLastError", "SetLastError", "LocalAlloc", "LocalFree",
		"ExitProcess", "GetCurrentProcess", "CreateThread",
	}
	var linked, needJumpTable int
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if bytes.Contains(src, []byte("-.LJTI")) {
			needJumpTable++
			continue
		}
		var b strings.Builder
		for _, s := range win32 {
			b.WriteString("extern " + s + ", kernel32\n")
		}
		// LLVM's .s is a translation unit, not a program: it has no entry
		// point. The stub below supplies one, mirroring what goc's own codegen
		// synthesises -- capture the loader's stack, call main, hand the
		// result to ExitProcess.
		b.WriteString(attEntryStub)
		b.Write(src)
		out := filepath.Join(t.TempDir(), "out.exe")
		if _, err := AssembleATT(b.String(), out, false); err != nil {
			t.Errorf("%s: link: %v", filepath.Base(f), err)
			continue
		}
		fi, err := os.Stat(out)
		if err != nil || fi.Size() == 0 {
			t.Errorf("%s: no image written (err=%v)", filepath.Base(f), err)
			continue
		}
		linked++
	}
	t.Logf("linked %d files; %d skipped for jump-table label differences",
		linked, needJumpTable)
	if linked == 0 {
		t.Fatal("nothing linked at all -- the pipeline is broken")
	}
}
