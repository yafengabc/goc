package compiler

// long double in the native back end: the lowering, and the calls into
// goclib's software binary128 runtime.
//
// goc's long double is IEEE binary128 on every target (16 bytes), so the
// native code generator never does arithmetic on it either. The value rides
// the struct by-address model -- a value-typed expression leaves its address
// in r10 and the resBig carrier describes it (resBigT is the long double
// type) -- and every operation becomes a call into goclib/fp128.c.
//
// The runtime's entry points take and return goc_tf128, a two-member struct
// of unsigned long long. Both the caller here and the callee (the library
// source compiled by this same back end) agree on goc's own convention: a
// struct result travels through a hidden first argument (the caller's result
// buffer), and a struct argument travels as a hidden pointer to caller-owned
// bytes. So "a + b" is one call with three addresses: arg0 = result buffer,
// arg1 = &a, arg2 = &b. goc_tf_cmp returns a plain int in rax instead, so it
// takes only the two operand addresses.
//
// This is the same convention the LLVM side already relies on under Win64
// (a 16-byte struct is passed by reference and returned through a hidden
// pointer), which is why the one runtime serves both back ends.
//
// No helper needs 16-byte alignment: the runtime reads and writes the two
// halves as integer words, and every move here is copyBytes.

import (
	"fmt"

	"goc/frontend"
)

// tfWords is the width of a long double value in 8-byte frame slots.
const tfWords = 2

// isLD reports whether t is long double.
func isLD(t *frontend.Type) bool {
	return t != nil && t.Kind == frontend.KLongDouble
}

// isLDExpr reports whether e is a long double expression. A numeric literal is
// in no type table (exprType reports nil for it), so a 1.5L literal is
// recognised by its own flag -- the same reason a _BitInt literal reaches
// codegen through its node's BigWords rather than through exprType.
func (c *CG) isLDExpr(e frontend.Expr) bool {
	if t := c.exprType(e); isLD(t) {
		return true
	}
	if nl, ok := e.(*frontend.NumLit); ok {
		return nl.IsLongDouble
	}
	// A negated long double expression ("-2.5L", "-x" with x long double)
	// is itself a long double value. Without this the genExprT intercept
	// misses "-<literal>" (exprType is nil for a literal, and the Unary
	// node is not a NumLit), the expression fell through to genUnary, and
	// the integer `neg` ran on the carrier address.
	if u, ok := e.(*frontend.Unary); ok && u.Op == "-" {
		return c.isLDExpr(u.E)
	}
	return false
}

// tfLitKey identifies a literal by its 128-bit encoding.
func tfLitKey(hi, lo uint64) string {
	return fmt.Sprintf("tf:%016x%016x", hi, lo)
}

// tfLiteralAddr emits the address of a long double literal: the 128-bit
// encoding goes into .rdata as a two-word array (low word first, little
// endian) shared across uses, and r10 is left holding its address. The pool
// is the _BitInt literal pool -- both are read-only 8-byte-aligned word
// arrays, and one emission loop serves both.
func (c *CG) tfLiteralAddr(n *frontend.NumLit) {
	if c.bigLab == nil {
		// The _BitInt literal path assumes the map exists (genBigLiteral
		// writes it directly); nothing initialises it in newCGFor, so a
		// literal pool user must be ready to create it.
		c.bigLab = map[string]string{}
	}
	key := tfLitKey(n.F128.Hi, n.F128.Lo)
	lab, ok := c.bigLab[key]
	if !ok {
		lab = fmt.Sprintf("%sLBIG%d", c.staticPrefix, len(c.bigLits))
		c.bigLits = append(c.bigLits, []uint64{n.F128.Lo, n.F128.Hi})
		c.bigLab[key] = lab
	}
	c.emit("lea r10, [rip+%s]", lab)
}

