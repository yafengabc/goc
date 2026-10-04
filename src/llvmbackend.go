package main

// Driving the LLVM backend: deciding what goes down the IR path, and linking it
// into one executable.
//
// One owner, not two. Every C function the program contains -- the user's and
// the runtime's alike -- is lowered to LLVM IR and compiled by libLLVM; goa's
// assembler contributes only the entry stub (which is not C) and links the
// resulting object into the image.
//
// llvmEligible decides which functions the IR front end can model; anything it
// rules out (inline assembly, bit-fields, _BitInt) is reported so the caller can
// fall back to the default goa code generator for that program.

// llvmEligible reports whether a function can be generated as IR.
//
// The exclusions are the constructs the IR front end does not model yet, not
// judgements about which generator produces better code. Bit-fields and C23
// _BitInt are the two: LLVM has no direct model for either, and goc's own
// generator already implements both, so sending those functions there is both
// simpler and more faithful.
func llvmEligible(f *FuncDecl) bool {
	if f == nil || f.Body == nil {
		return false
	}
	// A parameter or return type the front end cannot represent rules the whole
	// function out, whatever the body looks like.
	if typUnsupported(f.Ret) {
		return false
	}
	for _, pt := range f.ParamTypes {
		if typUnsupported(pt) {
			return false
		}
	}
	// A variadic function is now supported: the signature carries the ellipsis
	// and va_start / va_arg / va_end lower to LLVM's own intrinsics, which is
	// the only thing that knows the x86-64 register save area layout.
	return !usesUnsupportedLLVM(f.Body)
}

// usesUnsupportedLLVM walks a body for the constructs the front end skips.
func usesUnsupportedLLVM(s Stmt) bool {
	found := false
	walkStmts(s, func(x Stmt) {
		switch n := x.(type) {
		case *AsmStmt:
			found = true
		case *DeclStmt:
			if n.Typ != nil && typUnsupported(n.Typ) {
				found = true
			}
		}
	})
	if found {
		return true
	}
	// A member access has to be resolved to find out whether it names a
	// bit-field, and the parser does not record that on the node -- only the
	// struct type knows. The checker is not run here, so the walk looks the
	// member up by name in whatever struct type the base expression has.
	ok := true
	walkStmts(s, func(x Stmt) {
		walkExprsIn(x, func(e Expr) {
			switch n := e.(type) {
			case *MemberExpr:
				if memberIsBitField(n) {
					ok = false
				}
			case *DeclStmt:
				if n.Typ != nil && typUnsupported(n.Typ) {
					ok = false
				}
			case *CastExpr:
				if n.Typ != nil && typUnsupported(n.Typ) {
					ok = false
				}
			case *SizeofExpr:
				if n.Typ != nil && typUnsupported(n.Typ) {
					ok = false
				}
			}
		})
	})
	return !ok
}

// memberIsBitField reports whether a member access names a bit-field, which the
// IR front end cannot address: the value shares a storage unit with its
// neighbours and needs a read-modify-write the front end does not build.
//
// The parser does not record the resolved member on the node, and resolving it
// needs the struct type of the base expression -- which is available here,
// because eligibility is decided after the checker has run. Rather than rebuild
// that resolution, the search is by member name across the struct types the
// program declared: a name that is a bit-field anywhere is treated as one. That
// over-approximates, and deliberately so -- routing a function to the native
// generator costs a little speed, while sending a bit-field to a front end that
// cannot model it would compile it wrongly.
func memberIsBitField(n *MemberExpr) bool {
	for _, t := range structs {
		for _, m := range t.Members {
			if m.Name == n.Name && m.BitWidth > 0 {
				return true
			}
		}
	}
	return false
}

// typUnsupported reports whether a type has a feature the IR front end does not
// model yet.
func typUnsupported(t *Type) bool {
	if t == nil {
		return false
	}
	if t.Kind == KBitInt {
		return true
	}
	if t.Kind == KArr {
		return typUnsupported(t.Elem)
	}
	if t.Kind == KStruct || t.Kind == KUnion {
		for _, mem := range t.Members {
			if typUnsupported(mem.Type) {
				return true
			}
			// A bit-field has no representation in the IR front end's layout.
			if mem.BitWidth > 0 {
				return true
			}
		}
	}
	return false
}

// walkExprsIn calls fn for every expression reachable from a statement.
func walkExprsIn(s Stmt, fn func(Expr)) {
	walkStmts(s, func(x Stmt) {
		for _, e := range stmtExprs(x) {
			walkExpr(e, fn)
		}
	})
}

// stmtExprs returns the expressions a statement directly contains.
func stmtExprs(s Stmt) []Expr {
	switch n := s.(type) {
	case *DeclStmt:
		return []Expr{n.Init}
	case *AssignStmt:
		return []Expr{n.Lhs, n.Rhs}
	case *ExprStmt:
		return []Expr{n.E}
	case *ReturnStmt:
		return []Expr{n.E}
	case *IfStmt:
		return []Expr{n.Cond}
	case *WhileStmt:
		return []Expr{n.Cond}
	case *DoWhileStmt:
		return []Expr{n.Cond}
	case *SwitchStmt:
		return []Expr{n.Src}
	case *ForStmt:
		out := []Expr{n.Cond, n.Post}
		if e := firstExprOf(n.Init); e != nil {
			out = append(out, e)
		}
		return out
	}
	return nil
}

func firstExprOf(s Stmt) Expr {
	if s == nil {
		return nil
	}
	if es := stmtExprs(s); len(es) > 0 {
		return es[0]
	}
	return nil
}

// walkExpr calls fn for e and every sub-expression.
func walkExpr(e Expr, fn func(Expr)) {
	if e == nil {
		return
	}
	fn(e)
	switch n := e.(type) {
	case *Call:
		for _, a := range n.Args {
			walkExpr(a, fn)
		}
	case *IndirectCall:
		walkExpr(n.Fn, fn)
		for _, a := range n.Args {
			walkExpr(a, fn)
		}
	case *Unary:
		walkExpr(n.E, fn)
	case *Binary:
		walkExpr(n.L, fn)
		walkExpr(n.R, fn)
	case *AssignExpr:
		walkExpr(n.Lhs, fn)
		walkExpr(n.Rhs, fn)
	case *CommaExpr:
		walkExpr(n.Left, fn)
		walkExpr(n.Right, fn)
	case *Index:
		walkExpr(n.Base, fn)
		walkExpr(n.Idx, fn)
	case *CondExpr:
		walkExpr(n.Cond, fn)
		walkExpr(n.Then, fn)
		walkExpr(n.Else, fn)
	case *CastExpr:
		walkExpr(n.E, fn)
	case *IncDecExpr:
		walkExpr(n.E, fn)
	case *MemberExpr:
		walkExpr(n.Base, fn)
	case *VaArgExpr:
		walkExpr(n.Ap, fn)
	}
}
