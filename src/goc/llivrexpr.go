package compiler

import "goc/frontend"

// Expressions, in LLVM IR.
//
// Two things distinguish this from the assembly path. First, there is no
// separate lvalue path in the C grammar sense: an expression yields either a
// value or a pointer to one, and load and store are explicit, so genLValue
// returns an address and rvalue loads from one. Second, C's implicit
// conversions -- integer promotion, the usual arithmetic conversions, pointer
// scaling -- have to be materialised, because IR will not do them for us.

// val is an expression's result: either a register holding a value, or a
// constant. Instructions are emitted as a side effect.
type val struct {
	op string         // an operand: "%t3", "42", "null"
	ty *frontend.Type // the C type of the value
}

// rvalue evaluates an expression to a value.
func (e *irEmitter) rvalue(x frontend.Expr) string {
	v := e.eval(x)
	return v.op
}

// eval is rvalue with the type attached.
func (e *irEmitter) eval(x frontend.Expr) val {
	switch n := x.(type) {
	case nil:
		return val{op: "0", ty: frontend.IntType()}
	case *frontend.NumLit:
		return e.numLit(n)
	case *frontend.StrLit:
		return e.strLit(n)
	case *frontend.Ident:
		return e.ident(n)
	case *frontend.Unary:
		return e.unary(n)
	case *frontend.Binary:
		return e.binary(n)
	case *frontend.AssignExpr:
		return e.assignExpr(n)
	case *frontend.AssignStmt:
		e.assignStmt(n)
		return val{op: "0", ty: frontend.IntType()}
	case *frontend.Call:
		return e.callExpr(n)
	case *frontend.IndirectCall:
		return e.indirectCall(n)
	case *frontend.Index:
		return e.load(e.lvalue(n), e.tr.exprType(n))
	case *frontend.MemberExpr:
		return e.member(n)
	case *frontend.CastExpr:
		return e.cast(n)
	case *frontend.VaArgExpr:
		return e.vaArg(n)
	case *frontend.IncDecExpr:
		return e.incDec(n)
	case *frontend.CondExpr:
		return e.condExpr(n)
	case *frontend.CommaExpr:
		e.discard(n.Left)
		return e.eval(n.Right)
	case *frontend.SizeofExpr:
		t := n.Typ
		if t == nil && n.E != nil {
			t = e.tr.exprType(n.E)
		}
		return val{op: itoa(frontend.Sizeof(t)), ty: frontend.UnsignedType()}
	case *frontend.CompoundLit:
		return e.compoundLit(n)
	case *frontend.GenericExpr:
		if n.Chosen != nil {
			return e.eval(n.Chosen)
		}
		return val{op: "0", ty: frontend.IntType()}
	case *frontend.TmpLoad:
		// A temporary the native path parks in a frame slot; the IR path has
		// no such slots, so the node never appears here.
		return val{op: "0", ty: n.Typ}
	}
	return val{op: "0", ty: frontend.IntType()}
}

// cond evaluates a controlling expression and reduces it to i1.
// vaArg lowers the va_arg builtin against goc's own va_list model.
//
// goc models a va_list as a flat cursor pointer: every variable argument
// occupies one eight-byte slot, and reading one advances the cursor by eight
// (see genVaArg, which does the same in registers). That is the same model the
// native generator uses, so a variadic function behaves identically whichever
// back end built it.
//
// LLVM's own va_arg does not fit here, and the difference is not cosmetic.
// LLVM's va_list is a target-defined structure and va_arg is an *instruction*
// -- "vaarg %ap, i32" -- rather than a call, so there is no intrinsic to call;
// the tuple spelling that older LLVM accepted ("@llvm.va_arg(ptr, [i32, i8*])")
// is rejected by LLVM 23 ("expected number in address space"). Reading the
// cursor here also keeps the two back ends agreeing on where an argument lives,
// which is the whole reason goc models va_list as a plain char* rather than
// deferring to a second, target-specific opinion.
//
// What does the intrinsic do, then? llvm.va_start on Windows x64 writes a
// single pointer into the va_list: the address of the register save area the
// caller built (general-purpose slots first, the overflow area past them), so
// the cursor that va_start establishes is exactly goc's flat eight-byte cursor
// and reading an argument means loading that cursor, loading the value, and
// advancing the cursor by eight.
//
// The Windows x64 "structure" view of a va_list ({ unsigned gp_offset; unsigned
// fp_offset; void *overflow_arg_area; void *reg_save_area; }) is the layout
// clang's SysV lowering writes -- llvm.va_start stores no such structure here.
// An earlier version of this function consulted those four fields anyway, so
// every field read garbage from the single stored pointer and every variadic
// call that consumed an argument crashed: printf("v=%d", x) crashed where
// printf("hi") did not.
func (e *irEmitter) vaArg(n *frontend.VaArgExpr) val {
	// Every va_list -- a local `va_list ap;` or one that arrived as a
	// parameter -- is backed by a writable slot (vaListSlot returns its
	// address), and llvm.va_start stores the cursor into that slot. The slot
	// is what the cursor lives in; writing it back is what makes a second
	// va_arg read the next argument.
	ap := e.vaListSlot(n.Ap)
	ty := n.Typ
	if ty == nil {
		ty = frontend.IntType()
	}
	lty := e.ty(ty)

	// What llvm.va_start actually fills in on Windows x64 is a single pointer:
	// the address of the register save area the CALLER built, with every
	// variadic argument occupying one eight-byte slot (general registers
	// first, then the stack area past them). That is the flat-cursor model the
	// native generator uses, so reading an argument means loading the cursor,
	// loading the value, and advancing the cursor by eight. An earlier version
	// consulted the 24-byte __va_list_tag structure instead (gp_offset,
	// fp_offset, overflow_arg_area, reg_save_area) -- the layout clang's SysV
	// lowering writes -- but llvm.va_start stores no such structure on
	// Windows, so every field read garbage and every variadic call that
	// consumed an argument crashed.
	cur := e.newTmp()
	e.line("%s = load ptr, ptr %s, align 8", cur, ap)

	// The slot is eight bytes wide, so a narrow type is read at its own width
	// from the same address.
	slot := e.newTmp()
	switch lty {
	case "i1":
		raw := e.newTmp()
		e.line("%s = load i8, ptr %s, align 1", raw, cur)
		e.line("%s = trunc i8 %s to i1", slot, raw)
	case "float":
		bits := e.newTmp()
		e.line("%s = load i32, ptr %s, align 4", bits, cur)
		e.line("%s = bitcast i32 %s to float", slot, bits)
	case "double":
		bits := e.newTmp()
		e.line("%s = load i64, ptr %s, align 8", bits, cur)
		e.line("%s = bitcast i64 %s to double", slot, bits)
	default:
		e.line("%s = load %s, ptr %s, align %d", slot, lty, cur, alignOfIr(lty))
	}
	next := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i64 8", next, cur)
	e.line("store ptr %s, ptr %s, align 8", next, ap)
	return val{op: slot, ty: ty}
}

