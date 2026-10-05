package gocld

import "fmt"

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
