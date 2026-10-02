package main

// ptrcap.go -- T1.6 C4: ptrCapable analysis.
//
// An int-typed local or parameter can legally hold a 64-bit pointer value
// ("int s = strchr(hay, 'e'); ... s[1]") -- C permits storing a pointer in an
// integer variable, and loadVar keeps the full 64 bits in the 8-byte slot.
// After the int-carry-model change the narrow consumption sites (N5b mixed
// compares, N11 indexing, N12 64-bit arithmetic, N17 widening stores/casts,
// N22 int->double) sign-extend a materialized int with movsxd; for a variable
// that actually holds a pointer that sign-extension TRUNCATES the value (the
// high 32 bits are meaningful pointer bits, not the materialized int's zero
// padding). This pass conservatively marks every int-typed local/parameter
// that may hold a pointer; the narrow sites then skip the movsxd for marked
// values.
//
// Rule granularity is the variable NAME (per function), matching how
// declareVar consults the flag. A missed mark truncates a pointer in a
// pathological program (semantics implementation-defined anyway); a false
// mark would corrupt ordinary negative ints, so the rules only fire on a
// direct static pointer-type contact -- assignment/initialisation from a
// pointer-typed expression, passing to a pointer parameter, returning from a
// pointer-returning function, comparing with a pointer, or being used as a
// subscript/dereference/-> base.

func isIntVar(t *Type) bool {
	return t != nil && t.Kind == KInt && t.Width == 4
}

// isPtrLike reports whether t is a pointer-valued type: KPtr, a function
// designator (decays to KPtr), or an array (decays to a pointer in value
// contexts). String literals carry PtrType(CharType()) so they already hit
// KPtr.
func isPtrLike(t *Type) bool {
	return t != nil && (t.Kind == KPtr || t.IsFunc() || t.IsArray())
}

func isCmpOp(op string) bool {
	switch op {
	case "<", ">", "<=", ">=", "==", "!=":
		return true
	}
	return false
}

// isPlainIntSrc reports whether a value of static type t is an ordinary
// integer that is NOT a pointer candidate. Storing such a value into a
// marked variable means the variable no longer carries a pointer, so the
// mark must be dropped -- otherwise the narrow sites keep skipping the
// movsxd and an ordinary negative int reads back as a huge positive
// (e.g. "int v = \"abc\"; v = -1; long p = (long)v;" produced 4294967295).
//
// A nil (undeterminable) type counts as a plain int: an unresolvable source
// is far more likely an ordinary integer than a pointer, and the asymmetry
// of this analysis makes a false mark much more damaging than a missed one.
func isPlainIntSrc(t *Type) bool {
	if t == nil {
		return true
	}
	if isPtrLike(t) {
		return false
	}
	return t.IsIntClass()
}

