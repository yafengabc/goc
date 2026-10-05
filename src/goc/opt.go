// Optimisation passes that are kept out of codegen.go
// (which is already very large). T1.6 / T1.4 live here, each with an
// independent switch and unit tests.
package compiler

import (
	"strconv"
	"strings"
)

// elimRedundantExtSkip bypasses the pass in Gen() -- the independent switch
// the plan requires for every new pass. Tests exercise it to compare
// with/without behaviour.
var elimRedundantExtSkip bool

// elimRedundantExt removes redundant int sign-extension pairs.
//
// goc models "int" as 32-bit and re-canonicalises it inside a 64-bit
// register after every int operation and int load by emitting a marked
// pair `shl rax, 32; sar rax, 32` (canonInt / extendInt). Sign-extension
// is idempotent: applying the pair to a value that is already sign-extended
// from 32 bits is a no-op. This pass tracks, over a linear window, which
// registers and which exact stack/global slots hold sign-canonical values,
// and drops a marked pair whose input register is already sign-canonical.
//
// Correctness discipline (mirrors peepholeIR / constProp):
//   - only compiler-marked pairs (Inst.IntWrap) are ever touched; a user's
//     own `x << 32 >> 32` never is;
//   - flags: the pair writes flags (shl/sar set SF/ZF/CF/OF), so a pair is
//     deleted only when no flag-reading instruction appears between it and
//     the next flag-writing instruction (or a window break);
//   - windows break at labels, inline __asm, calls, branches, returns and
//     indirect memory writes: all knowledge is discarded;
//   - exact slots ([rbp-N], [rip+lab]) are distinct, so a watched store to
//     one never perturbs another; sized/partial stores and non-"mov" stores
//     (movsd, fstp, ...) clobber the slot's canonicality;
//   - movsxd of a 32-bit source is sign-canonical by construction.
func elimRedundantExt(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	signCanon := make(map[string]bool) // reg -> holds a sign-canonical int
	slotSign := make(map[string]bool)  // "[rbp-N]"/"[rip+lab]" -> holds one
	var prevWrap map[string]bool       // regs whose kept pair is output-adjacent
	clearAll := func() {
		signCanon = make(map[string]bool)
		slotSign = make(map[string]bool)
		prevWrap = nil
	}
	clearSlots := func() { slotSign = make(map[string]bool) }

	skip := 0 // 1 while the sar half of a dropped pair is being skipped
	for i := 0; i < len(insts); i++ {
		if skip > 0 {
			skip--
			continue
		}
		in := insts[i]
		if in.Kind != instInstr {
			clearAll()
			out = append(out, in)
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			clearAll()
			out = append(out, in)
			continue
		}

		// A marked sign-wrap pair: `shl r, 32; sar r, 32` with IntWrap on both
		// halves. Drop the whole pair when r already holds a sign-canonical
		// value. Two flag arguments make a drop safe: (a) the immediately
		// preceding emitted instruction was the sar of a kept marked pair on the
		// same register -- then shl/sar is idempotent, the value and every flag
		// (SF/ZF/PF/CF/OF) after the pair are identical with or without it; or
		// (b) no flag-reading instruction appears between the pair and the next
		// flag-writing instruction (extDeletable). Otherwise keep the pair and
		// record that r now holds a sign-canonical value.
		if in.IntWrap && pl.op == "shl" && len(pl.operands) == 2 &&
			pl.operands[1] == "32" && i+1 < len(insts) {
			r := pl.operands[0]
			nxt := insts[i+1]
			if np, ok2 := parseBodyLine(nxt.Text); ok2 && nxt.IntWrap &&
				(np.op == "sar" || np.op == "shr") && len(np.operands) == 2 &&
				np.operands[0] == r && np.operands[1] == "32" {
				if signCanon[r] && (prevWrap[r] || extDeletable(insts, i+2)) {
					skip = 1 // drop both halves; the output tail keeps the same flags
					continue
				}
				// T1.6 (C5-b): the pair cannot be dropped, but it is still exactly
				// equivalent to ONE widening move -- `shl r,32; sar r,32` is
				// `movsxd r, r32` and `shl r,32; shr r,32` is `mov r32, r32`
				// (writing a 32-bit register zero-extends). The only behavioural
				// difference is that shl/sar/shr write the flags and the moves do
				// not, so the collapse is legal only when nothing reads the flags
				// before the next flag writer -- which is exactly extDeletable.
				if extDeletable(insts, i+2) {
					r32 := reg32Name(r)
					if np.op == "sar" {
						out = append(out, Inst{Kind: instInstr,
							Text: "\tmovsxd " + r + ", " + r32})
					} else {
						out = append(out, Inst{Kind: instInstr,
							Text: "\tmov " + r32 + ", " + r32})
					}
					// movsxd leaves a sign-canonical value; the zero-extending
					// form does not (the low half may have its top bit set).
					signCanon[r] = np.op == "sar"
					prevWrap = nil
					i++
					continue
				}
				signCanon[r] = true
				prevWrap = map[string]bool{r: true}
				out = append(out, in, nxt)
				i++
				continue
			}
		}
		// Any other instruction breaks the adjacency that makes a preceding kept
		// pair flag-equivalent, so prevWrap no longer applies.
		prevWrap = nil

		// Memory writes of any kind: an exact slot stays canonical only when
		// this is a plain full-width `mov [slot], src` of a canonical source;
		// a sized/partial store or a non-mov store (movsd, fstp, ...) clobbers
		// the slot; any indirect or non-slot write clobbers every slot.
		if len(pl.operands) > 0 && isMemOperand(pl.operands[0]) {
			sz, bare := splitSizedMem(pl.operands[0])
			switch {
			case !isSlotOperand(bare):
				clearSlots()
			case pl.op != "mov" || sz != "" || len(pl.operands) != 2:
				slotSign[bare] = false
			default:
				slotSign[bare] = sourceSignCanon(pl.operands[1], signCanon)
			}
		}

		switch pl.op {
		case "mov":
			if len(pl.operands) != 2 {
				break
			}
			dst, src := pl.operands[0], pl.operands[1]
			// Memory-ness must be judged on the size-prefix-stripped operand:
			// "dword [r10]" and "byte [rbp-3]" are memory operands too, and a
			// store to either must stay in the output.
			if _, dbare := splitSizedMem(dst); isMemOperand(dbare) {
				break // store (sized or not): slot bookkeeping done above
			}
			if _, sbare := splitSizedMem(src); isMemOperand(sbare) {
				// Load into dst. A full 8-byte load from an exact slot
				// inherits the slot's canonicality; a sized load or any other
				// source is unknown. A 32-bit destination zero-extends and
				// therefore also destroys the wide register's canonicality.
				sz, bare := splitSizedMem(src)
				if !gp64Regs[dst] {
					signCanon[reg64Name(dst)] = false
					break
				}
				if sz != "" || !isSlotOperand(bare) {
					signCanon[dst] = false
				} else {
					signCanon[dst] = slotSign[bare]
				}
				break
			}
			// Register / immediate move. A 32-bit destination (mov eax, imm)
			// zero-extends into the wide register and clears canonicality.
			if !gp64Regs[dst] {
				signCanon[reg64Name(dst)] = false
				break
			}
			if gp64Regs[src] {
				signCanon[dst] = signCanon[src]
			} else if v, ok := immValue(src); ok {
				signCanon[dst] = int32Fit(v)
			} else {
				signCanon[dst] = false
			}
		case "movsxd", "movslq":
			// Sign-extension of a 32-bit source is sign-canonical.
			if len(pl.operands) == 2 && gp64Regs[pl.operands[0]] {
				signCanon[pl.operands[0]] = true
			}
		case "lea":
			if len(pl.operands) > 0 && gp64Regs[pl.operands[0]] {
				signCanon[pl.operands[0]] = false
			}
		case "cqo", "cdq":
			signCanon["rax"] = false
			signCanon["rdx"] = false
		case "call", "ret", "push", "pop", "leave":
			clearAll()
			out = append(out, in)
			continue
		case "jmp", "loop":
			clearAll()
			out = append(out, in)
			continue
		case "neg", "not", "inc", "dec", "shl", "shr", "sar", "sal",
			"rol", "ror", "and", "or", "xor", "add", "sub", "adc", "sbb":
			// Two/one-operand ALU and shifts write operand[0]. A 32-bit
			// destination (xor eax, eax, add eax, ebx, ...) zero-extends into
			// the wide register and clears its canonicality too.
			if len(pl.operands) > 0 {
				if gp64Regs[pl.operands[0]] {
					signCanon[pl.operands[0]] = false
				} else if !isMemOperand(pl.operands[0]) {
					signCanon[reg64Name(pl.operands[0])] = false
				}
			}
		case "imul", "mul", "div", "idiv":
			// Two-operand imul writes operand[0]; one-operand forms write
			// rax/rdx. Clear the widest safe set.
			if len(pl.operands) == 1 {
				signCanon["rax"] = false
				signCanon["rdx"] = false
			} else if len(pl.operands) > 0 {
				if gp64Regs[pl.operands[0]] {
					signCanon[pl.operands[0]] = false
				} else if !isMemOperand(pl.operands[0]) {
					signCanon[reg64Name(pl.operands[0])] = false
				}
			}
		case "cmp", "test":
			// Flag-only: reads values, writes flags, changes no register.
		default:
			// jcc readers and set/cmov families are flag readers; any other
			// instruction that writes a GP register invalidates it.
			if strings.HasPrefix(pl.op, "j") || strings.HasPrefix(pl.op, "set") ||
				strings.HasPrefix(pl.op, "cmov") {
				// branch: full window break below
				if strings.HasPrefix(pl.op, "j") {
					clearAll()
					out = append(out, in)
					continue
				}
				// set*/cmov* write their operand[0].
				if len(pl.operands) > 0 {
					if gp64Regs[pl.operands[0]] {
						signCanon[pl.operands[0]] = false
					} else if !isMemOperand(pl.operands[0]) {
						signCanon[reg64Name(pl.operands[0])] = false
					}
				}
				break
			}
			if len(pl.operands) > 0 {
				if gp64Regs[pl.operands[0]] {
					signCanon[pl.operands[0]] = false
				} else if !isMemOperand(pl.operands[0]) {
					signCanon[reg64Name(pl.operands[0])] = false
				}
			}
		}
		out = append(out, in)
	}
	return out
}

