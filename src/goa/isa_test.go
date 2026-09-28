package main

import (
	"reflect"
	"strings"
	"testing"
)

// textOf returns the bytes of the .text section of an assembled program.
func textOf(a *Assembler) []byte {
	for _, s := range a.sections {
		if s.Name == ".text" {
			return s.Data
		}
	}
	return nil
}

// asmSeq assembles a multi-line source and returns the Assembler, so a test
// can inspect labels and fixups and not just the emitted bytes.
func asmSeq(t *testing.T, lines ...string) *Assembler {
	t.Helper()
	src := "section .text\n"
	for _, l := range lines {
		src += l + "\n"
	}
	a := NewAssembler()
	if err := a.Assemble(src); err != nil {
		t.Fatalf("assemble %q failed: %v", src, err)
	}
	return a
}

// checkBytes compares machine code byte by byte. The "got/want" dump is whole
// sequences rather than single bytes because a one-byte disagreement in a
// prefix silently shifts everything after it.
func checkBytes(t *testing.T, line string, got, want []byte) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s\n got % x\nwant % x", line, got, want)
	}
}

func checkCases(t *testing.T, cases []struct {
	line string
	want []byte
}) {
	t.Helper()
	for _, c := range cases {
		checkBytes(t, c.line, encAsm(t, c.line), c.want)
	}
}

// ---- movzx / movsx / movsxd ------------------------------------------------

func TestMovExtend(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		// movzx eax, al -> 0F B6 C0 (32-bit dst: no REX at all)
		{"movzx eax, al", []byte{0x0F, 0xB6, 0xC0}},
		// movsx rax, al -> 0F BE with REX.W (64-bit dst)
		{"movsx rax, al", []byte{0x48, 0x0F, 0xBE, 0xC0}},
		// word source switches to the B7/BF opcode
		{"movzx eax, bx", []byte{0x0F, 0xB7, 0xC3}},
		{"movsx rax, bx", []byte{0x48, 0x0F, 0xBF, 0xC3}},
		// 16-bit destination takes a 0x66 operand-size prefix
		{"movzx ax, bl", []byte{0x66, 0x0F, 0xB6, 0xC3}},
		// r8d carries REX.R (reg field >= 8) even in 32-bit operand size
		{"movzx r8d, bl", []byte{0x44, 0x0F, 0xB6, 0xC3}},
		// memory sources: the byte/word prefix supplies the source width
		{"movzx rax, byte [rbp-8]", []byte{0x48, 0x0F, 0xB6, 0x45, 0xF8}},
		{"movzx eax, word [rbp-8]", []byte{0x0F, 0xB7, 0x45, 0xF8}},
		{"movsx rax, byte [rbp-8]", []byte{0x48, 0x0F, 0xBE, 0x45, 0xF8}},
		// RIP-relative memory source, with the disp32 left for the linker
		{"movzx r8d, byte [rip+G_g]", []byte{0x44, 0x0F, 0xB6, 0x05, 0, 0, 0, 0}},
		// movsxd / movslq is the odd one: its own opcode, always into a 64-bit register
		{"movsxd rax, eax", []byte{0x48, 0x63, 0xC0}},
		{"movsxd rax, ecx", []byte{0x48, 0x63, 0xC1}},
		{"movsxd rax, dword [rbp-8]", []byte{0x48, 0x63, 0x45, 0xF8}},
		// GAS spellings, mostly seen in hand-written Linux asm
		{"movslq rax, ecx", []byte{0x48, 0x63, 0xC1}},
		{"movzbl eax, al", []byte{0x0F, 0xB6, 0xC0}},
		{"movsbq rax, al", []byte{0x48, 0x0F, 0xBE, 0xC0}},
		{"movzwl eax, bx", []byte{0x0F, 0xB7, 0xC3}},
		{"movswq rax, bx", []byte{0x48, 0x0F, 0xBF, 0xC3}},
	})
}

