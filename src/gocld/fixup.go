package gocld

import (
	"encoding/binary"
	"fmt"
)

// Applying one relocation: the field's final value is written into the section
// bytes, and a mistake here is invisible until the program crashes in a way
// that points nowhere. So each shape is spelled out rather than derived from
// the field width, because the width is not enough to tell them apart -- a
// 4-byte displacement from the next instruction and a 4-byte address differ
// only in whether the image base is added.

// applyFixup patches one recorded displacement into s.Data. `target` is the
// resolved address of f.Sym and `base` the address of the section holding the
// displacement field; the CPU measures both rel32 and rel8 from the byte that
// follows the displacement (plus any instruction trailer in ripAdj).
// applyFixup patches one recorded relocation into place. target is the resolved
// address of f.Sym; sym2, when non-empty, is the address subtracted from it for
// the label-difference form (see Fixup.sym2); base is the address of the section
// holding the field.
func applyFixup(s *Section, f Fixup, target, sym2, base int) error {
	size := 4
	switch {
	case f.Short:
		size = 1
	case f.Wide:
		size = 8
	}
	if f.Off+size > len(s.Data) {
		return fmt.Errorf("fixup out of range for %s", f.Sym)
	}
	// An absolute fixup stores the address itself; a relative one stores the
	// distance from the byte after the field (plus any instruction trailer).
	if f.Absolute {
		if f.Off+size > len(s.Data) {
			return fmt.Errorf("absolute fixup out of range for %s", f.Sym)
		}
		addr := uint64(target + f.Addend)
		if f.Virtual {
			// The preferred load address is above 4GB, so a 32-bit field
			// would truncate it; only the 64-bit form can hold one.
			addr += uint64(ImageBase)
		}
		for i := 0; i < size; i++ {
			s.Data[f.Off+i] = byte(addr >> (8 * i))
		}
		return nil
	}
	if f.Sym2 != "" {
		// A jump-table entry: the field wants (sym - sym2), not
		// "sym relative to wherever the field sits". The two live in different
		// sections, so the usual displacement arithmetic is meaningless here --
		// the value is an offset into the table, which happens to be what
		// `base + i*4` lands on because the table's own base is sym2.
		diff := int32(target - sym2)
		for i := 0; i < size; i++ {
			s.Data[f.Off+i] = byte(diff >> (8 * i))
		}
		return nil
	}
	disp := int32(target + f.Addend - (base + f.Off + size + f.RipAdjust))
	if f.Short && (disp < -128 || disp > 127) {
		return fmt.Errorf("short jump to %s is %d bytes away (limit +/-127)", f.Sym, disp)
	}
	for i := 0; i < size; i++ {
		s.Data[f.Off+i] = byte(disp >> (8 * i))
	}
	return nil
}

// applyReloc patches one non-x86 relocation into place. symVA is the resolved
// address of r.Sym; base is the load address of the section holding the field,
// so the field's own address P is base + r.Off. The encodings follow the
// AArch64 ELF psABI and were verified against a real object run under Unicorn
// (tmp/archtest/run_a64.py is the reference this ports from).
func applyReloc(s *Section, r Reloc, symVA int, base int) error {
	if r.Machine == emARM {
		return applyRelocARM32(s, r, symVA, base)
	}
	if r.Off < 0 || r.Off+8 > len(s.Data) {
		return fmt.Errorf("reloc out of range for %s", r.Sym)
	}
	S := symVA
	A := int(r.Addend)
	P := base + r.Off
	get := func() uint32 { return binary.LittleEndian.Uint32(s.Data[r.Off:]) }
	put := func(v uint32) { binary.LittleEndian.PutUint32(s.Data[r.Off:], v) }
	switch r.Type {
	case rAARCH64_ABS64:
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(S+A))
		copy(s.Data[r.Off:], b[:])
	case rAARCH64_PREL32:
		binary.LittleEndian.PutUint32(s.Data[r.Off:], uint32(S+A-P))
	case rAARCH64_ADR_PREL_PG_HI21:
		page := ((S + A) &^ 0xfff) - (P &^ 0xfff)
		imm21 := int64(page) >> 12
		w := get()
		immlo := uint32(imm21 & 0x3)
		immhi := uint32((imm21 >> 2) & 0x1ffff)
		w = (w &^ 0x60ffffe0) | (immlo << 29) | (immhi << 5)
		put(w)
	case rAARCH64_ADD_ABS_LO12_NC:
		// add/sub (immediate): the field is the unscaled 12-bit page offset.
		imm12 := uint32((S + A) & 0xfff)
		w := get()
		w = (w &^ 0x3ffc00) | (imm12 << 10)
		put(w)
	case rAARCH64_LDST8_ABS_LO12_NC, rAARCH64_LDST16_ABS_LO12_NC,
		rAARCH64_LDST32_ABS_LO12_NC, rAARCH64_LDST64_ABS_LO12_NC:
		// ldr/str (immediate, unsigned offset): the 12-bit field holds the
		// offset DIVIDED BY THE ACCESS SIZE, so each width shifts differently.
		// Writing the raw offset here scales it by 8/4/2 -- the relocation
		// looks plausible and the instruction still assembles, so the program
		// loads from the wrong address and faults at run time rather than at
		// link time. (This is why a program whose only cross-section reference
		// is an ADRP+ADD pair -- a string literal, say -- works fine while one
		// that also loads a 64-bit global pointer does not.)
		shift := uint(0)
		switch r.Type {
		case rAARCH64_LDST16_ABS_LO12_NC:
			shift = 1
		case rAARCH64_LDST32_ABS_LO12_NC:
			shift = 2
		case rAARCH64_LDST64_ABS_LO12_NC:
			shift = 3
		}
		imm12 := uint32((S+A)&0xfff) >> shift
		w := get()
		w = (w &^ 0x3ffc00) | (imm12 << 10)
		put(w)
	case rAARCH64_CALL26, rAARCH64_JUMP26:
		offset := (S + A) - P
		imm26 := int64(offset) >> 2
		w := get()
		w = (w &^ 0x3ffffff) | uint32(imm26&0x3ffffff)
		put(w)
	default:
		return fmt.Errorf("applyReloc: unsupported AArch64 type %#x", r.Type)
	}
	return nil
}

