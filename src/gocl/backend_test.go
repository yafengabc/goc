package gocl

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
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// findUp walks up from the package directory looking for a file under bin/,
// and returns its path. Both helpers below need it and both used to hardcode a
// ".." count, which the move from src/ to cmd/goc/ silently invalidated -- the
// failure was a test that reported "no libLLVM configured" rather than a wrong
// path, so nothing pointed at the real cause.
//
// Dir() per level, not filepath.Join(dir, ".."): Join cleans the ".." away and
// returns the same directory, so the loop would never climb.
// findUp walks up from the working directory looking for bin/name, and returns
// "" when it reaches the root without a hit. name is the bare file name: the
// bin directory is this function's business, so callers must not pass "bin/..."
// (that made it look for bin/bin/... and never match, which is why the three
// volume tests below skipped instead of running).
//
// src/ is skipped. Walking up from src/gocl passes src/ itself, and a stale
// src/bin/gocl.exe from an earlier `go build ./...` sits closer than the real
// bin/gocl.exe -- so the tests would silently exercise an expired artifact.
func findUp(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir, i := wd, 0; i < 6; i++ {
		if filepath.Base(dir) != "src" {
			p := filepath.Join(dir, "bin", name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// llvmAvailable reports whether a -fllvm build can run here.
func llvmAvailable(t *testing.T) bool {
	t.Helper()
	if os.Getenv("GOC_LLVM_DLL") == "" {
		dll := "libLLVM.dll"
		if runtime.GOOS != "windows" {
			dll = "libLLVM.so"
		}
		return findUp(t, dll) != ""
	}
	return true
}

// containsBytes reports whether hay holds needle. strings.Contains would do,
// but the output arrives as bytes from a program's stdout and this keeps the
// conversion local to the one place it is needed.
func containsBytes(hay, needle string) bool {
	return strings.Contains(hay, needle)
}

// exePath finds the compiler these tests drive. It is gocl, not goc: the
// -fllvm flag this file used to pass belongs to the back end that has since
// been split out into its own command.
func exePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GOC_TEST_GOCL"); p != "" {
		return p
	}
	name := "gocl.exe"
	if runtime.GOOS != "windows" {
		name = "gocl"
	}
	if p := findUp(t, name); p != "" {
		return p
	}
	wd, _ := os.Getwd()
	t.Skipf("no bin/%s found above %s; run `bash build.sh` first", name, wd)
	return ""
}
