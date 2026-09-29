package main

// UFCS: uniform function call syntax for struct methods, Go/Nim style.
//
// A member call "x.f(args)" (or "p->f(args)") whose member f does not exist
// may still name a method: a plain function spelled T_f (T = x's struct tag)
// whose first parameter accepts x. The checker rewrites such a call into the
// direct call T_f(recv, args) with the receiver prepended -- its address for
// a struct-pointer receiver, the value itself for a value receiver. This is
// pure compile-time rewriting: no vtables, no metadata, zero runtime cost.
//
// Member lookups always win, so an existing function-pointer member keeps
// its C meaning ("s.cb(x)" is untouched) -- same precedence rule as Go.

import "fmt"

// tryUFCS attempts to resolve n as a method call. On success it fills
// n.UFCS with the rewritten direct call and returns the method's return
// type. Any mismatch -- non-member callee, non-struct or anonymous base,
// existing member, missing or wrongly-shaped T_f -- returns ok = false so
// the ordinary indirect-call error paths run unchanged.
func (c *checker) tryUFCS(n *IndirectCall, fn *FuncDecl) (*Type, bool) {
	me, ok := n.Fn.(*MemberExpr)
	if !ok {
		return nil, false
	}
	bt := c.checkExpr(me.Base, fn)
	st := bt
	if me.Arrow {
		if !bt.IsPtr() {
			return nil, false
		}
		st = bt.Elem
	}
	// Methods need a named struct: the T_ prefix is derived from the tag.
	if st == nil || st.Kind != KStruct || st.Tag == "" {
		return nil, false
	}
	for _, m := range st.Members {
		if m.Name == me.Name {
			return nil, false // a real member (e.g. a function pointer) wins
		}
	}
	meth := st.Tag + "_" + me.Name
	fd, ok := c.funcs[meth]
	if !ok {
		fd = c.protos[meth]
	}
	if fd == nil || len(fd.ParamTypes) == 0 {
		return nil, false // no such method: report "no member" as before
	}
	// The receiver must fit the method's first parameter: struct T* takes
	// the address (or the pointer itself through ->), struct T the value
	// (dereferenced through ->).
	p0 := fd.ParamTypes[0]
	sameStruct := func(t *Type) bool {
		return t != nil && t.Kind == KStruct && t.Tag == st.Tag
	}
	var recv Expr
	switch {
	case p0.IsPtr() && sameStruct(p0.Elem):
		if me.Arrow {
			recv = me.Base // p is already a struct T*
		} else {
			recv = &Unary{Op: "&", E: me.Base} // &x
		}
	case sameStruct(p0):
		if me.Arrow {
			recv = &Unary{Op: "*", E: me.Base} // *p, passed by value
		} else {
			recv = me.Base // x, passed by value
		}
	default:
		return nil, false // wrong receiver shape: report the normal error
	}
	args := make([]Expr, 0, len(n.Args)+1)
	args = append(args, recv)
	args = append(args, n.Args...)
	n.UFCS = &Call{Name: meth, Args: args}
	return c.checkArgs(meth, fd.ParamTypes, fd.Variadic, args, fn, fd.Ret), true
}

// ufcsHint builds the "define a T_f method" hint used in error messages when
// a member lookup on a named struct fails. Empty for anonymous structs.
func ufcsHint(st *Type, name string) string {
	if st == nil || st.Kind != KStruct || st.Tag == "" {
		return ""
	}
	return fmt.Sprintf(" (a method can be defined as a function %s_%s and called as x.%s())",
		st.Tag, name, name)
}