func TestMovExtendBad(t *testing.T) {
	bad := []string{
		"movzx al, bl",       // 8-bit destination cannot be widened into
		"movzx rax, [rbp-8]", // memory source without a width prefix
		"movzx rax, ecx",     // dword source is movsxd's job, not movzx's
		"movsx eax, ecx",     // ditto: sign-extending dword needs a 64-bit dst
		"movzx ax, eax",      // destination narrower than the source
		"movzx rax, 5",       // immediate source
		"movzx rax",          // missing operand
		"movzx [rbp-8], al",  // memory destination
	}
	for _, l := range bad {
		a := NewAssembler()
		if err := a.processLine(l); err == nil {
			t.Errorf("expected error for %q", l)
		}
	}
}

// ---- setcc -----------------------------------------------------------------

func TestSetCC(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"sete al", []byte{0x0F, 0x94, 0xC0}},
		{"setne al", []byte{0x0F, 0x95, 0xC0}},
		{"setl al", []byte{0x0F, 0x9C, 0xC0}},
		{"setge al", []byte{0x0F, 0x9D, 0xC0}},
		{"setle al", []byte{0x0F, 0x9E, 0xC0}},
		{"setg al", []byte{0x0F, 0x9F, 0xC0}},
		{"setb al", []byte{0x0F, 0x92, 0xC0}},
		{"seta al", []byte{0x0F, 0x97, 0xC0}},
		{"sets al", []byte{0x0F, 0x98, 0xC0}},
		{"seto al", []byte{0x0F, 0x90, 0xC0}},
		{"setp al", []byte{0x0F, 0x9A, 0xC0}},
		// aliases land on the same opcode as their canonical form
		{"setz al", []byte{0x0F, 0x94, 0xC0}},
		{"setnz al", []byte{0x0F, 0x95, 0xC0}},
		{"setc al", []byte{0x0F, 0x92, 0xC0}},
		{"setnl al", []byte{0x0F, 0x9D, 0xC0}},
		{"setnle al", []byte{0x0F, 0x9F, 0xC0}},
		// sil/bpl/spl/dil need a bare REX or they decode as dh/ch/bh
		{"setne dil", []byte{0x40, 0x0F, 0x95, 0xC7}},
		{"setl sil", []byte{0x40, 0x0F, 0x9C, 0xC6}},
		// r8b lands in ModRM.rm, so it is REX.B (0x41) extending it
		{"sete r8b", []byte{0x41, 0x0F, 0x94, 0xC0}},
		// byte memory target
		{"setl byte [rbp-4]", []byte{0x0F, 0x9C, 0x45, 0xFC}},
	})
}

func TestSetCCBad(t *testing.T) {
	// A bare memory target is legal here: SETcc is inherently byte-sized, so
	// no explicit `byte` prefix is needed to name one.
	for _, l := range []string{"sete eax", "sete rax", "sete 5", "sete", "setq al"} {
		a := NewAssembler()
		if err := a.processLine(l); err == nil {
			t.Errorf("expected error for %q", l)
		}
	}
}

// ---- cmovcc ----------------------------------------------------------------

func TestCmovCC(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"cmove eax, ebx", []byte{0x0F, 0x44, 0xC3}},
		{"cmovne eax, ebx", []byte{0x0F, 0x45, 0xC3}},
		{"cmovl eax, ebx", []byte{0x0F, 0x4C, 0xC3}},
		{"cmovg eax, ebx", []byte{0x0F, 0x4F, 0xC3}},
		// 64-bit destination takes REX.W
		{"cmovnz rax, rbx", []byte{0x48, 0x0F, 0x45, 0xC3}},
		// 16-bit destination takes 0x66
		{"cmove ax, bx", []byte{0x66, 0x0F, 0x44, 0xC3}},
		// memory source follows the destination width
		{"cmovl eax, dword [rbp-8]", []byte{0x0F, 0x4C, 0x45, 0xF8}},
		{"cmovg rax, [rbp-8]", []byte{0x48, 0x0F, 0x4F, 0x45, 0xF8}},
		// aliases
		{"cmovz eax, ebx", []byte{0x0F, 0x44, 0xC3}},
		{"cmovnge eax, ebx", []byte{0x0F, 0x4C, 0xC3}},
		{"cmovnle eax, ebx", []byte{0x0F, 0x4F, 0xC3}},
	})
}