// markLD records that the value just produced is a long double at the claimed
// buffer (k, sl); r10 is left holding its address. The carrier is the shared
// resBig one, with resBigT distinguishing the type.
func (c *CG) markLD(t *frontend.Type, k, sl int) {
	c.resBig = true
	c.resBigT = t
	c.resBigK = k
	c.resBigSl = sl
	c.emit("lea r10, [rbp%+d]", c.tmpSlotBlock(k, sl))
	c.resTyp = frontend.TInt
	c.resW = 8
	c.resSigned = true
}

// tfTemp claims a fresh 16-byte temporary and returns its slot index, slot
// count and rbp offset (the base of the block).
func (c *CG) tfTemp() (int, int, int) {
	k, off := c.claimBig(tfWords)
	return k, tfWords, off
}

// tfCall3 calls a runtime entry point with three address arguments: the
// result buffer first, then the two operands. The call is emitted through
// callBigLib, which books c.need (the reachability sweep cannot see these
// calls) and keeps the frame and RSP discipline.
func (c *CG) tfCall3(name string, retOff, aOff, bOff int) {
	c.callBigLib(name, []bigArg{
		{addrOff: retOff}, {addrOff: aOff}, {addrOff: bOff},
	})
}

// tfOperand evaluates e and materialises its value -- converted to long
// double -- into a freshly claimed 16-byte temporary. Returns the slot index,
// slot count and rbp offset. Claims stay live until the caller's unwind,
// mirroring bigOperand: the operation's other operand and the result buffer
// are claimed after this one, and everything below the carried result is
// abandoned together.
func (c *CG) tfOperand(e frontend.Expr) (int, int, int, error) {
	k, sl, off := c.tfTemp()
	et := c.exprType(e)
	if isLD(et) {
		// Long double operand: a value expression leaves its address in r10
		// (lvalue-shaped) or in its own claimed buffer (computed); either
		// way the bytes are copied into the operand temporary.
		if _, err := c.genExprT(e); err != nil {
			return 0, 0, 0, err
		}
		if !c.resBig {
			return 0, 0, 0, fmt.Errorf("internal: long double operand did not produce a value address")
		}
		c.emit("lea r11, [rbp%+d]", off)
		c.copyBytes("r11", "r10", 16)
		// The copied value's own carrier is dead now (an lvalue has no
		// buffer to release; a computed one does).
		c.releaseResBig()
		c.emit("lea r10, [rbp%+d]", off)
		return k, sl, off, nil
	}
	// A 1.5L literal used directly ("LITS(1.0) / LITS(3.0)"): its .rdata
	// address IS the value.
	if nl, ok := e.(*frontend.NumLit); ok && nl.IsLongDouble {
		c.tfLiteralAddr(nl)
		c.emit("lea r11, [rbp%+d]", off)
		c.copyBytes("r11", "r10", 16)
		c.emit("lea r10, [rbp%+d]", off)
		return k, sl, off, nil
	}
	// A floating operand: a real double/float type, or a bare 0.5 literal
	// (which is in no type table -- exprType reports nil for it -- and is
	// recognised by its own Kind, exactly as the scalar NumLit path does).
	// An f-suffixed literal (0.1f, IsFloat) really is a single: it must go
	// through goc_tf_from_float, or its extra binary64 precision widens
	// into the binary128 and "(float)0.1" round-trips differently.
	floatLit, isSingle := false, false
	if nl, ok := e.(*frontend.NumLit); ok && nl.Kind == frontend.TDouble && !nl.IsLongDouble {
		floatLit = true
		isSingle = nl.IsFloat
	}
	if floatLit || (et != nil && (et.Kind == frontend.KDouble || et.Kind == frontend.KFloat)) {
		if _, err := c.genExprT(e); err != nil {
			return 0, 0, 0, err
		}
		if err := c.ensureType(frontend.TDouble); err != nil {
			return 0, 0, 0, err
		}
		// The widening entry point wants the SOURCE's own bit pattern: a
		// double operand hands over its 8 bytes, a float operand re-narrows
		// to the single it really is (a float value is always carried as a
		// double in the scalar carrier) and hands over 4.
		if isSingle || (et != nil && et.Kind == frontend.KFloat) {
			c.emit("cvtsd2ss xmm0, xmm0")
			c.emit("movd eax, xmm0")
			c.callBigLib("goc_tf_from_float", []bigArg{{addrOff: off}, {reg: "rax"}})
		} else {
			c.emit("movq rax, xmm0")
			c.callBigLib("goc_tf_from_double", []bigArg{{addrOff: off}, {reg: "rax"}})
		}
		c.emit("lea r10, [rbp%+d]", off)
		return k, sl, off, nil
	}
	// Integer operand (int, long long, bool, enum, ...): widen by the C
	// signedness. A nil type (a bare literal) converts as signed, matching
	// the "the checker usually inserted an explicit cast by now" reality --
	// and a signed widening is what a plain "7" means.
	if _, err := c.genExprT(e); err != nil {
		return 0, 0, 0, err
	}
	if err := c.ensureType(frontend.TInt); err != nil {
		return 0, 0, 0, err
	}
	fn := "goc_tf_from_ull"
	if et == nil || et.Signed {
		// A materialized signed int must be sign-extended first, or "-5"
		// widens to 4294967291.0 (the N17 trap, same as bi_from_i64).
		if c.resW == 4 && c.resSigned && !c.resPtr {
			c.emit("movsxd rax, eax")
		}
		fn = "goc_tf_from_ll"
	}
	c.callBigLib(fn, []bigArg{{addrOff: off}, {reg: "rax"}})
	c.emit("lea r10, [rbp%+d]", off)
	return k, sl, off, nil
}

