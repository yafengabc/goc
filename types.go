package main

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
	KPtr
	KArr
	KFunc
)

type Type struct {
	Kind   TypeKind
	Elem   *Type   // KPtr / KArr: element type
	Len    int     // KArr: number of elements (0 = incomplete)
	Params []*Type // KFunc: parameter types
	Ret    *Type   // KFunc: return type
	Width  int     // KInt: 1=char, 4=int, 8=long (toy still uses 8-byte slots)
	Signed bool    // KInt
}

// --- constructors -----------------------------------------------------------

func IntType() *Type                  { return &Type{Kind: KInt, Width: 8, Signed: true} }
func CharType() *Type                 { return &Type{Kind: KInt, Width: 1, Signed: true} }
func UnsignedType() *Type             { return &Type{Kind: KInt, Width: 8, Signed: false} }
func UnsignedCharType() *Type         { return &Type{Kind: KInt, Width: 1, Signed: false} }
func DoubleType() *Type               { return &Type{Kind: KDouble} }
func VoidType() *Type                 { return &Type{Kind: KVoid} }
func PtrType(elem *Type) *Type        { return &Type{Kind: KPtr, Elem: elem} }
func ArrType(elem *Type, n int) *Type { return &Type{Kind: KArr, Elem: elem, Len: n} }
func FuncType(ret *Type, params []*Type) *Type {
	return &Type{Kind: KFunc, Ret: ret, Params: params}
}

// --- predicates -------------------------------------------------------------

// Class returns the low-level codegen scalar class for this type. Everything
// that is not a double is an 8-byte integer-class value (pointers included).
func (t *Type) Class() CType {
	if t.Kind == KDouble {
		return TDouble
	}
	return TInt
}

func (t *Type) IsArith() bool    { return t.Kind == KInt || t.Kind == KDouble }
func (t *Type) IsIntClass() bool { return t.Kind == KInt }
func (t *Type) IsScalar() bool   { return t.IsArith() || t.Kind == KPtr }
func (t *Type) IsVoid() bool     { return t.Kind == KVoid }
func (t *Type) IsPtr() bool      { return t.Kind == KPtr }
func (t *Type) IsArray() bool    { return t.Kind == KArr }
func (t *Type) IsFunc() bool     { return t.Kind == KFunc }

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
	case KInt:
		s := ""
		if !t.Signed {
			s = "unsigned "
		}
		switch t.Width {
		case 1:
			return s + "char"
		case 4:
			return s + "int"
		default:
			return s + "int"
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
	case KFunc:
		return "function"
	}
	return "?"
}