func TestCmovCCBad(t *testing.T) {
	for _, l := range []string{"cmove [rbp-8], eax", "cmove eax", "cmove eax, 5", "cmovxyz eax, ebx"} {
		a := NewAssembler()
		if err := a.processLine(l); err == nil {
			t.Errorf("expected error for %q", l)
		}
	}
}

// ---- group-encoded unary (not / mul / inc / dec / neg / div / idiv) --------

func TestGroupUnary(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"not rax", []byte{0x48, 0xF7, 0xD0}},
		{"mul rbx", []byte{0x48, 0xF7, 0xE3}},
		{"div rbx", []byte{0x48, 0xF7, 0xF3}},
		{"idiv rbx", []byte{0x48, 0xF7, 0xFB}},
		{"neg rax", []byte{0x48, 0xF7, 0xD8}},
		{"inc rax", []byte{0x48, 0xFF, 0xC0}},
		{"dec rax", []byte{0x48, 0xFF, 0xC8}},
		// a 32-bit register destination now yields a 32-bit op (no REX.W):
		// this used to be silently encoded as its 64-bit counterpart.
		{"inc eax", []byte{0xFF, 0xC0}},
		{"dec ebx", []byte{0xFF, 0xCB}},
		{"neg ecx", []byte{0xF7, 0xD9}},
		// 16-bit takes 0x66
		{"inc ax", []byte{0x66, 0xFF, 0xC0}},
		// memory operands work in place now
		{"inc qword [rbp-8]", []byte{0x48, 0xFF, 0x45, 0xF8}},
		{"dec dword [rbp-8]", []byte{0xFF, 0x4D, 0xF8}},
		{"not byte [rbp-1]", []byte{0xF7, 0x55, 0xFF}},
		{"inc qword [rip+G_n]", []byte{0x48, 0xFF, 0x05, 0, 0, 0, 0}},
	})
}

// ---- xchg ------------------------------------------------------------------

func TestXchg(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"xchg rax, rbx", []byte{0x48, 0x87, 0xC3}},
		{"xchg eax, ebx", []byte{0x87, 0xC3}},
		{"xchg al, bl", []byte{0x86, 0xC3}},
		{"xchg ax, bx", []byte{0x66, 0x87, 0xC3}},
		{"xchg rax, qword [rbp-8]", []byte{0x48, 0x87, 0x45, 0xF8}},
		// spl-family needs the bare REX in 8-bit mode
		{"xchg al, dil", []byte{0x40, 0x86, 0xC7}},
	})
}

// ---- bit test family -------------------------------------------------------

func TestBitOps(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		// register index form: ModRM.reg carries the bit number
		{"bt rax, rbx", []byte{0x48, 0x0F, 0xA3, 0xD8}},
		{"bts rax, rbx", []byte{0x48, 0x0F, 0xAB, 0xD8}},
		{"btr rax, rbx", []byte{0x48, 0x0F, 0xB3, 0xD8}},
		{"btc rax, rbx", []byte{0x48, 0x0F, 0xBB, 0xD8}},
		// immediate index form: group 0F BA with the operation in ModRM.reg
		{"bt rax, 3", []byte{0x48, 0x0F, 0xBA, 0xE0, 0x03}},
		{"bts eax, 5", []byte{0x0F, 0xBA, 0xE8, 0x05}},
		{"btr eax, 5", []byte{0x0F, 0xBA, 0xF0, 0x05}},
		{"btc eax, 5", []byte{0x0F, 0xBA, 0xF8, 0x05}},
		{"btc qword [rbp-8], 3", []byte{0x48, 0x0F, 0xBA, 0x7D, 0xF8, 0x03}},
	})
}

func TestBitOpsBad(t *testing.T) {
	for _, l := range []string{"bt rax", "bt rax, 300", "bt 5, rax", "bt rax, [rbp-8]"} {
		a := NewAssembler()
		if err := a.processLine(l); err == nil {
			t.Errorf("expected error for %q", l)
		}
	}
}

