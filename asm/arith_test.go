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
