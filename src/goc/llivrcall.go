package compiler

// Calls, subscripting, member access and address-of.

import (
	"goc/frontend"
	"strings"
)

// lvalue renders an expression that must be an address, and returns it. This is
// the "genLValue" of the assembly path: everything that can appear on the left
// of an assignment or under & goes through here.
func (e *irEmitter) lvalue(x frontend.Expr) string {
	switch n := x.(type) {
	case *frontend.Ident:
		ty := e.tr.exprType(n)
		if uid, ok := e.tr.lookupUID(n.Name); ok {
			return e.slotFor(uid, e.ty(ty))
		}
		if sym := e.c.globalSym(n.Name); sym != "" {
			return "@" + sym
		}
		// A name with no storage is an enum constant; it is not addressable,
		// but the caller only gets here for something the checker accepted.
		return "null"
	case *frontend.Unary:
		if n.Op == "*" {
			// Dereferencing: the operand already evaluated to the address.
			return e.rvalue(n.E)
		}
	case *frontend.Index:
		base, idx := e.indexOperands(n)
		// A[i] is *(a + i); the element address is what is wanted.
		ety := e.tr.exprType(n)
		esz := frontend.Sizeof(ety)
		if esz == 0 {
			esz = 1
		}
		scaled := idx
		if esz != 1 {
			scaled = e.newTmp()
			e.line("%s = mul i64 %s, %d", scaled, idx, esz)
		}
		bp := e.newTmp()
		e.line("%s = ptrtoint ptr %s to i64", bp, base)
		sum := e.newTmp()
		e.line("%s = add i64 %s, %s", sum, bp, scaled)
		r := e.newTmp()
		e.line("%s = inttoptr i64 %s to ptr", r, sum)
		return r
	case *frontend.MemberExpr:
		return e.memberAddr(n)
	case *frontend.CastExpr:
		return e.lvalue(n.E)
	case *frontend.CompoundLit:
		return e.compoundLit(n).op
	}
	// Anything else is not an lvalue; evaluating it is the best that can be done
	// and the checker will already have complained.
	return e.rvalue(x)
}

// addressOf yields the address of an lvalue as a value, which is what an array
// decays to.
func (e *irEmitter) addressOf(x frontend.Expr, t *frontend.Type) val {
	p := e.lvalue(x)
	return val{op: p, ty: frontend.PtrType(t.Elem)}
}

// indexOperands evaluates the base and the subscript of an frontend.Index, applying C's
// array-to-pointer decay. It returns the base as an address and the index as an
// i64.
func (e *irEmitter) indexOperands(n *frontend.Index) (string, string) {
	bty := e.tr.exprType(n.Base)
	// A real array decays: the base is its address, not a load of it.
	if bty != nil && bty.Kind == frontend.KArr {
		return e.lvalue(n.Base), e.indexValue(n.Idx)
	}
	return e.rvalue(n.Base), e.indexValue(n.Idx)
}

func (e *irEmitter) indexValue(x frontend.Expr) string {
	v := e.eval(x)
	return e.convert(v.op, v.ty, &frontend.Type{Kind: frontend.KInt, Width: 8})
}

// --- calls ------------------------------------------------------------------

// callExpr lowers a direct call. Arguments are converted to the parameter types
// the front end resolved, because IR will not convert them.
func (e *irEmitter) callExpr(n *frontend.Call) val {
	// The same constant-format specialisation the native generator applies in
	// genCallExpr, from the one shared decision (see printfspec.go). Without
	// it a hello-world drags in the whole float exponent machine: printf
	// resolves to vfmt, vfmt's switch mentions every conversion the C library
	// defines, and the call-graph prune pulls vfmt's callees in with it.
	//
	// LLVM cannot do this for us. SimplifyLibCalls recognises calls to *known
	// libc symbols*, and this module has none: goclib's printf is emitted here
	// as a plain `define i32 @printf(ptr, ...)`, so from LLVM's point of view
	// it is a local function that happens to have a standard prototype.
	if repl := specializePrintfCall(n, e.printfQueries()); repl != nil {
		return e.callExpr(repl)
	}
	// va_start and va_end are compiler built-ins in goc, recognised by name.
	// Both take the cursor's address: the intrinsics write through it.
	switch n.Name {
	case "va_start":
		if len(n.Args) == 0 {
			return val{op: "0", ty: frontend.IntType()}
		}
		// The operand is always a local `va_list ap;`. What llvm.va_start
		// stores into it on Windows x64 is a single pointer -- the cursor into
		// the caller's register save area -- so vaListSlot gives the local its
		// own widened slot and the intrinsic writes through it. Sharing the
		// ordinary pointer slot made va_start scribble over the locals that
		// followed it.
		ap := e.vaListSlot(n.Args[0])
		e.c.noteIntrinsic("llvm.va_start", "void", []string{"ptr"})
		e.line("call void @llvm.va_start(ptr %s)", ap)
		return val{op: "0", ty: frontend.IntType()}
	case "va_end":
		if len(n.Args) == 0 {
			return val{op: "0", ty: frontend.IntType()}
		}
		ap := e.vaListSlot(n.Args[0])
		e.c.noteIntrinsic("llvm.va_end", "void", []string{"ptr"})
		e.line("call void @llvm.va_end(ptr %s)", ap)
		return val{op: "0", ty: frontend.IntType()}
	}
	// A call through a function-pointer VARIABLE arrives here as a frontend.Call naming
	// the variable, not the function it points at. Emitting "call i32 @fn" for
	// a local such as "void (*fn)(void)" produces a reference to a symbol that
	// does not exist, and the link ends with "undefined symbol: fn". When the
	// name resolves to a pointer to function rather than to a function, lower
	// it as the indirect call it is.
	if _, _, ok := e.tr.fnPtrVar(n.Name); ok {
		return e.indirectCall(&frontend.IndirectCall{Fn: &frontend.Ident{Name: n.Name}, Args: n.Args})
	}
	// Resolve through the front end so a prototype supplies the parameter
	// types; without one the arguments keep their own types.
	paramTys, ret := e.calleeSig(n.Name, len(n.Args))
	e.c.noteExtern(n.Name, ret, paramTys)

	// Every argument carries its type explicitly. LLVM will infer them from a
	// declaration when there is one, but a function defined later in the same
	// module -- or one whose only definition this module does not have -- leaves
	// nothing to infer from, and the call is rejected with "invalid type for
	// function argument". Spelling the types out is always accepted.
	args := make([]string, 0, len(paramTys)+len(n.Args))
	for i, a := range n.Args {
		v := e.eval(a)
		if i < len(paramTys) {
			v = e.coerce(v, paramTys[i])
		} else {
			// A variadic argument keeps its own promoted type; a small
			// integer is widened to int, as C requires.
			v = e.defaultPromote(v)
		}
		args = append(args, e.ty(v.ty)+" "+v.op)
	}
	argText := ""
	if len(args) > 0 {
		argText = strings.Join(args, ", ")
	}
	rty := e.c.externRet(n.Name)
	rs := e.ty(rty)
	call := e.newTmp()
	if rs == "void" {
		e.line("call void @%s(%s)", n.Name, argText)
		return val{op: "", ty: frontend.VoidType()}
	}
	e.line("%s = call %s @%s(%s)", call, rs, n.Name, argText)
	return val{op: call, ty: rty}
}

