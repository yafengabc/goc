package main

import "fmt"

// checker implements the front-end type system: it resolves identifiers
// against a scope stack, computes the type of every expression, and enforces
// the rules a real C front end would (no undeclared names, argument/return
// compatibility, lvalues for & / * / [], pointer element compatibility, ...).
//
// It is deliberately permissive about numeric conversions (any integer-family
// value mixes with any other, and with double) because the code generator uses
// an 8-byte slot for every scalar. The checks that matter for catching real
// mistakes -- scoping, call signatures, pointer element types, lvalues -- are
// strict.

type cScope struct {
	vars map[string]*Type
}

type checker struct {
	funcs  map[string]*FuncDecl // function definitions
	protos map[string]*FuncDecl // forward declarations from headers
	scopes []*cScope
	errs   []error
}

func (c *checker) push() { c.scopes = append(c.scopes, &cScope{vars: map[string]*Type{}}) }
func (c *checker) pop()  { c.scopes = c.scopes[:len(c.scopes)-1] }

func (c *checker) put(name string, t *Type, line int) {
	top := c.scopes[len(c.scopes)-1]
	if _, dup := top.vars[name]; dup {
		c.errf(line, "redefinition of %q in the same scope", name)
		return
	}
	top.vars[name] = t
}

func (c *checker) lookup(name string) *Type {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if t, ok := c.scopes[i].vars[name]; ok {
			return t
		}
	}
	return nil
}

func (c *checker) errf(line int, format string, a ...any) {
	if line > 0 {
		c.errs = append(c.errs, fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, a...)))
	} else {
		c.errs = append(c.errs, fmt.Errorf(format, a...))
	}
}

// Check walks the whole program and returns every diagnostic found. A normal
// (successful) run returns a nil or empty slice.
func Check(prog *Program) []error {
	c := &checker{
		funcs:  map[string]*FuncDecl{},
		protos: map[string]*FuncDecl{},
	}
	for _, f := range prog.Funcs {
		c.funcs[f.Name] = f
	}
	for _, f := range prog.Prototypes {
		// A later prototype overrides an earlier one; a definition (in
		// c.funcs) always wins because checkCall looks there first.
		if _, ok := c.funcs[f.Name]; !ok {
			c.protos[f.Name] = f
		}
	}
	for _, f := range prog.Funcs {
		if f.Ret.IsArray() || f.Ret.IsFunc() {
			c.errf(0, "function %q cannot return %s", f.Name, f.Ret)
		}
		for i, pt := range f.ParamTypes {
			if pt.IsArray() || pt.IsFunc() {
				c.errf(0, "parameter %d of %q has invalid type %s", i+1, f.Name, pt)
			}
		}
		c.push()
		for i, p := range f.Params {
			c.put(p, f.ParamTypes[i], 0)
		}
		c.checkBlock(f.Body, f)
		c.pop()
	}
	if _, ok := c.funcs["main"]; !ok {
		c.errf(0, "program has no main()")
	}
	return c.errs
}

func (c *checker) checkBlock(b *Block, fn *FuncDecl) {
	c.push()
	for _, st := range b.Stmts {
		c.checkStmt(st, fn)
	}
	c.pop()
}

func (c *checker) checkStmt(st Stmt, fn *FuncDecl) {
	switch n := st.(type) {
	case *DeclStmt:
		c.put(n.Name, n.Typ, n.Line)
		if n.Typ.IsArray() && n.Init != nil {
			c.errf(n.Line, "array %q cannot be initialised here (use memset / a loop)", n.Name)
		}
		if n.Init != nil {
			t := c.checkExpr(n.Init, fn)
			if !assignable(n.Typ, t) {
				c.errf(n.Line, "initialiser of type %s is not assignable to %s", t, n.Typ)
			}
		}
	case *DeclList:
		for _, d := range n.Decls {
			c.checkStmt(d, fn)
		}
	case *AssignStmt:
		lt, ok := c.checkLValue(n.Lhs, fn)
		var line int
		if id, ok2 := n.Lhs.(*Ident); ok2 {
			line = id.Line
		}
		if ok {
			rt := c.checkExpr(n.Rhs, fn)
			if !assignable(lt, rt) {
				c.errf(line, "cannot assign %s to %s", rt, lt)
			}
		}
	case *ExprStmt:
		c.checkExpr(n.E, fn)
	case *ReturnStmt:
		if n.E != nil {
			t := c.checkExpr(n.E, fn)
			if fn.Ret.IsVoid() {
				c.errf(0, "returning a value from void function %q", fn.Name)
			} else if !assignable(fn.Ret, t) {
				c.errf(0, "returning %s from %q which returns %s", t, fn.Name, fn.Ret)
			}
		} else if !fn.Ret.IsVoid() {
			c.errf(0, "function %q returning %s must return a value", fn.Name, fn.Ret)
		}
	case *IfStmt:
		c.checkExpr(n.Cond, fn)
		c.checkStmt(n.Then, fn)
		if n.Else != nil {
			c.checkStmt(n.Else, fn)
		}
	case *WhileStmt:
		c.checkExpr(n.Cond, fn)
		c.checkStmt(n.Body, fn)
	case *Block:
		c.checkBlock(n, fn)
	}
}