// tfCopyFromR10 copies the 16 bytes at r10 into a fresh temporary (used when
// the source address must survive its own register window).
func (c *CG) tfCopyFromR10() (int, int, int, error) {
	k, sl, off := c.tfTemp()
	c.emit("lea r11, [rbp%+d]", off)
	c.copyBytes("r11", "r10", 16)
	return k, sl, off, nil
}

// tfScalarToLD converts the scalar value in rax (int) or xmm0 (double) to
// long double in a fresh temporary under the shared carrier. Used by
// ensureType's "scalar -> TF128" branch (an assignment's or call's implicit
// conversion, where the target type is known only by its class).
func (c *CG) tfScalarToLD() error {
	k, sl, off := c.tfTemp()
	if c.resTyp == frontend.TDouble {
		c.emit("movq rax, xmm0")
		c.callBigLib("goc_tf_from_double", []bigArg{{addrOff: off}, {reg: "rax"}})
	} else {
		// The carrier's signedness is all this class-only path knows: a
		// materialized unsigned int is zero-extended (correct for from_ull).
		fn := "goc_tf_from_ull"
		if c.resSigned {
			fn = "goc_tf_from_ll"
		}
		c.callBigLib(fn, []bigArg{{addrOff: off}, {reg: "rax"}})
	}
	c.markLD(frontend.LongDoubleType(), k, sl)
	return nil
}

