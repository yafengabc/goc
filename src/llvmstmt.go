package main

// Statements and expressions, in LLVM IR.
//
// C's grammar and LLVM's are close but not aligned: a C statement list has no
// notion of basic blocks, while every LLVM block must end in a terminator. The
// translation therefore has to invent blocks wherever control can arrive from
// somewhere else, and it has to be careful that a block is never left open -- an
// unterminated block is rejected by LLVM with a message that points at the block
// rather than at the construct responsible.
//
// The one rule that keeps this manageable: every path out of a statement leaves
// the current block closed, so appending the next statement simply opens a fresh
// block. That is what e.line does when it sees e.closed.

import "strings"

// --- statements -------------------------------------------------------------

func (e *irEmitter) stmt(s Stmt) {
	switch n := s.(type) {
	case nil:
		return
	case *Block:
		e.tr.pushScope()
		for _, x := range n.Stmts {
			e.stmt(x)
		}
		e.tr.popScope()
	case *DeclList:
		for _, d := range n.Decls {
			e.stmt(d)
		}
	case *DeclStmt:
		e.localDecl(n)
	case *ExprStmt:
		e.discard(n.E)
	case *ReturnStmt:
		e.doReturn(n)
	case *IfStmt:
		e.doIf(n)
	case *WhileStmt:
		e.doWhile(n)
	case *DoWhileStmt:
		e.doDoWhile(n)
	case *ForStmt:
		e.doFor(n)
	case *BreakStmt:
		if n := len(e.breakTo); n > 0 {
			e.term("br label %%%s", e.breakTo[n-1])
		}
	case *ContinueStmt:
		if n := len(e.continueTo); n > 0 {
			e.term("br label %%%s", e.continueTo[n-1])
		}
	case *LabelStmt:
		e.doLabel(n)
	case *GotoStmt:
		e.doGoto(n)
	case *SwitchStmt:
		e.doSwitch(n)
	case *AsmStmt:
		// Inline assembly is goa assembler text with no IR equivalent; the
		// eligibility check keeps such functions off this path.
	}
}

// localDecl gives a local its stack slot and runs the initialiser.
func (e *irEmitter) localDecl(d *DeclStmt) {
	if d.Name == "" {
		return // a bare "struct S;" declares nothing
	}
	ty := d.Typ
	if ty == nil {
		return
	}
	lty := e.ty(ty)
	uid := e.tr.declareVar(d.Name, varInfo{typ: ty})
	slot := e.slotFor(uid, lty)
	e.tr.declUID[d] = uid
	if d.Init != nil {
		if bi, ok := d.Init.(*BraceInit); ok {
			e.storeBrace(bi, ty, slot)
			return
		}
		// A string initialising a char array copies the bytes; one
		// initialising a char pointer stores the address. goc's parser already
		// built the right node for each case, but they arrive here as the same
		// expression, so the target type decides.
		if sl, ok := d.Init.(*StrLit); ok && ty.Kind == KArr {
			e.storeString(sl, ty, slot)
			return
		}
		v := e.eval(d.Init)
		e.store(slot, e.coerce(v, ty))
	}
}

// doReturn emits a return, converting the value to the function's return type.
func (e *irEmitter) doReturn(n *ReturnStmt) {
	if n.E == nil || e.retTy == "void" {
		e.term("ret void")
		return
	}
	v := e.eval(n.E)
	// The value's own IR type decides, not the static one: a comparison or a
	// short-circuit && / || produces an i1 even though C types it as int, so
	// "static type == return type" can hold while the value is still i1 and
	// `ret i32 %v` is rejected.
	if e.ty(v.ty) != e.retTy {
		v.op = e.convertTo(v.op, v.ty, e.retTy)
	}
	e.term("ret %s %s", e.retTy, v.op)
}

