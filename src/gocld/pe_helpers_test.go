//go:build windows

package gocld

// Minimal PE reading, for the assertions in this package's tests.
//
// They were goa's test helpers, and goa's link tests no longer exist: the
// image-building code they inspect is here now. Repeating three small readers
// is cheaper than exporting them from a package that has no other use for them.

// The tests in this directory read fields back out of a built image so they can
// assert on them, which needs three small PE readers.

import (
	"testing"
)

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

func peDataDir(t *testing.T, img []byte, idx int) (rva, size int) {
	t.Helper()
	pe := peOff(t, img)
	oh := pe + 24
	if oh+112+idx*8+8 > len(img) {
		t.Fatalf("data directory %d out of range", idx)
	}
	return rd32(img, oh+112+idx*8), rd32(img, oh+112+idx*8+4)
}

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