// genTFValue emits a value-typed long double expression: the value's address
// is left in r10 and the resBig carrier describes it.
func (c *CG) genTFValue(e frontend.Expr, t *frontend.Type) (frontend.CType, error) {
	switch n := e.(type) {
	case *frontend.NumLit:
		if n.IsLongDouble {
			c.tfLiteralAddr(n)
			c.resBig = true
			c.resBigT = t
			c.resBigK = 0
			c.resBigSl = 0
			c.resTyp = frontend.TInt
			c.resW = 8
			c.resSigned = true
			return frontend.TInt, nil
		}
	case *frontend.Binary:
		return c.genTFBinary(n)
	case *frontend.CondExpr:
		// "cond ? a : b" with long double arms. Each arm's value lives at
		// its own address (an lvalue's slot, or a temporary a computed one
		// claimed), so the two paths cannot simply leave r10 pointing at
		// their own buffer -- the merge would name one and the other path
		// would never write it. Both arms therefore copy into ONE buffer
		// claimed before the branch.
		if err := c.genTruth(n.Cond); err != nil {
			return frontend.TInt, err
		}
		rk, rsl, roff := c.tfTemp()
		lElse := c.newLabel("ldelse")
		lEnd := c.newLabel("ldendif")
		c.emit("cmp rax, 0")
		c.emit("je %s", lElse)
		_, _, aoff, err := c.tfOperand(n.Then)
		if err != nil {
			return frontend.TInt, err
		}
		c.emit("lea r10, [rbp%+d]", roff)
		c.emit("lea r11, [rbp%+d]", aoff)
		c.copyBytes("r10", "r11", 16)
		c.tmpDepth -= tfWords // the arm's temporary is dead; the else arm reuses it
		c.emit("jmp %s", lEnd)
		c.line(lElse + ":\n")
		_, _, boff, err := c.tfOperand(n.Else)
		if err != nil {
			return frontend.TInt, err
		}
		c.emit("lea r10, [rbp%+d]", roff)
		c.emit("lea r11, [rbp%+d]", boff)
		c.copyBytes("r10", "r11", 16)
		c.tmpDepth -= tfWords
		c.line(lEnd + ":\n")
		c.markLD(t, rk, rsl)
		return frontend.TInt, nil
	case *frontend.CastExpr:
		return c.genTFCast(n)
	case *frontend.Unary:
		switch n.Op {
		case "-":
			_, _, aoff, err := c.tfOperand(n.E)
			if err != nil {
				return frontend.TInt, err
			}
			rk, rsl, roff := c.tfTemp()
			// neg takes one operand; the third address is dead but keeps the
			// emission uniform with the two-operand helpers.
			c.tfCall3("goc_tf_neg", roff, aoff, aoff)
			c.markLD(t, rk, rsl)
			return frontend.TInt, nil
		case "!":
			// A long double is false only when it compares equal to zero; a
			// NaN is not zero. The result is an int, not a long double.
			if err := c.tfTruthIntoRax(n.E); err != nil {
				return frontend.TInt, err
			}
			c.emit("xor rax, 1")
			return frontend.TInt, nil
		case "+":
			return c.genTFValue(n.E, t)
		}
	case *frontend.IncDecExpr:
		return c.genTFIncDec(n)
	case *frontend.VaArgExpr:
		// va_arg(ap, long double) already leaves the shared by-address
		// carrier behind (genVaArg's ld branch: a pointer out of the
		// cursor, the 16 bytes it names copied into a fresh temporary).
		// genExprT cannot be used here -- its TF intercept would call
		// genTFValue again and loop.
		if _, err := c.genVaArg(n); err != nil {
			return frontend.TInt, err
		}
		return frontend.TInt, nil
	case *frontend.Call:
		return c.genBigFromCall(func() (frontend.CType, error) { return c.genCall(n.Name, nil, nil, n.Args) }, t)
	case *frontend.IndirectCall:
		return c.genBigFromCall(func() (frontend.CType, error) { return c.genIndirectCall(n) }, t)
	}
	// Default: lvalue-shaped (Ident / Member / Index / *p / CompoundLit).
	if err := c.genLValue(e); err != nil {
		return frontend.TInt, err
	}
	c.resBig = true
	c.resBigT = t
	c.resBigK = 0
	c.resBigSl = 0
	c.resTyp = frontend.TInt
	c.resW = 8
	c.resSigned = true
	return frontend.TInt, nil
}

