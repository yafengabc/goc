package main

// typeResolver answers "what type is this expression" and "where does this
// name live", and it belongs to neither back end.
//
// The assembly generator and the LLVM IR translator both drive it, so the two
// never disagree about a program's meaning -- and, crucially, the IR translator
// needs no other handle on CG. When the resolver is serving the assembly path
// it holds a *CG and reads that generator's live tables (funcDefs, globalTyp,
// scopes, ...); when it is serving the IR path that back-pointer is nil and it
// uses tables built from the typed AST and the C runtime instead. One set of
// rules, no second opinion about the same program.
type typeResolver struct {
	cg *CG // nil for the IR path; for the assembly path, the owning generator

	// Tables used only when cg == nil (the IR path). When cg != nil the
	// resolver reads the equivalent maps off cg instead.
	funcDefs   map[string]*FuncDecl
	globalTyp  map[string]*Type
	staticVars map[string]string
	lib        *clibCProgram

	// Per-function emission scope state, used only when cg == nil.
	scopes  []map[string]int
	varEnts map[int]varInfo
	varUID  int
	declUID map[*DeclStmt]int
}

// --- dispatch helpers -------------------------------------------------------
// When cg is present the resolver reads the assembly generator's live tables;
// otherwise it reads the ones built for the IR path.

func (tr *typeResolver) funcDef(name string) (*FuncDecl, bool) {
	if tr.cg != nil {
		f, ok := tr.cg.funcDefs[name]
		return f, ok
	}
	f, ok := tr.funcDefs[name]
	return f, ok
}

func (tr *typeResolver) globalType(name string) (*Type, bool) {
	if tr.cg != nil {
		t, ok := tr.cg.globalTyp[name]
		return t, ok
	}
	t, ok := tr.globalTyp[name]
	return t, ok
}

func (tr *typeResolver) staticLabel(name string) (string, bool) {
	if tr.cg != nil {
		l, ok := tr.cg.staticVars[name]
		return l, ok
	}
	l, ok := tr.staticVars[name]
	return l, ok
}

func (tr *typeResolver) libFunc(name string) (*FuncDecl, bool) {
	if tr.cg != nil {
		if lib := clibCStore(tr.cg.linux); lib != nil {
			if f, ok := lib.funcs[name]; ok {
				return f, true
			}
			// The header prototypes count too: a Win32 entry point is declared
			// by a header and defined by the DLL, so it is never among the
			// library's own function definitions.
			for _, p := range lib.protos {
				if p.Name == name {
					return p, true
				}
			}
		}
		return nil, false
	}
	if tr.lib != nil {
		if f, ok := tr.lib.funcs[name]; ok {
			return f, true
		}
		// A Win32 entry point is declared by a header and defined by the DLL, so
		// it appears among the library's prototypes and never among its
		// functions. Missing those left every such call with no parameter types,
		// and calleeSig's fallback then typed them all as int -- which truncated
		// the HANDLE GetStdHandle returns to 32 bits and left WriteFile holding
		// a handle with no high half, so any program that printed anything died
		// on a write it should never have attempted.
		for _, p := range tr.lib.protos {
			if p.Name == name {
				return p, true
			}
		}
	}
	return nil, false
}

// --- scope state ------------------------------------------------------------

func (tr *typeResolver) pushScope() {
	if tr.cg != nil {
		tr.cg.pushScope()
		return
	}
	tr.scopes = append(tr.scopes, map[string]int{})
}

func (tr *typeResolver) popScope() {
	if tr.cg != nil {
		tr.cg.popScope()
		return
	}
	if len(tr.scopes) > 0 {
		tr.scopes = tr.scopes[:len(tr.scopes)-1]
	}
}

func (tr *typeResolver) declareVar(name string, info varInfo) int {
	if tr.cg != nil {
		return tr.cg.declareVar(name, info)
	}
	uid := tr.varUID
	tr.varUID++
	tr.varEnts[uid] = info
	tr.scopes[len(tr.scopes)-1][name] = uid
	return uid
}

