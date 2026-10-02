// Package main: optimisation passes that are kept out of codegen.go
// (which is already very large). T1.6 / T1.4 live here, each with an
// independent switch and unit tests.
package main

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
	slotSign := make(map[string]bool)   // "[rbp-N]"/"[rip+lab]" -> holds one
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
			np.op == "sar" && len(np.operands) == 2 &&
			np.operands[0] == r && np.operands[1] == "32" {
			if signCanon[r] && (prevWrap[r] || extDeletable(insts, i+2)) {
				skip = 1 // drop both halves; the output tail keeps the same flags
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
	clearAll := func() { cache = map[string]slotVal{} }
	clearSlot := func(s string) { delete(cache, s) }
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
							out = append(out, in)
							continue
						}
						in = Inst{Kind: instInstr, Text: "\tmov " + dst + ", " + v.reg}
						out = append(out, in)
						continue
					}
				}
				// No forwardable value: the destination register is still
				// written (killing any cache entry sourced from it).
				killReg(dst)
				out = append(out, in)
				continue
			}
			killReg(dst)
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
			out = append(out, in)
		case "imul", "mul", "div", "idiv":
			if len(pl.operands) == 1 {
				killReg("rax")
				killReg("rdx")
			} else if len(pl.operands) > 0 {
				killReg(pl.operands[0])
			}
			out = append(out, in)
		default:
			if len(pl.operands) > 0 && !isMemOperand(pl.operands[0]) {
				killReg(pl.operands[0])
			}
			out = append(out, in)
		}
	}
	return out
}

// algebraicIdentSkip bypasses the algebraic-identity pass in Gen().
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
	// `cmp r, 0` ≡ `test r, r`. ZF-based (je/jne) and signed (jg/jl/jge/jle,
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
