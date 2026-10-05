package compiler

// livenessDSE is the control-flow-aware replacement surface for dead-store
// elimination. The linear deadStores pass only sees a straight window: any
// label, call or branch resets it, so a store whose only reader sits in
// another basic block -- or that is dead because no path reads it at all --
// survives. This pass builds a real CFG per function, runs a backward
// liveness to a fixpoint, and deletes stores that no reachable path reads.
//
// What counts as a variable here is a *direct frame slot*: the exact shapes
// goc emits for every local and every inlined argument spill, "[rbp-N]" and
// "[rbp+N]". The analysis is deliberately conservative along the same lines
// as constProp, because guessing x86 semantics is how this project ended up
// retiring a hand-written interpreter:
//
//   - Anything that could alias a slot kills the analysis for the whole
//     function: indirect memory operands ("[r10]", "[rax+8]"), lea over
//     anything but a direct slot or a rip-relative global, inline __asm
//     lines, an unmodelled opcode touching memory, a branch target outside
//     the function, and a segment that does not end in ret. Functions with
//     pointer traffic therefore opt out entirely; plain scalar code -- the
//     kind inlining produces -- opts in.
//   - A `lea reg, [rbp-N]` makes slot N *escaped*: its address now exists in
//     a register, so any indirect access downstream may reach it through
//     that pointer. Escaped slots are permanently live and their stores are
//     never deleted. Un-escaped slots have no address anywhere in the
//     program, which is exactly why an indirect access through a
//     register-held pointer cannot reach them: the only way a slot address
//     comes to exist is a lea, and every lea is accounted for.
//   - Narrow accesses (size keywords, 32/16/8-bit registers, the *ss family,
//     movzx/movsx family) poison the slot: its stores stay, but its uses
//     still keep earlier stores alive.
//   - A call reads every escaped slot (a callee may receive that pointer)
//     and nothing else: callee frames live below the caller's rsp, so a
//     callee cannot write our un-escaped slots. This is what lets stores
//     dead across a call finally go.
//   - ret reads nothing: the frame is dead on return, and every legitimate
//     reader (a callee-save restore) is an explicit load the model sees.
//     leave, syscall and anything else unmodelled reads every slot seen in
//     the function. Stores feeding a callee-save save/restore are never
//     deleted.
//   - Immediate stores define their slot (killing earlier live-ness) but are
//     themselves never deleted: their encoded width is not modelled here.
//
// The pass runs at -O1 after deadStores. Everything it removes is a strict
// superset situation: a store whose value no path ever reads cannot affect
// observable behaviour, so -O0 output stays byte-identical by construction.

import (
	"strings"
)

// gprWidth maps every integer register spelling to its width in bytes.
var gprWidth = map[string]int{}

func init() {
	for _, r := range []string{"rax", "rbx", "rcx", "rdx", "rsi", "rdi", "rbp", "rsp",
		"r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"} {
		gprWidth[r] = 8
	}
	for _, r := range []string{"eax", "ebx", "ecx", "edx", "esi", "edi",
		"r8d", "r9d", "r10d", "r11d", "r12d", "r13d", "r14d", "r15d"} {
		gprWidth[r] = 4
	}
	for _, r := range []string{"ax", "bx", "cx", "dx", "si", "di",
		"r8w", "r9w", "r10w", "r11w", "r12w", "r13w", "r14w", "r15w"} {
		gprWidth[r] = 2
	}
	for _, r := range []string{"al", "bl", "cl", "dl", "sil", "dil", "bpl", "spl",
		"r8b", "r9b", "r10b", "r11b", "r12b", "r13b", "r14b", "r15b",
		"ah", "bh", "ch", "dh"} {
		gprWidth[r] = 1
	}
}

// sizeKw gives the byte width of x86 size keywords. Absence means the
// operand carries no explicit width keyword.
var sizeKw = map[string]int{
	"byte": 1, "word": 2, "dword": 4, "qword": 8,
}

// stripSize removes a leading size keyword from an operand, returning the
// keyword's width (0 when absent) and the remainder.
func stripSize(s string) (int, string) {
	for kw, w := range sizeKw {
		if strings.HasPrefix(s, kw+" ") {
			return w, s[len(kw)+1:]
		}
	}
	return 0, s
}

