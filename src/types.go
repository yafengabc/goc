package main

import "strings"

// Type is the structured C type used by the front end (parser + checker).
//
// The code generator does not consume Type directly: it only cares whether a
// value rides in a GP register (integer class) or an XMM register (double), so
// Type.Class() collapses everything except double into the CType TInt. Pointers
// and all integer widths share that 8-byte integer slot on the toy stack model.
type TypeKind int

const (
	KVoid TypeKind = iota
	KInt
	KDouble
	KFloat
	KPtr
	KArr
	KFunc
	KStruct
	KUnion
)

// Member is a single field of a struct or union.
type Member struct {
	Name   string // field name (may be "" for an anonymous nested member)
	Type   *Type  // field type
	Offset int    // byte offset within the struct/union (0 for all union members)
	// BitWidth > 0 marks this member as a bit-field occupying BitWidth bits
	// starting at bit offset BitOff within its natural storage unit. Bit-fields
	// are handled later; for now only the data members are materialised.
	BitWidth int
	BitOff   int
}

type Type struct {
	Kind     TypeKind
	Elem     *Type     // KPtr / KArr: element type
	Len      int       // KArr: number of elements (0 = incomplete)
	Params   []*Type   // KFunc: parameter types
	Ret      *Type     // KFunc: return type
	Variadic bool      // KFunc: declared with a trailing "..."
	Width    int       // KInt: 1=char, 2=short, 4=int, 8=long
	Signed   bool      // KInt
	Members  []*Member // KStruct / KUnion: ordered fields
	Size     int       // KStruct / KUnion: total size in bytes (aligned)
	Align    int       // KStruct / KUnion: required alignment (0 = not computed)
	Tag      string    // KStruct / KUnion: optional struct tag (named structs)
}

// --- constructors -----------------------------------------------------------

func IntType() *Type                  { return &Type{Kind: KInt, Width: 4, Signed: true} }
func CharType() *Type                 { return &Type{Kind: KInt, Width: 1, Signed: true} }
func UnsignedType() *Type             { return &Type{Kind: KInt, Width: 4, Signed: false} }
func UnsignedCharType() *Type         { return &Type{Kind: KInt, Width: 1, Signed: false} }
func DoubleType() *Type               { return &Type{Kind: KDouble} }
func FloatType() *Type                { return &Type{Kind: KFloat} }
func VoidType() *Type                 { return &Type{Kind: KVoid} }
func PtrType(elem *Type) *Type        { return &Type{Kind: KPtr, Elem: elem} }
func ArrType(elem *Type, n int) *Type { return &Type{Kind: KArr, Elem: elem, Len: n} }
func FuncType(ret *Type, params []*Type) *Type {
	return &Type{Kind: KFunc, Ret: ret, Params: params}
}

// StructType builds an (initially incomplete) struct/union type carrying the
// supplied members; the caller must call computeLayout once the members are
// final.
func StructType(members []*Member) *Type {
	return &Type{Kind: KStruct, Members: members}
}

func UnionType(members []*Member) *Type {
	return &Type{Kind: KUnion, Members: members}
}

// --- layout -----------------------------------------------------------------

// alignOf returns the natural alignment of a type on the Win64 / System V x86-64
// ABIs: char=1, short=2, int/long=4/8, float=4, double/pointer=8, struct=its own
// Align.
func alignOf(t *Type) int {
	if t == nil {
		return 1
	}
	switch t.Kind {
	case KInt:
		return t.Width // 1, 2, 4, or 8
	case KFloat:
		return 4
	case KDouble, KPtr, KFunc:
		return 8
	case KArr:
		return alignOf(t.Elem)
	case KStruct, KUnion:
		if t.Align != 0 {
			return t.Align
		}
	}
	return 8
}

// sizeOf returns the total byte size of a type (the storage a value occupies).
func sizeOf(t *Type) int {
	if t == nil {
		return 1
	}
	switch t.Kind {
	case KInt:
		return t.Width
	case KFloat:
		return 4
	case KDouble, KPtr, KFunc:
		return 8
	case KArr:
		return sizeOf(t.Elem) * t.Len
	case KStruct, KUnion:
		if t.Size != 0 {
			return t.Size
		}
	}
	return 8
}