// extDeletable reports whether deleting a marked wrap pair whose instructions
// occupy from-2/from-1 cannot change any later flag read: the scan stops at
// the next flag-writing instruction, and no flag-reading instruction (and no
// window break) may appear before it.
func extDeletable(insts []Inst, from int) bool {
	for j := from; j < len(insts); j++ {
		in := insts[j]
		if in.Kind != instInstr {
			return false
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			return false
		}
		switch {
		case isFlagReadOp(pl.op):
			return false
		case isFlagWriteOp(pl.op):
			return true
		case pl.op == "call" || pl.op == "ret" || pl.op == "jmp" ||
			strings.HasPrefix(pl.op, "j"):
			return false
		}
	}
	return false
}

// isFlagReadOp lists the spellings whose result depends on the flags: the
// conditional jumps, setcc and cmov families.
func isFlagReadOp(op string) bool {
	if strings.HasPrefix(op, "set") || strings.HasPrefix(op, "cmov") {
		return true
	}
	return strings.HasPrefix(op, "j") && op != "jmp"
}

// isFlagWriteOp lists the spellings that overwrite the flags, so a flag read
// after them is insulated from anything earlier. "not" is deliberately
// absent: it does not write flags.
func isFlagWriteOp(op string) bool {
	switch op {
	case "cmp", "test", "add", "sub", "adc", "sbb", "and", "or", "xor",
		"inc", "dec", "neg", "shl", "shr", "sar", "sal", "rol", "ror",
		"imul", "mul", "div", "idiv":
		return true
	}
	return false
}

// sourceSignCanon reports whether a store source operand carries a
// sign-canonical value: a full 64-bit register whose tracked value is
// canonical, or a constant that fits in a signed 32-bit int.
func sourceSignCanon(src string, signCanon map[string]bool) bool {
	if gp64Regs[src] {
		return signCanon[src]
	}
	if v, ok := immValue(src); ok {
		return int32Fit(v)
	}
	return false
}

// slotCacheSkip bypasses the slot-cache pass in Gen() -- its independent
// switch.
var slotCacheSkip bool

// slotCacheSnapshotSkip bypasses the value-snapshot (copy-chain) extension of
// slotCache (F4) -- its independent switch. When set, slotCache runs the
// original T1.4 semantics (kill-by-source); when clear, the value-snapshot
// semantics (kill-by-root, GP-GP copy-chain recording + root forwarding) are
// active on top of the existing cache.
var slotCacheSnapshotSkip bool

// slotVal is the value the slot cache believes a slot holds: a tracked
// immediate, or a full-width register (with the register's current write
// version at record time, so a later write to that register invalidates it).
type slotVal struct {
	isImm bool
	imm   int64
	reg   string
}

