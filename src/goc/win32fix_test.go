package compiler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// readGoclib reads a goclib header from the source tree for source-level checks.
// readGoclib reads a file out of the C library, through the same lookup the
// compiler itself uses. Reading it by the relative path "goclib/<name>" would
// work only from the directory that happens to contain the library, which is
// exactly the assumption the move to cmd/goc broke -- and it would keep
// breaking as the layout changes again. Going through findGoclibRoot also means
// these tests exercise the real search order, so a library that moved and
// stopped being found fails here rather than silently skipping the assertions.
func readGoclib(t *testing.T, name string) string {
	t.Helper()
	b, err := goclibHeaders.ReadFile("goclib/" + name)
	if err != nil {
		t.Fatalf("read %s: %v (GOCLIB_PATH=%q)", name, err, os.Getenv(goclibRootEnv))
	}
	return string(b)
}

// TestCastUnsignedCharZeroExtend (bug 1): (unsigned char)x must mask the value
// to its low 8 bits, never leaving the high bits of a sign-extended source
// (which made `(unsigned char)0xE2` come out as 0xFFFFFFE2 and broke UTF-8
// byte emission).
func TestCastUnsignedCharZeroExtend(t *testing.T) {
	asm := genAsm(t, `int main(void) { int x = 0x1234; return (unsigned char)x; }`)
	if !strings.Contains(asm, "and eax, 0xFF") {
		t.Errorf("cast to unsigned char missing `and eax, 0xFF` masking:\n%s", asm)
	}
}

// TestCastUnsignedShortZeroExtend (bug 1): (unsigned short)x masks to its low
// 16 bits.
func TestCastUnsignedShortZeroExtend(t *testing.T) {
	asm := genAsm(t, `int main(void) { int x = 0x5678; return (unsigned short)x; }`)
	if !strings.Contains(asm, "and eax, 0xFFFF") {
		t.Errorf("cast to unsigned short missing `and eax, 0xFFFF` masking:\n%s", asm)
	}
}

// TestGoclibObjConstantsMatchSDK (bug 3): OBJ_* must carry the real Windows
// values (OBJ_BITMAP=7, ...). The old 0-based table made GetCurrentObject(dc,
// OBJ_BITMAP) ask GDI for "type 0", which silently returns NULL.
func TestGoclibObjConstantsMatchSDK(t *testing.T) {
	h := readGoclib(t, "wingdi.h")
	want := map[string]string{
		"OBJ_BITMAP": "7",
		"OBJ_PEN":    "1",
		"OBJ_FONT":   "6",
		"OBJ_REGION": "8",
	}
	for name, val := range want {
		re := regexp.MustCompile(`(?m)#define\s+` + regexp.QuoteMeta(name) + `\s+` + regexp.QuoteMeta(val) + `\b`)
		if !re.MatchString(h) {
			t.Errorf("wingdi.h missing `#define %s %s`", name, val)
		}
	}
}

// TestGoclibWin32SurfacePresent (bug 4): the four previously-missing Win32
// surface declarations must be present so a real GUI app (gomd) links.
func TestGoclibWin32SurfacePresent(t *testing.T) {
	wingdi := readGoclib(t, "wingdi.h")
	winbase := readGoclib(t, "winbase.h")
	windef := readGoclib(t, "windef.h")
	if !strings.Contains(wingdi, "extern DWORD GetObjectType") {
		t.Error("wingdi.h missing GetObjectType")
	}
	if !strings.Contains(wingdi, "#define HGDI_ERROR") {
		t.Error("wingdi.h missing HGDI_ERROR")
	}
	if !strings.Contains(winbase, "extern FARPROC GetProcAddress") {
		t.Error("winbase.h missing GetProcAddress")
	}
	if !strings.Contains(windef, "typedef int (*FARPROC)(void)") {
		t.Error("windef.h missing FARPROC typedef")
	}
}

// TestWinMainCmdLineStripped (bug 5): the synthesised wWinMain stub must hand
// wWinMain a command line with argv[0] already stripped (via the goclib
// helper), not the raw GetCommandLineW that still embeds the program name --
// otherwise `wcscmp(cmdline, L"--test")` style self-tests never match.
func TestWinMainCmdLineStripped(t *testing.T) {
	stub := fnAsm(genAsmGUI(t, wmSrc), "_start")
	if !strings.Contains(stub, "call __goclib_lp_cmdline_w") {
		t.Errorf("entry stub does not call __goclib_lp_cmdline_w:\n%s", stub)
	}
	if strings.Contains(stub, "call GetCommandLineW") {
		t.Errorf("entry stub still calls GetCommandLineW directly (argv[0] not stripped):\n%s", stub)
	}
}
