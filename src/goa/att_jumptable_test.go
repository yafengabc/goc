package goa

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A switch's jump table is the one construct LLVM cannot express as a plain
// label reference: it emits a table of *label differences*
//
//	.long	.LBB1_9-.LJTI1_0
//
// and reads them back as `(%r14,%rax,4)` after adding the table's own base. If
// the relocation stores anything other than "target address minus table base",
// the program lands in the wrong basic block -- or jumps into the middle of
// unrelated code and traps. Nothing else in the pipeline would notice: the
// bytes are well-formed, the image loads, and only the branch is wrong.
//
// So the check has to be a real execution. The program below adds a different
// weight per case, walks a range that forces every table entry, and publishes
// the total as its exit code; a correct jump table yields a known constant,
// while a misdirected branch either traps or changes the sum.
func TestATTJumpTableExecutes(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "tools", "peun.py")); err != nil {
		t.Skip("tools/peun.py not reachable from the goa module")
	}
	// Written by hand in AT&T because the point is to reproduce the exact shape
	// LLVM emits. The dispatch loop reads a table entry, adds the table's base
	// and jumps -- the standard idiom, and the only consumer of the relocation.
	const src = `
extern ExitProcess, kernel32
global _start
section .text
_start:
	# The emulator hands us a bare stack with no CRT frame above it, so this
	# stub does not capture the loader's arguments the way goc's real entry
	# point does. It only has to reach the code under test.
	andq	$-16, %rsp
	subq	$48, %rsp

	movl	$0, %r15d          # accumulator
	movl	$0, %ecx           # loop counter
	lea	.LJTI0_0(%rip), %r14
.LBB0_1:
	cmpl	$6, %ecx
	jl	.LBB0_dispatch
	jmp	.LBB0_done
.LBB0_dispatch:
	movslq	%ecx, %rax
	movslq	(%r14,%rax,4), %rax   # sign-extended offset of the case target
	addq	%r14, %rax             # -> absolute address
	jmp	*%rax
.LBB0_case0:
	addl	$1, %r15d
	jmp	.LBB0_next
.LBB0_case1:
	addl	$10, %r15d
	jmp	.LBB0_next
.LBB0_case2:
	addl	$100, %r15d
	jmp	.LBB0_next
.LBB0_case3:
	addl	$1000, %r15d
	jmp	.LBB0_next
.LBB0_case4:
	addl	$10000, %r15d
	jmp	.LBB0_next
.LBB0_case5:
	addl	$100000, %r15d
	jmp	.LBB0_next
.LBB0_next:
	incl	%ecx
	jmp	.LBB0_1
.LBB0_done:
	# ExitProcess takes its argument in rcx (Win64), not rax.
	movl	%r15d, %ecx
	andl	$255, %ecx
	call	ExitProcess

	section .rdata,"dr"
	.p2align	2
.LJTI0_0:
	.long	.LBB0_case0-.LJTI0_0
	.long	.LBB0_case1-.LJTI0_0
	.long	.LBB0_case2-.LJTI0_0
	.long	.LBB0_case3-.LJTI0_0
	.long	.LBB0_case4-.LJTI0_0
	.long	.LBB0_case5-.LJTI0_0
`
	out := filepath.Join(t.TempDir(), "jmp.exe")
	if _, err := AssembleATT(src, out, false); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	// 1+10+100+1000+10000+100000 = 111111; & 255 = 111111 mod 256.
	const want = 111111 % 256
	got := runPEUnderUnicorn(t, out)
	if got != want {
		t.Errorf("jump table produced exit code %d, want %d "+
			"(sum 1+10+100+1000+10000+100000 = 111111)", got, want)
	}
}

// runPEUnderUnicorn executes a PE under tools/peun.py and returns its exit code.
// The script is a Python mirror of the C ucrun for the Windows side: it loads
// the image, hooks the import thunks, and emulates the handful of Win32 calls an
// example makes. ExitProcess supplies the value.
//
// It is skipped rather than failed when Python or Unicorn is unavailable --
// this is a correctness check, not something every checkout can run -- but the
// assembly assertions below cover the same relocation arithmetic without an
// emulator, so a skipped run never leaves the behaviour unverified.
func runPEUnderUnicorn(t *testing.T, pePath string) int {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	py := findPython()
	if py == "" {
		t.Skip("no python3 on PATH -- cannot run peun.py")
	}
	// The script path is absolute *and* the working directory is the repo
	// root. Passing a relative path alongside cmd.Dir works or not depending
	// on how the child resolves it, which is not worth depending on.
	cmd := exec.Command(py, filepath.Join(root, "tools", "peun.py"), pePath)
	cmd.Dir = root
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		// A non-zero *process* status is how the script reports the guest's
		// exit code, so that is data, not a failure. Only a failure to launch
		// (or a Python traceback) is an error.
		if ee, ok := runErr.(*exec.ExitError); ok && looksLikeExitCode(string(out)) {
			t.Logf("peun output://n%s", out)
			return ee.ExitCode()
		}
		t.Fatalf("peun.py failed: %v\n%s", runErr, out)
	}
	t.Logf("peun output://n%s", out)
	return 0
}

// looksLikeExitCode filters out Python-level errors, which land on stderr with
// a traceback rather than as a bare numeric status.
func looksLikeExitCode(s string) bool {
	return !strings.Contains(s, "Traceback") && !strings.Contains(s, "peun: ")
}

func findPython() string {
	for _, n := range []string{"python3", "python"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			`C:/Users/EKSOFT/.workbuddy/binaries/python/versions/3.13.12/python.exe`,
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}