// slotCache removes redundant slot loads and stores (T1.4). A linear scan
// maintains, per exact stack/global slot ([rbp-N], [rip+lab]), the value last
// stored into it, and rewrites:
//
//  1. load forwarding: `mov rD, [s]` whose cached value is register rS
//     becomes `mov rD, rS` (or `mov rD, imm` for a cached immediate); when
//     rD == rS the load is a self-move and is dropped outright;
//  2. redundant store: `mov [s], rS` (or an immediate) when the slot already
//     holds exactly that value is dropped.
//
// Correctness discipline (mirrors constProp / elimRedundantExt):
//   - only full-width, unsized 8-byte loads/stores touch the cache; sized or
//     partial stores, non-mov stores (movsd, fstp) and any store with a
//     non-GP source clear the slot;
//   - an indirect or non-slot memory write, a lea of a slot address (address
//     escape), a call, a branch, a label, inline __asm and a return clear
//     everything;
//   - any write to a register invalidates cache entries sourced from it
//     (32-bit and 8-bit destinations map to the 64-bit name first);
//   - a rewritten load carries the same 64-bit value and writes no flags, so
//     no condition-code boundary moves.
func slotCache(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	cache := map[string]slotVal{}
	// F4 value-snapshot state (only consulted when !slotCacheSnapshotSkip).
	copyOf := map[string]string{} // reg -> copy-chain root
	killed := map[string]bool{}   // reg -> current value dead/unknown
	clearAll := func() {
		cache = map[string]slotVal{}
		copyOf = map[string]string{}
		killed = map[string]bool{}
	}
	clearSlot := func(s string) { delete(cache, s) }

	// rootOf walks the copy chain from r to the ultimate root. If any node on
	// the path (or the root itself) is killed, returns "" (invalid).
	rootOf := func(r string) string {
		if slotCacheSnapshotSkip {
			return r
		}
		seen := map[string]bool{}
		for r != "" {
			if killed[r] || seen[r] {
				return ""
			}
			seen[r] = true
			if next, ok := copyOf[r]; ok {
				r = next
			} else {
				return r
			}
		}
		return ""
	}

	// markLive records that dst now holds a fresh, known value (a register
	// move from imm, an ALU result, a memory load). dst is its own root.
	markLive := func(dst string) {
		if !slotCacheSnapshotSkip {
			dst = reg64Name(dst)
			delete(copyOf, dst)
			killed[dst] = false
		}
	}

	killReg := func(r string) {
		r = reg64Name(r)
		switch r {
		case "al":
			r = "rax"
		case "bl":
			r = "rbx"
		case "cl":
			r = "rcx"
		case "dl":
			r = "rdx"
		case "sil":
			r = "rsi"
		case "dil":
			r = "rdi"
		}
		if len(r) == 3 && r[0] == 'r' && r[2] == 'b' { // r8b..r15b
			r = r[:2]
		}
		for k, v := range cache {
			if !v.isImm && v.reg == r {
				delete(cache, k)
			}
		}
		if !slotCacheSnapshotSkip {
			// Break copies pointing to r: any register whose root is r now
			// has an invalid root.
			for x, root := range copyOf {
				if root == r {
					delete(copyOf, x)
					killed[x] = true
				}
			}
			delete(copyOf, r)
			killed[r] = true
		}
	}

	for _, in := range insts {
		if in.Kind != instInstr {
			clearAll()
			out = append(out, in)
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			clearAll()
			out = append(out, in)
			continue
		}
		// Control flow, calls and stack ops break every window.
		if pl.op == "call" || pl.op == "ret" || pl.op == "leave" ||
			pl.op == "push" || pl.op == "pop" || pl.op == "loop" ||
			strings.HasPrefix(pl.op, "j") {
			clearAll()
			out = append(out, in)
			continue
		}
		// Memory writes of any kind come first.
		if len(pl.operands) > 0 {
			if _, bare := splitSizedMem(pl.operands[0]); isMemOperand(bare) {
				sz, _ := splitSizedMem(pl.operands[0])
				if !isSlotOperand(bare) {
					clearAll()
					out = append(out, in)
					continue
				}
				if pl.op != "mov" || sz != "" || len(pl.operands) != 2 {
					clearSlot(bare)
					out = append(out, in)
					continue
				}
				src := pl.operands[1]
				if gp64Regs[src] {
					if !slotCacheSnapshotSkip {
						// F4: store the copy-chain root, not the immediate source.
						root := rootOf(src)
						if root == "" {
							clearSlot(bare)
							out = append(out, in)
							continue
						}
						if v, ok := cache[bare]; ok && !v.isImm && v.reg == root {
							continue // the slot already holds the same root value
						}
						cache[bare] = slotVal{reg: root}
						out = append(out, in)
						continue
					}
					if v, ok := cache[bare]; ok && !v.isImm && v.reg == src {
						continue // the slot already holds exactly this value
					}
					cache[bare] = slotVal{reg: src}
					out = append(out, in)
					continue
				}
				if v, ok := immValue(src); ok {
					if cv, ok := cache[bare]; ok && cv.isImm && cv.imm == v {
						continue // redundant same-immediate store
					}
					cache[bare] = slotVal{isImm: true, imm: v}
					out = append(out, in)
					continue
				}
				// xmm or other untracked source: value unknown.
				clearSlot(bare)
				out = append(out, in)
				continue
			}
		}
		// lea of a slot address is an address escape: the slot can now be
		// written through any pointer, so every slot is unknown. Any other
		// lea just writes its destination register.
		if pl.op == "lea" && len(pl.operands) == 2 {
			if _, bare := splitSizedMem(pl.operands[1]); isSlotOperand(bare) {
				clearAll()
				out = append(out, in)
				continue
			}
			killReg(pl.operands[0])
			markLive(pl.operands[0])
			out = append(out, in)
			continue
		}
		// Plain mov: store handled above; a load forwards the cached value;
		// anything else is a register write.
		if pl.op == "mov" && len(pl.operands) == 2 {
			dst, src := pl.operands[0], pl.operands[1]
			if _, dbare := splitSizedMem(dst); isMemOperand(dbare) {
				out = append(out, in) // store (handled above)
				continue
			}
			if _, sbare := splitSizedMem(src); isMemOperand(sbare) {
				if gp64Regs[dst] {
					sz, bare := splitSizedMem(src)
					if v, ok := cache[bare]; ok && sz == "" {
						if v.reg == dst {
							continue // reloading what the register already holds
						}
						// The rewritten move still writes dst, so cache entries sourced
						// from it must be invalidated (a forwarded load that skipped this
						// killed the linux register-arg spills and miscomputed sum7).
						killReg(dst)
						if v.isImm {
							in = Inst{Kind: instInstr,
								Text: "\tmov " + dst + ", " + strconv.FormatInt(v.imm, 10)}
							markLive(dst)
							out = append(out, in)
							continue
						}
						if !slotCacheSnapshotSkip && killed[v.reg] {
							// Root is dead: cannot forward. Fall through to real load.
							goto realLoad
						}
						in = Inst{Kind: instInstr, Text: "\tmov " + dst + ", " + v.reg}
						if !slotCacheSnapshotSkip {
							copyOf[dst] = v.reg
							killed[dst] = false
						}
						out = append(out, in)
						continue
					}
				}
			realLoad:
				// No forwardable value: the destination register is still
				// written (killing any cache entry sourced from it).
				killReg(dst)
				markLive(dst)
				out = append(out, in)
				continue
			}
			// GP-GP register move or immediate move.
			killReg(dst)
			if !slotCacheSnapshotSkip && gp64Regs[dst] && gp64Regs[src] {
				// GP-GP mov: record the copy chain.
				root := rootOf(src)
				if root != "" {
					copyOf[dst] = root
					killed[dst] = false
				} else {
					killed[dst] = true
				}
			} else {
				markLive(dst)
			}
			out = append(out, in)
			continue
		}
		// ALU, shifts, extensions and flag-only ops.
		switch pl.op {
		case "cmp", "test":
			out = append(out, in) // reads only; no register write
		case "cqo", "cdq":
			killReg("rax")
			killReg("rdx")
			// cqo/cdq produce unknown values in rdx (and rax sign-extends):
			// mark both dead in the snapshot model.
			if !slotCacheSnapshotSkip {
				killed["rax"] = true
				killed["rdx"] = true
			}
			out = append(out, in)
		case "imul", "mul", "div", "idiv":
			if len(pl.operands) == 1 {
				killReg("rax")
				killReg("rdx")
				if !slotCacheSnapshotSkip {
					killed["rax"] = true
					killed["rdx"] = true
				}
			} else if len(pl.operands) > 0 {
				killReg(pl.operands[0])
				markLive(pl.operands[0])
			}
			out = append(out, in)
		default:
			if len(pl.operands) > 0 && !isMemOperand(pl.operands[0]) {
				killReg(pl.operands[0])
				markLive(pl.operands[0])
			}
			out = append(out, in)
		}
	}
	return out
}

// copyElimSkip bypasses the basic-block local copy-elimination pass in Gen().
var copyElimSkip bool