// doIf lowers `if (c) A else B`. The condition is materialised into a
// temporary first so the comparison is not repeated in each arm.
func (e *irEmitter) doIf(n *IfStmt) {
	cond := e.cond(n.Cond)
	thenL := e.newLabel()
	doneL := e.newLabel()
	var elseL string
	if n.Else != nil {
		elseL = e.newLabel()
	}
	e.term("br i1 %s, label %%%s, label %%%s", cond, thenL, pick(elseL, doneL))

	e.blockLabel(thenL)
	e.flushPendingLabels()
	e.stmt(n.Then)
	e.term("br label %%%s", doneL)

	if n.Else != nil {
		e.blockLabel(elseL)
		e.flushPendingLabels()
		e.stmt(n.Else)
		e.term("br label %%%s", doneL)
	}
	e.blockLabel(doneL)
	e.flushPendingLabels()
}

// doWhile lowers `while (c) B`, testing before each iteration.
func (e *irEmitter) doWhile(n *WhileStmt) {
	testL := e.newLabel()
	bodyL := e.newLabel()
	doneL := e.newLabel()
	e.term("br label %%%s", testL)

	e.blockLabel(testL)
	e.flushPendingLabels()
	cond := e.cond(n.Cond)
	e.term("br i1 %s, label %%%s, label %%%s", cond, bodyL, doneL)

	e.blockLabel(bodyL)
	e.breakTo = append(e.breakTo, doneL)
	e.continueTo = append(e.continueTo, testL)
	e.stmt(n.Body)
	e.breakTo = e.breakTo[:len(e.breakTo)-1]
	e.continueTo = e.continueTo[:len(e.continueTo)-1]
	e.term("br label %%%s", testL)

	e.blockLabel(doneL)
	e.flushPendingLabels()
}

// doDoWhile lowers `do B while (c)`, running the body before the first test and
// sending continue to the condition rather than to the top of the body.
func (e *irEmitter) doDoWhile(n *DoWhileStmt) {
	bodyL := e.newLabel()
	testL := e.newLabel()
	doneL := e.newLabel()
	e.term("br label %%%s", bodyL)

	e.blockLabel(bodyL)
	e.breakTo = append(e.breakTo, doneL)
	e.continueTo = append(e.continueTo, testL)
	e.stmt(n.Body)
	e.breakTo = e.breakTo[:len(e.breakTo)-1]
	e.continueTo = e.continueTo[:len(e.continueTo)-1]
	e.term("br label %%%s", testL)

	e.blockLabel(testL)
	e.flushPendingLabels()
	cond := e.cond(n.Cond)
	e.term("br i1 %s, label %%%s, label %%%s", cond, bodyL, doneL)

	e.blockLabel(doneL)
	e.flushPendingLabels()
}

// doFor lowers the three-clause `for`. The initialiser runs once, the condition
// is tested before each iteration, and the post expression runs at the bottom --
// so `continue` has to reach the post expression, not the top.
func (e *irEmitter) doFor(n *ForStmt) {
	e.tr.pushScope()
	defer e.tr.popScope()
	if n.Init != nil {
		e.stmt(n.Init)
	}
	testL := e.newLabel()
	bodyL := e.newLabel()
	postL := e.newLabel()
	doneL := e.newLabel()
	e.term("br label %%%s", testL)

	e.blockLabel(testL)
	e.flushPendingLabels()
	if n.Cond != nil {
		cond := e.cond(n.Cond)
		e.term("br i1 %s, label %%%s, label %%%s", cond, bodyL, doneL)
	} else {
		e.term("br label %%%s", bodyL)
	}

	e.blockLabel(bodyL)
	e.breakTo = append(e.breakTo, doneL)
	e.continueTo = append(e.continueTo, postL)
	e.stmt(n.Body)
	e.breakTo = e.breakTo[:len(e.breakTo)-1]
	e.continueTo = e.continueTo[:len(e.continueTo)-1]
	e.term("br label %%%s", postL)

	e.blockLabel(postL)
	e.flushPendingLabels()
	if n.Post != nil {
		e.discard(n.Post)
	}
	e.term("br label %%%s", testL)

	e.blockLabel(doneL)
	e.flushPendingLabels()
}