// genTFBinary emits an arithmetic or comparison operation with at least one
// long double operand. Both operands are materialised as long doubles (the
// usual arithmetic conversions happen here, one operand at a time), then one
// runtime call does the work.
func (c *CG) genTFBinary(n *frontend.Binary) (frontend.CType, error) {
	entryDepth := c.tmpDepth
	switch n.Op {
	case "+", "-", "*", "/":
		_, _, aoff, err := c.tfOperand(n.L)
		if err != nil {
			return frontend.TInt, err
		}
		_, _, boff, err := c.tfOperand(n.R)
		if err != nil {
			return frontend.TInt, err
		}
		rk, rsl, roff := c.tfTemp()
		fn := map[string]string{
			"+": "goc_tf_add", "-": "goc_tf_sub",
			"*": "goc_tf_mul", "/": "goc_tf_div",
		}[n.Op]
		c.tfCall3(fn, roff, aoff, boff)
		c.markLD(frontend.LongDoubleType(), rk, rsl)
		return frontend.TInt, nil

	case "==", "!=", "<", "<=", ">", ">=":
		_, _, aoff, err := c.tfOperand(n.L)
		if err != nil {
			return frontend.TInt, err
		}
		_, _, boff, err := c.tfOperand(n.R)
		if err != nil {
			return frontend.TInt, err
		}
		// goc_tf_cmp answers -1 / 0 / 1 for ordered pairs and 2 for an
		// unordered one (any NaN operand); it returns an int in rax. An int
		// result is materialized (low 32 bits valid, high bits irrelevant),
		// so sign-extend before any 64-bit compare against -1.
		c.callBigLib("goc_tf_cmp", []bigArg{
			{addrOff: aoff}, {addrOff: boff},
		})
		c.emit("movsxd rax, eax")
		c.tmpDepth = entryDepth
		// The one answer C's six operators disagree about is 2 (unordered):
		// every comparison involving a NaN is false except "!=". ">=" uses an
		// unsigned test so the -1 (a huge unsigned) is excluded along with 2.
		switch n.Op {
		case "==":
			c.emit("cmp rax, 0")
			c.emit("sete al")
		case "!=":
			c.emit("cmp rax, 0")
			c.emit("setne al")
		case "<":
			c.emit("cmp rax, -1")
			c.emit("sete al")
		case ">":
			c.emit("cmp rax, 1")
			c.emit("sete al")
		case "<=":
			c.emit("cmp rax, 0")
			c.emit("setle al")
		case ">=":
			c.emit("cmp rax, 2")
			c.emit("setb al")
		}
		c.emit("movzx rax, al")
		c.resBig = false
		c.resBigT = nil
		c.resTyp = frontend.TInt
		c.resSigned = true
		c.resW = 4
		return frontend.TInt, nil
	}
	return frontend.TInt, fmt.Errorf("operator %q is not defined for long double", n.Op)
}

// tfTruthIntoRax evaluates e as a condition: rax becomes 1 iff the long double
// is not a zero (a NaN is not zero, so it is true). Used by "!", by genTruth's
// long double branch, and by bool casts. Leaves no resBig carrier.
func (c *CG) tfTruthIntoRax(e frontend.Expr) error {
	entryDepth := c.tmpDepth
	_, _, aoff, err := c.tfOperand(e)
	if err != nil {
		return err
	}
	// The zero operand: a fresh 16-byte temporary filled with zeros.
	_, _, zoff := c.tfTemp()
	c.emit("lea r11, [rbp%+d]", zoff)
	c.zeroBytes("r11", 16)
	c.callBigLib("goc_tf_cmp", []bigArg{
		{addrOff: aoff}, {addrOff: zoff},
	})
	c.emit("movsxd rax, eax")
	c.emit("cmp rax, 0")
	c.emit("setne al")
	c.emit("movzx rax, al")
	c.tmpDepth = entryDepth
	c.resBig = false
	c.resBigT = nil
	c.resTyp = frontend.TInt
	c.resW = 4
	c.resSigned = true
	return nil
}