// copyElim is a basic-block-local copy-chain pass. Unlike the old F3
// copyProp (which suspended moves across instructions and caused 28
// regressions), this pass NEVER reorders or suspends a mov: every mov is
// either emitted in place, or deleted only when the very next instruction
// consumes its destination AND a backward scan confirms the destination is
// dead before its next write.
//
// Correctness discipline:
//   - basic-block local: label/jmp/jcc/call/ret/instRaw/inline asm breaks the
//     window (clearAll + emit as-is);
//   - only full 64-bit GP-GP mov builds a copy chain; 32/8-bit mov kills the
//     chain without building one;
//   - consumer substitution rewrites the operand at the consuming instruction
//     (rebuilt from parsed operands, never text Replace);
//   - a mov is deleted only when the next instruction consumes D and a
//     backward scan (鈮? insns) shows D is not read before its next write;
//   - op0 of a writing instruction is never substituted (would move the write
//     to the root register).
func copyElim(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	copyOf := map[string]string{} // D -> root it copies
	killed := map[string]bool{}   // D's tracked value is dead

	clearAll := func() {
		copyOf = map[string]string{}
		killed = map[string]bool{}
	}

	// rootOf walks the copy chain to the ultimate live root.
	rootOf := func(r string) string {
		seen := map[string]bool{}
		for r != "" {
			if killed[r] || seen[r] {
				return ""
			}
			seen[r] = true
			if next, ok := copyOf[r]; ok {
				r = next
			} else {
				return r
			}
		}
		return ""
	}

	killReg := func(r string) {
		r = reg64Name(r)
		switch r {
		case "al":
			r = "rax"
		case "bl":
			r = "rbx"
		case "cl":
			r = "rcx"
		case "dl":
			r = "rdx"
		case "sil":
			r = "rsi"
		case "dil":
			r = "rdi"
		}
		if len(r) == 3 && r[0] == 'r' && r[2] == 'b' {
			r = r[:2]
		}
		// Break copies pointing to r.
		for x, root := range copyOf {
			if root == r {
				delete(copyOf, x)
				killed[x] = true
			}
		}
		delete(copyOf, r)
		killed[r] = true
	}

	// goclib library functions contain long mov chains that this basic-block
	// pass does not yet handle safely (observed corruption). Skip them.
	skipFunc := false

	for i := 0; i < len(insts); i++ {
		in := insts[i]
		// Detect function labels (any line ending in ':' that is not a
		// local jump label starting with '.').
		if in.Kind != instInstr {
			if t := strings.TrimSpace(in.Text); strings.HasSuffix(t, ":") && !strings.HasPrefix(t, ".") {
				fn := t[:len(t)-1]
				skipFunc = false
				for _, p := range []string{"__goclib_", "bi_", "fmt_", "os_", "double_", "goclib_"} {
					if strings.HasPrefix(fn, p) {
						skipFunc = true
						break
					}
				}
				if !skipFunc {
					switch fn {
					case "vfmt", "vfprintf", "printf", "sprintf", "snprintf", "fprintf",
						"fwrite", "fread", "fopen", "fclose", "fflush", "fputc", "fputs", "puts",
						"memcpy", "memmove", "memset", "strlen", "strcmp", "strncmp", "strcpy", "strcat",
						"malloc", "calloc", "realloc", "free", "exit", "abort", "qsort",
						"log", "log10", "frexp", "signbit", "floor", "ceil", "pow", "sqrt":
						skipFunc = true
					}
				}
			}
			clearAll()
			out = append(out, in)
			continue
		}
		if skipFunc {
			out = append(out, in)
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			clearAll()
			out = append(out, in)
			continue
		}
		// frontend.Block boundaries.
		if pl.op == "call" || pl.op == "ret" || pl.op == "leave" ||
			pl.op == "push" || pl.op == "pop" || pl.op == "loop" ||
			strings.HasPrefix(pl.op, "j") {
			clearAll()
			out = append(out, in)
			continue
		}

		// --- GP-GP mov: the core copy-chain case ---
		if pl.op == "mov" && len(pl.operands) == 2 &&
			!isMemOperand(pl.operands[0]) && !isMemOperand(pl.operands[1]) {
			dst, src := pl.operands[0], pl.operands[1]
			if gp64Regs[dst] && gp64Regs[src] {
				// killReg(dst) first.
				killReg(dst)
				r := rootOf(src)
				if r != "" {
					copyOf[dst] = r
					killed[dst] = false
				} else {
					killed[dst] = true
					// Emit the mov as-is (src has no live root chain).
					out = append(out, in)
					continue
				}
				// --- frontend.Block-local deletion (B version) ---
				// Scan the whole basic block: every mov consumer of D (X,D /
				// [s],D) before the first rewrite of D is substituted to read
				// the root directly and the mov is deleted. Any non-mov read
				// of D, any write of src (the substitution target), any
				// dstReadWrite of D, any memory operand using D, or a window
				// that ends at a block boundary without D being rewritten
				// aborts the deletion.
				type b1Consumer struct {
					idx  int
					text string
				}
				var b1cs []b1Consumer
				safe := true
				terminated := false
				lastC := -1
				for j := i + 1; j < len(insts); j++ {
					if insts[j].Kind != instInstr {
						safe = false
						break
					}
					tj := strings.TrimSpace(insts[j].Text)
					if tj == "" || strings.HasPrefix(tj, ".") {
						safe = false
						break
					}
					plj, okj := parseBodyLine(insts[j].Text)
					if !okj {
						safe = false
						break
					}
					if plj.op == "call" || plj.op == "ret" || plj.op == "leave" ||
						plj.op == "push" || plj.op == "pop" || plj.op == "loop" ||
						strings.HasPrefix(plj.op, "j") {
						break // block boundary: window ends without rewrite
					}
					if (plj.op == "div" || plj.op == "idiv" || plj.op == "mul") &&
						(dst == "rax" || dst == "rdx" || dst == "eax" || dst == "edx") {
						safe = false
						break
					}
					// D rewritten without being read first: window ends.
					if writesReg(insts[j].Text, dst) {
						if dstReadWrite(plj.op) && readsReg(insts[j].Text, dst) {
							safe = false // dstReadWrite consumer not handled
						} else {
							terminated = true
						}
						break
					}
					// src must stay live for substituted consumers.
					if writesReg(insts[j].Text, src) {
						safe = false
						break
					}
					// Any read of D inside a memory operand breaks analysis.
					for _, o := range plj.operands {
						if isMemOperand(o) {
							for _, rr := range regsInMem(o) {
								if rr == dst {
									safe = false
									break
								}
							}
						}
					}
					if !safe {
						break
					}
					if readsReg(insts[j].Text, dst) {
						if plj.op == "mov" && len(plj.operands) == 2 && plj.operands[1] == dst {
							if !isMemOperand(plj.operands[0]) && gp64Regs[plj.operands[0]] {
								b1cs = append(b1cs, b1Consumer{j, "\tmov " + plj.operands[0] + ", " + r})
								lastC = j
								continue
							}
							if isMemOperand(plj.operands[0]) {
								b1cs = append(b1cs, b1Consumer{j, "\tmov " + plj.operands[0] + ", " + r})
								lastC = j
								continue
							}
						}
						safe = false // non-mov consumer of D
						break
					}
				}
				deleted := false
				if safe && terminated && len(b1cs) > 0 {
					// Delete the mov; emit the window up to the last consumer
					// with consumers substituted; restart the chain cleanly.
					for k := i + 1; k <= lastC; k++ {
						ct := ""
						for _, c := range b1cs {
							if c.idx == k {
								ct = c.text
								break
							}
						}
						if ct != "" {
							out = append(out, Inst{Kind: insts[k].Kind, Text: ct})
						} else {
							out = append(out, insts[k])
						}
					}
					clearAll()
					i = lastC
					deleted = true
				} else {
					// Not deletable: emit the mov, flattened to the root.
					out = append(out, Inst{Kind: in.Kind,
						Text: "\tmov " + dst + ", " + r})
					continue
				}
				if deleted {
					continue
				}
			}
			// 32-bit/8-bit mov: killReg, no chain.
			if !gp64Regs[dst] || !gp64Regs[src] {
				killReg(dst)
				out = append(out, in)
				continue
			}
		}

		// --- Non-mov instructions: substitute GP operands ---
		ops := pl.operands
		substituted := false
		for oi := 0; oi < len(ops); oi++ {
			o := ops[oi]
			if !gp64Regs[o] {
				continue
			}
			// op0 of a writing instruction: never substitute (would move the
			// write to the root register).
			if oi == 0 && writesReg(in.Text, o) {
				continue
			}
			if r := rootOf(o); r != "" && r != o {
				ops[oi] = r
				substituted = true
			}
		}
		if substituted {
			out = append(out, Inst{Kind: in.Kind,
				Text: "\t" + pl.op + " " + strings.Join(ops, ", ")})
		} else {
			out = append(out, in)
		}

		// --- Update state for writes ---
		if len(pl.operands) > 0 && !isMemOperand(pl.operands[0]) {
			dst0 := pl.operands[0]
			if writesReg(in.Text, dst0) {
				killReg(dst0)
				// Known-value write: mark live (its own root).
				if pl.op != "div" && pl.op != "idiv" && pl.op != "mul" &&
					pl.op != "cdq" && pl.op != "cqo" {
					full := reg64Name(dst0)
					delete(copyOf, full)
					killed[full] = false
				}
			}
		}
		// Implicit writes.
		if pl.op == "div" || pl.op == "idiv" || pl.op == "mul" {
			killReg("rax")
			killReg("rdx")
			killed["rax"] = true
			killed["rdx"] = true
		}
		if pl.op == "cdq" || pl.op == "cqo" {
			killReg("rdx")
			killed["rdx"] = true
		}
	}
	return out
}