// slotOf returns the operand itself when it is a direct frame slot -- the
// exact "[rbp-N]" / "[rbp+N]" shapes goc emits for locals and inlined
// spills -- and "" for anything else.
func slotOf(s string) string {
	if !strings.HasPrefix(s, "[rbp") || len(s) < 7 || s[len(s)-1] != ']' {
		return ""
	}
	if s[4] != '-' && s[4] != '+' {
		return ""
	}
	for i := 5; i < len(s)-1; i++ {
		if s[i] < '0' || s[i] > '9' {
			return ""
		}
	}
	return s
}

// isImmediate reports whether an operand is a plain integer literal.
func isImmediate(s string) bool {
	body := strings.TrimPrefix(s, "-")
	if body == "" {
		return false
	}
	for i := 0; i < len(body); i++ {
		if body[i] < '0' || body[i] > '9' {
			return false
		}
	}
	return true
}

// calleeSaveRegs are the registers whose save/restore stores must never be
// deleted: the prologue spills them and the epilogue reads them back.
var calleeSaveRegs = map[string]bool{
	"rbx": true, "r12": true, "r13": true, "r14": true, "r15": true,
}

// opMemWidth derives the memory access width of an opcode from its shape
// when no size keyword is present. 0 means "not derivable"; the caller then
// falls back to 4 (narrow, poisoning) to stay safe.
func opMemWidth(op string, ops []string) int {
	switch op {
	case "movsd", "addsd", "subsd", "mulsd", "divsd", "sqrtsd",
		"ucomisd", "comisd", "maxsd", "minsd", "andpd", "orpd", "xorpd":
		return 8
	case "movss", "addss", "subss", "mulss", "divss", "sqrtss",
		"ucomiss", "comiss":
		return 4
	case "movzx", "movsx", "movsxd", "movslq", "movzbl", "movzbw", "movzbq",
		"movzwl", "movzwq", "movsbl", "movsbw", "movsbq", "movswl", "movswq":
		return 1
	}
	for _, o := range ops {
		if w, ok := gprWidth[o]; ok {
			return w
		}
	}
	return 0
}

// fxEffects are the frame-slot effects of one instruction, as collected by
// scanFX in the first pass over a function segment.
type fxEffects struct {
	use     []string // direct slots whose stored value this instruction reads
	def     string   // direct slot this instruction overwrites ("" when none)
	store   bool     // def is a deletable full-width store
	csStore bool     // the store's source is a callee-save register
	unknown bool     // unmodelled: reads every slot seen in the function
	jump    string   // branch target (jmp/jcc), without colon
	cond    bool     // jump is conditional (also falls through)
	ret     bool     // function return
	call    bool     // call: reads the escape set, no CFG edge
}

// opWritesDst reports whether the opcode overwrites its first operand. For
// memory destinations that means a write to the slot (RMW ops read it too).
func opWritesDst(op string) bool {
	switch op {
	case "mov", "movsd", "movss", "movzx", "movsx", "movsxd", "movslq",
		"add", "sub", "adc", "sbb", "and", "or", "xor",
		"shl", "sal", "sar", "shr", "neg", "not", "inc", "dec", "pop":
		return true
	}
	return strings.HasPrefix(op, "set")
}

// opReadsDst reports whether the opcode also reads its first operand when
// that operand is memory: the read-modify-write family, and comparisons.
func opReadsDst(op string) bool {
	switch op {
	case "add", "sub", "adc", "sbb", "and", "or", "xor",
		"shl", "sal", "sar", "shr", "neg", "not", "inc", "dec",
		"cmp", "test", "bt", "bts", "btr", "btc":
		return true
	}
	return false
}

