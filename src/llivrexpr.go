package main

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
	op string // an operand: "%t3", "42", "null"
	ty *Type  // the C type of the value
}

// rvalue evaluates an expression to a value.
func (e *irEmitter) rvalue(x Expr) string {
	v := e.eval(x)
	return v.op
}

// eval is rvalue with the type attached.
func (e *irEmitter) eval(x Expr) val {
	switch n := x.(type) {
	case nil:
		return val{op: "0", ty: IntType()}
	case *NumLit:
		return e.numLit(n)
	case *StrLit:
		return e.strLit(n)
	case *Ident:
		return e.ident(n)
	case *Unary:
		return e.unary(n)
	case *Binary:
		return e.binary(n)
	case *AssignExpr:
		return e.assignExpr(n)
	case *AssignStmt:
		e.assignStmt(n)
		return val{op: "0", ty: IntType()}
	case *Call:
		return e.callExpr(n)
	case *IndirectCall:
		return e.indirectCall(n)
	case *Index:
		return e.load(e.lvalue(n), e.tr.exprType(n))
	case *MemberExpr:
		return e.member(n)
	case *CastExpr:
		return e.cast(n)
	case *VaArgExpr:
		return e.vaArg(n)
	case *IncDecExpr:
		return e.incDec(n)
	case *CondExpr:
		return e.condExpr(n)
	case *CommaExpr:
		e.discard(n.Left)
		return e.eval(n.Right)
	case *SizeofExpr:
		t := n.Typ
		if t == nil && n.E != nil {
			t = e.tr.exprType(n.E)
		}
		return val{op: itoa(sizeOf(t)), ty: UnsignedType()}
	case *CompoundLit:
		return e.compoundLit(n)
	case *GenericExpr:
		if n.Chosen != nil {
			return e.eval(n.Chosen)
		}
		return val{op: "0", ty: IntType()}
	case *TmpLoad:
		// A temporary the native path parks in a frame slot; the IR path has
		// no such slots, so the node never appears here.
		return val{op: "0", ty: n.Typ}
	}
	return val{op: "0", ty: IntType()}
}