func (e *irEmitter) cond(x frontend.Expr) string {
	if x == nil {
		return "true"
	}
	// The value's IR type decides, not the C type: a comparison already yields
	// an i1 even though C types it as int, and re-comparing that against zero
	// would both be redundant and type-wrong.
	v := e.eval(x)
	ty := e.ty(v.ty)
	if ty == "i1" {
		return v.op
	}
	if v.ty != nil && (v.ty.Kind == frontend.KFloat || v.ty.Kind == frontend.KDouble) {
		t := e.newTmp()
		e.line("%s = fcmp une %s %s, 0.0", t, ty, v.op)
		return t
	}
	t := e.newTmp()
	if ty == "ptr" {
		// A pointer condition is "non-null"; compare against the null pointer
		// constant, not the integer 0, or LLVM rejects the mixed types.
		e.line("%s = icmp ne ptr %s, null", t, v.op)
	} else {
		e.line("%s = icmp ne %s %s, 0", t, ty, v.op)
	}
	return t
}

// discard evaluates an expression for its effects only.
func (e *irEmitter) discard(x frontend.Expr) {
	switch n := x.(type) {
	case nil:
		return
	case *frontend.Call:
		e.callExpr(n)
	case *frontend.IndirectCall:
		e.indirectCall(n)
	case *frontend.CommaExpr:
		e.discard(n.Left)
		e.discard(n.Right)
	case *frontend.AssignExpr:
		e.assignExpr(n)
	case *frontend.AssignStmt:
		e.assignStmt(n)
	default:
		// A pure expression needs no code; the operand it produced is unused.
		e.eval(x)
	}
}

// --- leaves -----------------------------------------------------------------

func (e *irEmitter) numLit(n *frontend.NumLit) val {
	// Kind == frontend.TDouble marks the literal as floating point; IsFloat then only
	// says whether it is float or double ("1.5f" versus "1.5"). Testing IsFloat
	// alone made every unsuffixed literal -- "1.0", "0.0", and so the NAN and
	// INFINITY macros -- look like an integer, so a division of them was
	// emitted as "sdiv i32 0, 0" and the surrounding double arithmetic lost its
	// type.
	if n.Kind == frontend.TDouble {
		// A float literal lives in an i32 on this path (the same choice the
		// native generator makes), so it is written as a bit pattern.
		t := e.newTmp()
		// The 16-digit form is what LLVM reads for float as well as double,
		// and it must be exactly representable in the type: for float that is
		// the bit pattern of the double the float widens to, so 5.0f is
		// 0x4014000000000000 and not the 32-bit pattern 0x40a00000 padded
		// out, which lands in the subnormals and is rejected.
		if n.IsFloat {
			e.line("%s = bitcast float 0x%016x to float", t,
				f64bits(float64(float32(n.Fval))))
		} else {
			e.line("%s = bitcast double 0x%016x to double", t, f64bits(n.Fval))
		}
		return val{op: t, ty: floatOrDouble(n)}
	}
	if n.BigWords != nil {
		// _BitInt: the value is a little-endian word array. The caller keeps
		// such functions on the native path, but rendering the low word keeps
		// the module valid if one ever arrives.
		if len(n.BigWords) > 0 {
			return val{op: itoa64(int64(n.BigWords[0])), ty: &frontend.Type{Kind: frontend.KBitInt, Bits: n.BigBits}}
		}
		return val{op: "0", ty: &frontend.Type{Kind: frontend.KBitInt, Bits: n.BigBits}}
	}
	ty := numLitType(n)
	return val{op: itoa64(n.Val), ty: ty}
}

