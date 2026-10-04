package goa

import (
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// AT&T (GAS) front end
// ---------------------------------------------------------------------------
//
// goc's own assembly syntax is Intel/NASM-flavoured: operands are written
// destination-first and carry no sigil -- `mov rax, [rbp-8]`, `push rbp`,
// `jmp .Lloop`. LLVM's AsmPrinter emits AT&T instead: every register is
// prefixed with '%', every immediate with '$', memory is
// disp(base,index,scale), and operands read source-first -- `movq %rsp, %rax`
// stores rsp into rax, the opposite of what the Intel spelling would mean.
//
// Rather than write a second x86 encoder, this file translates each AT&T line
// into goa's own spelling and hands it to processLine, so every existing
// encode* method applies unchanged. What lives here is purely the syntax
// layer: sigil stripping, the source/destination flip, suffix removal, and
// the GAS directives.

// attTranslate renders one AT&T instruction as a goa-syntax line. It returns
// ok=false for lines that carry no code (directives, comments, blank), which
// the caller drops.
//
// The translation is three steps: strip the mnemonic's operand-size suffix,
// flip the operand order, and rewrite each operand's sigil. Two AT&T shapes
// need more than a mechanical rewrite:
//
//   - Indirect call/jmp carry a leading '*' (`callq *%rax`); it is dropped and
//     the operand surfaces as a register or memory reference, which is exactly
//     what encodeCall/encodeJmp expect.
//   - movabs is the only 64-bit-immediate-to-register form, and it is spelled
//     `movabsq $imm, %reg`. goa has no such mnemonic, so it becomes a plain
//     `mov` -- encodeMov infers the 64-bit width from the register and emits
//     the imm32 sign-extended or the full imm64 as required.
func attTranslate(ln string, alias map[string]string) (string, bool, error) {
	raw, rest := attSplitMnemonic(ln)
	if raw == "" {
		return "", false, nil
	}
	ops := attSplitOperands(rest)
	if len(ops) == 0 {
		m, _ := attMnemonic(raw, nil)
		return m, true, nil // retq, leave, cqto, ...
	}

	// Indirect call/jmp carry a dereference marker that is part of the operand
	// token itself (`callq *%rax`), not a separate word, so it is stripped
	// per operand rather than by dropping a `*` element.
	for i := range ops {
		if t := strings.TrimSpace(ops[i]); strings.HasPrefix(t, "*") {
			ops[i] = strings.TrimSpace(strings.TrimPrefix(t, "*"))
		}
	}
	if len(ops) == 1 && ops[0] == "" {
		return "", false, fmt.Errorf("bare indirect branch in %q", ln)
	}

	// AT&T is source-first; goa is destination-first, so a two-operand
	// instruction normally reverses. The exception is the case where AT&T has
	// already put the destination last: `addq %r14, 64(%r15)` adds r14 *into*
	// memory, so the memory operand is the target and the pair must not be
	// swapped. Flipping it would produce `add [r15+64], r14`, which goa rejects
	// (and rightly -- an arithmetic r/m destination needs the 0F opcode, not
	// the short form). So the direction is decided from the operands, not from
	// a fixed rule about the mnemonic.
	mnem, width := attMnemonic(raw, ops)
	if len(ops) == 3 && mnem == "imul" {
		// The three-operand imul is a rotate, not a swap. AT&T writes it
		// `imul $imm, %src, %dst` (immediate first) while Intel writes
		// `imul %dst, %src, $imm` -- and both agree the destination is the
		// register named last in AT&T and first in Intel. A plain two-way
		// swap would leave the immediate in the middle and encode a
		// different product.
		ops[0], ops[2] = ops[2], ops[0]
	} else if len(ops) == 2 && attFlips(mnem, ops) {
		ops[0], ops[1] = ops[1], ops[0]
	}

	// A memory operand carries no width of its own -- goa infers it from the
	// register it is paired with, which is exactly the information AT&T's
	// mnemonic suffix supplies. `decl -4(%rbp)` is a 32-bit decrement even
	// though the memory reference mentions no register at all, so the suffix
	// has to be pushed onto the operand as an explicit size keyword or the
	// encoder defaults to 64 bits and emits a spurious REX.W.
	//
	// Register operands need no help: their own name carries the width.
	sizeKw := ""
	if width > 0 {
		sizeKw = attSizeKeywords[width]
	}

	out := make([]string, len(ops))
	for i, o := range ops {
		s, err := attOperand(o, alias)
		if err != nil {
			return "", false, fmt.Errorf("%s: %w", ln, err)
		}
		if sizeKw != "" && isMemOperand(o) && !hasSizeKeyword(s) {
			s = sizeKw + s
		}
		out[i] = s
	}
	return mnem + " " + strings.Join(out, ", "), true, nil
}

// attSizeKeywords maps an operand width in bytes onto the NASM size keyword
// goa's operand parser understands. Indexed by width, so entry 0 is unused and
// the list runs to 8 (qword).
var attSizeKeywords = [...]string{
	0: "",
	1: "byte ",
	2: "word ",
	4: "dword ",
	8: "qword ",
}

// isMemOperand reports whether an AT&T operand is a memory reference, i.e.
// whether it carries a parenthesised base/index group.
func isMemOperand(tok string) bool {
	t := strings.TrimSpace(tok)
	return strings.Contains(t, "(") && strings.HasSuffix(t, ")")
}

// hasSizeKeyword reports whether a rewritten memory operand already states its
// width. attMemory only ever produces a bare `[...]`, so this is a guard
// against double-prefixing if that ever changes.
func hasSizeKeyword(s string) bool {
	l := strings.ToLower(s)
	for _, kw := range []string{"byte ", "word ", "dword ", "qword "} {
		if strings.HasPrefix(l, kw) {
			return true
		}
	}
	return false
}

// attMnemonic maps a raw AT&T mnemonic (size suffix possibly still attached)
// onto goa's spelling, and reports the operand width the suffix carried (0 when
// the mnemonic states none).
//
// Most of the time this is just "cut the operand-size suffix". Three families
// need more:
//
//   - movabs is GAS's dedicated 64-bit-immediate move. goa has no such
//     mnemonic; encodeMov infers the width from the destination register and
//     emits the imm32 sign-extended or a full imm64 as required, so the plain
//     `mov` is exactly right.
//
//   - movq and movd are ambiguous in AT&T: `movq %rsi, %rcx` is a 64-bit GPR
//     move while `movq %xmm0, %rax` moves a quadword between an SSE and a GPR
//     register. The name alone cannot say which, so the operand list decides --
//     any XMM operand means the SSE form.
//
//   - the conditional moves pack the condition and the width into one token
//     (`cmovgeq`, `cmovlneq`). goa dispatches on a "cmov" prefix and reads the
//     condition off the rest, so the size letter has to come off first; the
//     peel loop tries successive stems until one is recognised.
//
// The width is reported separately because a register operand already names
// its own size, while a memory operand has no such clue: `decl -4(%rbp)` is a
// 32-bit decrement and only the 'l' in the mnemonic says so.
//
// attNoOps maps AT&T's operand-less sign-extension idioms onto goa's
// spellings. GAS names them for their *source* width rather than their
// destination: `cqto` widens eax into edx:eax (so the result is 64-bit, which
// is goa's `cqo`), and `cwtq`/`cltq` widen ax/eax (goa's `cwde`/`cdqe`).
// Reading the names literally would encode the wrong width -- `cqto` as a
// 32-bit `cdq` leaves the upper half of rdx undefined.
var attNoOps = map[string]string{
	"cqto": "cqo",
	"cwtq": "cwde",
	"cltq": "cdqe",
	"cltd": "cdq",
	"cwtd": "cwd",
}

func attMnemonic(raw string, ops []string) (mnem string, width int) {
	if len(ops) == 0 {
		if m, ok := attNoOps[raw]; ok {
			return m, 0
		}
	}
	if attAmbiguous[raw] {
		if attHasXMM(ops) {
			return raw, 8 // movq/movd between xmm and gpr
		}
		return "mov", 8 // plain GPR move; the register fixes the width
	}
	if attIsSSEMnemonic(raw) {
		return raw, 0
	}
	// Peel the operand-size suffix, accepting the stem only once it names a
	// known instruction. The peeled stem is then normalised too, so the
	// movabs mapping below applies to `movabsq` as well as `movabs`.
	stem, w := raw, 0
	for len(stem) > 0 {
		switch stem[len(stem)-1] {
		case 'b':
			w = 1
		case 'w':
			w = 2
		case 'l':
			w = 4
		case 'q':
			w = 8
		default:
			return raw, 0
		}
		stem = stem[:len(stem)-1]
		if attMnemonics[stem] || attPrefixedMnemonic(stem) {
			return attCanonical(stem), w
		}
		// Keep peeling: `movslq` loses 'q' to become `movsl`, which is not a
		// mnemonic, but `movs` after one more letter is.
		w = 0
	}
	return raw, 0
}

// attPrefixedMnemonic accepts the families goa's encoder dispatches on by
// prefix rather than by exact name, so a condition code and a size suffix can
// share one token.
func attPrefixedMnemonic(stem string) bool {
	if strings.HasPrefix(stem, "cmov") && len(stem) > len("cmov") {
		_, ok := ccOf(stem[len("cmov"):])
		return ok
	}
	return false
}

// attCanonical normalises a stem that already has its size suffix removed.
// movabs is GAS's dedicated 64-bit-immediate move; goa has no such mnemonic,
// and encodeMov infers the width from the destination register, so the plain
// `mov` encodes it correctly.
func attCanonical(stem string) string {
	if stem == "movabs" {
		return "mov"
	}
	return stem
}

// attHasXMM reports whether any operand names an SSE register.
func attHasXMM(ops []string) bool {
	for _, o := range ops {
		t := strings.TrimPrefix(strings.TrimSpace(o), "%")
		if strings.HasPrefix(t, "xmm") {
			return true
		}
	}
	return false
}

// attAmbiguous lists mnemonics whose trailing letter is both a size suffix and
// part of a distinct instruction name.
var attAmbiguous = map[string]bool{"movq": true, "movd": true}

// attFlips reports whether a two-operand instruction's operands must be
// reversed to reach goa's destination-first convention.
//
// AT&T's default is source-first, so most things reverse: mov/movzx/movsx/lea
// and the SSE moves among them. Three groups do not:
//
//   - push/pop/jmp/jcc/inc/dec/shl/sar never reach here with two operands; a
//     single operand already means the same thing in both syntaxes.
//
//   - mul/div/idiv write their result to a fixed register pair, so their one
//     operand is a plain source with nothing to reorder.
//
//   - a trailing memory operand is already the destination, so the pair is in
//     goa's order -- with two exceptions where goa lists them the other way
//     round, both marked attIsPureStore below: a plain store, whose source
//     still has to be written before its destination, and an immediate, which
//     physically trails the instruction's other bytes.
//
// The three-operand imul is handled by the caller, not here: it is a rotate
// rather than a swap, because its destination is last in AT&T *and* first in
// Intel.
func attFlips(mnem string, ops []string) bool {
	switch mnem {
	case "mul", "div", "idiv", "neg", "not":
		return false
	}
	if len(ops) != 2 {
		return true
	}
	// A trailing memory operand is the destination in AT&T, so the pair is
	// already in goa's order -- except in the two cases where goa lists the
	// operands the other way round:
	//
	//   - a plain store. `movq %rax, -8(%rbp)` means the same thing as
	//     `mov [rbp-8], rax`; keeping AT&T's order would have goa read it as
	//     a *load* of rax from memory, which encodes as a completely
	//     different instruction.
	//   - an immediate. An immediate has no field of its own -- it rides in
	//     the trailing bytes -- so it must come after the memory operand:
	//     `movl $x, 32(%rbp)` becomes `mov dword [rbp+32], x`, and
	//     `addl $1000, -12(%rbp)` becomes `add dword [rbp-12], 1000`.
	//
	// A *register* source paired with memory is the case that must not flip:
	// goa's encodeArith puts a register source in ModRM.reg and the memory
	// destination in rm, so `add r14, [r15+64]` is already right.
	if isMemOperand(ops[1]) && isImmOperand(ops[0]) {
		return true
	}
	if isMemOperand(ops[1]) && !attIsPureStore(mnem, ops) {
		return false
	}
	return true
}

// attIsPureStore reports whether a two-operand instruction writes to memory
// without reading it first -- which is to say, whether the memory operand is a
// pure destination. Only then does the source/destination pair have to be
// reversed to reach goa's spelling.
func attIsPureStore(mnem string, ops []string) bool {
	switch mnem {
	case "mov", "movabs", "movzx", "movsx", "movsxd", "movd", "movq",
		"movaps", "movapd", "movdqa", "movss", "movsd":
		return true
	}
	return false
}

// isImmOperand reports whether an AT&T operand is an immediate (a '$' sigil).
func isImmOperand(tok string) bool {
	return strings.HasPrefix(strings.TrimSpace(tok), "$")
}

// attSplitMnemonic peels the mnemonic off the line and returns it *unmodified*
// -- the operand-size suffix is still attached. Deciding whether that suffix
// can be cut needs the operand list (see attMnemonic), so the two steps are
// deliberately separate: this one only finds the token boundary.
//
// The boundary is found with IndexAny rather than strings.SplitN, because
// SplitN's separator is a literal string, not a character set: passing " \t"
// looks like it should split on either character but actually searches for
// that exact two-byte sequence, so a tab-indented `movq<TAB>%rax, %rcx` comes
// back as a single unsplit token.
func attSplitMnemonic(ln string) (mnem, rest string) {
	s := strings.TrimSpace(ln)
	sp := strings.IndexAny(s, " \t")
	if sp < 0 {
		return s, ""
	}
	return s[:sp], strings.TrimSpace(s[sp+1:])
}

// attKeepWhole lists mnemonics whose trailing letter is part of the name, so
// the size-suffix stripper must leave them alone. The packed SSE operations end
// in a type letter (b=byte, w=word, d=dword, q=qword, s=single) that is *not* an
// operand-size suffix: `andpd` is a 66 0F 54 packed-double bitwise and, while
// `andq` does not exist at all. Reading the letter as a width would strip it and
// leave `andp`, which is not an instruction.
//
// The packed SSE names are matched by *shape* rather than listed one by one.
// GAS has a large, regular family of them -- addsd, cmplesd, minss,
// cvtdq2ps, ucomiss -- and every one ends in a two-letter element-type suffix
// (sd/ss/ps/pd) that looks exactly like an operand-size letter but is not one.
// Enumerating them would mean a new table entry for every comparison
// condition, and forgetting one turns a valid instruction into a bogus name.
var attKeepWhole = map[string]bool{
	"movsq": true, "movdqa": true, "movdqu": true, "movups": true, "movupd": true,
	"cvtsi2sd": true, "cvtsi2ss": true,
	"pand": true, "pandn": true, "por": true, "pxor": true,
	"punpck": true, "pshufd": true, "pshufb": true,
	// Packed conversions carry the *source* type mid-name and the
	// destination at the end, so neither suffix is an operand-size letter.
	"cvttpd2dq": true, "cvtpd2dq": true, "cvtdq2pd": true,
	"cvttpd2pi": true, "cvtpi2pd": true, "cvtsi2dp": true,
}

// attSSERoots are the instruction-name prefixes GAS combines with a two-letter
// element-type suffix to form the packed-SSE family.
var attSSERoots = []string{
	"mov", "add", "sub", "mul", "div", "sqrt", "min", "max", "cmp", "comi",
	"ucomi", "and", "or", "xor", "cvt", "rcp", "rsqrt", "hadd", "hsub",
}

// attIsSSEMnemonic reports whether a mnemonic belongs to the packed-SSE family
// and must therefore survive the size-suffix stripper intact.
func attIsSSEMnemonic(m string) bool {
	if attKeepWhole[m] {
		return true
	}
	for _, suf := range []string{"sd", "ss", "ps", "pd"} {
		if len(m) > len(suf) && strings.HasSuffix(m, suf) {
			stem := m[:len(m)-len(suf)]
			for _, root := range attSSERoots {
				if strings.HasPrefix(stem, root) {
					return true
				}
			}
		}
	}
	return false
}

// attMnemonics is the set of base instruction names the size stripper will
// accept. A stem not in this set keeps its original spelling, so an unknown
// mnemonic reaches encode() and produces a real error rather than being
// silently mangled into a different instruction.
var attMnemonics = map[string]bool{
	// data movement
	"mov": true, "movabs": true, "movzx": true, "movsx": true, "movsxd": true,
	// arithmetic / logic
	"add": true, "sub": true, "adc": true, "sbb": true, "and": true, "or": true,
	"xor": true, "cmp": true, "test": true, "inc": true, "dec": true,
	"neg": true, "not": true, "adcx": true,
	// multiply / divide
	"imul": true, "mul": true, "div": true, "idiv": true, "cqo": true,
	"cdq": true, "cwd": true, "cdqe": true, "cwde": true, "cltq": true, "cwtq": true,
	"cltd": true,
	// shifts and rotates
	"shl": true, "shr": true, "sar": true, "sal": true, "rol": true, "ror": true,
	"rcl": true, "rcr": true, "shld": true, "shrd": true,
	// control flow
	"call": true, "jmp": true, "ret": true, "leave": true,
	"jrcxz": true, "loop": true,
	// address computation
	"lea": true,
	// stack
	"push": true, "pop": true,
	// bit manipulation
	"bt": true, "bsf": true, "bsr": true, "bts": true, "btr": true, "btc": true,
	"bswap": true, "popcnt": true, "lzcnt": true, "tzcnt": true,
	// SSE scalar float
	"movsd": true, "movss": true, "movaps": true, "movapd": true,
	"addsd": true, "addss": true, "subsd": true, "subss": true,
	"mulsd": true, "mulss": true, "divsd": true, "divss": true,
	"sqrtsd": true, "sqrtss": true, "minsd": true, "minss": true,
	"maxsd": true, "maxss": true, "xorpd": true, "xorps": true,
	"ucomisd": true, "ucomiss": true, "comisd": true, "comiss": true,
	"cvtsi2sd": true, "cvtsi2ss": true, "cvtss2sd": true, "cvtsd2ss": true,
	"cvttsd2si": true, "cvttss2si": true, "cvtsd2si": true, "cvtss2si": true,
	// misc
	"nop": true, "pause": true, "hlt": true, "ud2": true, "int3": true,
	"cpuid": true, "rdtsc": true, "syscall": true, "endbr64": true,
	"lfence": true, "mfence": true, "sfence": true, "xchg": true,
	"cwtl": true,
}

// attSplitOperands breaks the operand list on commas that are at paren depth
// zero.
//
// This is the one piece of real parsing in the file. An AT&T memory operand is
// `disp(base,index,scale)` -- commas and all -- so splitting on every comma
// would tear `-8(%rbp,%rbx,4), %eax` into four pieces and lose the addressing
// entirely. Strings must also stay intact: `.asciz "a,b"` carries a comma
// inside quotes, and GAS character constants use the same quoting.
func attSplitOperands(rest string) []string {
	if strings.TrimSpace(rest) == "" {
		return nil
	}
	var out []string
	depth := 0
	inStr := byte(0)
	start := 0
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if inStr != 0 {
			if c == '\\' {
				i++ // skip the escaped character
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inStr = c
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(rest[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(rest[start:]))
	return out
}

// attOperand rewrites one AT&T operand into goa's spelling:
//
//	%rax            -> rax
//	$42 / $-1       -> 42 / -1
//	-8(%rbp)        -> [rbp-8]
//	(%rax,%rbx,4)   -> [rax+rbx*4]
//	G_x(%rip)       -> [rip+G_x]
//	.LBB0_2         -> .LBB0_2 (bare symbol)
//
// The memory form is rebuilt explicitly rather than by string surgery because
// goa's parser wants `[base+index*scale+disp]` with the displacement last and
// the sign carried on the base, matching what it already accepts in NASM
// source.
func attOperand(tok string, alias map[string]string) (string, error) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", fmt.Errorf("empty operand")
	}

	// Immediate.
	if strings.HasPrefix(tok, "$") {
		return attImmediate(tok[1:])
	}
	// Memory reference: the paren group must start somewhere in the token.
	if i := strings.Index(tok, "("); i >= 0 && strings.HasSuffix(tok, ")") {
		return attMemory(tok[:i], tok[i+1:len(tok)-1], alias)
	}
	// Register (possibly with a %-less spelling) or a bare symbol.
	sym := strings.TrimPrefix(tok, "%")
	if sym == "" {
		return "", fmt.Errorf("malformed operand %q", tok)
	}
	if t, ok := alias[sym]; ok {
		sym = t
	}
	return sym, nil
}

// attImmediate converts an AT&T immediate body. GAS accepts several spellings
// goa's parser does not:
//
//	$0x2a    hex            -> 42
//	$42      decimal        -> 42
//	$-1      negative       -> -1
//	$symbol  named constant -> left as a symbol
//
// A leading '$' on a symbol reference is a PC-relative absolute address
// (`$foo`); goa represents those as bare symbols too, since its `mov reg, sym`
// encoding already resolves the symbol rather than storing its address.
func attImmediate(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("empty immediate")
	}
	// Character literal: $'A'.
	if len(body) >= 2 && body[0] == '\'' && body[len(body)-1] == '\'' {
		return strconv.Itoa(int(body[1])), nil
	}
	// Plain number, possibly negative or in another base.
	if v, err := strconv.ParseInt(body, 0, 64); err == nil {
		return strconv.FormatInt(v, 10), nil
	}
	// A 64-bit value whose top bit is set does not fit an int64 literal:
	// GAS writes INT64_MIN as 0x8000000000000000, and ParseInt rejects it as
	// out of range. Parsing it as unsigned and reinterpreting recovers the
	// same bit pattern, which is what the encoding wants anyway. Without
	// this the value falls through to the symbol branch below and the link
	// fails with a nonsense "undefined symbol: 0x8000...".
	if v, err := strconv.ParseUint(body, 0, 64); err == nil && v >= 1<<63 {
		return strconv.FormatInt(int64(v), 10), nil
	}
	// Anything else is a symbol or an expression we do not fold; pass it
	// through as a symbol reference and let the assembler's own diagnostics
	// speak for it.
	if isIdentStart(body[0]) {
		return body, nil
	}
	return "", fmt.Errorf("unsupported immediate %q", body)
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '.' || c == '$' || c == '@' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// attMemory rebuilds `disp(base,index,scale)` as goa's `[base+index*scale+disp]`.
//
// The displacement may be absent (`(%rax)`), bare (`G_x(%rip)`), negative, or
// an expression. goa's parser accepts a leading minus on the displacement, so
// the sign is preserved rather than folded into the base.
func attMemory(disp, inner string, alias map[string]string) (string, error) {
	parts := strings.Split(inner, ",")
	base := strings.TrimSpace(parts[0])
	index := ""
	scale := ""
	if len(parts) > 1 {
		index = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		scale = strings.TrimSpace(parts[2])
	}
	base = strings.TrimPrefix(base, "%")
	index = strings.TrimPrefix(index, "%")

	var b strings.Builder
	b.WriteByte('[')
	if base != "" {
		b.WriteString(base)
	}
	if index != "" {
		if base != "" {
			b.WriteByte('+')
		}
		b.WriteString(index)
		if scale != "" && scale != "1" {
			b.WriteByte('*')
			b.WriteString(scale)
		}
	}
	if disp != "" {
		d := strings.TrimSpace(disp)
		// A symbolic displacement may carry an offset: `G_gm+4(%rip)` addresses
		// four bytes into G_gm. goa's memory syntax spells that
		// `[rip+G_gm+4]`, so the numeric tail has to be split off the name
		// rather than glued to it -- otherwise the whole thing looks like one
		// undefined symbol.
		if sym, off, ok := attSplitDispOffset(d); ok {
			if t, ok2 := alias[sym]; ok2 {
				sym = t
			}
			if base == "rip" {
				b.WriteByte('+')
				b.WriteString(sym)
				b.WriteString(off)
			} else if base == "" {
				b.WriteByte('[')
				b.WriteString("rip+")
				b.WriteString(sym)
				b.WriteString(off)
				b.WriteByte(']')
				return b.String(), nil
			} else {
				b.WriteByte('+')
				b.WriteString(sym)
				b.WriteString(off)
			}
			b.WriteByte(']')
			return b.String(), nil
		}
		if t, ok := alias[d]; ok {
			d = t
		}
		// A symbolic displacement is a RIP-relative target in AT&T; goa spells
		// the same reference `[rip+sym]`.
		if !isNumeric(d) {
			if base == "rip" {
				b.WriteByte('+')
				b.WriteString(d)
			} else if base == "" {
				b.WriteByte('[')
				b.WriteString("rip+")
				b.WriteString(d)
				b.WriteByte(']')
				return b.String(), nil
			} else {
				b.WriteByte('+')
				b.WriteString(d)
			}
		} else if d != "0" {
			if !strings.HasPrefix(d, "-") && b.Len() > 1 {
				b.WriteByte('+')
			}
			b.WriteString(d)
		}
	}
	b.WriteByte(']')
	return b.String(), nil
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	if _, err := strconv.ParseInt(s, 0, 64); err == nil {
		return true
	}
	// A float displacement (legal in GAS for .long) is not something goa's
	// memory parser can encode, but treating it as symbolic at least keeps the
	// line parseable and the error actionable.
	return false
}

// attSplitDispOffset separates a trailing numeric offset from a symbolic
// displacement: `G_gm+4` -> ("G_gm", "+4"), `.LCPI0_3-8` -> (".LCPI0_3", "-8").
// Returns ok=false when the displacement is purely numeric or purely symbolic,
// and when the name would be empty.
func attSplitDispOffset(d string) (sym, off string, ok bool) {
	if d == "" {
		return "", "", false
	}
	// Scan back over a run of digits, then require a sign just before them.
	i := len(d)
	for i > 0 && d[i-1] >= '0' && d[i-1] <= '9' {
		i--
	}
	if i == len(d) || i == 0 {
		return "", "", false // no digits, or nothing before them
	}
	if d[i-1] != '+' && d[i-1] != '-' {
		return "", "", false
	}
	sym = d[:i-1]
	if sym == "" || !isIdentStart(sym[0]) {
		return "", "", false
	}
	// Reject a name that merely ends in digits preceded by a letter, e.g.
	// `.L2` -- there `2` is part of the name, not an offset. A real offset
	// always has a sign, which is the condition already checked above.
	return sym, d[i-1:], true
}