// scanFX walks one function segment once, classifying every instruction and
// collecting the function-wide facts the analysis needs: the escaped slot
// set, every slot ever seen, and per-instruction effects. It returns nil
// when the segment must be left untouched (any aliasing risk, inline asm,
// or shape the analysis does not model).
func scanFX(seg []Inst) ([]fxEffects, map[string]bool, map[string]bool) {
	fxs := make([]fxEffects, len(seg))
	esc := map[string]bool{}   // slots whose address exists (lea) or are poisoned
	known := map[string]bool{} // every direct slot seen
	for i, in := range seg {
		switch in.Kind {
		case instRaw:
			return nil, nil, nil // inline __asm: off limits
		case instLabel:
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		fx := &fxs[i]
		if !ok {
			t := strings.TrimSpace(strings.TrimPrefix(in.Text, "\t"))
			switch t {
			case "cqo", "cqto", "cdq", "cltd", "cdqe", "cltq", "cwde", "cwtl", "cbtw":
				// sign-extension/nop-like: register-only, touches no slot
			case "ret":
				fx.ret = true
				// ret reads no frame slot: the frame is dead on
				// return. Every legitimate reader (a callee-save
				// restore) is an explicit load the model sees.
			default:
				fx.unknown = true // leave, syscall, anything unmodelled
			}
			continue
		}
		op := pl.op
		switch {
		case op == "call":
			fx.call = true
			continue
		case op == "jmp" || (strings.HasPrefix(op, "j") && op != "jmp"):
			if len(pl.operands) != 1 || strings.Contains(pl.operands[0], "[") {
				return nil, nil, nil // indirect branch: unmodelled
			}
			fx.jump = strings.TrimSuffix(strings.TrimSpace(pl.operands[0]), ":")
			fx.cond = op != "jmp"
			continue
		case op == "ret":
			fx.ret = true
			// No frame-slot reads: see the parse-failure branch above.
			continue
		case op == "lea":
			if len(pl.operands) != 2 {
				return nil, nil, nil
			}
			_, rest := stripSize(pl.operands[1])
			switch {
			case strings.HasPrefix(rest, "[rip+"):
				// data-segment address: harmless
			default:
				sl := slotOf(rest)
				if sl == "" {
					return nil, nil, nil // address arithmetic over the frame
				}
				esc[sl] = true
				known[sl] = true
			}
			continue
		}
		// General instruction: scan every memory operand.
		for j, o := range pl.operands {
			w, rest := stripSize(o)
			if !strings.HasPrefix(rest, "[") {
				continue // register or immediate
			}
			if strings.HasPrefix(rest, "[rip+") || strings.HasPrefix(rest, "[rsp") {
				continue // data segment / outgoing shadow space: not our slots
			}
			sl := slotOf(rest)
			if sl == "" {
				return nil, nil, nil // indirect access: may alias anything
			}
			known[sl] = true
			// An immediate store defines its slot (killing earlier
			// live-ness) but is itself never deleted and never poisons:
			// its encoded width is the assembler's business, and goc's
			// codegen keeps the same-slot-same-width invariant, so the
			// definition covers exactly what later same-slot reads get.
			if j == 0 && (op == "mov" || op == "movsd") && len(pl.operands) == 2 &&
				isImmediate(strings.TrimSpace(pl.operands[1])) {
				fx.def = sl
				continue
			}
			if w == 0 {
				w = opMemWidth(op, pl.operands)
			}
			if w == 0 || w < 8 {
				esc[sl] = true // narrow access poisons the slot
			}
			// Classify the effect on this slot.
			switch {
			case (op == "mov" || op == "movsd") && j == 0:
				fx.def = sl
				srcRest := strings.TrimSpace(pl.operands[1])
				regWide := gprWidth[srcRest] == 8
				// A callee-save SOURCE used to disqualify the store outright:
				// the prologue/epilogue save-restore pairs were assumed to look
				// like this. They never do -- goc spills those registers with
				// `push` and reloads them with `mov reg, [rbp-N]`, so a `mov`
				// store of rbx/r12/r13/r14 is always a homed local being
				// materialised, and liveness is the right judge of it. What the
				// guard really bought was silence, at the price of every
				// callee-save-homed local's spill surviving forever: fib_iter's
				// loop wrote [rbp-56] twice per iteration and never read it.
				//
				// Keep a trace of the shape anyway, and let dseSegment delete
				// such a store only under the stronger condition that the slot
				// is never read anywhere in the function -- a genuine
				// save/restore must have a reader, so this cannot touch one.
				fx.store = w == 8 && (regWide || (op == "movsd" && strings.HasPrefix(srcRest, "xmm")))
				fx.csStore = calleeSaveRegs[srcRest]
			case j == 0 && opWritesDst(op):
				fx.def = sl
				if opReadsDst(op) {
					fx.use = append(fx.use, sl)
				}
			case j == 0 && (op == "div" || op == "idiv" || op == "mul"):
				fx.use = append(fx.use, sl) // quotient/remainder land in rax/rdx
			default:
				fx.use = append(fx.use, sl) // plain read
			}
		}
	}
	return fxs, esc, known
}

// lvBlock is one basic block of a function segment under analysis.
type lvBlock struct {
	start, end int // [start, end) instruction indices into the segment
	succ       []*lvBlock
	use, def   map[string]bool // upward-exposed uses / first kills
	liveIn     map[string]bool
	liveOut    map[string]bool
}

// splitTopSegments cuts the whole-program stream into per-symbol segments:
// each begins at a top-level label (a name without the internal ".L" dot
// prefix) and runs to the next top-level label or raw line. Raw lines start
// and end segments but never join one, so inline-asm regions are skipped by
// construction -- and the instructions *after* a raw line (the tail of the
// same function, up to the next symbol) open a segment of their own: they
// must never be dropped, or an inline-asm-using function loses its body.
func splitTopSegments(insts []Inst) [][]Inst {
	isTop := func(in Inst) bool {
		if in.Kind != instLabel {
			return false
		}
		name := strings.TrimSuffix(strings.TrimSpace(in.Text), ":")
		return name != "" && !strings.HasPrefix(name, ".")
	}
	var segs [][]Inst
	start := -1
	for i, in := range insts {
		if in.Kind == instRaw {
			if start >= 0 {
				segs = append(segs, insts[start:i])
				start = -1
			}
			// The raw line itself is a segment of one: dseSegment
			// passes short segments through untouched, so inline asm
			// survives the rebuild verbatim.
			segs = append(segs, insts[i:i+1])
			continue
		}
		if start < 0 {
			// A top label always opens a segment; so does the first
			// instruction after an inline-asm block.
			start = i
		} else if isTop(in) {
			segs = append(segs, insts[start:i])
			start = i
		}
	}
	if start >= 0 {
		segs = append(segs, insts[start:])
	}
	return segs
}

// livenessDSE runs the CFG-based dead-store elimination over the whole
// program stream. Segments that fail any conservativeness check pass
// through untouched.
func livenessDSE(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	for _, seg := range splitTopSegments(insts) {
		out = append(out, dseSegment(seg)...)
	}
	return out
}

// dseSegment analyses one function segment: build the CFG, solve backward
// liveness, and drop stores that no path reads. It returns the segment
// unchanged whenever anything falls outside the modelled shape.
func dseSegment(seg []Inst) []Inst {
	if len(seg) < 2 {
		return seg
	}
	fxs, esc, known := scanFX(seg)
	if fxs == nil {
		return seg
	}

	// neverRead: slots no instruction in the function reads at all. This is
	// the licence a callee-save-sourced store needs (see fxEffects.csStore).
	// A slot touched by an unmodelled instruction never gets here: fx.unknown
	// marks every known slot live, which already blocks deletion.
	neverRead := map[string]bool{}
	for k := range known {
		if !esc[k] {
			neverRead[k] = true
		}
	}
	saveMax := saveAreaLimit(seg)
	for i := range fxs {
		if fxs[i].unknown {
			// Reads every slot: nothing is provably unread.
			for k := range neverRead {
				delete(neverRead, k)
			}
			break
		}
		for _, u := range fxs[i].use {
			delete(neverRead, u)
		}
	}

	// Leaders: the first instruction, every label, and anything following a
	// control transfer.
	leader := make([]bool, len(seg))
	leader[0] = true
	for i, in := range seg {
		switch in.Kind {
		case instLabel:
			leader[i] = true
		case instInstr:
			if fxs[i].jump != "" || fxs[i].ret {
				if i+1 < len(seg) {
					leader[i+1] = true
				}
			}
		}
	}

	var blocks []*lvBlock
	labBlock := map[string]*lvBlock{}
	for i := 0; i < len(seg); i++ {
		if !leader[i] {
			continue
		}
		j := i + 1
		for j < len(seg) && !leader[j] {
			j++
		}
		b := &lvBlock{start: i, end: j}
		blocks = append(blocks, b)
		if seg[i].Kind == instLabel {
			name := strings.TrimSuffix(strings.TrimSpace(seg[i].Text), ":")
			labBlock[name] = b
		}
	}

	// Edges. Anything that cannot be resolved inside the segment -- a branch
	// out of the function, a conditional at the very end, a fall-through off
	// the tail -- opts the whole segment out.
	for bi, b := range blocks {
		last := b.end - 1
		tail := &fxs[last]
		switch {
		case tail.jump != "":
			tb, ok := labBlock[tail.jump]
			if !ok {
				return seg
			}
			b.succ = append(b.succ, tb)
			if tail.cond {
				if last+1 >= len(seg) {
					return seg
				}
				b.succ = append(b.succ, blocks[bi+1])
			}
		case tail.ret:
			// no successors
		default:
			if last+1 >= len(seg) {
				return seg // no closing ret: drop the segment
			}
			b.succ = append(b.succ, blocks[bi+1])
		}
	}

	// Per-block use/def, computed forward inside each block: use[B] holds
	// slots read before any redefinition in B, def[B] slots defined before
	// any read. A call's uses are exactly the escape set; an unmodelled
	// instruction uses everything not already defined earlier in the block.
	for _, b := range blocks {
		useSet := map[string]bool{}
		defSet := map[string]bool{}
		for i := b.start; i < b.end; i++ {
			if seg[i].Kind != instInstr {
				continue
			}
			fx := &fxs[i]
			if fx.call {
				for k := range esc {
					if !defSet[k] {
						useSet[k] = true
					}
				}
				continue
			}
			for _, u := range fx.use {
				if !defSet[u] {
					useSet[u] = true
				}
			}
			if fx.unknown {
				for k := range known {
					if !defSet[k] {
						useSet[k] = true
					}
				}
			}
			if fx.def != "" {
				defSet[fx.def] = true
			}
		}
		b.use, b.def = useSet, defSet
	}

	// Backward fixpoint: liveIn = use | (liveOut - def).
	for _, b := range blocks {
		b.liveIn = map[string]bool{}
		b.liveOut = map[string]bool{}
	}
	changed := true
	for changed {
		changed = false
		for bi := len(blocks) - 1; bi >= 0; bi-- {
			b := blocks[bi]
			out := map[string]bool{}
			for _, s := range b.succ {
				for k := range s.liveIn {
					out[k] = true
				}
			}
			in := map[string]bool{}
			for k := range out {
				if !b.def[k] {
					in[k] = true
				}
			}
			for k := range b.use {
				in[k] = true
			}
			if !mapEq(in, b.liveIn) || !mapEq(out, b.liveOut) {
				b.liveIn, b.liveOut = in, out
				changed = true
			}
		}
	}

	// Second backwards walk with the fixpoint results: a store dies when its
	// slot is not live after it. Deleted stores stop killing live-ness, so
	// whole chains of covered stores go in one sweep.
	dead := make([]bool, len(seg))
	for _, b := range blocks {
		live := map[string]bool{}
		for k := range b.liveOut {
			live[k] = true
		}
		for i := b.end - 1; i >= b.start; i-- {
			if seg[i].Kind != instInstr {
				continue
			}
			fx := &fxs[i]
			if fx.store && fx.def != "" && !esc[fx.def] && !live[fx.def] &&
				(!fx.csStore || (neverRead[fx.def] && slotOffset(fx.def) > saveMax)) {
				dead[i] = true
				continue // gone: its def no longer covers anything
			}
			if fx.call {
				for k := range esc {
					live[k] = true
				}
				continue
			}
			if fx.def != "" {
				delete(live, fx.def)
			}
			for _, u := range fx.use {
				live[u] = true
			}
			if fx.unknown {
				for k := range known {
					live[k] = true
				}
			}
		}
	}

	out := make([]Inst, 0, len(seg))
	for i, in := range seg {
		if !dead[i] {
			out = append(out, in)
		}
	}
	return out
}

func mapEq(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// ---------- -O1: dead register-to-register copies ----------

// retLiveRegs are the registers that must still hold a meaningful value when a
// function returns: the Win64/SysV callee-save set the caller is entitled to,
// plus rax (the return value) and rsp. The caller-save registers are
// deliberately absent -- no caller may read them across a return, so a copy
// whose only reader is one of them really is dead. Getting this set right
// matters: modelling `ret` as "everything is live" keeps r10 alive around an
// entire loop and hides every dead copy out of it.
var retLiveRegs = map[string]bool{
	"rax": true, "rbx": true, "rbp": true, "rsp": true,
	"r12": true, "r13": true, "r14": true, "r15": true,
}

var retLiveList = []string{"rax", "rbx", "rbp", "rsp", "r12", "r13", "r14", "r15"}

// reg32Forms are the 32-bit GP register spellings. Writing one zero-extends
// into the whole 64-bit register, so it defines all of it.
var reg32Forms = map[string]bool{
	"eax": true, "ebx": true, "ecx": true, "edx": true,
	"esi": true, "edi": true, "ebp": true, "esp": true,
	"r8d": true, "r9d": true, "r10d": true, "r11d": true,
	"r12d": true, "r13d": true, "r14d": true, "r15d": true,
}

// opPureWrite reports whether op writes its first operand without reading it
// (mov, lea, the widening loads). Everything else on opDefinesReg's list is
// read-modify-write and genuinely consumes the old value.
func opPureWrite(op string) bool {
	switch op {
	case "mov", "lea", "movzx", "movsx", "movsxd", "movslq":
		return true
	}
	return false
}

// opDefinesReg reports whether op overwrites its first operand outright when
// that operand is a register. Anything off the list is treated as defining
// nothing, which can only widen liveness -- the safe direction.
func opDefinesReg(op string) bool {
	switch op {
	case "mov", "movzx", "movsx", "movsxd", "movslq", "lea",
		"add", "sub", "adc", "sbb", "and", "or", "xor",
		"shl", "sal", "sar", "shr", "neg", "not", "inc", "dec", "imul":
		return true
	}
	return false
}

// regsInside returns the base/index registers of a memory operand, so that
// `mov rax, [r10]` counts as a read of r10 and not as a bare address.
func regsInside(mem string) []string {
	var out []string
	inner := strings.Trim(mem, "[]")
	for _, tok := range strings.FieldsFunc(inner, func(r rune) bool {
		return r == '+' || r == '-' || r == '*' || r == ' ' || r == '\t'
	}) {
		if full, ok := regToFull64[tok]; ok {
			out = append(out, full)
		}
	}
	return out
}

// regFX is the register-level effect of one instruction: which full 64-bit
// registers it reads, and which single one it completely overwrites.
type regFX struct {
	use     []string
	def     string
	jump    string
	cond    bool
	ret     bool
	unknown bool // reads every register: a call, or anything unmodelled
}

func scanRegFX(seg []Inst) ([]regFX, bool) {
	fxs := make([]regFX, len(seg))
	for i, in := range seg {
		switch in.Kind {
		case instRaw:
			return nil, false // inline __asm: off limits
		case instLabel:
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		fx := &fxs[i]
		if !ok {
			t := strings.TrimSpace(strings.TrimPrefix(in.Text, "\t"))
			if t == "ret" {
				fx.ret = true
				fx.use = append(fx.use, retLiveList...)
			} else {
				fx.unknown = true
			}
			continue
		}
		switch {
		case pl.op == "call":
			fx.unknown = true // arguments and clobbers: read everything
			continue
		case pl.op == "jmp" || (strings.HasPrefix(pl.op, "j") && pl.op != "jmp"):
			if len(pl.operands) != 1 || strings.Contains(pl.operands[0], "[") {
				return nil, false // indirect branch
			}
			fx.jump = pl.operands[0]
			fx.cond = pl.op != "jmp"
			continue
		case pl.op == "ret":
			fx.ret = true
			fx.use = append(fx.use, retLiveList...)
			continue
		}
		// Implicit operands. `div`/`idiv` consume the rdx:rax dividend and
		// overwrite both; `mul` and the one-operand `imul` do the same to
		// rax. None of them name those registers, so modelling them from the
		// operand list alone reports rax dead and deletes the copy that set
		// it up -- which is exactly how a `%ld` in vfmt started printing
		// zeros. Reading every register is the honest description.
		switch {
		case pl.op == "div" || pl.op == "idiv" || pl.op == "mul" ||
			(pl.op == "imul" && len(pl.operands) == 1) || pl.op == "xchg":
			fx.unknown = true
			continue
		}
		for j, o := range pl.operands {
			_, rest := stripSize(o)
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "[") {
				fx.use = append(fx.use, regsInside(rest)...)
				continue
			}
			// A variable shift counts cl, which the mnemonic never names.
			if j == 1 && rest == "cl" && strings.HasPrefix(pl.op, "s") {
				fx.use = append(fx.use, "rcx")
				continue
			}
			full, isReg := regToFull64[rest]
			if !isReg {
				continue // immediate
			}
			// An 8/16-bit destination leaves the upper bits alone, so it is
			// not a complete definition and liveness must survive it.
			fullDef := j == 0 && opDefinesReg(pl.op) && (gp64Regs[rest] || reg32Forms[rest])
			if fullDef {
				fx.def = full
				// A pure write does not read its destination. Counting it as
				// a use made every copy's target upward-exposed in its block,
				// so liveness saturated and nothing was ever deleted.
				if opPureWrite(pl.op) {
					continue
				}
			}
			fx.use = append(fx.use, full)
		}
	}
	return fxs, true
}

// isRegCopy reports whether in is a plain register-to-register copy, and
// returns its destination. rsp/rbp are never candidates: unwinding and the
// frame itself depend on them regardless of what the liveness says.
func isRegCopy(in Inst) (string, bool) {
	if in.Kind != instInstr {
		return "", false
	}
	pl, ok := parseBodyLine(in.Text)
	if !ok || pl.op != "mov" || len(pl.operands) != 2 {
		return "", false
	}
	d, dOK := regToFull64[pl.operands[0]]
	if !dOK || !regToFull64OK(pl.operands[1]) {
		return "", false
	}
	if d == "rsp" || d == "rbp" {
		return "", false
	}
	return d, true
}

func regToFull64OK(o string) bool {
	_, ok := regToFull64[o]
	return ok
}

// deadMoveElim drops register-to-register copies whose destination no path
// reads again, using the same per-function CFG and backward fixpoint as
// livenessDSE -- only over registers instead of frame slots.
//
// This is the last third of fib_iter's loop overhead. The loop is split by
// labels into four short blocks, so no within-block rule reaches the copies:
// `mov r10, r13` and `mov rax, r13` are each the last write of their register
// on that path and only global liveness proves them dead.
func deadMoveElim(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	for _, seg := range splitTopSegments(insts) {
		out = append(out, dmeSegment(seg)...)
	}
	return out
}

func dmeSegment(seg []Inst) []Inst {
	if len(seg) < 2 {
		return seg
	}
	fxs, ok := scanRegFX(seg)
	if !ok {
		return seg
	}

	leader := make([]bool, len(seg))
	leader[0] = true
	for i, in := range seg {
		switch in.Kind {
		case instLabel:
			leader[i] = true
		case instInstr:
			if fxs[i].jump != "" || fxs[i].ret {
				if i+1 < len(seg) {
					leader[i+1] = true
				}
			}
		}
	}
	var blocks []*lvBlock
	labBlock := map[string]*lvBlock{}
	for i := 0; i < len(seg); i++ {
		if !leader[i] {
			continue
		}
		j := i + 1
		for j < len(seg) && !leader[j] {
			j++
		}
		b := &lvBlock{start: i, end: j}
		blocks = append(blocks, b)
		if seg[i].Kind == instLabel {
			name := strings.TrimSuffix(strings.TrimSpace(seg[i].Text), ":")
			labBlock[name] = b
		}
	}
	for bi, b := range blocks {
		tail := &fxs[b.end-1]
		switch {
		case tail.jump != "":
			tb, found := labBlock[tail.jump]
			if !found {
				return seg
			}
			b.succ = append(b.succ, tb)
			if tail.cond {
				if b.end >= len(seg) {
					return seg
				}
				b.succ = append(b.succ, blocks[bi+1])
			}
		case tail.ret:
			// no successors
		default:
			if b.end >= len(seg) {
				return seg
			}
			b.succ = append(b.succ, blocks[bi+1])
		}
	}

	for _, b := range blocks {
		useSet := map[string]bool{}
		defSet := map[string]bool{}
		for i := b.start; i < b.end; i++ {
			if seg[i].Kind != instInstr {
				continue
			}
			fx := &fxs[i]
			if fx.unknown {
				for r := range gp64Regs {
					if !defSet[r] {
						useSet[r] = true
					}
				}
				continue
			}
			for _, u := range fx.use {
				if !defSet[u] {
					useSet[u] = true
				}
			}
			if fx.def != "" {
				defSet[fx.def] = true
			}
		}
		b.use, b.def = useSet, defSet
	}

	for _, b := range blocks {
		b.liveIn = map[string]bool{}
		b.liveOut = map[string]bool{}
	}
	changed := true
	for changed {
		changed = false
		for bi := len(blocks) - 1; bi >= 0; bi-- {
			b := blocks[bi]
			outSet := map[string]bool{}
			for _, s := range b.succ {
				for k := range s.liveIn {
					outSet[k] = true
				}
			}
			inSet := map[string]bool{}
			for k := range outSet {
				if !b.def[k] {
					inSet[k] = true
				}
			}
			for k := range b.use {
				inSet[k] = true
			}
			if !mapEq(inSet, b.liveIn) || !mapEq(outSet, b.liveOut) {
				b.liveIn, b.liveOut = inSet, outSet
				changed = true
			}
		}
	}

	dead := make([]bool, len(seg))
	for _, b := range blocks {
		live := map[string]bool{}
		for k := range b.liveOut {
			live[k] = true
		}
		for i := b.end - 1; i >= b.start; i-- {
			if seg[i].Kind != instInstr {
				continue
			}
			// A removed copy neither reads nor writes anything observable,
			// so liveness across it is unchanged -- do not touch `live`.
			if dst, isCopy := isRegCopy(seg[i]); isCopy && !live[dst] {
				dead[i] = true
				continue
			}
			fx := &fxs[i]
			if fx.unknown {
				for r := range gp64Regs {
					live[r] = true
				}
				continue
			}
			if fx.def != "" {
				delete(live, fx.def)
			}
			for _, u := range fx.use {
				live[u] = true
			}
		}
	}
	res := make([]Inst, 0, len(seg))
	for i, in := range seg {
		if !dead[i] {
			res = append(res, in)
		}
	}
	return res
}

// saveAreaLimit returns the deepest rbp-relative offset, in bytes, that can
// belong to this function's callee-save save area: the prologue spills those
// registers with `push`, so n pushes own [rbp-8] .. [rbp-8n]. A store into one
// of them is never deleted even when nothing in the stream reads it back --
// that area is what an unwinder and the callee-save contract describe, and its
// contents are not ours to reason about from the instructions alone.
//
// Without a visible prologue (a synthetic snippet, a hand-written body) there
// is no such evidence, so the whole conservative range stays reserved.
func saveAreaLimit(seg []Inst) int {
	n := 0
	for i := 0; i < len(seg); i++ {
		if seg[i].Kind != instInstr {
			if seg[i].Kind == instLabel && i > 0 {
				break
			}
			continue
		}
		pl, ok := parseBodyLine(seg[i].Text)
		if !ok {
			continue
		}
		if pl.op == "push" && len(pl.operands) == 1 && calleeSaveRegs[pl.operands[0]] {
			n++
			continue
		}
		if n > 0 {
			break
		}
	}
	if n == 0 {
		return 32
	}
	return 8 * n
}

// slotOffset returns the byte offset of a direct frame slot from rbp, as a
// positive number, or -1 when s is not that shape.
func slotOffset(s string) int {
	if !strings.HasPrefix(s, "[rbp") || len(s) < 7 || s[len(s)-1] != ']' {
		return -1
	}
	sign := s[4]
	if sign != '-' && sign != '+' {
		return -1
	}
	n := 0
	for i := 5; i < len(s)-1; i++ {
		if s[i] < '0' || s[i] > '9' {
			return -1
		}
		n = n*10 + int(s[i]-'0')
	}
	if sign == '+' {
		return -n
	}
	return n
}
