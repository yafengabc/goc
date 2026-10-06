package gocl

// Calls, subscripting, member access and address-of.

import (
	"goc/common"
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
		// A thread-local variable lives in the linker-owned .tls section; its
		// address is the per-thread slot the __goc_tls_slot helper returns.
		if off, ok := e.c.tlsOffset(n.Name); ok {
			return e.tlsAddr(off)
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
	if repl := common.SpecializePrintfCall(n, e.printfQueries()); repl != nil {
		return e.callExpr(repl)
	}
	// Compiler intrinsics that exist only to carry information to the back end.
	// They are calls in the C source but instructions (or LLVM intrinsics) in
	// the output, so they have to be recognised before the ordinary call path
	// turns them into references to symbols no library defines. See builtin.go.
	if v, ok := e.markerCall(n); ok {
		return v
	}
	if v, ok := e.overflowCall(n); ok {
		return v
	}
	if v, ok := e.atomicCall(n); ok {
		return v
	}
	// va_start, va_end and va_copy are compiler built-ins in goc, recognised by
	// name. All three take the cursor's address: the intrinsics write through it.
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
	case "va_copy":
		// C99 7.16.1.1: va_copy makes an independent copy of a va_list, so the
		// copy can be walked to the end without consuming the original. This is
		// what lets a formatter measure first and then write.
		//
		// The copy size is ABI-defined, which is why this is the one variadic
		// operation goclib/stdarg.h declines to define as a macro on Linux: on
		// x86-64 SysV a va_list is a 24-byte __va_list_tag carrying four
		// cursors, and copying only the first 8 bytes leaves the copy sharing
		// the original's register save area (and with a null overflow area, so
		// the first stack argument dereferences null). llvm.va_copy is
		// target-aware -- it copies the whole object on SysV and the single
		// pointer on Windows x64 -- so the same intrinsic serves both without a
		// compile-time branch here.
		if len(n.Args) < 2 {
			return val{op: "0", ty: frontend.IntType()}
		}
		dst := e.vaListSlot(n.Args[0])
		src := e.vaListSlot(n.Args[1])
		e.c.noteIntrinsic("llvm.va_copy", "void", []string{"ptr", "ptr"})
		e.line("call void @llvm.va_copy(ptr %s, ptr %s)", dst, src)
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
		// An argument that is a known va_list does not go through the ordinary
		// `char *` read, because on x86-64 SysV a va_list is a 24-byte
		// __va_list_tag and its first eight bytes are two packed cursor
		// integers, not a pointer. The callee wants the ADDRESS of that tag
		// (a SysV va_list parameter is an array type, so it decays to a
		// pointer), and advancing it must write back into the caller's tag --
		// which is what the C semantics require and what lets a callee consume
		// the list. On Windows x64 the value already is the cursor pointer, so
		// the ordinary read is exactly right and the slot's own address is not
		// wanted.
		if e.c.linux {
			if id, ok := a.(*frontend.Ident); ok && e.vaNames[id.Name] {
				args = append(args, "ptr "+e.vaListSlot(a))
				continue
			}
		}
		v := e.eval(a)
		if i < len(paramTys) {
			v = e.coerce(v, paramTys[i])
		} else {
			// A variadic argument keeps its own promoted type; a small
			// integer is widened to int, as C requires.
			v = e.defaultPromote(v)
			// ...and then widened again to fill its whole eight-byte slot.
			// A vararg occupies one eight-byte slot per argument (C 7.16.1.1,
			// and the SysV/Win64 register save areas are arrays of eight-byte
			// slots), while the callee reads the slot as a full register. For a
			// 32-bit int LLVM fills only the low half of the slot and leaves the
			// high half undefined, so a callee that reads it as a pointer --
			// which is exactly how goclib's own `printf_lite_with(int (*fmtfn)
			// (char *, long, const char *, va_list), const char *, va_list)` sees
			// its second and third parameters, the front end having typed
			// va_list as `char *` -- read whatever the register happened to hold
			// above the value.
			//
			// The symptom was every signed 32-bit variadic argument arriving as
			// its unsigned counterpart: `printf("%d", -8)` printed 4294967288,
			// because `sub i32 0, 8` only wrote edx and the stale upper half of
			// rdx was read back as part of the value. Clang, compiling this same
			// IR, widens with `xor %edx,%edx; sub $0x8,%edx` and is right.
			// Extending here is what makes the two agree -- and extending
			// rather than leaving it to the callee is the only place that can:
			// the callee is reached through a pointer whose signature already
			// says `ptr`, so the information about the argument's real width is
			// gone by then.
			v = e.widenVarargSlot(v)
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

// common.PrintfQueries adapts the IR path's resolver to the questions
// specializePrintfCall asks.
//
// userDefs, not funcDefs: the latter also holds the C runtime's own fwrite and
// printf_lite, so testing against it would report every library function as
// shadowed and the rewrite would never fire.
func (e *irEmitter) printfQueries() common.PrintfQueries {
	return common.PrintfQueries{
		UserDefines: func(name string) bool { return e.tr.userDefs[name] },
		ShadowedByVar: func(name string) bool {
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

// widenVarargSlot extends a variadic argument to the full eight bytes of its
// slot.
//
// Only a narrow integer needs it. A double is already 64 bits and a pointer is
// already 64 bits, and an i64 integer occupies the whole slot on its own -- so
// in every case but the 1/2/4-byte integers the value is already the width the
// callee will read. Those are exactly the ones LLVM stores in the low half of
// the register and leaves the high half undefined.
//
// The extension follows the type's own signedness, not its width: `sext` for a
// signed int reproduces C's integer promotion, and `zext` for unsigned (and for
// _Bool, whose values are 0 and 1 and must not become 0xFFFFFFFF and 0) keeps
// the value non-negative. Getting this backwards is the bug being fixed, only
// with the sign bit set in the other half.
func (e *irEmitter) widenVarargSlot(v val) val {
	if v.ty == nil || v.ty.Kind != frontend.KInt {
		// A pointer, a double, or a type the emitter did not classify: already
		// a full slot.
		return v
	}
	lty := e.ty(v.ty)
	switch lty {
	case "i8", "i16", "i32":
	default:
		// i64 and anything wider already fills the slot.
		return v
	}
	out := e.newTmp()
	opc := "zext"
	if v.ty.Signed {
		opc = "sext"
	}
	e.line("%s = %s %s %s to i64", out, opc, lty, v.op)
	// The widened value is a long as far as the IR is concerned: eight bytes
	// carrying the argument, which is what the callee's eight-byte read wants.
	// The original type stays for the caller's own arithmetic on the result.
	return val{op: out, ty: &frontend.Type{Kind: frontend.KInt, Width: 8, Signed: true}}
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
		// A va_list argument goes over as the address of the caller's tag on
		// SysV, for the same reason as in callExpr: the callee's parameter is a
		// decayed array, and the ordinary `char *` read would hand it the tag's
		// first eight bytes -- two packed cursors -- instead of an address.
		if e.c.linux {
			if id, ok := a.(*frontend.Ident); ok && e.vaNames[id.Name] {
				args = append(args, "ptr "+e.vaListSlot(a))
				continue
			}
		}
		v := e.eval(a)
		v = e.defaultPromote(v)
		// Same eight-byte slot rule as a direct variadic call (see callExpr):
		// a narrow integer would otherwise leave the upper half of the slot
		// undefined for the callee to read. Every argument of an indirect call
		// is variadic in the sense that matters here -- the callee's signature
		// is not known at this call site, so nothing else will widen them.
		v = e.widenVarargSlot(v)
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
	// An array member decays to a pointer to its first element, exactly as an
	// array variable does in ident(). `stat(e->d_name, &st)` passes that
	// address; it does not pass the 256 bytes it points at. Loading it here
	// handed the callee a value of type `[256 x i8]` where a ptr was expected,
	// which LLVM rejected with "'%t28' defined with type '[256 x i8]' but
	// expected 'ptr'" -- so every program that passed a struct's char array to
	// a function failed to compile on this back end while compiling fine on
	// the native one. Deciding it here is what C means by array-to-pointer
	// conversion, and a subscript of the member still reads one element
	// because Index asks for the base's address, which is the same thing.
	if ty != nil && ty.Kind == frontend.KArr {
		return val{op: p, ty: frontend.PtrType(ty.Elem)}
	}
	return e.load(p, ty)
}