var algebraicIdentSkip bool

// algebraicIdent folds a small set of algebraic identities that goc emits
// literally (T1.3). The headline case is comparison-to-zero:
//
//   - truthiness of if/while/for/do-while/&&/|| conditions, and the generic
//     `x == 0` / `x != 0` / `x < 0` / ... all end up as `cmp <r>, 0` (an
//     immediate zero) or `cmp <r2>, <r>` where r was materialised as zero by
//     `xor r, r` / `mov r, 0`;
//   - `test <r>, <r>` is byte-identical in effect for the ZF-based and signed
//     condition codes (je/jne/jg/jl/jge/jle and the setcc equivalents), at a
//     smaller encoding and one fewer uop than `cmp`.
//
// `test` clears CF to 0 whereas `cmp r, 0` sets CF = (r <u 0); the unsigned
// reads (ja/jb/jae/jbe and seta/setb/setae/setbe) therefore must NOT be
// rewritten. The pass only rewrites when the immediately following
// instruction is a flag read in the SAFE set, so an unsigned branch after a
// `cmp r, 0` is left untouched.
//
// Correctness discipline (mirrors the other passes): a label, inline __asm, a
// call, a return or any unparseable line clears all knowledge; a write to a
// tracked register clears its zero fact; mul/div/idiv/cqo/cdq write rax/rdx.
func algebraicIdent(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	zeroReg := map[string]bool{} // register (64-bit name) -> holds the constant 0
	clearAll := func() { zeroReg = map[string]bool{} }
	// safeCmpZeroRead reports whether a flag-read instruction makes
	// `cmp r, 0` 鈮?`test r, r`. ZF-based (je/jne) and signed (jg/jl/jge/jle,
	// setg/setl/setge/setle) comparisons are identical; unsigned (ja/jb/jae/
	// jbe, seta/setb/setae/setbe) are not, because `test` clears CF.
	safeCmpZeroRead := func(op string) bool {
		switch op {
		case "je", "jne", "jz", "jnz", "jg", "jl", "jge", "jle",
			"sete", "setne", "setg", "setl", "setge", "setle":
			return true
		}
		return false
	}
	for i := 0; i < len(insts); i++ {
		in := insts[i]
		if in.Kind != instInstr {
			clearAll()
			out = append(out, in)
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			clearAll()
			out = append(out, in)
			continue
		}
		// A call or return clobbers every register we track (rax plus the
		// caller-save set), so discard all zero facts.
		if pl.op == "call" || pl.op == "ret" {
			clearAll()
			out = append(out, in)
			continue
		}
		// Attempt a comparison-to-zero rewrite on the original instruction.
		if pl.op == "cmp" && len(pl.operands) == 2 {
			dst := pl.operands[0]
			if gp64Regs[dst] || gpRegs[dst] {
				zero := false
				if v, ok := immValue(pl.operands[1]); ok && v == 0 {
					zero = true
				} else if gp64Regs[pl.operands[1]] && zeroReg[pl.operands[1]] {
					zero = true
				}
				if zero && i+1 < len(insts) {
					if np, ok := parseBodyLine(insts[i+1].Text); ok && safeCmpZeroRead(np.op) {
						in = Inst{Kind: instInstr, Text: "\ttest " + dst + ", " + dst}
					}
				}
			}
		}
		// Update zeroReg knowledge from the ORIGINAL instruction (a rewrite to
		// `test` neither creates nor destroys a zero fact, so it is ignored).
		switch pl.op {
		case "cmp", "test":
			// flag-only: no register is written
		case "xor", "sub":
			if len(pl.operands) == 2 {
				dst := pl.operands[0]
				if gp64Regs[dst] || gpRegs[dst] {
					if pl.operands[0] == pl.operands[1] {
						zeroReg[reg64Name(dst)] = true
					} else {
						zeroReg[reg64Name(dst)] = false
					}
				}
			}
		case "mov":
			if len(pl.operands) == 2 {
				dst := pl.operands[0]
				if gp64Regs[dst] || gpRegs[dst] {
					if v, ok := immValue(pl.operands[1]); ok && v == 0 {
						zeroReg[reg64Name(dst)] = true
					} else {
						zeroReg[reg64Name(dst)] = false
					}
				}
			}
		case "mul", "div", "idiv", "cqo", "cdq":
			// these write rax/rdx (cqo/cdq also derive rdx from rax); the
			// safe conservative choice is to forget both.
			zeroReg["rax"] = false
			zeroReg["rdx"] = false
		default:
			// any other GP-register write clears its zero fact
			if len(pl.operands) > 0 {
				dst := pl.operands[0]
				if gp64Regs[dst] || gpRegs[dst] {
					zeroReg[reg64Name(dst)] = false
				}
			}
		}
		out = append(out, in)
	}
	return out
}

// immValue parses the constant of a mov immediate operand: decimal, 0x hex,
// and 0b/0o (ParseInt base 0). The 64-bit pattern is reinterpreted as int64
// so that 0xffffffffffffffff reads as -1.
func immValue(s string) (int64, bool) {
	s = strings.TrimPrefix(s, "#")
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		if v, err := strconv.ParseUint(s[2:], 16, 64); err == nil {
			return int64(v), true
		}
		return 0, false
	}
	if v, err := strconv.ParseInt(s, 0, 64); err == nil {
		return v, true
	}
	return 0, false
}

// int32Fit reports whether v, as a 64-bit constant, is already the
// sign-extension of a 32-bit int.
func int32Fit(v int64) bool {
	return v >= -(1<<31) && v < (1<<31)
}

// splitSizedMem separates a size-prefixed memory operand ("dword [rbp-8]",
// "byte [rip+lab]") into the prefix and the bare slot text. Unsized operands
// return an empty prefix and the operand unchanged.
func splitSizedMem(m string) (sz, bare string) {
	for _, p := range []string{"qword ", "dword ", "word ", "byte "} {
		if strings.HasPrefix(m, p) {
			return p[:len(p)-1], m[len(p):]
		}
	}
	return "", m
}

// sibFoldSkip bypasses the SIB-index-address fold pass in Gen() -- its
// independent switch.
var sibFoldSkip bool

// f2IdxRegSkip bypasses the F2 direct-register index consumption in
// genLValue -- its independent switch.
var f2IdxRegSkip bool