// doSwitch lowers a switch. C allows fall-through, and keeping the case labels
// as ordinary blocks preserves that for free: control simply runs on into the
// next one.
func (e *irEmitter) doSwitch(n *SwitchStmt) {
	sv := e.rvalue(n.Src)
	sty := e.tr.exprType(n.Src)
	// LLVM's switch takes an i32. A conversion is needed only when the
	// controlling expression is not already one: "trunc i32 %v to i32" is a
	// cast from a type to itself, which LLVM rejects, and a trunc is wrong in
	// the other direction too -- a char (i8) must be zero-extended, not
	// truncated. convert picks the right one from the two widths. The i32 case
	// uses the operand directly; a copy would need a real instruction, and a
	// bare "%t = %t" is not one.
	if e.ty(sty) != "i32" {
		sv = e.convert(sv, sty, IntType())
	}

	doneL := e.newLabel()
	// Collect the cases first: LLVM's switch needs every target up front, but
	// the blocks are emitted as the body is walked.
	var cases []switchCase
	defIdx := -1
	cur := -1
	e.tr.pushScope()
	for _, st := range n.Body.Stmts {
		switch c := st.(type) {
		case *CaseStmt:
			// A case label applies to the statements that follow it, up to the
			// next case or default. The block it opens is created here so the
			// switch can name it before the body has been walked.
			cases = append(cases, switchCase{val: c.Val, l: e.newLabel()})
			// An index, not a pointer: append may move the backing array, and a
			// pointer taken before the next append would then name the wrong
			// case -- which showed up as a default arm's statements being
			// emitted under another arm's label.
			cur = len(cases) - 1
		case *DefaultStmt:
			// A default arm is a branch target like any other, and it keeps
			// its own statements: they belong to the default block, not to
			// whichever case happens to precede it.
			cases = append(cases, switchCase{val: -1, l: e.newLabel()})
			defIdx = len(cases) - 1
			cur = defIdx
		default:
			if cur < 0 {
				continue // statements before any label are unreachable
			}
			if cases[cur].stmt == nil {
				cases[cur].stmt = st
			} else {
				cases[cur].stmt = &Block{Stmts: []Stmt{cases[cur].stmt, st}}
			}
		}
	}
	e.tr.popScope()

	var real []switchCase
	for _, c := range cases {
		if c.val >= 0 {
			real = append(real, c)
		}
	}
	def := ""
	if defIdx >= 0 {
		def = cases[defIdx].l
	}

	e.term("switch i32 %s, label %%%s [ %s ]", sv, pick(def, doneL), caseArms(real))

	for i := range cases {
		c := &cases[i]
		e.blockLabel(c.l)
		if c.stmt != nil {
			e.stmt(c.stmt)
		}
		// Every arm needs a terminator, including one whose statements all
		// ended in a jump: LLVM requires each basic block to end in one, and
		// a label followed straight by the next label is not a block at all
		// ("expected instruction opcode").
		e.term("br label %%%s", doneL)
	}
	e.blockLabel(doneL)
	e.flushPendingLabels()
}

// switchCase pairs a case value with the block that handles it and the
// statements that follow it in the source.
type switchCase struct {
	val  int
	l    string
	stmt Stmt
}

func caseArms(cases []switchCase) string {
	if len(cases) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range cases {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString("i32 " + itoa(c.val) + ", label %" + c.l)
	}
	return b.String()
}

// doLabel attaches a C label to the block that follows it.
func (e *irEmitter) doLabel(n *LabelStmt) {
	e.pendingLabels = append(e.pendingLabels, n.Name)
	e.stmt(n.Stmt)
}

// flushPendingLabels binds any C labels queued by doLabel to the current block,
// which is where control must arrive.
func (e *irEmitter) flushPendingLabels() {
	if len(e.pendingLabels) == 0 {
		return
	}
	l := e.currentLabel()
	for _, cl := range e.pendingLabels {
		e.userLabels[cl] = l
	}
	e.pendingLabels = nil
}

// currentLabel returns the name of the block being written, which is the last
// label emitted.
func (e *irEmitter) currentLabel() string {
	// The emitter writes labels as it goes; the most recent one is the block
	// currently open. Tracking it explicitly is cheaper than re-parsing.
	return e.lastLabel
}

func pick(alt, def string) string {
	if alt != "" {
		return alt
	}
	return def
}