// cond evaluates a controlling expression and reduces it to i1.
// vaArg lowers the va_arg builtin to LLVM's intrinsic.
//
// goc models a va_list as a flat cursor pointer, while LLVM's x86-64 va_list is
// a structure holding register-save offsets and an overflow area. LLVM's
// intrinsic is what knows that layout, so the front end hands it the cursor and
// asks for the next value; reimplementing the layout here would be a second
// opinion about where an argument lives, and the two would eventually disagree
// about a program's arguments.
// vaArg lowers va_arg(ap, T) against goc's own va_list, which is a flat cursor
// pointer: every variable argument occupies one eight-byte slot, and reading one
// advances the cursor by eight (see genVaArg, which does the same in
// registers). That is the same model the native generator uses, so a variadic
// function behaves identically whichever back end built it.
//
// LLVM's own va_arg does not fit here, and the difference is not cosmetic.
// LLVM's va_list is a target-defined structure and va_arg is an *instruction*
// -- "vaarg %ap, i32" -- rather than a call, so there is no intrinsic to call;
// the tuple spelling that older LLVM accepted ("@llvm.va_arg(ptr, [i32, i8*])")
// is rejected by LLVM 23 ("expected number in address space"). Reading the
// cursor here also keeps the two back ends agreeing on where an argument lives,
// which is the whole reason goc models va_list as a plain char* rather than
// deferring to a second, target-specific opinion.
// On Windows x64 a va_list is a one-element array of
//
//	struct { unsigned gp_offset; unsigned fp_offset;
//	         void *overflow_arg_area; void *reg_save_area; }
//
// and llvm.va_start fills that structure in, so reading an argument means
// consulting those four fields: take it from the register save area while the
// relevant offset is below the limit, and from the overflow area (advancing it)
// once it is not. Both offsets and the overflow cursor are then updated in
// place, which is what makes a second va_arg read the next argument.
//
// This has to agree with the va_start that created the list. An earlier version
// walked a flat eight-byte cursor instead -- goc's own native model -- which
// pairs with neither llvm.va_start nor the register save area the caller
// actually built, so every variadic call that passed an argument read garbage:
// printf("v=%d", x) crashed where printf("hi") did not.
//
// There is no vaarg *instruction* to fall back on either. LLVM spells it
// "vaarg %ap, i32" and the ExpandVariadics pass lowers it, but the textual
// parser no longer accepts the keyword, and the older "@llvm.va_arg(ptr, [i32,
// i8*])" intrinsic call is rejected too ("expected number in address space").
// Clang lowers va_arg in the frontend for the same reason; this is that
// lowering, for the one ABI goc targets.
func (e *irEmitter) vaArg(n *VaArgExpr) val {
	// A va_list is a char* in goc's front end but the 24-byte Windows x64
	// structure everywhere else. vaListSlot sorts out which object is meant: a
	// local `va_list ap;` gets storage with that layout, and one that arrived
	// as a parameter is already a pointer to such a structure. Reading one
	// level off is what made every argument come back as whatever the caller's
	// frame happened to hold, so printf("%d", x) crashed where printf("hi") did
	// not.
	ap := e.vaListSlot(n.Ap)
	ty := n.Typ
	if ty == nil {
		ty = IntType()
	}
	lty := e.ty(ty)
	isFP := lty == "double" || lty == "float"

	// The four fields, in declaration order.
	gpPtr := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i64 0", gpPtr, ap)
	fpPtr := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i64 4", fpPtr, ap)
	ovPtr := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i64 8", ovPtr, ap)
	rsPtr := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i64 16", rsPtr, ap)

	offAddr, offStep, offLimit := gpPtr, int64(8), int64(48)
	if isFP {
		// Floating arguments live in the second half of the save area, in
		// sixteen-byte slots; the general-purpose half starts at 48.
		offAddr, offStep, offLimit = fpPtr, 16, 176
	}
	off := e.newTmp()
	e.line("%s = load i32, ptr %s, align 4", off, offAddr)
	over := e.newTmp()
	e.line("%s = load ptr, ptr %s, align 8", over, ovPtr)
	reg := e.newTmp()
	e.line("%s = load ptr, ptr %s, align 8", reg, rsPtr)

	// While the offset is below the limit the argument is still in the register
	// save area; past it, the rest is on the stack.
	inReg := e.newTmp()
	e.line("%s = icmp ult i32 %s, %d", inReg, off, offLimit)

	fromReg := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i32 %s", fromReg, reg, off)
	addr := e.newTmp()
	e.line("%s = select i1 %s, ptr %s, ptr %s", addr, inReg, fromReg, over)

	// Advance whichever cursor was used, and the offset to match.
	offNext := e.newTmp()
	e.line("%s = add i32 %s, %d", offNext, off, offStep)
	overNext := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr %s, i64 %d", overNext, over, offStep)
	newOff := e.newTmp()
	e.line("%s = select i1 %s, i32 %s, i32 %s", newOff, inReg, offNext, off)
	newOver := e.newTmp()
	e.line("%s = select i1 %s, ptr %s, ptr %s", newOver, inReg, over, overNext)
	e.line("store i32 %s, ptr %s, align 4", newOff, offAddr)
	e.line("store ptr %s, ptr %s, align 8", newOver, ovPtr)

	// The slot is eight bytes wide (sixteen for a double), so a narrow type is
	// read at its own width from the same address.
	slot := e.newTmp()
	switch lty {
	case "i1":
		raw := e.newTmp()
		e.line("%s = load i8, ptr %s, align 1", raw, addr)
		e.line("%s = trunc i8 %s to i1", slot, raw)
	case "float":
		bits := e.newTmp()
		e.line("%s = load i32, ptr %s, align 4", bits, addr)
		e.line("%s = bitcast i32 %s to float", slot, bits)
	case "double":
		bits := e.newTmp()
		e.line("%s = load i64, ptr %s, align 8", bits, addr)
		e.line("%s = bitcast i64 %s to double", slot, bits)
	default:
		e.line("%s = load %s, ptr %s, align %d", slot, lty, addr, alignOfIr(lty))
	}
	return val{op: slot, ty: ty}
}