// sibFold collapses the address-computation idiom that genLValue emits for
// array indexing
//
//	imul r11, K       ; index * element_width (K is 1/2/4/8)
//	add  r10, r11     ; base + scaled index
//	mov  D, [r10]     ; (or mov [r10], S) -- the element load / store
//
// into a single SIB memory operand
//
//	mov  D, [r10+r11*K]
//
// A signed index was movsxd'd into r11 before the imul, so a negative index
// has already been sign-extended and the 64-bit two's-complement scaled add
// wraps exactly like the SIB scale (which uses the full 64-bit index register
// times the scale); the fold is therefore safe for negative indices too.
//
// Correctness discipline (mirrors slotCache's window model):
//   - only the exact three-instruction idiom, strictly adjacent, all
//     instInstr lines;
//   - the load/store must address plain [r10] (no displacement) and its
//     destination / source must not reference r10 or r11 (a load into the
//     base register would destroy the SIB base; a store whose source is the
//     base or index would race the SIB read);
//   - after the fold, r10 holds only the BASE address and r11 the scaled
//     index, whereas before it held base+index / scaled index; the fold is
//     allowed only when neither register is read again before it is next
//     written;
//   - flags: imul/add write flags, so no flag-reading instruction may sit
//     between the fold point and the next flag-writing instruction;
//   - windows break at labels, calls, jumps, returns and inline asm.
func sibFold(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	reAdd := []byte("\tadd r10, r11")
	reImul := []byte("\timul r11, ")
	for i := 0; i < len(insts); i++ {
		in := insts[i]
		if in.Kind != instInstr || !strings.HasPrefix(in.Text, string(reImul)) {
			out = append(out, in)
			continue
		}
		k, err := strconv.Atoi(strings.TrimSpace(in.Text[len(reImul):]))
		if err != nil || (k != 1 && k != 2 && k != 4 && k != 8) {
			out = append(out, in)
			continue
		}
		if i+2 >= len(insts) || insts[i+1].Kind != instInstr ||
			!strings.HasPrefix(insts[i+1].Text, string(reAdd)) {
			out = append(out, in)
			continue
		}
		// The store that consumes the scaled address normally sits
		// immediately after the `add r10, r11`. But codegen emits the
		// store's source load *between* the add and the store (e.g.
		//   imul r11,4 / add r10,r11 / mov rax,[slot] / mov dword [r10],eax),
		// which breaks the strict three-instruction adjacency. Allow exactly
		// one gap instruction provided it neither touches r10/r11 nor reads
		// flags (the deleted imul/add set them) -- it is kept verbatim so the
		// store still gets its source.
		storeIdx := i + 2
		gapIdx := -1
		if _, ok := sibFoldMov(insts[storeIdx].Text, k); !ok {
			if i+3 < len(insts) && insts[i+2].Kind == instInstr &&
				sibFoldGapSafe(insts[i+2]) && insts[i+3].Kind == instInstr {
				if _, ok2 := sibFoldMov(insts[i+3].Text, k); ok2 {
					gapIdx = i + 2
					storeIdx = i + 3
				}
			}
		}
		mv := insts[storeIdx]
		if mv.Kind != instInstr {
			out = append(out, in)
			continue
		}
		folded, ok := sibFoldMov(mv.Text, k)
		if !ok || !sibFoldWindowSafe(insts, storeIdx+1) {
			out = append(out, in)
			continue
		}
		if gapIdx >= 0 {
			out = append(out, insts[gapIdx]) // keep the source load
		}
		out = append(out, Inst{Kind: instInstr, Text: folded})
		i = storeIdx // drop the imul, the add, the optional gap, and the store
	}
	return out
}

// sibFoldGapSafe reports whether a single instruction may sit between
// `add r10, r11` and the store it feeds, to be preserved verbatim when the
// imul/add are folded away. It must not read or write r10/r11 (its value is
// about to change), must not read flags (the deleted imul/add wrote them),
// and must not be control flow or a call.
func sibFoldGapSafe(in Inst) bool {
	if in.Kind != instInstr {
		return false
	}
	t := strings.TrimSpace(in.Text)
	if t == "" {
		return false
	}
	if strings.Contains(t, "r10") || strings.Contains(t, "r11") {
		return false
	}
	if strings.HasPrefix(t, "lock") || strings.HasPrefix(t, "rep") {
		return false
	}
	op := strings.SplitN(t, " ", 2)[0]
	if op == "call" || op == "jmp" || strings.HasPrefix(op, "j") {
		return false
	}
	if isFlagReadOp(op) {
		return false
	}
	return true
}

// sibFoldMov rewrites the element load/store of the idiom into its SIB form,
// or reports !ok when the mov is not the plain-[r10] form we fold.
func sibFoldMov(t string, k int) (string, bool) {
	rest := strings.TrimPrefix(t, "\tmov ")
	if rest == t {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	scale := strconv.Itoa(k)
	// store direction: "mov [r10], S"
	if strings.HasPrefix(rest, "[r10]") {
		after := rest[len("[r10]"):]
		if !strings.HasPrefix(after, ",") {
			return "", false
		}
		src := strings.TrimSpace(after[1:])
		if strings.Contains(src, "r10") || strings.Contains(src, "r11") {
			return "", false
		}
		return "\tmov [r10+r11*" + scale + "], " + src, true
	}
	// sized store: "mov byte [r10], S" / "mov word [r10], rax" / "mov dword [r10], eax"
	for _, pre := range []string{"byte ", "word ", "dword "} {
		if strings.HasPrefix(rest, pre+"[r10]") {
			after := rest[len(pre+"[r10]"):]
			if !strings.HasPrefix(after, ",") {
				return "", false
			}
			src := strings.TrimSpace(after[1:])
			if strings.Contains(src, "r10") || strings.Contains(src, "r11") {
				return "", false
			}
			return "\tmov " + pre + "[r10+r11*" + scale + "], " + src, true
		}
	}
	// load direction: "mov D, [r10]" with D not referencing r10/r11
	if strings.HasSuffix(rest, "[r10]") {
		dst := strings.TrimSpace(strings.TrimSuffix(rest, "[r10]"))
		dst = strings.TrimSpace(strings.TrimSuffix(dst, ","))
		if dst == "" || strings.Contains(dst, "r10") || strings.Contains(dst, "r11") {
			return "", false
		}
		return "\tmov " + dst + ", [r10+r11*" + scale + "]", true
	}
	return "", false
}

// sibFoldWindowSafe reports whether nothing between from and the next write of
// r10/r11 (or a window break) reads either register, and no flag read
// observes the deleted imul/add's flags.
func sibFoldWindowSafe(insts []Inst, from int) bool {
	flagsFresh := false // false until the first flag write after the fold point
	for j := from; j < len(insts); j++ {
		in := insts[j]
		if in.Kind != instInstr {
			// labels break the window; other non-instruction lines (rare) too
			return true
		}
		t := strings.TrimSpace(in.Text)
		if t == "" {
			continue
		}
		op := strings.SplitN(t, " ", 2)[0]
		// hard breaks: unconditional control flow, calls, inline asm (its flag
		// effects are the author's business), stack ops that may alias r10/r11
		// slots. Conditional jumps (jcc) are NOT breaks: they read flags, so
		// they fall through to the flag check below.
		if op == "call" || op == "jmp" ||
			op == "ret" || op == "syscall" || op == "int" || op == "lock" || op == "rep" {
			return true
		}
		if strings.Contains(t, "__asm") {
			return true
		}
		// a write of r10 or r11 ends the observation window: after the fold
		// the register carries a fresh value, so no later read can observe the
		// deleted add/imul's result. (A write that also READS the register,
		// e.g. "mov r10, [r10]", fails the read check below.)
		if writesReg(t, "r10") || writesReg(t, "r11") {
			// the write itself must not read the old value
			if readsReg(in.Text, "r10") || readsReg(in.Text, "r11") {
				return false
			}
			return true
		}
		// a read of r10/r11 would observe the pre-fold values: not foldable
		if readsReg(in.Text, "r10") || readsReg(in.Text, "r11") {
			return false
		}
		if isFlagWriteOp(op) {
			flagsFresh = true
			continue
		}
		if !flagsFresh && isFlagReadOp(op) {
			return false
		}
	}
	return true
}

// writesReg reports whether an instruction writes register reg (or a
// sub-register r10d/r10b/r10w). Only dst-writing ALU/mov forms count.
func writesReg(t, reg string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return false
	}
	parts := strings.SplitN(t, " ", 2)
	op := parts[0]
	switch op {
	case "mov", "movsxd", "movzx", "movslq", "add", "sub", "imul", "lea", "and", "or", "xor", "inc", "dec",
		"neg", "not", "shl", "shr", "sar", "sal", "rol", "ror", "pop",
		"movq", "movd", "movmskpd", "movmskps",
		"cvttsd2si", "cvtsd2si", "cvttss2si", "cvtss2si":
	case "div", "idiv", "mul":
		// implicit destination: quotient/remainder in rdx:rax (and their
		// sub-registers). The explicit operand is the divisor, which is
		// READ, not written.
		return strings.HasPrefix(reg, "rax") || strings.HasPrefix(reg, "rdx") ||
			strings.HasPrefix(reg, "eax") || strings.HasPrefix(reg, "edx")
	case "cdq", "cqo", "cdqe", "cwde":
		// sign-extend eax/rax into edx:rax; writes rdx (cdq/cqo) or rax.
		if op == "cdq" || op == "cqo" {
			return strings.HasPrefix(reg, "rdx") || strings.HasPrefix(reg, "edx")
		}
		return strings.HasPrefix(reg, "rax") || strings.HasPrefix(reg, "eax")
	default:
		return false
	}
	if len(parts) < 2 {
		return false
	}
	rest := strings.TrimSpace(parts[1])
	dst := strings.TrimSpace(strings.SplitN(rest, ",", 2)[0])
	if dst == reg || strings.HasPrefix(dst, reg+"d") ||
		strings.HasPrefix(dst, reg+"b") || strings.HasPrefix(dst, reg+"w") {
		return true
	}
	// Sub-register writes: ebx/bl/bx clobber rbx, r8d/r8w/r8b clobber r8.
	for _, s := range subRegWrites[reg] {
		if dst == s {
			return true
		}
	}
	return false
}

