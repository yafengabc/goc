package main

// Helpers shared by the IR expression and statement generators: literal
// classification, numeric conversions, string constants and the small type
// predicates the emitters ask for.

import (
	"math"
	"strconv"
)

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

func f64bits(f float64) uint64 { return math.Float64bits(f) }
func f32bits(f float64) uint32 { return math.Float32bits(float32(f)) }

// floatOrDouble picks the C type a floating literal has: an unsuffixed literal
// is a double, an "f" suffixed one a float.
func floatOrDouble(n *NumLit) *Type {
	// IsFloat is the "f" suffix: it picks float, and everything else that
	// reached here is a double.
	if n.IsFloat {
		return FloatType()
	}
	return DoubleType()
}

// numLitType gives an integer literal its C type, which the suffix and the
// value's magnitude together decide.
func numLitType(n *NumLit) *Type {
	w := 4
	switch {
	case n.Long:
		w = 8
	case n.Unsig:
		w = 4
	}
	if !n.Long && !n.Unsig && n.Val > 0x7fffffff {
		w = 8 // a value too large for int promotes to long
	}
	t := &Type{Kind: KInt, Width: w, Signed: !n.Unsig}
	return t
}

func bytesToBytes(b []byte) []byte { return b }

// --- conversions ------------------------------------------------------------

// convert renders a value of type from as type to. LLVM has no implicit
// conversions, so every C conversion becomes an explicit instruction; one that
// changes nothing is left alone so the IR stays readable.
//
// This is the type-to-type form. convertTo is the same thing for a target that
// is already rendered as text, and the two share the pointer cases below --
// having them drift apart is how a `trunc ptr to i32` reaches LLVM, which it
// rejects because no such cast exists.
func (e *irEmitter) convert(op string, from, to *Type) string {
	// An array target decays the same way an array source does. A string
	// literal is a char array whose value is already a pointer to its first
	// element, so asking to convert it "to [1 x i8]" asked for a bitcast of a
	// pointer to an array, which has no valid opcode in LLVM.
	if to != nil && to.Kind == KArr {
		to = PtrType(to.Elem)
	}
	return e.convertTo(op, from, e.ty(to))
}

func floatBits(t *Type) (int, bool) {
	if t == nil {
		return 0, false
	}
	if t.Kind == KFloat {
		return 32, true
	}
	if t.Kind == KDouble {
		return 64, true
	}
	return 0, false
}

func isFloatTy(t *Type) bool {
	_, ok := floatBits(t)
	return ok
}

// widthOf returns a type's width in bits, counting pointers as 64.
func widthOf(t *Type) int {
	if t == nil {
		return 32
	}
	switch t.Kind {
	case KInt, KBool:
		return t.Width * 8
	case KFloat:
		return 32
	case KDouble:
		return 64
	case KPtr, KFunc, KArr:
		return 64
	}
	return 64
}

// scratchSlot returns an alloca usable as a conversion staging buffer. The
// conversions that need one are rare and the slot is reused, so it is created
// once per function on first use.
func (e *irEmitter) scratchSlot(n int) string {
	if e.scratch != "" {
		return e.scratch
	}
	slot := e.newTmp()
	e.entry.WriteString("  " + slot + " = alloca [" + itoa(n) + " x i8], align 16\n")
	e.scratch = slot
	return slot
}

// storeBrace writes a braced initialiser into an object of type t at address p.
func (e *irEmitter) storeBrace(b *BraceInit, t *Type, p string) {
	e.storeInit(b, t, p, 0)
}

// storeInit writes the elements of b into the aggregate at p, starting at
// byte offset off within it.
func (e *irEmitter) storeInit(b *BraceInit, t *Type, p string, off int) {
	if b == nil || t == nil {
		return
	}
	switch t.Kind {
	case KArr:
		esz := sizeOf(t.Elem)
		for i, el := range b.Elems {
			idx := i
			if el.DesigIdx >= 0 {
				idx = el.DesigIdx
			}
			slot := e.gep(p, esz, int64(idx))
			if nested, ok := el.E.(*BraceInit); ok {
				e.storeInit(nested, t.Elem, slot, 0)
				continue
			}
			e.storeScalar(slot, el.E, t.Elem)
		}
	case KStruct, KUnion:
		for i, el := range b.Elems {
			if i >= len(t.Members) {
				break
			}
			mem := t.Members[i]
			if el.Desig != "" {
				for _, mm := range t.Members {
					if mm.Name == el.Desig {
						mem = mm
						break
					}
				}
			}
			if mem.Type == nil {
				continue
			}
			slot := e.gep(p, 1, int64(mem.Offset))
			if nested, ok := el.E.(*BraceInit); ok {
				e.storeInit(nested, mem.Type, slot, 0)
				continue
			}
			e.storeScalar(slot, el.E, mem.Type)
		}
	default:
		// A braced scalar: "{ 5 }" is just 5.
		if len(b.Elems) > 0 {
			e.storeScalar(p, b.Elems[0].E, t)
		}
	}
	_ = off
}

// storeScalar evaluates e and stores it into the object of type t at p.
func (e *irEmitter) storeScalar(p string, x Expr, t *Type) {
	if x == nil {
		return
	}
	if nested, ok := x.(*BraceInit); ok {
		e.storeInit(nested, t, p, 0)
		return
	}
	v := e.eval(x)
	e.store(p, e.coerce(v, t))
}

// coerce converts a value to a target type where the assignment requires it.
func (e *irEmitter) coerce(v val, to *Type) val {
	if to == nil || v.ty == nil {
		return v
	}
	if e.ty(v.ty) == e.ty(to) {
		return val{op: v.op, ty: to}
	}
	return val{op: e.convert(v.op, v.ty, to), ty: to}
}

// gep builds a getelementptr for an array of elements of size esz.
func (e *irEmitter) gep(p string, esz int, idx int64) string {
	v := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i64 %d", v, p, idx*int64(esz))
	return v
}

// storeString copies a string literal's bytes into a char array. A C initialiser
// of the form `char s[] = "x"` copies; assigning the same literal to a char*
// takes its address instead, and the two are different instructions.
func (e *irEmitter) storeString(sl *StrLit, t *Type, slot string) {
	n := t.Len
	if n <= 0 {
		n = len(sl.Bytes) + 1
	}
	// Copying literal bytes as i8 stores: LLVM has no memcpy here, and a byte
	// at a time is what the initialiser means.
	for i := 0; i < n; i++ {
		var b byte
		if i < len(sl.Bytes) {
			b = sl.Bytes[i]
		}
		dst := e.newTmp()
		e.line("%s = getelementptr inbounds i8, ptr %s, i64 %d", dst, slot, i)
		e.line("store i8 %d, ptr %s, align 1", int(b), dst)
	}
}
