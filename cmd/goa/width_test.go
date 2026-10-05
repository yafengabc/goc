package goa

import (
	"reflect"
	"testing"
)

// Width-awareness of the arithmetic encoders: a 32-bit register operand must
// not get REX.W. Before the fix, encodeArith/encodeImul called rexW
// unconditionally, so `cmp r10d, eax` was emitted as `cmp r10, rax` (64-bit).
// Under the int carry model a materialized int -1 is 0x00000000FFFFFFFF, and a
// 64-bit signed compare of that against 1 sees 4294967295 >= 1 -- a loop that
// exits on a negative counter never terminates (this was the goclib log(1.5)
// infinite loop). The same bug silently promoted every 32-bit add/sub/imul to
// 64-bit, polluting the high bits the zero-extension invariant relies on.
func TestArith32BitEncodings(t *testing.T) {
	cases := []struct {
		line string
		want []byte
	}{
		// cmp r10d, eax: reg field = src (eax, 0), rm field = dst (r10d, 2+R B).
		// REX 0x41 = REX.B only (no W) => 32-bit cmp, the log-loop killer fixed.
		{"cmp r10d, eax", []byte{0x41, 0x39, 0xC2}},
		// cmp eax, r10d: src r10d in the reg field needs REX.R (0x44), no W.
		{"cmp eax, r10d", []byte{0x44, 0x39, 0xD0}},
		// sub r10d, eax: REX.B only, opcode 0x29 = sub r/m32, r32.
		{"sub r10d, eax", []byte{0x41, 0x29, 0xC2}},
		// add eax, r10d: src r10d in reg field (0x44), opcode 0x01.
		{"add eax, r10d", []byte{0x44, 0x01, 0xD0}},
		// 32-bit forms with an immediate: add eax, 1 needs no REX at all.
		{"add eax, 1", []byte{0x83, 0xC0, 0x01}},
		// sub r10d, 2: imm8 form, REX.B only (0x41), /5 digit.
		{"sub r10d, 2", []byte{0x41, 0x83, 0xEA, 0x02}},
		// test eax, 1: imm8 fits, so the F6 /0 ib form (not F7 imm32).
		{"test eax, 1", []byte{0xF6, 0xC0, 0x01}},
		// imul eax, r10d: 0F AF /r, REX.B only.
		{"imul eax, r10d", []byte{0x41, 0x0F, 0xAF, 0xC2}},
		// imul r11d, 4: 6B /r ib, dst in both reg+rm fields => REX.R+REX.B (0x45).
		{"imul r11d, 4", []byte{0x45, 0x6B, 0xDB, 0x04}},
		// imul r11d, 300: 69 /r id (imm32 branch stays width-aware too).
		{"imul r11d, 300", []byte{0x45, 0x69, 0xDB, 0x2C, 0x01, 0x00, 0x00}},
		// sar eax, cl: shift with a 32-bit destination must not get REX.W
		// (the goa encodeShift fix: without it, sar eax,cl operated on rax).
		{"sar eax, cl", []byte{0xD3, 0xF8}},
	}
	for _, c := range cases {
		if got := encAsm(t, c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %v, want %v", c.line, got, c.want)
		}
	}
}

// The 64-bit forms must keep REX.W -- width-awareness must not break them.
func TestArith64BitEncodingsStillREXW(t *testing.T) {
	cases := []struct {
		line string
		want []byte
	}{
		{"cmp r10, rax", []byte{0x49, 0x39, 0xC2}},
		{"add rax, rbx", []byte{0x48, 0x01, 0xD8}},
		{"imul rax, r10", []byte{0x49, 0x0F, 0xAF, 0xC2}},
		{"imul r11, 8", []byte{0x4D, 0x6B, 0xDB, 0x08}},
	}
	for _, c := range cases {
		if got := encAsm(t, c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %v, want %v", c.line, got, c.want)
		}
	}
}

// mov reg, imm must respect the destination register width. Before the fix,
// encodeMov's reg<-imm path always emitted REX.W + imm64, so `mov eax,
// -2147483648` produced mov rax, 0xFFFFFFFF80000000 -- corrupting the
// materialized-int invariant (high 32 must be 0) that the int carry model
// relies on. Constant-folded shifts (1u<<31 -> 0x80000000) exposed this.
func TestMovImmWidthEncodings(t *testing.T) {
	cases := []struct {
		line string
		want []byte
	}{
		// mov eax, imm32: no REX, B8 opcode, imm32 little-endian.
		{"mov eax, -2147483648", []byte{0xB8, 0x00, 0x00, 0x00, 0x80}},
		{"mov eax, 0", []byte{0xB8, 0x00, 0x00, 0x00, 0x00}},
		// 32-bit destination with a high register: REX.B only (no W). r10d's
		// low 3 register bits are 2, so the opcode is B8|2 = BA.
		{"mov r10d, 7", []byte{0x41, 0xBA, 0x07, 0x00, 0x00, 0x00}},
		// 16-bit destination: 0x66 prefix + imm16.
		{"mov ax, -2", []byte{0x66, 0xB8, 0xFE, 0xFF}},
		// Full-width destination keeps REX.W + imm64 (sign-extended pattern).
		{"mov rax, -2147483648", []byte{0x48, 0xB8, 0x00, 0x00, 0x00, 0x80, 0xFF, 0xFF, 0xFF, 0xFF}},
		{"mov r11, 5", []byte{0x49, 0xBB, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
	}
	for _, c := range cases {
		if got := encAsm(t, c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %v, want %v", c.line, got, c.want)
		}
	}
}