// ---- bswap -----------------------------------------------------------------

func TestBswap(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"bswap eax", []byte{0x0F, 0xC8}},       // 32-bit default: no REX
		{"bswap rax", []byte{0x48, 0x0F, 0xC8}}, // REX.W for the full 64-bit swap
		{"bswap ebx", []byte{0x0F, 0xCB}},
		{"bswap rbx", []byte{0x48, 0x0F, 0xCB}},
		{"bswap r8", []byte{0x49, 0x0F, 0xC8}}, // REX.W | REX.B
		{"bswap r8d", []byte{0x41, 0x0F, 0xC8}},
	})
}

func TestBswapBad(t *testing.T) {
	for _, l := range []string{"bswap ax", "bswap al", "bswap [rbp-8]", "bswap"} {
		a := NewAssembler()
		if err := a.processLine(l); err == nil {
			t.Errorf("expected error for %q", l)
		}
	}
}

// ---- push / pop extras -----------------------------------------------------

func TestPushPopExtras(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"push rbx", []byte{0x53}},
		{"pop rax", []byte{0x58}},
		{"push r12", []byte{0x41, 0x54}}, // REX.B extends opcode+rd
		{"pop r12", []byte{0x41, 0x5C}},
		{"push ax", []byte{0x66, 0x50}}, // 0x66 for the 16-bit form
		{"push 0", []byte{0x6A, 0x00}},  // imm8
		{"push 5", []byte{0x6A, 0x05}},
		{"push -1", []byte{0x6A, 0xFF}},
		{"push 1000", []byte{0x68, 0xE8, 0x03, 0x00, 0x00}},    // imm32
		{"push qword [rbp-8]", []byte{0x48, 0xFF, 0x75, 0xF8}}, // FF /6
		{"pop qword [rbp-8]", []byte{0x48, 0x8F, 0x45, 0xF8}},  // 8F /0
		{"push qword [rip+G_g]", []byte{0x48, 0xFF, 0x35, 0, 0, 0, 0}},
		// a bare memory operand defaults to the 64-bit stack width
		{"pop [rbp-8]", []byte{0x48, 0x8F, 0x45, 0xF8}},
	})
}

func TestPushPopBad(t *testing.T) {
	for _, l := range []string{"push", "pop 5", "push 5000000000", "pop 5", "push rax, rbx"} {
		a := NewAssembler()
		if err := a.processLine(l); err == nil {
			t.Errorf("expected error for %q", l)
		}
	}
}

// ---- indirect call / jmp ---------------------------------------------------

func TestIndirectCallJmp(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"jmp rax", []byte{0x48, 0xFF, 0xE0}},  // FF /4
		{"call rax", []byte{0x48, 0xFF, 0xD0}}, // FF /2
		{"jmp qword [rax]", []byte{0x48, 0xFF, 0x20}},
		{"call qword [rbp-8]", []byte{0x48, 0xFF, 0x55, 0xF8}},
		{"jmp qword [rbp-8]", []byte{0x48, 0xFF, 0x65, 0xF8}},
		{"call qword [rip+G_fp]", []byte{0x48, 0xFF, 0x15, 0, 0, 0, 0}},
	})
}

// ---- rotations -------------------------------------------------------------

func TestRotations(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"rol rax, 1", []byte{0x48, 0xD1, 0xC0}},
		{"rol rax, 4", []byte{0x48, 0xC1, 0xC0, 0x04}},
		{"ror rax, 1", []byte{0x48, 0xD1, 0xC8}},
		{"ror rax, cl", []byte{0x48, 0xD3, 0xC8}},
		{"rcl rax, 1", []byte{0x48, 0xD1, 0xD0}},
		{"rcr rax, 2", []byte{0x48, 0xC1, 0xD8, 0x02}},
		{"rcl rax, cl", []byte{0x48, 0xD3, 0xD0}},
		// the classic shifts are unchanged
		{"shl rax, 1", []byte{0x48, 0xD1, 0xE0}},
		{"shr rax, 1", []byte{0x48, 0xD1, 0xE8}},
		{"sar rax, 1", []byte{0x48, 0xD1, 0xF8}},
	})
}