// subRegWrites maps a 64-bit register to the sub-register names that write it.
var subRegWrites = map[string][]string{
	"rax": {"eax", "ax", "al"},
	"rbx": {"ebx", "bx", "bl"},
	"rcx": {"ecx", "cx", "cl"},
	"rdx": {"edx", "dx", "dl"},
	"rsi": {"esi", "si", "sil"},
	"rdi": {"edi", "di", "dil"},
	"rbp": {"ebp", "bp", "bpl"},
	"rsp": {"esp", "sp", "spl"},
	"r8":  {"r8d", "r8w", "r8b"},
	"r9":  {"r9d", "r9w", "r9b"},
	"r10": {"r10d", "r10w", "r10b"},
	"r11": {"r11d", "r11w", "r11b"},
	"r12": {"r12d", "r12w", "r12b"},
	"r13": {"r13d", "r13w", "r13b"},
	"r14": {"r14d", "r14w", "r14b"},
	"r15": {"r15d", "r15w", "r15b"},
}

// readsReg reports whether an instruction reads register reg (any width).
// A write whose source also references the register ("mov r10, [r10]") counts
// as a read as well.
func readsReg(t, reg string) bool {
	if !strings.Contains(t, reg) {
		return false
	}
	if !writesReg(t, reg) {
		return true
	}
	return strings.Count(t, reg) > 1
}

// dstReadWrite reports whether the instruction's destination register is also
// read as a source: x86 two-operand ALU forms ("add r10, r11" starts from
// r10's old value) and the single-operand inc/dec/neg/not family. Pure-write
// destinations (mov/lea/movsxd/movzx/movslq) return false.
func dstReadWrite(op string) bool {
	switch op {
	case "add", "sub", "imul", "and", "or", "xor",
		"shl", "shr", "sar", "sal", "rol", "ror",
		"inc", "dec", "neg", "not":
		return true
	}
	return false
}

// regsInMem returns the general-purpose registers referenced inside a memory
// operand such as "[rbp-8]", "[r10+r11*4]" or "[rip+G_a]". Longer names
// first, so r10 is matched before r1.
func regsInMem(mem string) []string {
	inner := strings.Trim(strings.TrimSpace(mem), "[]")
	var out []string
	for _, r := range gp64RegOrder {
		if strings.Contains(inner, r) {
			out = append(out, r)
		}
	}
	return out
}

// gp64RegOrder lists the 64-bit GP registers longest-name-first for substring
// matching inside memory operands.
var gp64RegOrder = []string{
	"r15", "r14", "r13", "r12", "r11", "r10", "r9", "r8",
	"rax", "rbx", "rcx", "rdx", "rsi", "rdi", "rbp", "rsp",
}

// ---------- -O1: fold `mov rX, rA; add rX, rB` into `lea rX, [rA+rB]` ----------
//
// goc evaluates `t = a + b` by copying one operand into the accumulator and
// adding the other, because every expression result is materialised there.
// `lea` does the whole thing in one instruction -- but it does NOT write
// flags, while `add` does. That asymmetry is the only hazard: the fold is
// legal exactly when no later instruction reads the flags the add produced.
//
// fib_iter's loop is the motivating case: three instructions become one,
// across 30M iterations.

// leaFlagWrite names the instructions that overwrite every condition flag.
// inc/dec are deliberately absent: they leave CF alone, so they do not
// shield a later carry reader from the add's CF.
var leaFlagWrite = map[string]bool{
	"cmp": true, "test": true, "add": true, "sub": true, "adc": true,
	"sbb": true, "and": true, "or": true, "xor": true, "neg": true,
	"not": true, "shl": true, "shr": true, "sar": true, "sal": true,
	"imul": true, "mul": true, "div": true, "idiv": true,
}

// leaCFRead names the instructions that consume CF specifically.
var leaCFRead = map[string]bool{
	"jc": true, "jnc": true, "jb": true, "jnae": true, "jbe": true,
	"jna": true, "ja": true, "jnbe": true, "jae": true, "jnb": true,
	"adc": true, "sbb": true, "rcl": true, "rcr": true,
}

// leaFlagRead classifies how op consumes flags: 1 = carry only,
// 2 = some non-carry flag, 0 = none at all.
func leaFlagRead(op string) int {
	if leaCFRead[op] {
		return 1
	}
	if strings.HasPrefix(op, "j") && op != "jmp" {
		return 2
	}
	if strings.HasPrefix(op, "set") || strings.HasPrefix(op, "cmov") {
		return 2
	}
	return 0
}

// leaFlagsSafe reports whether the flags written by the instruction at index
// i are dead from there on, which is what licenses replacing it with a lea.
func leaFlagsSafe(insts []Inst, i int) bool {
	cfLive, otherLive := true, true
	for j := i + 1; j < len(insts) && j <= i+32; j++ {
		in := insts[j]
		if in.Kind == instRaw {
			return false
		}
		if in.Kind == instLabel {
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			// `ret` has no operands, so it will not parse. Reaching it
			// means no instruction from here to the end of the function
			// reads the flags, and no caller may rely on flags across a
			// call either -- so the flags really are dead. Anything else
			// unparsed is unmodelled and must be refused.
			if strings.TrimSpace(strings.TrimPrefix(in.Text, "\t")) == "ret" {
				return true
			}
			return false // call, inline asm: assume the worst
		}
		if pl.op == "inc" || pl.op == "dec" {
			otherLive = false // inc/dec rewrite SF/ZF/OF/AF but not CF
			continue
		}
		switch leaFlagRead(pl.op) {
		case 1:
			if cfLive {
				return false
			}
		case 2:
			if otherLive {
				return false
			}
		}
		if leaFlagWrite[pl.op] {
			return true // every flag is rewritten from here on
		}
	}
	return false
}

