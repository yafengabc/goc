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
	switch r.Machine {
	case emARM:
		return applyRelocARM32(s, r, symVA, base)
	case emRISCV:
		return applyRelocRISCV(s, r, symVA, base)
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

// applyRelocRISCV patches one RISC-V relocation. The object uses RELA, so the
// addend comes from the relocation entry -- it is not read out of the
// instruction the way the ARM32 REL cases have to do it.
//
// The one thing worth stating plainly is the +0x800 in HI20. A RISC-V address
// is `lui rd, %hi(sym)` followed by `addi rd, rd, %lo(sym)` (or an `ld` with
// the same immediate), and %lo is a SIGN-extended 12-bit field, so it can be
// negative down to -2048. %hi therefore has to be computed from the address
// rounded up by half a page:
//
//	%hi(x) = (x + 0x800) >> 12        %lo(x) = x & 0xfff
//
// With x = 0x1ff8 that gives lui 0x2000 plus an immediate of -8, which lands
// on 0x1ff8. Without the bias it gives lui 0x1000 plus 0xff8, landing 0x1000
// low -- a link that succeeds and a program that reads the wrong bytes, which
// is the worst combination these can produce.
func applyRelocRISCV(s *Section, r Reloc, symVA int, base int) error {
	S := symVA
	A := int(r.Addend)
	P := base + r.Off
	get := func() uint32 { return binary.LittleEndian.Uint32(s.Data[r.Off:]) }
	put := func(v uint32) { binary.LittleEndian.PutUint32(s.Data[r.Off:], v) }

	switch r.Type {
	case rRISCV64:
		if r.Off < 0 || r.Off+8 > len(s.Data) {
			return fmt.Errorf("reloc out of range for %s", r.Sym)
		}
		binary.LittleEndian.PutUint64(s.Data[r.Off:], uint64(int64(S+A)))
		return nil
	case rRISCV32:
		if r.Off < 0 || r.Off+4 > len(s.Data) {
			return fmt.Errorf("reloc out of range for %s", r.Sym)
		}
		binary.LittleEndian.PutUint32(s.Data[r.Off:], uint32(int32(S+A)))
		return nil
	case rRISCVCall, rRISCVCallPLT:
		// A call is an `auipc` followed by a `jalr` -- eight bytes, two
		// immediate fields, one relocation. The psABI describes it as
		// expanding into a PCREL_HI20 on the first word and a PCREL_LO12_I on
		// the second, and doing it in one place is the only way to get the
		// pair's arithmetic to agree: both halves come from the SAME
		// displacement, measured from the auipc's own address.
		//
		// The +0x800 bias is the same one the HI20 case explains. It is easy
		// to lose here, because this relocation does not look like a HI20 --
		// but it feeds an auipc, and the jalr below it carries a sign-extended
		// 12-bit immediate exactly as an `addi` would.
		if r.Off < 0 || r.Off+8 > len(s.Data) {
			return fmt.Errorf("reloc out of range for %s", r.Sym)
		}
		off := (S + A) - P
		hi := (off + 0x800) >> 12
		lo := off & 0xfff
		w1 := binary.LittleEndian.Uint32(s.Data[r.Off:])
		w1 = (w1 &^ 0xfffff000) | (uint32(hi&0xfffff) << 12)
		binary.LittleEndian.PutUint32(s.Data[r.Off:], w1)
		w2 := binary.LittleEndian.Uint32(s.Data[r.Off+4:])
		w2 = (w2 &^ 0xfff00000) | (uint32(lo) << 20)
		binary.LittleEndian.PutUint32(s.Data[r.Off+4:], w2)
		return nil
	}
	if r.Off < 0 || r.Off+4 > len(s.Data) {
		return fmt.Errorf("reloc out of range for %s", r.Sym)
	}
	w := get()
	switch r.Type {
	case rRISCVHI20:
		// lui: the 20-bit immediate sits in bits 31..12.
		hi := (S + A + 0x800) >> 12
		w = (w &^ 0xfffff000) | (uint32(hi&0xfffff) << 12)
	case rRISCVLO12I:
		// I-type (addi/ld/jalr): the 12-bit immediate sits in bits 31..20.
		lo := (S + A) & 0xfff
		w = (w &^ 0xfff00000) | (uint32(lo) << 20)
	case rRISCVLO12S:
		// S-type (sw/sd...): the immediate is split, imm[11:5] in bits 31..25
		// and imm[4:0] in bits 11..7 -- the low part is not contiguous with it.
		lo := (S + A) & 0xfff
		w = (w &^ 0xfe000000) | (uint32(lo&0xfe0) << 20) // imm[11:5]
		w = (w &^ 0x00000f80) | (uint32(lo&0x1f) << 7)   // imm[4:0]
	case rRISCVBranch:
		// SB-type: the 12-bit displacement is interleaved, and bit 0 of the
		// byte offset is not encoded at all (all branch targets are 2-byte
		// aligned in the base ISA).
		off := (S + A) - P
		imm := uint32(off)
		w = (w &^ 0x80000000) | ((imm>>12)&1)<<31   // imm[12]
		w = (w &^ 0x7e000000) | ((imm>>5)&0x3f)<<25 // imm[10:5]
		w = (w &^ 0x00000f00) | ((imm>>1)&0xf)<<8   // imm[4:1]
		w = (w &^ 0x00000080) | ((imm>>11)&1)<<7    // imm[11]
	case rRISCVJAL:
		// UJ-type: imm[20|10:1|11|19:12], also without bit 0.
		off := (S + A) - P
		imm := uint32(off)
		w = (w &^ 0x80000000) | ((imm>>20)&1)<<31 // imm[20]
		w = (w &^ 0x7fe00000) | ((imm>>1)&0x3ff)<<21
		w = (w &^ 0x00100000) | ((imm>>11)&1)<<20 // imm[11]
		w = (w &^ 0x000ff000) | ((imm>>12)&0xff)<<12
	default:
		return fmt.Errorf("applyReloc: unsupported RISC-V type %#x", r.Type)
	}
	put(w)
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
		// instruction's imm16 field, sign-extended.
		//
		// The imm16 field is NOT contiguous. ARM splits it across two ranges --
		// imm4 in bits 19:16 and imm12 in bits 11:0 -- with the destination
		// register sitting between them in bits 15:12. Writing the resolved
		// value into the low 16 bits therefore does not fill the immediate; it
		// overwrites Rd. For vfmt_i at 0x9000 the high nibble landed in the
		// register field, turning `movw r0, #0x9000` into `movw r9, #0`, and
		// the following `movt r0, #0` cleared the top of a register that had
		// never been loaded -- so the call went to whatever the low half of a
		// stack address happened to be.
		a := int64(int16(armImm16(w)))
		res := uint32((S + int(a)) & 0xffff)
		put((w &^ armImm16Mask) | armEncodeImm16(res))
	case rARMMovtAbs:
		// movt: high 16 bits of (S + A), into the same split field. The
		// addend is the sign-extended immediate shifted up by 16.
		a := int64(int16(armImm16(w))) << 16
		res := uint32(((S + int(a)) >> 16) & 0xffff)
		put((w &^ armImm16Mask) | armEncodeImm16(res))
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

// armImm16Mask is the set of bits MOVW and MOVT use for their 16-bit
// immediate: imm4 in 19:16 and imm12 in 11:0.
const armImm16Mask = 0x000f0fff

// armImm16 reads the split imm16 field of a MOVW/MOVT instruction.
func armImm16(w uint32) uint32 {
	return ((w >> 16) & 0xf) << 12 | (w & 0xfff)
}

// armEncodeImm16 places a 16-bit immediate into the split MOVW/MOVT field,
// leaving the destination register (bits 15:12) untouched.
func armEncodeImm16(v uint32) uint32 {
	return ((v >> 12) & 0xf) << 16 | (v & 0xfff)
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