// ---- adc / sbb -------------------------------------------------------------

func TestAdcSbb(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		// The rm-form of these opcodes puts the SOURCE in ModRM.reg, so the
		// register operand order reads reversed in the ModRM byte.
		{"adc rax, rbx", []byte{0x48, 0x11, 0xD8}},
		{"sbb rax, rbx", []byte{0x48, 0x19, 0xD8}},

		{"adc rax, 5", []byte{0x48, 0x83, 0xD0, 0x05}}, // 0x83 /2 ib
		{"sbb rax, 5", []byte{0x48, 0x83, 0xD8, 0x05}}, // 0x83 /3 ib
		{"adc rax, 300", []byte{0x48, 0x81, 0xD0, 0x2C, 0x01, 0x00, 0x00}},
		{"adc eax, [rbp-16]", []byte{0x13, 0x45, 0xF0}}, // 0x13 load form
		{"sbb eax, [rbp-16]", []byte{0x1B, 0x45, 0xF0}},
		{"adc rax, [rbp-16]", []byte{0x48, 0x13, 0x45, 0xF0}},
	})
}

// ---- condition-code aliases for jumps --------------------------------------

// TestJumpAliases pins the whole 0F 8x family, including every synonym we
// accept. Getting one of these wrong silently inverts a comparison at runtime,
// so the table is spelled out rather than derived.
func TestJumpAliases(t *testing.T) {
	cases := []struct {
		line string
		op   byte
	}{
		{"jo", 0x80}, {"jno", 0x81},
		{"jb", 0x82}, {"jc", 0x82}, {"jnae", 0x82},
		{"jae", 0x83}, {"jnb", 0x83}, {"jnc", 0x83},
		{"je", 0x84}, {"jz", 0x84},
		{"jne", 0x85}, {"jnz", 0x85},
		{"jbe", 0x86}, {"jna", 0x86},
		{"ja", 0x87}, {"jnbe", 0x87},
		{"js", 0x88}, {"jns", 0x89},
		{"jp", 0x8A}, {"jpe", 0x8A},
		{"jnp", 0x8B}, {"jpo", 0x8B},
		{"jl", 0x8C}, {"jnge", 0x8C},
		{"jge", 0x8D}, {"jnl", 0x8D},
		{"jle", 0x8E}, {"jng", 0x8E},
		{"jg", 0x8F}, {"jnle", 0x8F},
	}
	for _, c := range cases {
		line := c.line + " Label"
		want := []byte{0x0F, c.op, 0, 0, 0, 0}
		checkBytes(t, line, encAsm(t, line), want)
	}
}

// ---- misc zero-operand instructions ----------------------------------------

func TestMiscScalar(t *testing.T) {
	checkCases(t, []struct {
		line string
		want []byte
	}{
		{"ret", []byte{0xC3}},
		{"retq", []byte{0xC3}},
		{"retn", []byte{0xC3}},
		{"ret 8", []byte{0xC2, 0x08, 0x00}}, // pop 8 further bytes (stdcall cleanup)
		{"leave", []byte{0xC9}},
		{"nop", []byte{0x90}},
		{"pause", []byte{0xF3, 0x90}},
		{"hlt", []byte{0xF4}},
		{"ud2", []byte{0x0F, 0x0B}},
		{"int3", []byte{0xCC}},
		{"cpuid", []byte{0x0F, 0xA2}},
		{"rdtsc", []byte{0x0F, 0x31}},
		{"mfence", []byte{0x0F, 0xAE, 0xF0}},
		{"lfence", []byte{0x0F, 0xAE, 0xE8}},
		{"sfence", []byte{0x0F, 0xAE, 0xF8}},
		{"cqo", []byte{0x48, 0x99}},
		{"cqto", []byte{0x48, 0x99}},
		{"cdq", []byte{0x99}},
		{"cltd", []byte{0x99}},
		{"cdqe", []byte{0x48, 0x98}},
		{"cltq", []byte{0x48, 0x98}},
		{"cwde", []byte{0x98}},
		{"cwtl", []byte{0x98}},
		{"syscall", []byte{0x0F, 0x05}},
	})
}