func (e *irEmitter) strLit(n *frontend.StrLit) val {
	// A C string literal carries a trailing NUL; the lexer keeps only the
	// quoted bytes, so append the terminator here. Without it the literal is
	// an unterminated [N x i8] and the runtime reads straight past its end
	// into the next global (e.g. vfmt formatting "x" then walking into the
	// "assertion \"%s\"..." template and hitting the %s branch).
	b := bytesToBytes(n.Bytes)
	b = append(b, 0)
	name := e.c.addString(b)
	t := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr @%s, i64 0", t, name)
	// The literal's value is its address: a char array decays to a pointer.
	return val{op: t, ty: frontend.PtrType(frontend.CharType())}
}

func (e *irEmitter) ident(n *frontend.Ident) val {
	ty := e.tr.exprType(n)
	// A local is a slot; a global is a symbol. Both are addresses, and the
	// value is a load from it -- except for an array, which decays to its
	// address. Deciding that here rather than at every use is what C means by
	// "an array is converted to a pointer", and it is why a[j] on an array
	// parameter does not try to load the whole array as a value.
	if ty != nil && ty.Kind == frontend.KArr {
		return e.addressOf(n, ty)
	}
	// A va_list that va_start has already given its real storage reads as the
	// address of that storage -- the cursor the intrinsic wrote -- not as the
	// address of the slot holding it. Without the load, vfprintf was handed the
	// slot's address instead of the register save area, so it dereferenced one
	// level short and every variadic call crashed while a program that merely
	// linked printf ran fine.
	if s, ok := e.vaSlots[n.Name]; ok {
		t := e.newTmp()
		e.line("%s = load ptr, ptr %s, align 8", t, s)
		return val{op: t, ty: frontend.PtrType(frontend.CharType())}
	}
	if uid, ok := e.tr.lookupUID(n.Name); ok {
		slot := e.slotFor(uid, e.ty(ty))
		return e.load(slot, ty)
	}
	if sym := e.c.globalSym(n.Name); sym != "" {
		if ty != nil && ty.Kind == frontend.KArr {
			return val{op: "@" + sym, ty: frontend.PtrType(ty.Elem)}
		}
		return e.load("@"+sym, ty)
	}
	// A name with no storage is a constant (an enum member) or a function used
	// as a value. The constant case is what the checker leaves behind.
	if v, ok := e.tr.constValue(n.Name); ok {
		return val{op: itoa64(v), ty: ty}
	}
	// A function used as a value decays to its own address. C spells this
	// "function designator conversion"; in LLVM a function *is* its address, so
	// the symbol reference is already the pointer and no load is involved --
	// loading would read the instruction bytes at the entry point instead.
	//
	// This is what makes `int (*fp)(int) = twice;` and `apply(twice, 21)` work.
	// Without it they silently produced a null pointer, and the program crashed
	// at the first indirect call -- which is how goclib's own
	// `printf_lite_with(vfmt_i, ...)` took the program down: the formatter was
	// handed a null function pointer and called through it.
	if e.tr.isFuncName(n.Name) {
		return val{op: "@" + n.Name, ty: frontend.PtrType(e.fnPtrTy(n.Name))}
	}
	// A name with no storage at all. The checker's own view is that this can
	// only be reached when the operand is an integer (an enum member whose
	// value the front end did not fold) or a pointer to nothing, so the zero
	// value is rendered for the operand's own type. Emitting "null"
	// unconditionally put a pointer constant where a long was expected, and
	// LLVM rejected the comparison "icmp sle i64 null, %v".
	return val{op: e.zeroLiteral(ty), ty: ty}
}

// zeroLiteral renders the zero value of a type as an operand of that type.
func (e *irEmitter) zeroLiteral(t *frontend.Type) string {
	if t == nil {
		return "0"
	}
	switch t.Kind {
	case frontend.KPtr, frontend.KFunc, frontend.KArr, frontend.KStruct, frontend.KUnion:
		return "null"
	case frontend.KFloat, frontend.KDouble:
		return "0.0"
	}
	return "0"
}

// load reads a value of type t from the address p.
func (e *irEmitter) load(p string, t *frontend.Type) val {
	ty := e.ty(t)
	v := e.newTmp()
	e.line("%s = load %s, ptr %s, align %d", v, ty, p, alignOfIr(ty))
	return val{op: v, ty: t}
}

// store writes value v of type t to the address p.
func (e *irEmitter) store(p string, v val) {
	ty := e.ty(v.ty)
	e.line("store %s %s, ptr %s, align %d", ty, v.op, p, alignOfIr(ty))
}