func (tr *typeResolver) lookupVar(name string) (varInfo, bool) {
	if tr.cg != nil {
		return tr.cg.lookupVar(name)
	}
	for i := len(tr.scopes) - 1; i >= 0; i-- {
		if uid, ok := tr.scopes[i][name]; ok {
			return tr.varEnts[uid], true
		}
	}
	return varInfo{}, false
}

func (tr *typeResolver) lookupUID(name string) (int, bool) {
	if tr.cg != nil {
		// Mirror CG.lookupVar's scope walk, but return the uid rather than the
		// binding -- the assembly path keeps the uid table on the generator.
		for i := len(tr.cg.scopes) - 1; i >= 0; i-- {
			if uid, ok := tr.cg.scopes[i][name]; ok {
				return uid, true
			}
		}
		return 0, false
	}
	for i := len(tr.scopes) - 1; i >= 0; i-- {
		if uid, ok := tr.scopes[i][name]; ok {
			return uid, true
		}
	}
	return 0, false
}

// resetScope clears the per-function emission scope state. The assembly path
// resets its own scope in genFunc, so this is only ever called on the IR path
// (cg == nil); the guard keeps it a no-op if somehow reached otherwise.
func (tr *typeResolver) resetScope() {
	if tr.cg != nil {
		return
	}
	tr.varEnts = map[int]varInfo{}
	tr.scopes = nil
	tr.varUID = 0
	tr.declUID = map[*DeclStmt]int{}
}

// --- type analysis ----------------------------------------------------------

func (tr *typeResolver) exprType(e Expr) *Type {
	switch n := e.(type) {
	case *NumLit:
		// A literal's own type, which decides whether the arithmetic around it
		// is floating or integral. Kind == TDouble marks it floating and IsFloat
		// picks the width; without this an expression like "1.0 / 0.0" -- the
		// INFINITY macro -- reported no type at all, so the division was
		// emitted as an integer one.
		if n.BigWords != nil {
			return &Type{Kind: KBitInt, Bits: n.BigBits, Signed: n.BigSigned}
		}
		if n.Kind == TDouble {
			if n.IsFloat {
				return FloatType()
			}
			return DoubleType()
		}
		if n.Wide {
			return WCharType()
		}
		return numLitType(n)
	case *StrLit:
		// A string literal is an array of char, which decays to a pointer when
		// it is used as a value.
		return &Type{Kind: KArr, Elem: &Type{Kind: KInt, Width: 1, Signed: true}}
	case *CompoundLit:
		// The unnamed object's own declared type (arrays included: callers
		// decide between value-address and decay handling).
		return n.Typ
	case *GenericExpr:
		// The selection's type is the type of the chosen branch (the
		// controlling expression's own type is irrelevant after the pick).
		if n.Chosen != nil {
			return tr.exprType(n.Chosen)
		}
		return nil
	case *Ident:
		if vi, ok := tr.lookupVar(n.Name); ok {
			return vi.typ
		}
		// A static local's type is registered under its .data label, not its
		// source name (which may even collide with a file-scope global).
		if lab, ok := tr.staticLabel(n.Name); ok {
			if gt, ok := tr.globalType(lab); ok {
				return gt
			}
		}
		// A function designator decays to a pointer to that function.
		if fd, ok := tr.funcDef(n.Name); ok {
			ft := FuncType(fd.Ret, fd.ParamTypes)
			ft.Variadic = fd.Variadic
			return PtrType(ft)
		}
		if gt, ok := tr.globalType(n.Name); ok {
			return gt
		}
		return nil
	case *Unary:
		// Unary '-' and '~' keep the operand's (promoted) type; this matters
		// for width-sensitive nesting such as "~x | y" or "z = ~mask", where a
		// nil here would make an enclosing operator pick the wrong width.
		if n.Op == "-" || n.Op == "~" {
			return tr.exprType(n.E)
		}
		if n.Op == "*" {
			if t := tr.exprType(n.E); t != nil && t.IsPtr() {
				return t.Elem
			}
		}
		if n.Op == "&" {
			// "&x" is a pointer to x's type.
			if t := tr.exprType(n.E); t != nil {
				if t.IsFunc() {
					return PtrType(t)
				}
				if t.IsPtr() && t.Elem != nil && t.Elem.IsFunc() {
					return t
				}
				return PtrType(t)
			}
		}
	case *Index:
		if t := tr.exprType(n.Base); t != nil {
			if t.IsPtr() || t.IsArray() {
				return t.Elem
			}
		}
	case *Binary:
		// Pointer arithmetic keeps a pointer type, so "(p + 1) - base" is
		// still pointer-minus-pointer.
		return tr.binaryType(n)
	case *AssignExpr:
		// An assignment yields the left operand's value, and its type is the
		// left operand's type -- "(*d++ = *src++) != 0" compares a char against
		// zero, and reading the type from anywhere else made the comparison
		// i32 against a one-byte load.
		return tr.exprType(n.Lhs)
	case *AssignStmt:
		return tr.exprType(n.Lhs)
	case *CondExpr:
		// A ternary yields the common type of its two arms, after the usual
		// conversions. Without this the arm types were invisible, so an
		// enclosing operator saw no type and fell back to the default width:
		// "(y >= 0 ? y : y - 399) / 400" divided an i64 phi by an i32.
		return arithCommon(tr.exprType(n.Then), tr.exprType(n.Else))
	case *IncDecExpr:
		return tr.exprType(n.E)
	case *TmpLoad:
		// Internal node produced only by genCompoundAssign: the type is the
		// parked lvalue's static type.
		return n.Typ
	case *MemberExpr:
		return tr.memberType(n.Base, n.Name)
	case *CastExpr:
		return n.Typ
	case *Call:
		// A call's type is the callee's declared return type (nil for
		// goclib / extern calls, which return int). This is how struct-
		// returning calls are recognised at argument / assignment / return
		// positions so their result buffer can be consumed by address.
		if fd, ok := tr.funcDef(n.Name); ok {
			return fd.Ret
		}
		// The name may designate a function-pointer VARIABLE rather than a
		// function: its result type comes from the pointer's static type.
		if _, ft, ok := tr.fnPtrVar(n.Name); ok {
			return ft.Ret
		}
		return nil
	case *IndirectCall:
		// A UFCS method call types like the direct call it was rewritten to.
		if n.UFCS != nil {
			if fd, ok := tr.funcDef(n.UFCS.Name); ok {
				return fd.Ret
			}
			return nil
		}
		if ft := funcTypeOf(tr.exprType(n.Fn)); ft != nil {
			return ft.Ret
		}
		return nil
	}
	return nil
}