func (e *irEmitter) cond(x Expr) string {
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
	if v.ty != nil && (v.ty.Kind == KFloat || v.ty.Kind == KDouble) {
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
func (e *irEmitter) discard(x Expr) {
	switch n := x.(type) {
	case nil:
		return
	case *Call:
		e.callExpr(n)
	case *IndirectCall:
		e.indirectCall(n)
	case *CommaExpr:
		e.discard(n.Left)
		e.discard(n.Right)
	case *AssignExpr:
		e.assignExpr(n)
	case *AssignStmt:
		e.assignStmt(n)
	default:
		// A pure expression needs no code; the operand it produced is unused.
		e.eval(x)
	}
}

// --- leaves -----------------------------------------------------------------

func (e *irEmitter) numLit(n *NumLit) val {
	// Kind == TDouble marks the literal as floating point; IsFloat then only
	// says whether it is float or double ("1.5f" versus "1.5"). Testing IsFloat
	// alone made every unsuffixed literal -- "1.0", "0.0", and so the NAN and
	// INFINITY macros -- look like an integer, so a division of them was
	// emitted as "sdiv i32 0, 0" and the surrounding double arithmetic lost its
	// type.
	if n.Kind == TDouble {
		// A float literal lives in an i32 on this path (the same choice the
		// native generator makes), so it is written as a bit pattern.
		t := e.newTmp()
		if n.IsFloat {
			e.line("%s = bitcast float 0x%016x to float", t, f32bits(n.Fval))
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
			return val{op: itoa64(int64(n.BigWords[0])), ty: &Type{Kind: KBitInt, Bits: n.BigBits}}
		}
		return val{op: "0", ty: &Type{Kind: KBitInt, Bits: n.BigBits}}
	}
	ty := numLitType(n)
	return val{op: itoa64(n.Val), ty: ty}
}

func (e *irEmitter) strLit(n *StrLit) val {
	name := e.c.addString(bytesToBytes(n.Bytes))
	t := e.newTmp()
	e.line("%s = getelementptr inbounds i8, ptr @%s, i64 0", t, name)
	// The literal's value is its address: a char array decays to a pointer.
	return val{op: t, ty: PtrType(CharType())}
}

func (e *irEmitter) ident(n *Ident) val {
	ty := e.tr.exprType(n)
	// A local is a slot; a global is a symbol. Both are addresses, and the
	// value is a load from it -- except for an array, which decays to its
	// address. Deciding that here rather than at every use is what C means by
	// "an array is converted to a pointer", and it is why a[j] on an array
	// parameter does not try to load the whole array as a value.
	if ty != nil && ty.Kind == KArr {
		return e.addressOf(n, ty)
	}
	// A va_list that va_start has already given its real storage reads as the
	// address of that storage, not as a load from the ordinary eight-byte
	// pointer slot. Without this the two disagreed about which object `ap` is:
	// vfprintf was handed a pointer to a slot nothing had ever written, so
	// every variadic call crashed while a program that merely linked printf
	// ran fine.
	if s, ok := e.vaSlots[n.Name]; ok {
		return val{op: s, ty: PtrType(CharType())}
	}
	if uid, ok := e.tr.lookupUID(n.Name); ok {
		slot := e.slotFor(uid, e.ty(ty))
		return e.load(slot, ty)
	}
	if sym := e.c.globalSym(n.Name); sym != "" {
		if ty != nil && ty.Kind == KArr {
			return val{op: "@" + sym, ty: PtrType(ty.Elem)}
		}
		return e.load("@"+sym, ty)
	}
	// A name with no storage is a constant (an enum member) or a function used
	// as a value. The constant case is what the checker leaves behind.
	if v, ok := e.tr.constValue(n.Name); ok {
		return val{op: itoa64(v), ty: ty}
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
func (e *irEmitter) zeroLiteral(t *Type) string {
	if t == nil {
		return "0"
	}
	switch t.Kind {
	case KPtr, KFunc, KArr, KStruct, KUnion:
		return "null"
	case KFloat, KDouble:
		return "0.0"
	}
	return "0"
}

// load reads a value of type t from the address p.
func (e *irEmitter) load(p string, t *Type) val {
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