// applyRelocARM32 patches one ARM32 (ARMv7 EABI) relocation. The object uses
// REL relocations, so the addend is not in the relocation entry: it is the
// value the assembler left in the instruction field, and each case reads it out.
// The encodings follow the ARM ELF ABI; the reference for the bit layouts is the
// same Unicorn-run object this port was validated against.
func applyRelocARM32(s *Section, r Reloc, symVA int, base int) error {
	if r.Off < 0 || r.Off+4 > len(s.Data) {
		return fmt.Errorf("reloc out of range for %s", r.Sym)
	}
	S := symVA
	P := base + r.Off
	w := binary.LittleEndian.Uint32(s.Data[r.Off:])
	put := func(v uint32) { binary.LittleEndian.PutUint32(s.Data[r.Off:], v) }
	switch r.Type {
	case rARMAbs32:
		// S + A (absolute 32-bit). A is the 32-bit value already in the word.
		a := int64(int32(w))
		put(uint32(S + int(a)))
	case rARMRel32:
		// S + A - P (32-bit pc-relative).
		a := int64(int32(w))
		put(uint32(S + int(a) - P))
	case rARMCall, rARMJump24:
		// BL / B. The existing 24-bit field holds the addend displacement
		// (sign-extended). The ARM ELF ABI defines the addend relative to
		// P + 8 -- the address the processor itself forms when it executes
		// the branch -- so the field must encode ((S + A) - P) >> 2, and the
		// processor's (P + 8) + (imm24 << 2) then lands exactly on S + A.
		// A formula that also subtracts the 8 here -- (S + A) - (P + 8) --
		// lands every branch 8 bytes short, which is how a recursive call
		// reaches 8 bytes before its own function and crashes on the ELF
		// header it decodes there.
		imm := w & 0x00ffffff
		if imm&0x00800000 != 0 {
			imm |= 0xff000000
		}
		a := int64(int32(imm)) * 4
		disp := (S + int(a) - P) >> 2
		put((w &^ 0x00ffffff) | uint32(disp)&0x00ffffff)
	case rARMMovwAbsNC:
		// movw: low 16 bits of (S + A). A is the immediate already in the
		// instruction's imm16 field.
		a := int64(w & 0x0000ffff)
		res := uint32((S + int(a)) & 0xffff)
		put((w &^ 0x0000ffff) | res)
	case rARMMovtAbs:
		// movt: high 16 bits of (S + A).
		a := int64(w&0x0000ffff) << 16
		res := uint32(((S + int(a)) >> 16) & 0xffff)
		put((w &^ 0x0000ffff) | res)
	case rARMLdrPCG0, rARMLdrPcG1, rARMLdrPcG2:
		// PC-relative LDR from a literal pool: (S + A - P) as a 12-bit unsigned
		// immediate (the common G0 form; G1/G2 are rotated variants whose
		// addend is the same 12-bit field for the loads goc emits).
		a := int64(int32(w & 0xfff))
		disp := uint32(S + int(a) - P)
		put((w &^ 0xfff) | (disp & 0xfff))
	case rARMALUPcRel7_0, rARMALUPcRel19_12, rARMALUPcRel11_8, rARMALUPcRel15_12:
		// add/sub ..., pc, #imm (rotated 8-bit immediate). The addend is the
		// value the instruction already encodes; rebuild it from the resolved
		// displacement.
		a := armRotImm(w)
		disp := uint32(S + a - P)
		put(w&^0xfff | armEncodeRotImm(disp))
	case rARMPrel31:
		// Exception-table entry: (S + A - P) as a 31-bit signed value, bit 31
		// (the '1' data marker) preserved. We drop the section this applies to,
		// so this case is reached only defensively.
		a := int64(int32(w & 0x7fffffff))
		v := uint32(S + int(a) - P)
		put((w & 0x80000000) | (v & 0x7fffffff))
	case rARMV4BX:
		// BX -> no-op on ARMv5+: the instruction is already legal, so leave it.
	default:
		return fmt.Errorf("applyRelocARM32: unsupported ARM32 type %#x", r.Type)
	}
	return nil
}

// armRotImm decodes an ARM "modified immediate" held in the low 12 bits of an
// ALU instruction (8-bit value rotated right by 2*rot). It returns the value the
// field currently stands for, which the REL addend convention treats as the
// addend for a pc-relative ALU relocation.
func armRotImm(w uint32) int {
	imm := w & 0xff
	rot := (w >> 8) & 0xf
	return int(imm>>(rot*2) | imm<<(32-rot*2))
}

// armEncodeRotImm encodes v as an ARM modified-immediate (a rotated 8-bit
// constant) in the low 12 bits of an ALU instruction, or 0 if v cannot be so
// expressed (v=0's canonical form is rot=0, imm=0).
func armEncodeRotImm(v uint32) uint32 {
	for rot := 0; rot < 16; rot++ {
		shift := rot * 2
		cand := (v << shift) | (v >> (32 - shift))
		if cand <= 0xff {
			return (uint32(rot) << 8) | cand
		}
	}
	return 0
}