// genTFCast emits a cast to or from long double.
func (c *CG) genTFCast(n *frontend.CastExpr) (frontend.CType, error) {
	if isLD(n.Typ) {
		// To long double: materialise the operand, converted.
		k, sl, _, err := c.tfOperand(n.E)
		if err != nil {
			return frontend.TInt, err
		}
		c.markLD(n.Typ, k, sl)
		return frontend.TInt, nil
	}
	// From long double to a narrower type: evaluate to an address, convert
	// through the runtime, land in the ordinary scalar carrier.
	if _, err := c.genExprT(n.E); err != nil {
		return frontend.TInt, err
	}
	if c.resBig && isLD(c.resBigT) {
		switch n.Typ.Kind {
		case frontend.KDouble, frontend.KFloat:
			c.callBigLib("goc_tf_to_double", []bigArg{{reg: "r10"}})
			c.emit("movq xmm0, rax")
			if n.Typ.Kind == frontend.KFloat {
				// A float result is carried as the double it widens to (the
				// scalar float model), so re-widen after the narrow.
				c.emit("cvtsd2ss xmm0, xmm0")
				c.emit("cvtss2sd xmm0, xmm0")
			}
			c.releaseResBig()
			c.resTyp = frontend.TDouble
			c.resW = 8
			c.resSigned = true
			return frontend.TDouble, nil
		case frontend.KBool:
			// C: any nonzero value converts to 1, and a NaN is nonzero.
			c.callBigLib("goc_tf_to_ll", []bigArg{{reg: "r10"}})
			c.emit("cmp rax, 0")
			c.emit("setne al")
			c.emit("movzx rax, al")
			c.releaseResBig()
			c.resTyp = frontend.TInt
			c.resW = 4
			c.resSigned = false
			return frontend.TInt, nil
		default:
			// Integers: the signedness of the TARGET decides which entry
			// point runs -- the two disagree about a negative value and
			// about a NaN, so "(unsigned long long)x" and "(long long)x"
			// are different calls, exactly as on the LLVM side.
			fn := "goc_tf_to_ull"
			if n.Typ.Signed {
				fn = "goc_tf_to_ll"
			}
			c.callBigLib(fn, []bigArg{{reg: "r10"}})
			c.releaseResBig()
			c.resTyp = frontend.TInt
			c.resW = 8
			c.resSigned = n.Typ.Signed
			// A narrower integer target keeps only its low bits, re-extended
			// with the target's signedness (the same rule scalar int casts
			// follow; 8-byte targets need nothing).
			if n.Typ.Kind == frontend.KInt && n.Typ.Width > 0 && n.Typ.Width < 8 {
				c.extendInt(n.Typ.Width, n.Typ.Signed)
			}
			return frontend.TInt, nil
		}
	}
	// The operand was not a long double value (a stale carrier): fall back
	// to the scalar conversion path.
	if err := c.ensureType(n.Typ.Class()); err != nil {
		return frontend.TInt, err
	}
	c.resTyp = n.Typ.Class()
	return c.resTyp, nil
}