// checkLValue returns the type of e and whether e is a modifiable lvalue.
func (c *checker) checkLValue(e Expr, fn *FuncDecl) (*Type, bool) {
	switch n := e.(type) {
	case *Ident:
		t := c.lookup(n.Name)
		if t == nil {
			c.errf(n.Line, "undeclared identifier %q", n.Name)
			return IntType(), false
		}
		if t.IsArray() {
			c.errf(n.Line, "array %q is not a modifiable lvalue", n.Name)
			return t, false
		}
		if t.IsFunc() {
			c.errf(n.Line, "function %q is not a modifiable lvalue", n.Name)
			return t, false
		}
		return t, true
	case *Unary:
		if n.Op != "*" {
			c.errf(0, "expression is not an lvalue")
			return IntType(), false
		}
		t := c.checkExpr(n.E, fn)
		if !t.IsPtr() {
			c.errf(0, "cannot dereference non-pointer type %s", t)
			return IntType(), false
		}
		if t.Elem == nil {
			return VoidType(), true
		}
		return t.Elem, true
	case *Index:
		base := c.checkExpr(n.Base, fn)
		c.checkExpr(n.Idx, fn)
		if base.IsArray() {
			if base.Elem == nil {
				c.errf(0, "cannot index incomplete array type")
				return IntType(), false
			}
			return base.Elem, true
		}
		if base.IsPtr() {
			if base.Elem == nil {
				return VoidType(), true
			}
			return base.Elem, true
		}
		c.errf(0, "cannot index non-array/non-pointer type %s", base)
		return IntType(), false
	}
	c.errf(0, "expression is not an lvalue")
	return IntType(), false
}

func (c *checker) checkExpr(e Expr, fn *FuncDecl) *Type {
	switch n := e.(type) {
	case *NumLit:
		if n.Kind == TDouble {
			return DoubleType()
		}
		return IntType()
	case *StrLit:
		return PtrType(CharType()) // string literal decays to char*
	case *Ident:
		t := c.lookup(n.Name)
		if t == nil {
			c.errf(n.Line, "undeclared identifier %q", n.Name)
			return IntType()
		}
		if t.IsArray() {
			return PtrType(t.Elem) // array decays to pointer
		}
		return t
	case *Unary:
		switch n.Op {
		case "-":
			t := c.checkExpr(n.E, fn)
			if !t.IsArith() {
				c.errf(0, "operand of '-' must be arithmetic, got %s", t)
			}
			return t
		case "!":
			c.checkExpr(n.E, fn)
			return IntType()
		case "&":
			lt, ok := c.checkLValue(n.E, fn)
			if !ok {
				return IntType()
			}
			return PtrType(lt)
		case "*":
			t := c.checkExpr(n.E, fn)
			if !t.IsPtr() {
				c.errf(0, "cannot dereference non-pointer type %s", t)
				return IntType()
			}
			if t.Elem == nil {
				return VoidType()
			}
			return t.Elem
		}
	case *Binary:
		return c.checkBinary(n, fn)
	case *Call:
		return c.checkCall(n, fn)
	case *Index:
		base := c.checkExpr(n.Base, fn)
		idx := c.checkExpr(n.Idx, fn)
		if !idx.IsArith() {
			c.errf(0, "array index must be an integer, got %s", idx)
		}
		if base.IsArray() {
			if base.Elem == nil {
				c.errf(0, "cannot index incomplete array type")
				return VoidType()
			}
			return base.Elem
		}
		if base.IsPtr() {
			if base.Elem == nil {
				return VoidType()
			}
			return base.Elem
		}
		c.errf(0, "cannot index non-array/non-pointer type %s", base)
		return IntType()
	}
	return IntType()
}