// memberType resolves the type of a struct/union member access.
func (tr *typeResolver) memberType(base Expr, name string) *Type {
	t := tr.exprType(base)
	// "->" on an array-typed base is legal C too: it decays to a pointer, so
	// "buffer->field" addresses element 0. Both spellings unwrap to the elem.
	if t != nil && (t.Kind == KPtr || t.Kind == KArr) && t.Elem != nil {
		t = t.Elem
	}
	if t == nil || (t.Kind != KStruct && t.Kind != KUnion) {
		return nil
	}
	for _, m := range t.Members {
		if m.Name == name {
			return m.Type
		}
	}
	return nil
}

// binaryType mirrors the checker's rules (checkBinary) so the translator can
// classify an expression that is itself arithmetic.
//
// The runtime lowering already widens operands through arithCommon; what this
// must do is report, for a node that is itself an operand of a larger
// expression, the type that node will have. A pure arithmetic node (a+b, x/64,
// a&b) therefore resolves to the usual arithmetic common type, not nil -- a nil
// here makes an enclosing operator pick the wrong width and emit, say, an i32
// sdiv against an i64 operand.
func (tr *typeResolver) binaryType(n *Binary) *Type {
	lt := tr.exprType(n.L)
	rt := tr.exprType(n.R)
	if isBig(lt) || isBig(rt) {
		switch n.Op {
		case "+", "-", "*", "/", "%", "<<", ">>", "&", "|", "^":
			if (lt == nil || lt.IsIntClass() || isBig(lt)) && (rt == nil || rt.IsIntClass() || isBig(rt)) {
				lt2 := lt
				if lt2 == nil {
					lt2 = IntType()
				}
				rt2 := rt
				if rt2 == nil {
					rt2 = IntType()
				}
				return bigArithResult(n.Op, lt2, rt2)
			}
		}
		return nil
	}
	switch n.Op {
	case "&&", "||":
		// Logical operators yield int in C; the IR path short-circuits them,
		// but a surrounding expression still needs a width to resolve against.
		return IntType()
	case "==", "!=", "<", ">", "<=", ">=":
		// A comparison's result is i1, but its operands share the usual
		// arithmetic common type; that is the width-sensitive operand type a
		// surrounding expression needs.
		return arithCommon(lt, rt)
	case "+", "-":
		if lt != nil && lt.IsPtr() && rt != nil && rt.IsPtr() {
			if n.Op == "-" {
				// ptrdiff_t: 64 bits on this target, like the value the
				// emitter produces for the difference.
				return &Type{Kind: KInt, Width: 8, Signed: true}
			}
			return nil
		}
		if lt != nil && (lt.IsPtr() || lt.IsArray()) && (rt == nil || (!rt.IsPtr() && !rt.IsArray())) {
			if lt.IsArray() && lt.Elem != nil {
				return PtrType(lt.Elem)
			}
			return lt
		}
		if rt != nil && (rt.IsPtr() || rt.IsArray()) && (lt == nil || (!lt.IsPtr() && !lt.IsArray())) {
			if rt.IsArray() && rt.Elem != nil {
				return PtrType(rt.Elem)
			}
			return rt
		}
		return arithCommon(lt, rt)
	case "*", "/", "%", "&", "|", "^":
		return arithCommon(lt, rt)
	case "<<", ">>":
		// A shift keeps the left operand's type; the right operand only sizes it.
		if lt != nil {
			return lt
		}
		return arithCommon(lt, rt)
	}
	return nil
}

