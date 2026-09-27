package main

import (
	"reflect"
	"testing"
)

// encAsm assembles a single instruction line and returns the emitted bytes
// of the current (.text) section.
func encAsm(t *testing.T, line string) []byte {
	t.Helper()
	a := NewAssembler()
	if err := a.processLine(line); err != nil {
		t.Fatalf("processLine(%q) failed: %v", line, err)
	}
	return append([]byte(nil), a.sections[a.cur].Data...)
}

func TestImulImm(t *testing.T) {
	// imul r11, 8 -> REX.W(REX.R+REX.B, r11>=8)=0x4D 6B /r ib (modrm reg=rm=r11 => 0xDB)
	want := []byte{0x4D, 0x6B, 0xDB, 0x08}
	if got := encAsm(t, "imul r11, 8"); !reflect.DeepEqual(got, want) {
		t.Errorf("imul r11, 8 = %v, want %v", got, want)
	}
	// imul r11, 300 -> REX.W 69 /r id (imm32 branch)
	want = []byte{0x4D, 0x69, 0xDB, 0x2C, 0x01, 0x00, 0x00}
	if got := encAsm(t, "imul r11, 300"); !reflect.DeepEqual(got, want) {
		t.Errorf("imul r11, 300 = %v, want %v", got, want)
	}
}

func TestImulRegRegStillWorks(t *testing.T) {
	// imul rax, r10 -> REX.W(r10 in rm field => REX.B)=0x49 0F AF /r (reg=rax,rm=r10 => 0xC2)
	want := []byte{0x49, 0x0F, 0xAF, 0xC2}
	if got := encAsm(t, "imul rax, r10"); !reflect.DeepEqual(got, want) {
		t.Errorf("imul rax, r10 = %v, want %v", got, want)
	}
}

func TestShiftImm(t *testing.T) {
	cases := []struct {
		line string
		want []byte
	}{
		// shl r11,3 -> REX.W(REX.B)=0x49 C1 /4 ib (modrm reg=4,rm=r11 => 0xE3)
		{"shl r11, 3", []byte{0x49, 0xC1, 0xE3, 0x03}},
		// shl r11,1 -> REX.W D1 /4
		{"shl r11, 1", []byte{0x49, 0xD1, 0xE3}},
		// shr r11,3 -> REX.W C1 /5 ib (modrm reg=5 => 0xEB)
		{"shr r11, 3", []byte{0x49, 0xC1, 0xEB, 0x03}},
		// sar r11,3 -> REX.W C1 /7 ib (modrm reg=7 => 0xFB)
		{"sar r11, 3", []byte{0x49, 0xC1, 0xFB, 0x03}},
	}
	for _, c := range cases {
		if got := encAsm(t, c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestShiftByCL(t *testing.T) {
	// shl r11, cl -> REX.W D3 /4 (modrm reg=4,rm=r11 => 0xE3)
	want := []byte{0x49, 0xD3, 0xE3}
	if got := encAsm(t, "shl r11, cl"); !reflect.DeepEqual(got, want) {
		t.Errorf("shl r11, cl = %v, want %v", got, want)
	}
}

func TestMovDwordMem(t *testing.T) {
	cases := []struct {
		line string
		want []byte
	}{
		// mov rax, dword [rbp-8]: 8B /r, mod=01(disp8), rm=rbp => 0x45, disp -8.
		{"mov rax, dword [rbp-8]", []byte{0x8B, 0x45, 0xF8}},
		// mov dword [rbp-8], rax: 89 /r, same ModRM.
		{"mov dword [rbp-8], rax", []byte{0x89, 0x45, 0xF8}},
		// High reg in the R slot forces a REX prefix (0x44 = REX.R), no W bit.
		{"mov r10, dword [rbx+4]", []byte{0x44, 0x8B, 0x53, 0x04}},
		{"mov dword [rbx+4], r10", []byte{0x44, 0x89, 0x53, 0x04}},
		// Base r12 (12) needs REX.B + a SIB byte (base=100 in rm => 0x04).
		{"mov dword [r12], rax", []byte{0x41, 0x89, 0x04, 0x24}},
		// Base r10 (10) needs REX.B, no SIB: mod=00, rm=r10&7=2.
		{"mov rax, dword [r10]", []byte{0x41, 0x8B, 0x02}},
		// dword imm32 store: C7 /0, no REX.W (vs qword which is 48 C7).
		{"mov dword [rbp-8], 5", []byte{0xC7, 0x45, 0xF8, 0x05, 0x00, 0x00, 0x00}},
		{"mov qword [rbp-8], 5", []byte{0x48, 0xC7, 0x45, 0xF8, 0x05, 0x00, 0x00, 0x00}},
		// High base register with imm32: REX.B (0x41), no W.
		{"mov dword [r12+8], 7", []byte{0x41, 0xC7, 0x44, 0x24, 0x08, 0x07, 0x00, 0x00, 0x00}},
	}
	for _, c := range cases {
		if got := encAsm(t, c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestMovDwordRip(t *testing.T) {
	// RIP-relative dword load/store: no REX.W, disp32 fixup placeholder.
	want := []byte{0x8B, 0x05, 0x00, 0x00, 0x00, 0x00}
	if got := encAsm(t, "mov rax, dword [rip+g]"); !reflect.DeepEqual(got, want) {
		t.Errorf("mov rax, dword [rip+g] = %v, want %v", got, want)
	}
	want = []byte{0x89, 0x05, 0x00, 0x00, 0x00, 0x00}
	if got := encAsm(t, "mov dword [rip+g], rax"); !reflect.DeepEqual(got, want) {
		t.Errorf("mov dword [rip+g], rax = %v, want %v", got, want)
	}
}

func TestMovWordMem(t *testing.T) {
	cases := []struct {
		line string
		want []byte
	}{
		// 16-bit access uses the 0x66 operand-size prefix; a 64-bit GPR is
		// still the destination (low 16 bits written/read).
		{"mov rax, word [rbp-8]", []byte{0x66, 0x8B, 0x45, 0xF8}},
		{"mov word [rbp-8], rax", []byte{0x66, 0x89, 0x45, 0xF8}},
		{"mov word [rbp-8], 5", []byte{0x66, 0xC7, 0x45, 0xF8, 0x05, 0x00}},
		// High base register with a 16-bit store.
		{"mov word [r12], rax", []byte{0x66, 0x41, 0x89, 0x04, 0x24}},
	}
	for _, c := range cases {
		if got := encAsm(t, c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %v, want %v", c.line, got, c.want)
		}
	}
}