// printfQueries adapts the IR path's resolver to the questions
// specializePrintfCall asks.
//
// userDefs, not funcDefs: the latter also holds the C runtime's own fwrite and
// printf_lite, so testing against it would report every library function as
// shadowed and the rewrite would never fire.
func (e *irEmitter) printfQueries() printfQueries {
	return printfQueries{
		userDefines: func(name string) bool { return e.tr.userDefs[name] },
		shadowedByVar: func(name string) bool {
			_, _, ok := e.tr.fnPtrVar(name)
			return ok
		},
	}
}

// calleeSig finds a callee's declared signature.
func (e *irEmitter) calleeSig(name string, nargs int) ([]*frontend.Type, *frontend.Type) {
	if p, ok := e.tr.calleeParams(name); ok {
		ret := e.tr.calleeRet(name)
		return p, ret
	}
	// No declaration in sight: treat every argument as an int, which is the
	// common case and keeps the module valid for the linker to check.
	ps := make([]*frontend.Type, nargs)
	for i := range ps {
		ps[i] = frontend.IntType()
	}
	return ps, frontend.IntType()
}

// defaultPromote applies the default argument promotions: a char or short
// becomes an int, and a float becomes a double.
func (e *irEmitter) defaultPromote(v val) val {
	if v.ty == nil {
		return v
	}
	switch v.ty.Kind {
	case frontend.KFloat:
		return val{op: e.convert(v.op, v.ty, frontend.DoubleType()), ty: frontend.DoubleType()}
	case frontend.KInt:
		if v.ty.Width < 4 {
			return val{op: e.convert(v.op, v.ty, frontend.IntType()), ty: frontend.IntType()}
		}
	}
	return v
}

// indirectCall lowers a call through a computed callee.
func (e *irEmitter) indirectCall(n *frontend.IndirectCall) val {
	if n.UFCS != nil {
		// A method call resolved by the checker: the receiver is prepended and
		// the function is called directly.
		return e.callExpr(n.UFCS)
	}
	fn := e.rvalue(n.Fn)
	var args []string
	for _, a := range n.Args {
		v := e.eval(a)
		v = e.defaultPromote(v)
		args = append(args, e.ty(v.ty)+" "+v.op)
	}
	argText := ""
	if len(args) > 0 {
		argText = strings.Join(args, ", ")
	}
	t := e.newTmp()
	e.line("%s = call i32 %s(%s)", t, fn, argText)
	return val{op: t, ty: frontend.IntType()}
}

// --- members ----------------------------------------------------------------

// memberAddr returns the address of a struct member.
func (e *irEmitter) memberAddr(n *frontend.MemberExpr) string {
	sty := e.tr.exprType(n.Base)
	// In the arrow form the base is already a pointer to the struct; in the dot
	// form it is the struct itself, whose address is taken.
	var base string
	if n.Arrow {
		base = e.rvalue(n.Base)
	} else {
		base = e.lvalue(n.Base)
	}
	off, ok := e.tr.memberOffset(n, sty)
	if !ok {
		return base
	}
	if off == 0 {
		return base
	}
	r := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i64 %d", r, base, off)
	return r
}

func (e *irEmitter) member(n *frontend.MemberExpr) val {
	ty := e.tr.exprType(n)
	p := e.memberAddr(n)
	return e.load(p, ty)
}
