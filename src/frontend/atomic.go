package frontend

// C11 7.17 <stdatomic.h> builtins.
//
// atomic_fetch_add and its siblings cannot be written in C. Each one is a
// single locked read-modify-write that hands back the value the object held
// BEFORE the update, and no C expression can produce that: "old = *p; *p += v"
// is two operations, and a concurrent update between them is lost. goclib is
// deliberately pure C -- the LLVM back end cannot translate inline asm -- so
// <stdatomic.h> maps the standard names onto these builtins and each back end
// lowers them to its own atomic primitive.
//
// Every one of them takes the address of the atomic object first. The width
// and signedness of the locked access come from the pointee, never from the
// operand: atomic_fetch_add on an _Atomic char must not touch the seven bytes
// that follow it.

// An AtomicBuiltin describes what one of the builtins does.
type AtomicBuiltin struct {
	// Op is the binary operator applied to (old value, operand). It is empty
	// for atomic_exchange, which stores the operand unchanged.
	Op string
	// CAS marks the compare-exchange form, whose signature and result differ:
	// (object, expected, desired) -> bool, with *expected updated on failure.
	CAS bool
}

var atomicBuiltins = map[string]AtomicBuiltin{
	"__goc_atomic_fetch_add":        {Op: "+"},
	"__goc_atomic_fetch_sub":        {Op: "-"},
	"__goc_atomic_fetch_and":        {Op: "&"},
	"__goc_atomic_fetch_or":         {Op: "|"},
	"__goc_atomic_fetch_xor":        {Op: "^"},
	"__goc_atomic_exchange":         {},
	"__goc_atomic_compare_exchange": {CAS: true},
}

// LookupAtomicBuiltin reports whether name is one of the builtins above. Both
// back ends share this table so the recognised set cannot drift apart.
func LookupAtomicBuiltin(name string) (AtomicBuiltin, bool) {
	ab, ok := atomicBuiltins[name]
	return ab, ok
}

// atomicValueType returns the value type of the atomic object a builtin's
// first argument points at: the pointee with the Atomic flag dropped, since
// the fetch family returns a plain value, not an atomic lvalue. It reports a
// diagnostic and returns nil when the argument is not a pointer to an integer
// or _Bool.
func (c *checker) atomicValueType(n *Call, fn *FuncDecl) *Type {
	pt := c.checkExpr(n.Args[0], fn)
	if pt == nil || !pt.IsPtr() || pt.Elem == nil {
		got := "an untyped expression"
		if pt != nil {
			got = pt.String()
		}
		c.errf(0, "%s: first argument must be a pointer to an _Atomic integer, got %s", n.Name, got)
		return nil
	}
	et := pt.Elem
	if et.Kind != KInt && et.Kind != KBool {
		c.errf(0, "%s: atomic operations are only defined on integer types, got %s", n.Name, et)
		return nil
	}
	res := *et
	res.Atomic = false
	return &res
}

// checkAtomicBuiltin type-checks one of the builtins. The result of the fetch
// family and of atomic_exchange is the object's value type; the result of the
// compare-exchange form is _Bool.
func (c *checker) checkAtomicBuiltin(n *Call, fn *FuncDecl, ab AtomicBuiltin) *Type {
	want := 2
	if ab.CAS {
		want = 3
	}
	if len(n.Args) != want {
		c.errf(0, "call to %q: expected %d arguments, got %d", n.Name, want, len(n.Args))
		for _, a := range n.Args {
			c.checkExpr(a, fn)
		}
		return IntType()
	}
	vt := c.atomicValueType(n, fn)
	// The remaining arguments are ordinary expressions; expected is a pointer
	// to the value type and desired is a value.
	for _, a := range n.Args[1:] {
		c.checkExpr(a, fn)
	}
	if ab.CAS {
		return &Type{Kind: KBool, Width: 1, Signed: true}
	}
	if vt == nil {
		return IntType()
	}
	return vt
}