// analyzePtrCapable walks the current function's AST and returns the set of
// int-typed variable names that may hold a 64-bit pointer. It runs once per
// function in genFunc, before any declareVar consults the flag. Types come
// from a local name->type table (parameters first, then every local
// declaration collected in walk order, later declarations shadowing earlier
// ones) plus the global/function tables -- CG.exprType cannot be used here
// because the emission scopes are not built yet.
func (c *CG) analyzePtrCapable(f *FuncDecl) map[string]bool {
	pc := map[string]bool{}
	// dirty records the variables that also receive an ordinary integer
	// value. It is a separate set (rather than deleting from pc during the
	// walk) so the result does not depend on traversal order: a variable can
	// be seeded from a pointer early and reassigned from an int later, or the
	// other way round.
	dirty := map[string]bool{}
	vt := map[string]*Type{}
	for i, p := range f.Params {
		if i < len(f.ParamTypes) {
			vt[p] = f.ParamTypes[i]
		}
	}
	// Collect every local declaration (nested blocks included; a later
	// declaration with the same name shadows for the expression walk).
	var collect func(Stmt)
	collect = func(s Stmt) {
		switch n := s.(type) {
		case *Block:
			for _, st := range n.Stmts {
				collect(st)
			}
		case *DeclList:
			for _, d := range n.Decls {
				collect(d)
			}
		case *DeclStmt:
			if n.Storage == "" || n.Storage == "auto" || n.Storage == "register" {
				vt[n.Name] = n.Typ
			}
		case *IfStmt:
			collect(n.Then)
			if n.Else != nil {
				collect(n.Else)
			}
		case *WhileStmt:
			collect(n.Body)
		case *DoWhileStmt:
			collect(n.Body)
		case *ForStmt:
			if n.Init != nil {
				collect(n.Init)
			}
			collect(n.Body)
		case *SwitchStmt:
			collect(n.Body)
		case *LabelStmt:
			collect(n.Stmt)
		}
	}
	for _, st := range f.Body.Stmts {
		collect(st)
	}

	// pcTypeOf is the analysis' own static type lookup. It mirrors the shapes
	// CG.exprType handles, but resolves Ident through vt. nil means "cannot
	// determine" -- the rules treat nil as non-pointer (a missed mark is
	// acceptable for pathological programs).
	var pcTypeOf func(Expr) *Type
	pcTypeOf = func(e Expr) *Type {
		switch n := e.(type) {
		case *Ident:
			if t, ok := vt[n.Name]; ok {
				return t
			}
			if t, ok := c.globalTyp[n.Name]; ok {
				return t
			}
			if fd, ok := c.funcDefs[n.Name]; ok {
				return PtrType(FuncType(fd.Ret, fd.ParamTypes))
			}
			return nil
		case *StrLit:
			return PtrType(CharType())
		case *NumLit:
			return IntType()
		case *Unary:
			switch n.Op {
			case "&":
				if t := pcTypeOf(n.E); t != nil {
					if t.IsFunc() {
						return PtrType(t)
					}
					if t.IsPtr() && t.Elem != nil && t.Elem.IsFunc() {
						return t
					}
					return PtrType(t)
				}
				return nil
			case "*":
				if t := pcTypeOf(n.E); t != nil && t.IsPtr() {
					return t.Elem
				}
				return nil
			case "-", "~":
				return pcTypeOf(n.E)
			case "!":
				return IntType()
			}
			return nil
		case *CastExpr:
			if n.Typ != nil {
				return n.Typ
			}
			return nil
		case *Index:
			if t := pcTypeOf(n.Base); t != nil && (t.IsPtr() || t.IsArray()) {
				return t.Elem
			}
			return nil
		case *Binary:
			lt, rt := pcTypeOf(n.L), pcTypeOf(n.R)
			if isPtrLike(lt) || isPtrLike(rt) {
				if n.Op == "+" || n.Op == "-" {
					if isPtrLike(lt) {
						return lt
					}
					return rt
				}
				// comparisons and non-arithmetic pointer ops yield int
				return IntType()
			}
			return nil // pure arithmetic: not pointer-typed
		case *MemberExpr:
			if n.Arrow {
				if t := pcTypeOf(n.Base); t != nil && t.IsPtr() && t.Elem != nil {
					return t.Elem
				}
				return nil
			}
			// x.member on a struct value: member offset lookup is not worth the
			// machinery here; nil is conservative (missed marks are acceptable).
			return nil
		case *Call:
			if fd, ok := c.funcDefs[n.Name]; ok {
				return fd.Ret
			}
			return nil
		case *CondExpr:
			if t := pcTypeOf(n.Then); t != nil {
				return t
			}
			return pcTypeOf(n.Else)
		case *CommaExpr:
			return pcTypeOf(n.Right)
		case *CompoundLit:
			return n.Typ
		case *VaArgExpr:
			if n.Typ != nil {
				return n.Typ
			}
			return nil
		case *SizeofExpr:
			return IntType()
		case *GenericExpr:
			if n.Chosen != nil {
				return pcTypeOf(n.Chosen)
			}
			return nil
		case *TmpLoad:
			return n.Typ
		}
		return nil
	}

	// markVar marks e as ptrCapable when e is an int-typed variable reference.
	var markVar func(Expr)
	markVar = func(e Expr) {
		if id, ok := e.(*Ident); ok {
			if t, ok := vt[id.Name]; ok && isIntVar(t) {
				pc[id.Name] = true
			}
		}
	}

	// walkExpr applies the expression-level rules and recurses.
	var walkExpr func(Expr)
	walkExpr = func(e Expr) {
		switch n := e.(type) {
		case *Unary:
			if n.Op == "*" {
				markVar(n.E) // rule 6: dereferenced as a pointer
			}
			walkExpr(n.E)
		case *Binary:
			if isCmpOp(n.Op) {
				lt, rt := pcTypeOf(n.L), pcTypeOf(n.R)
				if isPtrLike(lt) {
					markVar(n.R) // rule 5: compared with a pointer
				}
				if isPtrLike(rt) {
					markVar(n.L)
				}
			}
			walkExpr(n.L)
			walkExpr(n.R)
		case *AssignExpr:
			// plain is false for a compound assignment ("v += 1"): that is
			// pointer arithmetic on an existing pointer value, so it must NOT
			// clear the mark even though the right operand is a plain int.
			apply := func(lhs, rhs Expr, plain bool) {
				if isPtrLike(pcTypeOf(lhs)) {
					markVar(rhs) // rule 4: stored into a pointer lvalue
				}
				if id, ok := lhs.(*Ident); ok {
					if t, ok := vt[id.Name]; ok && isIntVar(t) {
						if isPtrLike(pcTypeOf(rhs)) {
							pc[id.Name] = true // rule 1: pointer value stored into an int var
						} else if plain && isPlainIntSrc(pcTypeOf(rhs)) {
							dirty[id.Name] = true
						}
					}
				}
			}
			apply(n.Lhs, n.Rhs, n.Op == "")
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
		case *CastExpr:
			walkExpr(n.E)
		case *Index:
			markVar(n.Base) // rule 6: used as a subscript base
			walkExpr(n.Base)
			walkExpr(n.Idx)
		case *MemberExpr:
			if n.Arrow {
				markVar(n.Base) // rule 6: -> base
			}
			walkExpr(n.Base)
		case *Call:
			if fd, ok := c.funcDefs[n.Name]; ok {
				for i, a := range n.Args {
					if i < len(fd.ParamTypes) && isPtrLike(fd.ParamTypes[i]) {
						markVar(a) // rule 2: passed to a pointer parameter
					}
				}
			}
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *IndirectCall:
			if n.UFCS != nil {
				walkExpr(n.UFCS)
			}
			walkExpr(n.Fn)
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *CondExpr:
			walkExpr(n.Cond)
			walkExpr(n.Then)
			walkExpr(n.Else)
		case *IncDecExpr:
			walkExpr(n.E)
		case *CommaExpr:
			walkExpr(n.Left)
			walkExpr(n.Right)
		case *VaArgExpr:
			walkExpr(n.Ap)
		case *SizeofExpr:
			if n.E != nil {
				walkExpr(n.E)
			}
		case *GenericExpr:
			walkExpr(n.Control)
			for _, a := range n.Assocs {
				walkExpr(a.E)
			}
		}
	}

	// walk applies the statement-level rules (initialisers, assignments,
	// returns) and descends into blocks/loops.
	var walk func(Stmt)
	walk = func(s Stmt) {
		switch n := s.(type) {
		case *Block:
			for _, st := range n.Stmts {
				walk(st)
			}
		case *DeclList:
			for _, d := range n.Decls {
				walk(d)
			}
		case *DeclStmt:
			if n.Init != nil {
				if t, ok := vt[n.Name]; ok && isIntVar(t) {
					if isPtrLike(pcTypeOf(n.Init)) {
						pc[n.Name] = true // rule 1: initialised from a pointer expression
					} else if isPlainIntSrc(pcTypeOf(n.Init)) {
						dirty[n.Name] = true
					}
				}
				walkExpr(n.Init)
			}
		case *AssignStmt:
			if isPtrLike(pcTypeOf(n.Lhs)) {
				markVar(n.Rhs) // rule 4: stored into a pointer lvalue
			}
			if id, ok := n.Lhs.(*Ident); ok {
				if t, ok := vt[id.Name]; ok && isIntVar(t) {
					if isPtrLike(pcTypeOf(n.Rhs)) {
						pc[id.Name] = true // rule 1
					} else if isPlainIntSrc(pcTypeOf(n.Rhs)) {
						dirty[id.Name] = true
					}
				}
			}
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
		case *ExprStmt:
			walkExpr(n.E)
		case *ReturnStmt:
			if n.E != nil {
				if isPtrLike(f.Ret) {
					markVar(n.E) // rule 3: returned from a pointer-returning function
				}
				walkExpr(n.E)
			}
		case *IfStmt:
			walkExpr(n.Cond)
			walk(n.Then)
			if n.Else != nil {
				walk(n.Else)
			}
		case *WhileStmt:
			walkExpr(n.Cond)
			walk(n.Body)
		case *DoWhileStmt:
			walk(n.Body)
			walkExpr(n.Cond)
		case *ForStmt:
			if n.Init != nil {
				walk(n.Init)
			}
			if n.Cond != nil {
				walkExpr(n.Cond)
			}
			if n.Post != nil {
				walkExpr(n.Post)
			}
			walk(n.Body)
		case *SwitchStmt:
			walkExpr(n.Src)
			walk(n.Body)
		case *LabelStmt:
			walk(n.Stmt)
		}
	}
	for _, st := range f.Body.Stmts {
		walk(st)
	}
	// A variable that also receives an ordinary integer value is not
	// pointer-carrying: drop the mark so the narrow sites keep sign-extending
	// its materialized int.
	for n := range dirty {
		delete(pc, n)
	}
	return pc
}
