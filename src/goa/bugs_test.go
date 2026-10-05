package goa

import (
	"reflect"
	"testing"
)

// TestMovMemImmKeepsIndexScale pins the encodeMovMemImm fix: the memory
// operand used to arrive as (base, disp) only, so the index register and
// scale silently vanished -- "mov [rax+rbx*4], 10" became "mov [rax], 10".
// The encoder now takes the full Operand and encodes base/index/scale/disp
// through planMem. The SIB byte below must carry scale=4, index=rax, base=rbx.
func TestMovMemImmKeepsIndexScale(t *testing.T) {
	// mov dword [rbx+rax*4+8], 5
	// ModRM: mod=01 (disp8), reg=/0, rm=100 (SIB) => 0x44
	// SIB:   scale=10 (x4), index=rax(000), base=rbx(011) => 0x83
	// disp8=8, then imm32 5
	want := []byte{0xC7, 0x44, 0x83, 0x08, 0x05, 0x00, 0x00, 0x00}
	if got := encAsm(t, "mov dword [rbx+rax*4+8], 5"); !reflect.DeepEqual(got, want) {
		t.Errorf("mov dword [rbx+rax*4+8], 5 = %v, want %v", got, want)
	}
	// High base with an index: REX.W(0x48) + REX.B(r12) => 0x49
	// ModRM: mod=01, rm=100 => 0x44; SIB: scale=01 (x2), index=rax(000),
	// base=r12&7(100) => 0x44; disp8=0x10
	want = []byte{0x49, 0xC7, 0x44, 0x44, 0x10, 0x05, 0x00, 0x00, 0x00}
	if got := encAsm(t, "mov qword [r12+rax*2+16], 5"); !reflect.DeepEqual(got, want) {
		t.Errorf("mov qword [r12+rax*2+16], 5 = %v, want %v", got, want)
	}
	// The plain base+disp forms must stay byte-identical to before.
	want = []byte{0xC7, 0x45, 0xF8, 0x05, 0x00, 0x00, 0x00}
	if got := encAsm(t, "mov dword [rbp-8], 5"); !reflect.DeepEqual(got, want) {
		t.Errorf("mov dword [rbp-8], 5 = %v, want %v", got, want)
	}
}

// TestTestImmUsesTestOpcode pins the encodeArith fix: "test r, imm" shares
// the 83/81 group with add/sub/cmp, where /0 decodes as ADD. The old code
// emitted the group opcode, silently turning "test rax, 1" into
// "add rax, 1" -- corrupting the register and the flags it was probing.
// test r/m, imm has its own opcodes: F6 /0 ib and F7 /0 id. goc's codegen
// emits these in the 64-bit form, so REX.W is set (0x48/0x49).
func TestTestImmUsesTestOpcode(t *testing.T) {
	// test rax, 1 -> REX.W F6 /0 (modrm mod=11 reg=000 rm=rax=000 => 0xC0), imm8
	want := []byte{0x48, 0xF6, 0xC0, 0x01}
	if got := encAsm(t, "test rax, 1"); !reflect.DeepEqual(got, want) {
		t.Errorf("test rax, 1 = %v, want %v", got, want)
	}
	// test rax, 300 -> REX.W F7 /0 id (imm32 branch)
	want = []byte{0x48, 0xF7, 0xC0, 0x2C, 0x01, 0x00, 0x00}
	if got := encAsm(t, "test rax, 300"); !reflect.DeepEqual(got, want) {
		t.Errorf("test rax, 300 = %v, want %v", got, want)
	}
	// test r10, 7 -> REX.W(0x48) + REX.B (r10 = reg 10, rm needs REX.B) => 0x49
	want = []byte{0x49, 0xF6, 0xC2, 0x07}
	if got := encAsm(t, "test r10, 7"); !reflect.DeepEqual(got, want) {
		t.Errorf("test r10, 7 = %v, want %v", got, want)
	}
}

// TestBTRipImmFixupHasRipAdj pins the emitOpRM RIP fixup: for a RIP-relative
// rm with an instruction trailer ("bt [rip+sym], imm8"), the trailer lands
// AFTER the disp32, so the fixup must record ripAdj=1 -- otherwise the rel32
// was one byte short and the CPU jumped into the immediate's own byte.
func TestBTRipImmFixupHasRipAdj(t *testing.T) {
	a := NewAssembler()
	if err := a.Assemble("section .text\nbt [rip+sym], 8\nsym:\nret\n"); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(a.fixups) != 1 {
		t.Fatalf("want exactly 1 fixup, got %+v", a.fixups)
	}
	f := a.fixups[0]
	if f.ripAdj != 1 {
		t.Errorf("bt [rip+sym], 8 fixup must carry ripAdj=1, got %+v", f)
	}
	if f.sym != "sym" {
		t.Errorf("fixup must reference sym, got %+v", f)
	}
	// Bytes: REX.W(0x48, memSrcWidth defaults to qword) 0F BA /4
	// (modrm mod=00 rm=101 => 0x25), disp32 placeholder, then the imm8
	// trailer, then the ret of the label that follows. The trailer must sit
	// immediately after the disp32 (the byte ripAdj accounts for).
	data := a.sections[a.cur].Data
	want := []byte{0x48, 0x0F, 0xBA, 0x25, 0x00, 0x00, 0x00, 0x00, 0x08, 0xC3}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("bt [rip+sym], 8 bytes = %v, want %v", data, want)
	}
}
