package compiler

import (
	"goc/frontend"
	"strings"
	"testing"
)

// genAsmGUI is genAsmOpt with the Windows GUI subsystem selected, which is
// what a wWinMain program is built with (-mwindows).
func genAsmGUI(t *testing.T, src string) string {
	t.Helper()
	toks, err := Preprocess(src, "test.c")
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	prog, err := frontend.Parse(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if errs := frontend.Check(prog); len(errs) > 0 {
		t.Fatalf("type check: %v", errs)
	}
	asm, err := Gen(prog, false, 0, true)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	return asm
}

const wmSrc = `#include <windows.h>
int WINAPI wWinMain(HINSTANCE inst, HINSTANCE prev, PWSTR cmdline, int show) {
    (void)prev; (void)show; (void)inst;
    return cmdline != 0;
}
`

// TestWinMainAccepted proves a GUI program needs no main() shim: wWinMain is
// recognised as the entry point and the program assembles.
func TestWinMainAccepted(t *testing.T) {
	asm := genAsmGUI(t, wmSrc)
	if !strings.Contains(asm, "call wWinMain") {
		t.Fatalf("entry stub does not call wWinMain:/n%s", fnAsm(asm, "_start"))
	}
	if !strings.Contains(asm, "subsystem windows") {
		t.Error("GUI build is missing the `subsystem windows` directive")
	}
}

// TestWinMainStubArgs pins the synthesised WinMain arguments: hInstance from
// GetModuleHandleA(NULL), hPrevInstance NULL, the full command line from
// GetCommandLineW in r8, and SW_SHOWDEFAULT in r9d. Getting the Win64 argument
// registers wrong would compile fine and then read garbage.
func TestWinMainStubArgs(t *testing.T) {
	stub := fnAsm(genAsmGUI(t, wmSrc), "_start")
	for _, want := range []string{
		"call GetModuleHandleA",
		"call __goclib_lp_cmdline_w",
		"mov r8, rax",
		"mov r9d, 10",
	} {
		if !strings.Contains(stub, want) {
			t.Errorf("entry stub missing %q:/n%s", want, stub)
		}
	}
	// hPrevInstance must be NULL: the xor must come after hInstance is loaded
	// and before the wWinMain call, not before GetModuleHandleA.
	iMod := strings.Index(stub, "call GetModuleHandleA")
	iXor := strings.Index(stub, "xor rdx, rdx")
	iCall := strings.Index(stub, "call wWinMain")
	if !(iMod >= 0 && iMod < iXor && iXor < iCall) {
		t.Errorf("hPrevInstance zeroing out of order (mod=%d xor=%d call=%d):\n%s",
			iMod, iXor, iCall, stub)
	}
}

// TestWinMainImports proves the stub's own Win32 calls reach the PE import
// table. goa reports a missing import only at link time, so a regression here
// shows up as "goa failed: undefined symbol referenced", not a clean error.
func TestWinMainImports(t *testing.T) {
	asm := genAsmGUI(t, wmSrc)
	for _, want := range []string{
		"extern GetModuleHandleA, kernel32",
		"extern GetCommandLineW, kernel32",
	} {
		if !strings.Contains(asm, want) {
			t.Errorf("missing import line %q", want)
		}
	}
}

// TestWinMainPrefersMain proves main() still wins when a translation unit
// defines both, so adding a helper main to a GUI program cannot silently
// change which function the OS calls.
func TestWinMainPrefersMain(t *testing.T) {
	src := `#include <windows.h>
int WINAPI wWinMain(HINSTANCE a, HINSTANCE b, PWSTR c, int d) { (void)a;(void)b;(void)c;(void)d; return 1; }
int main(void) { return 0; }
`
	asm := genAsmGUI(t, src)
	stub := fnAsm(asm, "_start")
	if !strings.Contains(stub, "call main") {
		t.Errorf("main() should be the entry when both are defined:/n%s", stub)
	}
}

// TestWinMainAnsiVariant covers the ANSI WinMain, whose command line is LPSTR
// and therefore comes from GetCommandLineA.
func TestWinMainAnsiVariant(t *testing.T) {
	src := `#include <windows.h>
int WINAPI WinMain(HINSTANCE a, HINSTANCE b, LPSTR c, int d) { (void)a;(void)b;(void)c;(void)d; return 0; }
`
	stub := fnAsm(genAsmGUI(t, src), "_start")
	if !strings.Contains(stub, "call WinMain") {
		t.Fatalf("entry stub does not call WinMain:/n%s", stub)
	}
	if !strings.Contains(stub, "call __goclib_lp_cmdline_a") {
		t.Errorf("ANSI WinMain should take LPSTR from __goclib_lp_cmdline_a (argv[0] stripped):\n%s", stub)
	}
}