// genTFIncDec emits ++/-- on a long double lvalue. Postfix yields the OLD
// value from a claimed buffer; prefix yields the operand itself as an lvalue.
// The lvalue's address is parked in a frame slot across the helper call:
// the call clobbers every register that could have held it.
func (c *CG) genTFIncDec(n *frontend.IncDecExpr) (frontend.CType, error) {
	t := c.exprType(n.E)
	if !isLD(t) {
		return frontend.TInt, fmt.Errorf("internal: ++/-- on non-long double")
	}
	entryDepth := c.tmpDepth
	// The addend: a materialised 1.0L (its encoding is 0x3fff8...0).
	_, _, oneOff, err := c.tfOperand(&frontend.NumLit{
		IsLongDouble: true,
		F128:         frontend.Float128{Hi: 0x3fff000000000000},
	})
	if err != nil {
		return frontend.TInt, err
	}
	// Park the lvalue's address before the result buffer is claimed.
	if err := c.genLValue(n.E); err != nil {
		return frontend.TInt, err
	}
	c.emit("mov r11, r10")
	c.tmpDepth++
	lvOff := c.tmpSlot(c.tmpDepth) // an rbp OFFSET, not a slot index
	c.emit("mov [rbp%+d], r11", lvOff)
	resK, resSl := 0, 0
	if !n.Prefix {
		// Copy the old value into a result buffer while r10 is still valid.
		var resOff int
		resK, _, resOff = c.tfTemp()
		resSl = tfWords
		c.emit("lea r11, [rbp%+d]", c.tmpSlotBlock(resK, resSl))
		c.copyBytes("r11", "r10", 16)
		_ = resOff
	}
	name := "goc_tf_add"
	if n.Op == "--" {
		name = "goc_tf_sub"
	}
	// The operation writes through the lvalue's own address: destination =
	// lvalue, left = lvalue, right = the addend. The parked slot CONTAINS
	// the lvalue's address, so the first two arguments load it (valOff =
	// mov), they do not point at the slot itself.
	c.callBigLib(name, []bigArg{
		{valOff: lvOff}, {valOff: lvOff}, {addrOff: oneOff},
	})
	if !n.Prefix {
		c.markLD(t, resK, resSl)
	} else {
		c.tmpDepth = entryDepth
		// Prefix: the value IS the lvalue; hand out its address. It must be
		// re-loaded because the call clobbered r10.
		c.emit("mov r10, [rbp%+d]", lvOff)
		c.resBig = true
		c.resBigT = t
		c.resBigK = 0
		c.resBigSl = 0
		c.resTyp = frontend.TInt
		c.resW = 8
		c.resSigned = true
	}
	return frontend.TInt, nil
}

// genTFCompoundAssign emits "x op= y" on a long double lvalue: the lvalue is
// evaluated exactly once, the operation runs against the old value, and the
// result is stored back. The result is NOT carried as a value (the assignment
// expression's value is undefined for aggregates, as for structs).
func (c *CG) genTFCompoundAssign(n *frontend.AssignExpr) (frontend.CType, error) {
	lt := c.exprType(n.Lhs)
	entryDepth := c.tmpDepth
	fn := map[string]string{
		"+": "goc_tf_add", "-": "goc_tf_sub",
		"*": "goc_tf_mul", "/": "goc_tf_div",
	}[n.Op]
	if fn == "" {
		return frontend.TInt, fmt.Errorf("operator %q is not defined for long double", n.Op)
	}
	// Park the lvalue's address; the right operand may contain calls.
	if err := c.genLValue(n.Lhs); err != nil {
		return frontend.TInt, err
	}
	c.emit("mov r11, r10")
	c.tmpDepth++
	lvSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov [rbp%+d], r11", lvSlot)
	// The right operand, converted to long double.
	_, _, boff, err := c.tfOperand(n.Rhs)
	if err != nil {
		return frontend.TInt, err
	}
	// The left value, copied into a temporary: the helper writes its result
	// to a separate buffer, and the store-back rewrites the lvalue.
	c.emit("mov r10, [rbp%+d]", lvSlot)
	_, _, aoff, err := c.tfCopyFromR10()
	if err != nil {
		return frontend.TInt, err
	}
	rk, rsl, roff := c.tfTemp()
	c.tfCall3(fn, roff, aoff, boff)
	// Store back into the lvalue.
	c.emit("mov r11, [rbp%+d]", lvSlot) // the lvalue address
	c.emit("lea r10, [rbp%+d]", c.tmpSlotBlock(rk, rsl))
	c.copyBytes("r11", "r10", 16)
	c.tmpDepth = entryDepth
	c.resBig = false
	c.resBigT = nil
	c.resTyp = frontend.TInt
	c.resW = 8
	c.resSigned = true
	return lt.Class(), nil
}
