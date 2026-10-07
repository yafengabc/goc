package gocl

import "goc/frontend"

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
func llvmEligible(f *frontend.FuncDecl) bool {
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
func usesUnsupportedLLVM(s frontend.Stmt) bool {
	found := false
	frontend.WalkStmts(s, func(x frontend.Stmt) {
		switch n := x.(type) {
		case *frontend.AsmStmt:
			found = true
		case *frontend.DeclStmt:
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
	frontend.WalkStmts(s, func(x frontend.Stmt) {
		walkExprsIn(x, func(e frontend.Expr) {
			switch n := e.(type) {
			case *frontend.MemberExpr:
				if memberIsBitField(n) {
					ok = false
				}
			case *frontend.DeclStmt:
				if n.Typ != nil && typUnsupported(n.Typ) {
					ok = false
				}
			case *frontend.CastExpr:
				if n.Typ != nil && typUnsupported(n.Typ) {
					ok = false
				}
			case *frontend.SizeofExpr:
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
func memberIsBitField(n *frontend.MemberExpr) bool {
	for _, t := range frontend.Structs() {
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
func typUnsupported(t *frontend.Type) bool {
	if t == nil {
		return false
	}
	if t.Kind == frontend.KBitInt {
		return true
	}
	if t.Kind == frontend.KArr {
		return typUnsupported(t.Elem)
	}
	if t.Kind == frontend.KStruct || t.Kind == frontend.KUnion {
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
func walkExprsIn(s frontend.Stmt, fn func(frontend.Expr)) {
	frontend.WalkStmts(s, func(x frontend.Stmt) {
		for _, e := range stmtExprs(x) {
			walkExpr(e, fn)
		}
	})
}

// stmtExprs returns the expressions a statement directly contains.
func stmtExprs(s frontend.Stmt) []frontend.Expr {
	switch n := s.(type) {
	case *frontend.DeclStmt:
		return []frontend.Expr{n.Init}
	case *frontend.AssignStmt:
		return []frontend.Expr{n.Lhs, n.Rhs}
	case *frontend.ExprStmt:
		return []frontend.Expr{n.E}
	case *frontend.ReturnStmt:
		return []frontend.Expr{n.E}
	case *frontend.IfStmt:
		return []frontend.Expr{n.Cond}
	case *frontend.WhileStmt:
		return []frontend.Expr{n.Cond}
	case *frontend.DoWhileStmt:
		return []frontend.Expr{n.Cond}
	case *frontend.SwitchStmt:
		return []frontend.Expr{n.Src}
	case *frontend.ForStmt:
		out := []frontend.Expr{n.Cond, n.Post}
		if e := firstExprOf(n.Init); e != nil {
			out = append(out, e)
		}
		return out
	}
	return nil
}

func firstExprOf(s frontend.Stmt) frontend.Expr {
	if s == nil {
		return nil
	}
	if es := stmtExprs(s); len(es) > 0 {
		return es[0]
	}
	return nil
}

// walkExpr calls fn for e and every sub-expression.
func walkExpr(e frontend.Expr, fn func(frontend.Expr)) {
	if e == nil {
		return
	}
	fn(e)
	switch n := e.(type) {
	case *frontend.Call:
		for _, a := range n.Args {
			walkExpr(a, fn)
		}
	case *frontend.IndirectCall:
		walkExpr(n.Fn, fn)
		for _, a := range n.Args {
			walkExpr(a, fn)
		}
	case *frontend.Unary:
		walkExpr(n.E, fn)
	case *frontend.Binary:
		walkExpr(n.L, fn)
		walkExpr(n.R, fn)
	case *frontend.AssignExpr:
		walkExpr(n.Lhs, fn)
		walkExpr(n.Rhs, fn)
	case *frontend.CommaExpr:
		walkExpr(n.Left, fn)
		walkExpr(n.Right, fn)
	case *frontend.Index:
		walkExpr(n.Base, fn)
		walkExpr(n.Idx, fn)
	case *frontend.CondExpr:
		walkExpr(n.Cond, fn)
		walkExpr(n.Then, fn)
		walkExpr(n.Else, fn)
	case *frontend.CastExpr:
		walkExpr(n.E, fn)
	case *frontend.IncDecExpr:
		walkExpr(n.E, fn)
	case *frontend.MemberExpr:
		walkExpr(n.Base, fn)
	case *frontend.VaArgExpr:
		walkExpr(n.Ap, fn)
	case *frontend.GenericExpr:
		// _Generic resolves at check time, so only the chosen association is
		// ever emitted. Walking it matters all the same: the reachability pass
		// in translate.go discovers the runtime functions a program needs by
		// walking its expressions, and <stdbit.h> reaches every one of its
		// stdc_*_N entry points through _Generic. Skipping the branch made
		// markReachable miss them, so their definitions were pruned from the
		// object and the link failed with "undefined symbol" for a function
		// that is right there in stdbit.c. Only Chosen is walked -- the
		// unselected associations never reach codegen, and marking them
		// would keep five dead functions alive per _Generic.
		walkExpr(n.Chosen, fn)
	case *frontend.CompoundLit:
		if n.Init != nil {
			for _, el := range n.Init.Elems {
				walkExpr(el.E, fn)
			}
		}
	case *frontend.SizeofExpr:
		walkExpr(n.E, fn)
	}
}