// ---- short (rel8) jumps ----------------------------------------------------

// TestShortJumps pins the rel8 forms end to end: `jmp short`, `jcc short` and
// the always-short jrcxz/jecxz. The point is that a rel8 displacement is
// measured over ONE byte where a rel32 is measured over four, so these cases
// would all jump somewhere else entirely if the linker forgot the distinction.
func TestShortJumps(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []byte // .text once the fixup has been resolved
	}{
		{
			"jmp short forward over three nops",
			"jmp short .L1\nnop\nnop\nnop\n.L1:\nret\n",
			[]byte{0xEB, 0x03, 0x90, 0x90, 0x90, 0xC3},
		},
		{
			"jmp short to the very next instruction",
			"jmp short .L2\n.L2:\nret\n",
			[]byte{0xEB, 0x00, 0xC3},
		},
		{
			"jmp short backward takes a negative displacement",
			".L3:\nnop\njmp short .L3\nret\n",
			[]byte{0x90, 0xEB, 0xFD, 0xC3},
		},
		{
			"jz is je, and short makes it 74 rel8",
			"jz short .L4\nnop\n.L4:\nret\n",
			[]byte{0x74, 0x01, 0x90, 0xC3},
		},
		{
			"jrcxz has only a short form",
			"jrcxz .L5\nnop\n.L5:\nret\n",
			[]byte{0xE3, 0x01, 0x90, 0xC3},
		},
		{
			"jecxz adds 0x67, and the disp still follows the prefix",
			"jecxz .L6\nnop\n.L6:\nret\n",
			[]byte{0x67, 0xE3, 0x01, 0x90, 0xC3},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := NewAssembler()
			if err := a.Assemble("section .text\n" + c.src); err != nil {
				t.Fatalf("assemble: %v", err)
			}
			if len(a.fixups) != 1 {
				t.Fatalf("want exactly 1 fixup, got %+v", a.fixups)
			}
			f := a.fixups[0]
			if !f.short {
				t.Errorf("fixup %+v must be recorded as short (rel8)", f)
			}
			loc, ok := a.syms[f.sym]
			if !ok {
				t.Fatalf("symbol %q not defined", f.sym)
			}
			// Resolve the way both backends do, then compare the final bytes.
			if err := applyFixup(a.sections[f.sect], f, loc.off, 0); err != nil {
				t.Fatalf("applyFixup: %v", err)
			}
			checkBytes(t, c.name, textOf(a), c.want)
		})
	}
}

// TestShortJumpRange guards the +/-127 byte reach. Without this check a too-
// distant target would silently wrap into a wild jump in the other direction.
func TestShortJumpRange(t *testing.T) {
	src := "section .text\njmp short .Lfar\n" + strings.Repeat("nop\n", 200) + ".Lfar:\nret\n"
	a := NewAssembler()
	if err := a.Assemble(src); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	f := a.fixups[0]
	loc := a.syms[f.sym]
	s := a.sections[f.sect]
	if err := applyFixup(s, f, loc.off, 0); err == nil {
		t.Fatalf("expected out-of-range error, got disp byte 0x%02x", s.Data[f.off])
	}
}

// TestRel32Unaffected makes sure adding the short form did not perturb the
// ordinary rel32 jumps every compiled program actually uses.
func TestRel32Unaffected(t *testing.T) {
	a := NewAssembler()
	if err := a.Assemble("section .text\njmp .L1\n.L1:\nret\n"); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	f := a.fixups[0]
	if f.short {
		t.Errorf("plain `jmp label` must stay rel32, got short")
	}
	loc := a.syms[f.sym]
	if err := applyFixup(a.sections[f.sect], f, loc.off, 0); err != nil {
		t.Fatalf("applyFixup: %v", err)
	}
	// E9 rel32 landing on the ret two bytes later
	checkBytes(t, "jmp .L1", textOf(a), []byte{0xE9, 0x00, 0x00, 0x00, 0x00, 0xC3})
}
