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
	funcs   map[string]*FuncDecl // function definitions
	protos  map[string]*FuncDecl // forward declarations from headers
	scopes  []*cScope
	errs    []error
	swDepth int // how many switch statements enclose the statement being checked
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
	// A persistent global scope holds the top-level variables, visible to every
	// function. It is never popped.
	c.scopes = append(c.scopes, &cScope{vars: map[string]*Type{}})
	for _, g := range prog.Globals {
		c.checkStmt(g, nil)
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
		c.checkLabels(f.Body)
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
		// Initialiser forms, in order of specificity: a string literal for a
		// char array (the one array initialiser C allows outside braces), a
		// braced initialiser for any aggregate (or scalar), and everything
		// else through the normal expression/assignable path.
		strInit, braceInit := false, false
		if n.Typ.IsArray() && n.Init != nil {
			if sl, ok := n.Init.(*StrLit); ok && n.Typ.Elem.IsChar() {
				strInit = true
				need := len(sl.Bytes) + 1
				if n.Typ.Len == 0 {
					n.Typ.Len = need
				} else if n.Typ.Len < need {
					c.errf(n.Line, "initialiser string of length %d does not fit in char array %q of %d bytes",
						len(sl.Bytes), n.Name, n.Typ.Len)
				}
			} else if _, ok := n.Init.(*BraceInit); ok {
				braceInit = true
				c.checkBraceInit(n.Typ, n.Init.(*BraceInit), fn, n.Line)
			} else {
				c.errf(n.Line, "array %q cannot be initialised here (use memset / a loop)", n.Name)
			}
		} else if bi, ok := n.Init.(*BraceInit); ok {
			braceInit = true
			c.checkBraceInit(n.Typ, bi, fn, n.Line)
		}
		c.put(n.Name, n.Typ, n.Line)
		if n.Init != nil && !strInit && !braceInit {
			t := c.checkExpr(n.Init, fn)
			// A string literal "decays" to char*, which is not assignable to
			// the char-array type proper -- but it is the sanctioned form of
			// array initialisation, so the mismatch is not an error here.
			if !assignable(n.Typ, t) && !strInit {
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
	case *ForStmt:
		c.push()
		if n.Init != nil {
			c.checkStmt(n.Init, fn)
		}
		if n.Cond != nil {
			c.checkExpr(n.Cond, fn)
		}
		if n.Body != nil {
			c.checkStmt(n.Body, fn)
		}
		if n.Post != nil {
			c.checkExpr(n.Post, fn)
		}
		c.pop()
	case *DoWhileStmt:
		c.checkExpr(n.Cond, fn)
		if n.Body != nil {
			c.checkStmt(n.Body, fn)
		}
	case *SwitchStmt:
		st := c.checkExpr(n.Src, fn)
		if st != nil && st.Kind != KInt && !st.IsPtr() {
			c.errf(n.Line, "switch quantity must be an integer, not %s", st)
		}
		c.swDepth++
		c.checkSwitchBody(n, fn)
		c.swDepth--
	case *CaseStmt:
		if c.swDepth == 0 {
			c.errf(n.Line, "case label outside a switch")
		}
	case *DefaultStmt:
		if c.swDepth == 0 {
			c.errf(n.Line, "default label outside a switch")
		}
	case *GotoStmt:
		// The target is validated once the whole function body is known
		// (see checkLabels), because goto may jump forward.
	case *AsmStmt:
		// Inline assembly: the body is raw assembler text, not C, so there
		// is nothing to type-check here. Variable names it refers to are
		// bound to frame slots by the code generator (unknown identifiers
		// are left alone and reported by goa as undefined symbols). The
		// block cannot jump out of the function on its own, and any
		// control-flow labels it defines are assembler-local, so no scope
		// bookkeeping is needed either.
	case *LabelStmt:
		if n.Stmt != nil {
			c.checkStmt(n.Stmt, fn)
		}
	case *BreakStmt:
		// no type effect
	case *ContinueStmt:
		// no type effect
	case *Block:
		c.checkBlock(n, fn)
	}
}

// checkSwitchBody checks a switch body as a block, then enforces the rules C
// puts on its labels: case values must be unique and there may be at most one
// default. Statements before the first label are unreachable (C ignores them);
// they are still checked, so an undeclared name there is reported.
func (c *checker) checkSwitchBody(n *SwitchStmt, fn *FuncDecl) {
	seen := map[int]bool{}
	defaults := 0
	for _, st := range n.Body.Stmts {
		switch cs := st.(type) {
		case *CaseStmt:
			if seen[cs.Val] {
				c.errf(cs.Line, "duplicate case value %d in switch", cs.Val)
			}
			seen[cs.Val] = true
			continue
		case *DefaultStmt:
			defaults++
			if defaults > 1 {
				c.errf(cs.Line, "more than one default label in switch")
			}
			continue
		}
		c.checkStmt(st, fn)
	}
}

// checkLabels verifies that every goto in a function targets a label defined in
// that same function. C labels are function-scoped, so the whole body must be
// collected before any goto can be validated.
func (c *checker) checkLabels(b *Block) {
	if b == nil {
		return
	}
	defined := map[string]bool{}
	walkStmts(b, func(s Stmt) {
		if lab, ok := s.(*LabelStmt); ok {
			defined[lab.Name] = true
		}
	})
	walkStmts(b, func(s Stmt) {
		g, ok := s.(*GotoStmt)
		if !ok {
			return
		}
		if !defined[g.Label] {
			c.errf(g.Line, "goto %q: no such label in this function", g.Label)
		}
	})
}

// walkStmts visits every statement in a tree, including those nested inside
// if/loop/switch bodies and labelled statements. Expression trees are not
// visited: they contain no statements in goc's C subset.
func walkStmts(s Stmt, visit func(Stmt)) {
	if s == nil {
		return
	}
	visit(s)
	switch n := s.(type) {
	case *Block:
		for _, st := range n.Stmts {
			walkStmts(st, visit)
		}
	case *DeclList:
		for _, d := range n.Decls {
			walkStmts(d, visit)
		}
	case *IfStmt:
		walkStmts(n.Then, visit)
		walkStmts(n.Else, visit)
	case *WhileStmt:
		walkStmts(n.Body, visit)
	case *ForStmt:
		walkStmts(n.Init, visit)
		walkStmts(n.Body, visit)
	case *DoWhileStmt:
		walkStmts(n.Body, visit)
	case *SwitchStmt:
		walkStmts(n.Body, visit)
	case *LabelStmt:
		walkStmts(n.Stmt, visit)
	}
}

// checkLValue returns the type of e and whether e is a modifiable lvalue.
func (c *checker) checkLValue(e Expr, fn *FuncDecl) (*Type, bool) {
	switch n := e.(type) {
	case *Ident:
		t := c.lookup(n.Name)
		if t == nil {
			if _, ok := enumConsts[n.Name]; ok {
				// An enumerator is an integer constant, not an lvalue.
				c.errf(n.Line, "enum constant %q is not an lvalue", n.Name)
				return IntType(), false
			}
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
		if t.Const {
			c.errf(n.Line, "cannot assign to const-typed %q", n.Name)
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
	case *MemberExpr:
		return c.checkMemberLValue(n, fn)
	}
	c.errf(0, "expression is not an lvalue")
	return IntType(), false
}

// checkMemberLValue resolves the lvalue type of a struct/union member access
// (base.name or base->name) and reports an error if the base is not a
// struct/union (or pointer to one) or the member does not exist.
func (c *checker) checkMemberLValue(n *MemberExpr, fn *FuncDecl) (*Type, bool) {
	bt := c.checkExpr(n.Base, fn)
	var st *Type
	if n.Arrow {
		if !bt.IsPtr() {
			c.errf(n.Line, "left of '->' must be a pointer to struct/union, got %s", bt)
			return IntType(), false
		}
		st = bt.Elem
	} else {
		st = bt
	}
	if st == nil || (st.Kind != KStruct && st.Kind != KUnion) {
		c.errf(n.Line, "member access on non-struct/union type %s%s", st, c.arrayMethodHint(n))
		return IntType(), false
	}
	for _, m := range st.Members {
		if m.Name == n.Name {
			if m.Type.Const {
				c.errf(n.Line, "cannot assign to const member %q", n.Name)
				return m.Type, false
			}
			return m.Type, true
		}
	}
	c.errf(n.Line, "no member %q in %s", n.Name, st.String())
	return IntType(), false
}

// findStructMember resolves the *Member of a MemberExpr without reporting
// errors (used by the address-of check to reject bit-field members, and by
// brace-initialisation to reject initialising bit-fields).
func (c *checker) findStructMember(n *MemberExpr, fn *FuncDecl) *Member {
	bt := c.checkExpr(n.Base, fn)
	var st *Type
	if n.Arrow {
		if !bt.IsPtr() || bt.Elem == nil {
			return nil
		}
		st = bt.Elem
	} else {
		st = bt
	}
	if st == nil || (st.Kind != KStruct && st.Kind != KUnion) {
		return nil
	}
	for _, m := range st.Members {
		if m.Name == n.Name {
			return m
		}
	}
	return nil
}

func (c *checker) checkExpr(e Expr, fn *FuncDecl) *Type {
	switch n := e.(type) {
	case *NumLit:
		if n.Kind == TDouble {
			if n.IsFloat {
				return FloatType()
			}
			return DoubleType()
		}
		return IntType()
	case *StrLit:
		return PtrType(CharType()) // string literal decays to char*
	case *Ident:
		t := c.lookup(n.Name)
		if t == nil {
			// Functions live in their own namespace, not in the variable
			// scopes: a bare function name used as an expression is valid even
			// though no variable declaration provides a type for it.
			if ft := c.funcTypeByName(n.Name); ft != nil {
				t = ft
			} else if _, ok := enumConsts[n.Name]; ok {
				// Enumerators are integer constants: usable as rvalues.
				return IntType()
			} else {
				c.errf(n.Line, "undeclared identifier %q", n.Name)
				return IntType()
			}
		}
		if t.IsArray() {
			return PtrType(t.Elem) // array decays to pointer
		}
		if t.IsFunc() {
			// A function designator used as a value decays to a pointer to
			// that function, exactly like an array: "fp = add;".
			return PtrType(t)
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
			t := c.checkExpr(n.E, fn)
			if !t.IsScalar() {
				c.errf(0, "operand of '!' must be scalar, got %s", t)
			}
			return IntType()
		case "~":
			// Bitwise complement: integer operands only, and the operand
			// promotes to int exactly like the binary bitwise operators do.
			t := c.checkExpr(n.E, fn)
			if !t.IsIntClass() {
				c.errf(0, "operand of '~' must be an integer, got %s", t)
			}
			return IntType()
		case "&":
			// Taking the address of a function designator is how a function
			// pointer is initialised ("fp = &add"). The designator is not an
			// lvalue, so it must be resolved before the lvalue check.
			if id, ok := n.E.(*Ident); ok {
				if ft := c.funcTypeByName(id.Name); ft != nil {
					return PtrType(ft)
				}
				if t := c.lookup(id.Name); t != nil && t.IsArray() {
					// &array yields a pointer to the whole array
					// (int (*)[N]), NOT a decayed pointer to element 0. Arrays
					// are lvalues for the purpose of taking their address even
					// though they are not modifiable lvalues.
					return PtrType(t)
				}
			}
			// A bit-field member has no address of its own: "&s.bf" is an
			// error in C.
			if me, ok := n.E.(*MemberExpr); ok {
				if m := c.findStructMember(me, fn); m != nil && m.BitWidth > 0 {
					c.errf(me.Line, "cannot take address of bit-field member %q", me.Name)
					return IntType()
				}
			}
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
			// *fp on a pointer to function names the function itself, which
			// decays straight back to the pointer to it.
			if t.Elem.IsFunc() {
				return PtrType(t.Elem)
			}
			return t.Elem
		}
	case *Binary:
		return c.checkBinary(n, fn)
	case *Call:
		return c.checkCall(n, fn)
	case *IndirectCall:
		return c.checkIndirectCall(n, fn)
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
	case *CondExpr:
		c.checkExpr(n.Cond, fn)
		t := c.checkExpr(n.Then, fn)
		c.checkExpr(n.Else, fn)
		return t
	case *CommaExpr:
		// Evaluate the left operand (for its side effects / errors) and keep
		// the type of the right one as the result, exactly like C.
		c.checkExpr(n.Left, fn)
		return c.checkExpr(n.Right, fn)
	case *CastExpr:
		c.checkExpr(n.E, fn)
		return n.Typ
	case *IncDecExpr:
		// ++/-- operate in place, so the operand must be a modifiable lvalue
		// (this also rejects const-typed and bit-field operands through
		// checkLValue).
		lt, ok := c.checkLValue(n.E, fn)
		if !ok {
			return IntType()
		}
		if !lt.IsArith() && !lt.IsPtr() && !lt.IsBool() {
			c.errf(0, "operand of %s must be arithmetic or pointer, got %s", n.Op, lt)
		}
		return lt
	case *MemberExpr:
		_, ok := c.checkMemberLValue(n, fn)
		if !ok {
			return IntType()
		}
		// Re-resolve the member type (checkMemberLValue already validated it).
		bt := c.checkExpr(n.Base, fn)
		st := bt
		if n.Arrow {
			st = bt.Elem
		}
		for _, m := range st.Members {
			if m.Name == n.Name {
				// An array member used as a value decays to a pointer to its
				// element 0, exactly like a bare array identifier ("char *q =
				// s.name" must type-check the same way "char *q = s" does).
				if m.Type.IsArray() && m.Type.Elem != nil {
					return PtrType(m.Type.Elem)
				}
				return m.Type
			}
		}
		return IntType()
	case *SizeofExpr:
		return IntType()
	case *AssignExpr:
		// Assignment "a = b" (statement-level or in expression position) must
		// have a modifiable lvalue on the left. Statement-level assignments are
		// parsed as ExprStmt{AssignExpr}, so the lvalue check must live here,
		// not in checkStmt (whose *AssignStmt branch is never produced by the
		// parser).
		lt, ok := c.checkLValue(n.Lhs, fn)
		if !ok {
			return IntType()
		}
		rt := c.checkExpr(n.Rhs, fn)
		// Writing through a void* yields a void location; any value may be
		// stored there (the checker models it as "we do not know the element
		// type"), so skip the assignability check for that case.
		if !lt.IsVoid() && !assignable(lt, rt) {
			c.errf(0, "cannot assign %s to %s", rt, lt)
		}
		return rt
	case *VaArgExpr:
		ap := c.checkExpr(n.Ap, fn)
		if !ap.IsPtr() {
			c.errf(0, "va_arg first argument must be a va_list, got %s", ap)
		}
		return n.Typ
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
		// Usual arithmetic conversions: the wider floating type wins, so
		// float+double is double while float+float stays float. Either way the
		// value is carried in an XMM register; only its 4/8-byte storage
		// footprint differs.
		if lt.IsFloating() || rt.IsFloating() {
			if n.Op == "%" {
				c.errf(0, "operator '%%' requires integer operands, got %s and %s", lt, rt)
			}
			if lt.Kind == KDouble || rt.Kind == KDouble {
				return DoubleType()
			}
			return FloatType()
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
	case "<<", ">>", "&", "|", "^":
		if !(lt.IsIntClass() && rt.IsIntClass()) {
			c.errf(0, "operator %q requires integer operands, got %s and %s", n.Op, lt, rt)
		}
		return IntType()
	}
	return IntType()
}

func (c *checker) checkCall(n *Call, fn *FuncDecl) *Type {
	// print(...) is a compiler builtin: the checker sees every argument's
	// static type, so it builds the format string and lowers the call to
	// printf. A user-declared print function keeps precedence.
	if n.Name == "print" {
		if _, user := c.funcs["print"]; !user {
			return c.rewritePrint(n, fn)
		}
	}
	if fd, ok := c.funcs[n.Name]; ok {
		return c.checkArgs(n.Name, fd.ParamTypes, fd.Variadic, n.Args, fn, fd.Ret)
	}
	if pd, ok := c.protos[n.Name]; ok {
		return c.checkArgs(n.Name, pd.ParamTypes, pd.Variadic, n.Args, fn, pd.Ret)
	}
	// The name may designate a VARIABLE holding a function pointer: C allows
	// "fp(x)" exactly like "(*fp)(x)", and goc parses both against identifier
	// callees. Function names and variables are separate namespaces, so this
	// is unambiguous once the function tables above have been consulted.
	if ft := funcTypeOf(c.lookup(n.Name)); ft != nil {
		return c.checkArgs(n.Name, ft.Params, ft.Variadic, n.Args, fn, ft.Ret)
	}
	// External / goclib call whose signature we do not model: accept it and
	// assume an int result (true for every goclib function goc exposes).
	return IntType()
}

// funcTypeOf returns the function type behind t: KFunc as it stands, or the
// pointee of a pointer to function -- the only shape a function type can take
// once it is stored in a variable, parameter, struct member or return value.
func funcTypeOf(t *Type) *Type {
	if t == nil {
		return nil
	}
	if t.IsFunc() {
		return t
	}
	if t.IsPtr() && t.Elem != nil && t.Elem.IsFunc() {
		return t.Elem
	}
	return nil
}

// checkArgs validates a resolved argument list against a parameter list. It
// reports arity and argument-type mismatches and returns the call result type.
// A variadic signature requires at least its named parameters.
func (c *checker) checkArgs(what string, params []*Type, variadic bool, args []Expr, fn *FuncDecl, ret *Type) *Type {
	if variadic {
		if len(args) < len(params) {
			c.errf(0, "call to %q: expected at least %d arguments, got %d", what, len(params), len(args))
		}
		for i, a := range args {
			if i >= len(params) {
				break // trailing variadic arguments are not type-checked here
			}
			at := c.checkExpr(a, fn)
			if !assignable(params[i], at) {
				c.errf(0, "call to %q: argument %d has type %s, expected %s", what, i+1, at, params[i])
			}
		}
		return ret
	}
	if len(args) != len(params) {
		c.errf(0, "call to %q: expected %d arguments, got %d", what, len(params), len(args))
		return ret
	}
	for i, a := range args {
		at := c.checkExpr(a, fn)
		if !assignable(params[i], at) {
			c.errf(0, "call to %q: argument %d has type %s, expected %s",
				what, i+1, at, params[i])
		}
	}
	return ret
}

// funcTypeByName returns the type of a declared function (a definition or a
// prototype), or nil when the name is not one. Functions live in their own
// namespace, separate from variables.
func (c *checker) funcTypeByName(name string) *Type {
	var fd *FuncDecl
	if f, ok := c.funcs[name]; ok {
		fd = f
	} else if p, ok := c.protos[name]; ok {
		fd = p
	}
	if fd == nil {
		return nil
	}
	ft := FuncType(fd.Ret, fd.ParamTypes)
	ft.Variadic = fd.Variadic
	return ft
}

// checkIndirectCall validates a call through a computed function address.
func (c *checker) checkIndirectCall(n *IndirectCall, fn *FuncDecl) *Type {
	// A member call x.f(args) may be a method on x's struct type (UFCS).
	// Resolved calls carry the rewritten direct form in n.UFCS; everything
	// else falls through to the ordinary indirect-call rules.
	if t, ok := c.tryUFCS(n, fn); ok {
		return t
	}
	ft := funcTypeOf(c.checkExpr(n.Fn, fn))
	if ft == nil {
		c.errf(0, "called expression (%T) is not a function pointer", n.Fn)
		for _, a := range n.Args {
			c.checkExpr(a, fn) // still walk the arguments for their own errors
		}
		return IntType()
	}
	return c.checkArgs(ft.String(), ft.Params, ft.Variadic, n.Args, fn, ft.Ret)
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
	case dst.IsBool() && src.IsIntClass():
		return true // int -> bool conversion (0/1)
	case dst.IsIntClass() && src.IsBool():
		return true // bool -> int conversion
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
	case dst.IsStruct() && src.IsStruct():
		return typesEqual(dst, src) // whole-struct assignment
	case dst.IsUnion() && src.IsUnion():
		return typesEqual(dst, src)
	}
	return false
}

// checkBraceInit validates a braced initialiser against type t. An incomplete
// array ("int a[] = {...}") borrows its length from the number of top-level
// elements; every byte not explicitly initialised is zero (C semantics).
func (c *checker) checkBraceInit(t *Type, bi *BraceInit, fn *FuncDecl, line int) {
	switch {
	case t.IsArray():
		if t.Len == 0 {
			if len(bi.Elems) == 0 {
				c.errf(line, "empty initialiser for array of incomplete length")
				return
			}
			t.Len = len(bi.Elems)
		}
		if len(bi.Elems) > t.Len {
			c.errf(line, "too many initialisers for array of %d element(s)", t.Len)
		}
		for i, el := range bi.Elems {
			if el.Desig != "" {
				c.errf(line, "member designator %q is only valid in a struct/union initialiser", el.Desig)
				continue
			}
			if i >= t.Len {
				break
			}
			c.checkBraceElem(t.Elem, el.E, fn, line)
		}
	case t.IsStruct():
		c.checkStructBrace(t, bi, fn, line)
	case t.IsUnion():
		if len(bi.Elems) > 1 {
			c.errf(line, "too many initialisers for union (at most one member)")
		}
		if len(bi.Elems) == 0 {
			return
		}
		el := bi.Elems[0]
		mi := 0
		if el.Desig != "" {
			mi = memberIndex(t, el.Desig)
			if mi < 0 {
				c.errf(line, "union has no member %q", el.Desig)
				return
			}
		}
		if mi < len(t.Members) {
			if t.Members[mi].BitWidth > 0 {
				c.errf(line, "cannot brace-initialise bit-field member %q", t.Members[mi].Name)
				return
			}
			c.checkBraceElem(t.Members[mi].Type, el.E, fn, line)
		}
	default:
		// C allows a scalar to be initialised from a single braced value.
		if len(bi.Elems) != 1 || bi.Elems[0].Desig != "" {
			c.errf(line, "invalid initialiser for scalar type %s", t)
			return
		}
		c.checkBraceElem(t, bi.Elems[0].E, fn, line)
	}
}

// checkStructBrace validates a struct initialiser, either all-positional or
// all-designated (".x = ..."); mixing the two styles is rejected.
func (c *checker) checkStructBrace(t *Type, bi *BraceInit, fn *FuncDecl, line int) {
	allDesig := true
	for _, el := range bi.Elems {
		if el.Desig == "" {
			allDesig = false
			break
		}
	}
	if allDesig {
		for _, el := range bi.Elems {
			mi := memberIndex(t, el.Desig)
			if mi < 0 {
				c.errf(line, "struct has no member %q", el.Desig)
				continue
			}
			if t.Members[mi].BitWidth > 0 {
				c.errf(line, "cannot brace-initialise bit-field member %q", el.Desig)
				continue
			}
			c.checkBraceElem(t.Members[mi].Type, el.E, fn, line)
		}
		return
	}
	for i, el := range bi.Elems {
		if i >= len(t.Members) {
			c.errf(line, "too many initialisers for struct %s", t)
			break
		}
		if el.Desig != "" {
			// C99 allows continuing after a designator positionally, but
			// implementing that partial-order rule on top of the positional
			// walk would silently overwrite; reject the mix instead.
			c.errf(line, "cannot mix positional and designated (\".%s =\") initialisers", el.Desig)
			return
		}
		if t.Members[i].BitWidth > 0 {
			c.errf(line, "cannot brace-initialise bit-field member %q", t.Members[i].Name)
			continue
		}
		c.checkBraceElem(t.Members[i].Type, el.E, fn, line)
	}
}

// checkBraceElem validates one element of a braced initialiser: either a
// nested brace for an aggregate target, a string for a char array (or char*),
// or an ordinary expression checked through the normal expression path.
func (c *checker) checkBraceElem(t *Type, e Expr, fn *FuncDecl, line int) {
	if nbi, ok := e.(*BraceInit); ok {
		if t.IsArray() || t.IsStruct() || t.IsUnion() {
			c.checkBraceInit(t, nbi, fn, line)
			return
		}
		c.errf(line, "braced initialiser for scalar type %s", t)
		return
	}
	if sl, ok := e.(*StrLit); ok {
		if t.IsArray() && t.Elem.IsChar() {
			if t.Len != 0 && t.Len < len(sl.Bytes)+1 {
				c.errf(line, "string of length %d does not fit in char array of %d bytes", len(sl.Bytes), t.Len)
			}
			return
		}
		if t.IsPtr() && t.Elem.IsChar() {
			return // a char* member/element may hold a string literal
		}
		c.errf(line, "string literal cannot initialise %s", t)
		return
	}
	tt := c.checkExpr(e, fn)
	if !assignable(t, tt) {
		c.errf(line, "initialiser element of type %s is not assignable to %s", tt, t)
	}
}

// memberIndex returns the index of the named member, or -1.
func memberIndex(t *Type, name string) int {
	for i, m := range t.Members {
		if m.Name == name {
			return i
		}
	}
	return -1
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
	case KStruct, KUnion:
		// Named structs/unions compare by tag; anonymous ones compare
		// member-for-member.
		if a.Tag != "" || b.Tag != "" {
			return a.Tag == b.Tag
		}
		if len(a.Members) != len(b.Members) {
			return false
		}
		for i := range a.Members {
			if a.Members[i].Name != b.Members[i].Name {
				return false
			}
			if !typesEqual(a.Members[i].Type, b.Members[i].Type) {
				return false
			}
		}
		return true
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
