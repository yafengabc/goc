//go:build windows

package goa

// Test-side glue for the LLVM pipeline tests: locating the library, turning IR
// into an object, and reading a few fields back out of a built image so the
// tests can assert on them.

import (
	"os"
	"path/filepath"
	"testing"
)

// loadLLVM returns the LLVM binding, skipping the test when no library is
// installed. A backend that is present but broken should fail loudly; one that
// is absent is a normal configuration.
func loadLLVM(t *testing.T) *llvmAPI {
	t.Helper()
	api, err := LoadLLVM()
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	if api == nil {
		t.Skip("skipping: no LLVM library")
	}
	return api
}

// compileIRToObject runs IR text through LLVM and returns the object bytes.
// Objects are cached next to the IR so a repeated run costs nothing.
func compileIRToObject(t *testing.T, api *llvmAPI, irPath string) []byte {
	t.Helper()
	ir, err := os.ReadFile(irPath)
	if err != nil {
		t.Fatalf("read IR: %v", err)
	}
	objPath := filepath.Join(t.TempDir(), "out.obj")
	if err := api.CompileToObject(ir, objPath, LLVMOptAggressive, ""); err != nil {
		t.Fatalf("CompileToObject: %v", err)
	}
	obj, err := os.ReadFile(objPath)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if len(obj) == 0 {
		t.Fatal("LLVM produced an empty object")
	}
	return obj
}

// --- minimal PE reading, for assertions ------------------------------------

func peOff(t *testing.T, img []byte) int {
	t.Helper()
	if len(img) < 0x40 || img[0] != 'M' || img[1] != 'Z' {
		t.Fatal("not a PE image")
	}
	o := rd32(img, 0x3C)
	if o <= 0 || o+24 >= len(img) {
		t.Fatal("bad e_lfanew")
	}
	return o
}

// peDataDir returns the RVA and size of one data directory (0 export, 1 import,
// 3 exception, 9 TLS, 12 IAT).
func peDataDir(t *testing.T, img []byte, idx int) (rva, size int) {
	t.Helper()
	pe := peOff(t, img)
	oh := pe + 24
	if oh+112+idx*8+8 > len(img) {
		t.Fatalf("data directory %d out of range", idx)
	}
	return rd32(img, oh+112+idx*8), rd32(img, oh+112+idx*8+4)
}

// sectionRange returns the virtual address range [va, va+size) of a section.
// PE section names occupy eight bytes padded with SPACES, not NULs, so the
// comparison has to be a prefix match rather than an exact eight-byte compare.
func sectionRange(t *testing.T, img []byte, name string) (int, int) {
	t.Helper()
	pe := peOff(t, img)
	n := rd16(img, pe+6)
	oh := pe + 24
	st := oh + 240
	for i := 0; i < n; i++ {
		b := st + 40*i
		if b+40 > len(img) {
			break
		}
		if !sectionIs(img[b:b+8], name) {
			continue
		}
		va := rd32(img, b+12)
		vsz := rd32(img, b+8)
		return va, va + vsz
	}
	return 0, 0
}

func sectionIs(field []byte, name string) bool {
	if len(name) > len(field) {
		return false
	}
	for i := 0; i < len(name); i++ {
		if field[i] != name[i] {
			return false
		}
	}
	return true
}

// fileOffsetOfRVA maps a virtual address to a file offset.
func fileOffsetOfRVA(t *testing.T, img []byte, rva int) int {
	t.Helper()
	pe := peOff(t, img)
	n := rd16(img, pe+6)
	oh := pe + 24
	st := oh + 240
	for i := 0; i < n; i++ {
		b := st + 40*i
		if b+40 > len(img) {
			break
		}
		va := rd32(img, b+12)
		vsz := rd32(img, b+8)
		ra := rd32(img, b+20)
		rsz := rd32(img, b+16)
		if rva >= va && rva < va+vsz && rsz > 0 {
			return ra + (rva - va)
		}
	}
	t.Fatalf("RVA 0x%x is not inside any section with file contents", rva)
	return 0
}
