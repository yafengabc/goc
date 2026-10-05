package goa

import (
	"reflect"
	"testing"
)

// The LOCK# prefix (F0) turns a read-modify-write into an atomic operation,
// which is what C11 _Atomic lowers to. It is a legacy prefix, so it has to be
// emitted before REX and the opcode -- assembling the rest of the line
// unchanged once F0 is out is enough, and keeps every existing encoder usable
// under lock without touching it.
func TestLockPrefix(t *testing.T) {
	// lock add dword [rax], 1 -> F0 83 00 01
	want := []byte{0xF0, 0x83, 0x00, 0x01}
	if got := encAsm(t, "lock add dword [rax], 1"); !reflect.DeepEqual(got, want) {
		t.Errorf("lock add dword [rax], 1 = % X, want % X", got, want)
	}
	// 64-bit immediate add keeps REX.W after the prefix: F0 48 83 00 01
	want = []byte{0xF0, 0x48, 0x83, 0x00, 0x01}
	if got := encAsm(t, "lock add qword [rax], 1"); !reflect.DeepEqual(got, want) {
		t.Errorf("lock add qword [rax], 1 = % X, want % X", got, want)
	}
	// lock inc dword [rax] -> F0 FF 00
	want = []byte{0xF0, 0xFF, 0x00}
	if got := encAsm(t, "lock inc dword [rax]"); !reflect.DeepEqual(got, want) {
		t.Errorf("lock inc dword [rax] = % X, want % X", got, want)
	}
	if err := (func() error {
		a := NewAssembler()
		return a.processLine("lock")
	})(); err == nil {
		t.Error("a bare 'lock' with no instruction was accepted")
	}
}

func TestXadd(t *testing.T) {
	// xadd dword [rax], ecx -> 0F C1 08  (reg=ecx, rm=[rax])
	want := []byte{0x0F, 0xC1, 0x08}
	if got := encAsm(t, "xadd dword [rax], ecx"); !reflect.DeepEqual(got, want) {
		t.Errorf("xadd dword [rax], ecx = % X, want % X", got, want)
	}
	// 8-bit form is 0F C0 /r
	want = []byte{0x0F, 0xC0, 0x08}
	if got := encAsm(t, "xadd byte [rax], cl"); !reflect.DeepEqual(got, want) {
		t.Errorf("xadd byte [rax], cl = % X, want % X", got, want)
	}
	// 64-bit: REX.W + 0F C1
	want = []byte{0x48, 0x0F, 0xC1, 0x08}
	if got := encAsm(t, "xadd qword [rax], rcx"); !reflect.DeepEqual(got, want) {
		t.Errorf("xadd qword [rax], rcx = % X, want % X", got, want)
	}
	// fetch-and-add is the whole point: F0 0F C1 08
	want = []byte{0xF0, 0x0F, 0xC1, 0x08}
	if got := encAsm(t, "lock xadd dword [rax], ecx"); !reflect.DeepEqual(got, want) {
		t.Errorf("lock xadd dword [rax], ecx = % X, want % X", got, want)
	}
}

func TestCmpxchg(t *testing.T) {
	// cmpxchg dword [rax], ecx -> 0F B1 08
	want := []byte{0x0F, 0xB1, 0x08}
	if got := encAsm(t, "cmpxchg dword [rax], ecx"); !reflect.DeepEqual(got, want) {
		t.Errorf("cmpxchg dword [rax], ecx = % X, want % X", got, want)
	}
	// 8-bit form is 0F B0 /r
	want = []byte{0x0F, 0xB0, 0x08}
	if got := encAsm(t, "cmpxchg byte [rax], cl"); !reflect.DeepEqual(got, want) {
		t.Errorf("cmpxchg byte [rax], cl = % X, want % X", got, want)
	}
	// 64-bit: REX.W + 0F B1
	want = []byte{0x48, 0x0F, 0xB1, 0x08}
	if got := encAsm(t, "cmpxchg qword [rax], rcx"); !reflect.DeepEqual(got, want) {
		t.Errorf("cmpxchg qword [rax], rcx = % X, want % X", got, want)
	}
	// compare-and-swap: F0 0F B1 08
	want = []byte{0xF0, 0x0F, 0xB1, 0x08}
	if got := encAsm(t, "lock cmpxchg dword [rax], ecx"); !reflect.DeepEqual(got, want) {
		t.Errorf("lock cmpxchg dword [rax], ecx = % X, want % X", got, want)
	}
}