func (c *checker) checkBinary(n *Binary, fn *FuncDecl) *Type {
	lt := c.checkExpr(n.L, fn)
	rt := c.checkExpr(n.R, fn)
	switch n.Op {
	case "+", "-", "*", "/", "%":
		if n.Op == "%" && !(lt.IsIntClass() && rt.IsIntClass()) {
			c.errf(0, "operator '%%' requires integer operands, got %s and %s", lt, rt)
		}
		// Pointer arithmetic: ptr +/- int, ptr - ptr.
		if lt.IsPtr() || rt.IsPtr() {
			switch {
			case n.Op == "+" && lt.IsPtr() && rt.IsIntClass():
				return lt
			case n.Op == "+" && lt.IsIntClass() && rt.IsPtr():
				return rt
			case n.Op == "-" && lt.IsPtr() && rt.IsIntClass():
				return lt
			case n.Op == "-" && lt.IsPtr() && rt.IsPtr():
				return IntType() // pointer difference
			}
			c.errf(0, "invalid pointer arithmetic on %s and %s", lt, rt)
			return lt
		}
		if lt.Kind == KDouble || rt.Kind == KDouble {
			if n.Op == "%" {
				c.errf(0, "operator '%%' requires integer operands, got %s and %s", lt, rt)
			}
			return DoubleType()
		}
		return IntType()
	case "<", ">", "<=", ">=", "==", "!=":
		if !(lt.IsScalar() && rt.IsScalar()) {
			c.errf(0, "relational operator requires scalar operands, got %s and %s", lt, rt)
		}
		return IntType()
	case "&&", "||":
		if !(lt.IsScalar() && rt.IsScalar()) {
			c.errf(0, "logical operator requires scalar operands, got %s and %s", lt, rt)
		}
		return IntType()
	}
	return IntType()
}

func (c *checker) checkCall(n *Call, fn *FuncDecl) *Type {
	if fd, ok := c.funcs[n.Name]; ok {
		return c.checkCallSig(n, fn, fd, false)
	}
	if pd, ok := c.protos[n.Name]; ok {
		return c.checkCallSig(n, fn, pd, true)
	}
	// External / clib call whose signature we do not model: accept it and
	// assume an int result (true for every clib function c0 exposes).
	return IntType()
}

// checkCallSig validates a call against a known signature (a definition or a
// prototype). It reports arity and argument-type mismatches. isProto only
// affects the diagnostics wording.
func (c *checker) checkCallSig(n *Call, fn *FuncDecl, sig *FuncDecl, isProto bool) *Type {
	if len(n.Args) != len(sig.ParamTypes) {
		what := "function"
		if isProto {
			what = "prototype"
		}
		c.errf(0, "call to %q (%s): expected %d arguments, got %d", n.Name, what, len(sig.ParamTypes), len(n.Args))
		return sig.Ret
	}
	for i, a := range n.Args {
		at := c.checkExpr(a, fn)
		if !assignable(sig.ParamTypes[i], at) {
			c.errf(0, "call to %q: argument %d has type %s, expected %s",
				n.Name, i+1, at, sig.ParamTypes[i])
		}
	}
	return sig.Ret
}

// assignable reports whether a value of src may be stored into a location of
// dst under C's implicit conversion rules (relaxed for the toy model).
func assignable(dst, src *Type) bool {
	if dst == nil || src == nil {
		return true // an error was already reported upstream
	}
	if dst.IsVoid() || src.IsVoid() {
		return false
	}
	switch {
	case dst.IsArith() && src.IsArith():
		return true
	case dst.IsPtr() && src.IsPtr():
		de, se := dst.Elem, src.Elem
		if de == nil || se == nil || de.IsVoid() || se.IsVoid() {
			return true // any pointer compares with void* / unknown
		}
		return typesEqual(de, se)
	case dst.IsPtr() && src.IsIntClass():
		return true // int -> pointer (e.g. 0 / NULL)
	case dst.IsIntClass() && src.IsPtr():
		return true // pointer -> int
	}
	return false
}

func typesEqual(a, b *Type) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KPtr, KArr:
		return typesEqual(a.Elem, b.Elem)
	case KFunc:
		if !typesEqual(a.Ret, b.Ret) {
			return false
		}
		if len(a.Params) != len(b.Params) {
			return false
		}
		for i := range a.Params {
			if !typesEqual(a.Params[i], b.Params[i]) {
				return false
			}
		}
		return true
	default:
		return true
	}
}