// computeLayout fills Size and Align for a struct or union using MSVC x64
// packing rules (each member aligned to its natural alignment; the struct is
// padded to a multiple of its maximum member alignment). Union members all
// share offset 0 and the union size is the largest member size.
func (t *Type) computeLayout() {
	if t.Kind != KStruct && t.Kind != KUnion {
		return
	}
	align := 1
	if t.Kind == KUnion {
		maxSize := 0
		for _, m := range t.Members {
			a := alignOf(m.Type)
			if a > align {
				align = a
			}
			m.Offset = 0
			sz := sizeOf(m.Type)
			if sz > maxSize {
				maxSize = sz
			}
		}
		t.Align = align
		t.Size = maxSize
		return
	}
	off := 0
	for _, m := range t.Members {
		a := alignOf(m.Type)
		if a > align {
			align = a
		}
		if off%a != 0 {
			off += a - (off % a)
		}
		m.Offset = off
		off += sizeOf(m.Type)
	}
	t.Align = align
	size := off
	if align != 0 && size%align != 0 {
		size += align - (size % align)
	}
	t.Size = size
}

// --- predicates -------------------------------------------------------------

// Class returns the low-level codegen scalar class for this type. Everything
// that is not floating point is an 8-byte integer-class value (pointers
// included). float shares the double class: a float scalar is always widened to
// a double in registers/temporaries and only narrowed back to 4 bytes at real
// float storage (array elements, struct members, float parameters, returns).
func (t *Type) Class() CType {
	if t.Kind == KDouble || t.Kind == KFloat {
		return TDouble
	}
	return TInt
}

func (t *Type) IsArith() bool    { return t.Kind == KInt || t.Kind == KDouble || t.Kind == KFloat }
func (t *Type) IsIntClass() bool { return t.Kind == KInt }
func (t *Type) IsScalar() bool   { return t.IsArith() || t.Kind == KPtr }
func (t *Type) IsVoid() bool     { return t.Kind == KVoid }
func (t *Type) IsPtr() bool      { return t.Kind == KPtr }
func (t *Type) IsFloat() bool    { return t.Kind == KFloat }

// IsFloating reports whether the type is a real floating-point type (float or
// double), as opposed to the codegen scalar class TDouble which float also
// rides in.
func (t *Type) IsFloating() bool { return t.Kind == KDouble || t.Kind == KFloat }
func (t *Type) IsArray() bool    { return t.Kind == KArr }
func (t *Type) IsFunc() bool     { return t.Kind == KFunc }
func (t *Type) IsStruct() bool   { return t.Kind == KStruct }
func (t *Type) IsUnion() bool    { return t.Kind == KUnion }

// IsChar reports whether t is a char type. In this dialect char is a 1-byte
// int (signed or unsigned); it is the element type a string literal can
// initialise an array of.
func (t *Type) IsChar() bool { return t != nil && t.Kind == KInt && t.Width == 1 }

// PtrElem returns the element type of a pointer/array, or nil.
func (t *Type) PtrElem() *Type {
	if t.Kind == KPtr || t.Kind == KArr {
		return t.Elem
	}
	return nil
}

// String renders a type for diagnostics.
func (t *Type) String() string {
	switch t.Kind {
	case KVoid:
		return "void"
	case KDouble:
		return "double"
	case KFloat:
		return "float"
	case KInt:
		s := ""
		if !t.Signed {
			s = "unsigned "
		}
		switch t.Width {
		case 1:
			return s + "char"
		case 2:
			return s + "short"
		case 4:
			return s + "int"
		default:
			return s + "long"
		}
	case KPtr:
		if t.Elem == nil {
			return "void*"
		}
		return t.Elem.String() + "*"
	case KArr:
		if t.Elem == nil {
			return "void[]"
		}
		return t.Elem.String() + "[]"
	case KStruct:
		if t.Tag != "" {
			return "struct " + t.Tag
		}
		return "struct{}"
	case KUnion:
		if t.Tag != "" {
			return "union " + t.Tag
		}
		return "union{}"
	case KFunc:
		ret := "void"
		if t.Ret != nil {
			ret = t.Ret.String()
		}
		ps := make([]string, 0, len(t.Params))
		for _, p := range t.Params {
			if p == nil {
				ps = append(ps, "?")
				continue
			}
			ps = append(ps, p.String())
		}
		if t.Variadic {
			ps = append(ps, "...")
		}
		return ret + "(" + strings.Join(ps, ", ") + ")"
	}
	return "?"
}