// memberOffset resolves the byte offset of a struct member within its struct,
// walking the same path the checker uses.
func (tr *typeResolver) memberOffset(n *MemberExpr, sty *Type) (int, bool) {
	st := sty
	if n.Arrow {
		if st == nil || st.Kind != KPtr || st.Elem == nil {
			return 0, false
		}
		st = st.Elem
	}
	if st == nil || (st.Kind != KStruct && st.Kind != KUnion) {
		return 0, false
	}
	for _, mem := range st.Members {
		if mem.Name == n.Name {
			if mem.AnonBase != nil {
				return mem.AnonBase.Offset + mem.Offset, true
			}
			return mem.Offset, true
		}
	}
	return 0, false
}

// calleeParams returns the declared parameter types of a function, whether it
// is defined in this program or supplied by the C runtime.
func (tr *typeResolver) calleeParams(name string) ([]*Type, bool) {
	if fd, ok := tr.funcDef(name); ok {
		return fd.ParamTypes, true
	}
	if fd, ok := tr.libFunc(name); ok {
		return fd.ParamTypes, true
	}
	return nil, false
}

// calleeRet returns a function's declared return type.
func (tr *typeResolver) calleeRet(name string) *Type {
	if fd, ok := tr.funcDef(name); ok {
		return fd.Ret
	}
	if fd, ok := tr.libFunc(name); ok {
		return fd.Ret
	}
	return IntType()
}

// constValue resolves a name with no storage to an integer constant, which is
// what an enum member looks like by the time code generation runs.
func (tr *typeResolver) constValue(name string) (int64, bool) {
	if v, ok := enumConsts[name]; ok {
		return v, true
	}
	return 0, false
}

// fnPtrVar reports whether name denotes a function-pointer variable (or
// global), returning its function type.
func (tr *typeResolver) fnPtrVar(name string) (Expr, *Type, bool) {
	if vi, ok := tr.lookupVar(name); ok {
		if ft := funcTypeOf(vi.typ); ft != nil {
			return &Ident{Name: name}, ft, true
		}
	}
	if gt, ok := tr.globalType(name); ok {
		if ft := funcTypeOf(gt); ft != nil {
			return &Ident{Name: name}, ft, true
		}
	}
	return nil, nil, false
}