// foldLea rewrites `mov rX, rA` + `add rX, rB` into `lea rX, [rA+rB]`.
func foldLea(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	for i := 0; i < len(insts); {
		in := insts[i]
		if in.Kind != instInstr || i+1 >= len(insts) || insts[i+1].Kind != instInstr {
			out = append(out, in)
			i++
			continue
		}
		mv, ok1 := parseBodyLine(in.Text)
		ad, ok2 := parseBodyLine(insts[i+1].Text)
		if !ok1 || !ok2 || mv.op != "mov" || ad.op != "add" ||
			len(mv.operands) != 2 || len(ad.operands) != 2 {
			out = append(out, in)
			i++
			continue
		}
		dst, a, b := mv.operands[0], mv.operands[1], ad.operands[1]
		if ad.operands[0] != dst || dst == "rsp" || dst == "rbp" ||
			a == "rsp" || b == "rsp" { // rsp cannot be a SIB index
			out = append(out, in)
			i++
			continue
		}
		if !gp64Regs[dst] || !gp64Regs[a] || !gp64Regs[b] {
			out = append(out, in)
			i++
			continue
		}
		if !leaFlagsSafe(insts, i+1) {
			out = append(out, in)
			i++
			continue
		}
		out = append(out, Inst{Kind: instInstr, Text: "\tlea " + dst + ", [" + a + "+" + b + "]"})
		i += 2
	}
	return out
}

// ---------- -O1: register copy propagation ----------

// copyProp replaces later uses of a copied-to register with the register it
// was copied from, which leaves the copy itself with no reader so the
// existing deadMoveElim can remove it.
//
// fib_iter pays for this once per iteration: `mov r14, rax` exists only so
// that the following `mov r12, r14` has a name for the new value. Writing
// r12 straight from rax strands the middle copy.
func copyProp(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	for _, seg := range splitTopSegments(insts) {
		out = append(out, cpSegment(seg)...)
	}
	return out
}

func cpSegment(seg []Inst) []Inst {
	for i := 0; i < len(seg); i++ {
		if seg[i].Kind != instInstr {
			continue
		}
		pl, ok := parseBodyLine(seg[i].Text)
		if !ok || pl.op != "mov" || len(pl.operands) != 2 {
			continue
		}
		d, s := pl.operands[0], pl.operands[1]
		if !gp64Regs[d] || !gp64Regs[s] || d == s ||
			d == "rsp" || d == "rbp" || s == "rsp" || s == "rbp" {
			continue
		}
		cpForward(seg, i+1, d, s)
	}
	return seg
}

// operand0Reads reports whether an instruction's first operand is consumed
// rather than overwritten. Only these three are; everything else with a
// register destination writes it.
//
// This must be the short list and not an "instructions that define their
// destination" allowlist: the allowlist silently classifies `pop` and the
// setcc family as readers, which is backwards. A propagated `pop r14`
// rewritten to `pop r12` pops into the wrong register -- r14 keeps the stale
// value while r12 now holds what r14 should have -- and that is exactly the
// infinite loop nine examples fell into. Anything unrecognised is treated as
// a write, which can only stop propagation early.
func operand0Reads(op string) bool {
	return op == "cmp" || op == "test" || op == "push"
}

// cpForward rewrites uses of d into s from start onwards, stopping at the
// first thing that can break the d == s equivalence.
func cpForward(seg []Inst, start int, d, s string) {
	for i := start; i < len(seg); i++ {
		in := seg[i]
		if in.Kind == instLabel {
			return // a branch target: d may arrive holding anything
		}
		if in.Kind != instInstr {
			return
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			return // ret, inline asm: unmodelled reads and clobbers
		}
		if pl.op == "call" || pl.op == "div" || pl.op == "idiv" || pl.op == "mul" ||
			(pl.op == "imul" && len(pl.operands) == 1) || pl.op == "xchg" {
			return // implicit operands: rax/rdx are read and written silently
		}
		writes := !operand0Reads(pl.op)
		for j, o := range pl.operands {
			_, rest := stripSize(o)
			rest = strings.TrimSpace(rest)
			full, isReg := regToFull64[rest]
			if !isReg {
				continue
			}
			if j == 0 && writes && (full == d || full == s) {
				return // one side of the equivalence is overwritten here
			}
		}
		newOps := append([]string(nil), pl.operands...)
		changed := false
		for j, o := range pl.operands {
			if j == 0 && writes {
				continue // a defined destination is not a use
			}
			_, rest := stripSize(o)
			rest = strings.TrimSpace(rest)
			// Only a genuine 64-bit spelling may be renamed. regToFull64
			// maps `eax` to `rax` too, so matching on the parent alone
			// would rewrite `mov r10, eax` (zero-extended 32-bit read) into
			// `mov r10, r12` (full 64-bit read) -- a different value
			// whenever the source's high half is non-zero. The same rule
			// keeps `shl r10, cl` from becoming `shl r10, r12`, which is
			// not encodable at all.
			if !gp64Regs[rest] {
				continue
			}
			full, isReg := regToFull64[rest]
			if isReg && full == d {
				newOps[j] = s
				changed = true
			}
		}
		if changed {
			seg[i].Text = "\t" + pl.op + " " + strings.Join(newOps, ", ")
		}
	}
}

// ---------- -O1: fold a loop-invariant load into the compare that reads it ----

// foldCmpMem rewrites `mov rX, [mem]` + `cmp rY, rX` into `cmp rY, [mem]`.
//
// goc reloads a variable from its home slot every time it is mentioned, so a
// loop bound that never changes still costs a load per iteration. Folding the
// load into the compare's source operand keeps the value in the compare and
// drops the register entirely -- legal only when rX has no further reader
// before something overwrites it.
func foldCmpMem(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	for i := 0; i < len(insts); {
		in := insts[i]
		if in.Kind != instInstr || i+1 >= len(insts) || insts[i+1].Kind != instInstr {
			out = append(out, in)
			i++
			continue
		}
		mv, ok1 := parseBodyLine(in.Text)
		cm, ok2 := parseBodyLine(insts[i+1].Text)
		if !ok1 || !ok2 || mv.op != "mov" || cm.op != "cmp" ||
			len(mv.operands) != 2 || len(cm.operands) != 2 {
			out = append(out, in)
			i++
			continue
		}
		x, mem := mv.operands[0], mv.operands[1]
		y, rhs := cm.operands[0], cm.operands[1]
		if !gp64Regs[x] || !gp64Regs[y] || rhs != x || !strings.HasPrefix(mem, "[") {
			out = append(out, in)
			i++
			continue
		}
		// The memory operand must not depend on the register being dropped.
		deps := false
		for _, r := range regsInside(mem) {
			if r == x {
				deps = true
			}
		}
		if deps || !regDeadBeforeRedef(insts, i+1, x) {
			out = append(out, in)
			i++
			continue
		}
		out = append(out, Inst{Kind: instInstr, Text: "\tcmp " + y + ", " + mem})
		i += 2
	}
	return out
}

// regDeadBeforeRedef reports whether x has no reader between at (exclusive)
// and the first instruction that overwrites it. A label in between makes the
// answer unknowable from the linear stream alone, so it is refused.
func regDeadBeforeRedef(insts []Inst, at int, x string) bool {
	for j := at + 1; j < len(insts); j++ {
		in := insts[j]
		if in.Kind == instLabel {
			return false
		}
		if in.Kind != instInstr {
			return false
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			return false
		}
		if pl.op == "call" || pl.op == "div" || pl.op == "idiv" || pl.op == "mul" ||
			(pl.op == "imul" && len(pl.operands) == 1) || pl.op == "xchg" {
			return false
		}
		writes := opDefinesReg(pl.op)
		for j2, o := range pl.operands {
			_, rest := stripSize(o)
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "[") {
				for _, r := range regsInside(rest) {
					if r == x {
						return false
					}
				}
				continue
			}
			full, isReg := regToFull64[rest]
			if !isReg {
				continue
			}
			if j2 == 0 && writes {
				if full == x {
					return true // overwritten before anything read it
				}
				continue
			}
			if full == x {
				return false
			}
		}
	}
	return false
}
