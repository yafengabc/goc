package goa

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// GAS directive handling
// ---------------------------------------------------------------------------
//
// LLVM's Windows AsmPrinter wraps every function in a COFF symbol definition
// (.def/.scl/.type/.endef) and a SEH unwind block (.seh_proc ... .seh_endproc),
// emits an .ident, and lays data out with .p2align/.long/.quad/.asciz/.zero.
// None of that exists in goa's assembler, and all of it is either metadata we
// already derive ourselves or padding we can reproduce. This file consumes the
// directives and passes the real instructions through to processLine.

// attAssemble runs the full pipeline over AT&T source: collect externs, then
// emit. It mirrors Assemble() so the caller can treat an LLVM .s file exactly
// like a hand-written .asm.
func attAssemble(a *Assembler, src string) error {
	lines := strings.Split(src, "\n")

	// Pass 1: externs. goc's C code calls Win32/Libc entry points that LLVM
	// simply emits as undefined `.globl` symbols; the C front end knows which
	// DLL each belongs to and registers them before handing us the assembly.
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "extern ") {
			if err := a.parseExtern(strings.TrimSpace(ln)); err != nil {
				return err
			}
		}
	}

	if a.target == targetELF {
		if err := a.emitSyscallStubs(); err != nil {
			return err
		}
	}

	// Pass 2: emit. attState carries the little that cannot be recovered from
	// the instruction stream alone.
	st := &attState{}
	a.attAlias = attDefaultAliases()
	a.attMode = true
	for i, raw := range lines {
		ln := strings.TrimSpace(stripATTComment(raw))
		if ln == "" {
			continue
		}
		if err := st.line(a, ln, i+1); err != nil {
			return fmt.Errorf("line %d: %q: %w", i+1, strings.TrimSpace(raw), err)
		}
	}
	if a.uwCur != nil {
		a.uwCur.end = len(a.sections[a.uwCur.sect].Data)
		if a.uwCur.hasProlog {
			a.uwRecs = append(a.uwRecs, a.uwCur)
		}
		a.uwCur = nil
	}
	return nil
}

// attDefaultAliases maps the symbols LLVM's Windows AsmPrinter invents onto
// the ones goa actually defines.
//
// `__main` is the C runtime's constructor hook: a real link resolves it to the
// CRT's stub, which runs static initialisers before main. goc has no separate
// static-initialiser phase -- file-scope data is emitted as ordinary .data --
// so the hook has nothing to do and the call is folded onto main itself.
// Leaving it undefined would fail the link with a bare "undefined symbol".
func attDefaultAliases() map[string]string {
	return map[string]string{"__main": "main"}
}

// attAliased resolves a symbol through the alias table, reporting whether one
// applied.
func (a *Assembler) attAliased(sym string) (string, bool) {
	if a.attAlias == nil {
		return sym, false
	}
	t, ok := a.attAlias[sym]
	return t, ok
}

// attState tracks the few pieces of context that spans multiple lines.
type attState struct {
	// pendingDef holds the symbol name between `.def name;` and its
	// `.scl`/`.type`/`.endef` block, so `.endef` knows what it closes.
	pendingDef string
	// inDef suppresses symbol emission inside a .def block.
	inDef bool
	// sehFunc records the function whose unwind block is open, and the SEH
	// directives are translated into the same uwFunc record goa builds from a
	// NASM prolog. LLVM states the frame explicitly (.seh_stackalloc N,
	//	// .seh_pushreg %rbx) where goa infers it, so we capture the same facts.
	seh *attSEH
	// curSectionName remembers the last real section so a function body that
	// omits a repeated `.text` still lands in the right place.
	curSectionName string
}

// attSEH accumulates one function's unwind description.
type attSEH struct {
	name   string
	pushes []int
	alloc  int
}

// stripATTComment removes a `#` comment. GAS uses '#' (a ';' starts a comment
// only in some flavours, and LLVM emits '#'), and the marker must be ignored
// inside string literals -- a '#' in .asciz data is data.
func stripATTComment(ln string) string {
	inStr := byte(0)
	for i := 0; i < len(ln); i++ {
		c := ln[i]
		if inStr != 0 {
			if c == '\\' {
				i++
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
		case '#':
			return ln[:i]
		case ';':
			// Some GAS dialects -- and plenty of hand-written assembly --
			// use ';' where x86 GAS uses '#'. LLVM emits '#', so this is not
			// needed for the machine-generated path, but accepting it costs
			// nothing and turns a silently mis-parsed line (the trailing text
			// would be read as more operands) into a comment.
			//
			// A ';' *inside* a string is data, and one that opens a `.def`
			// statement is GAS's own terminator, so the character is only
			// treated as a comment marker at the start of a line or after
			// whitespace.
			if i == 0 || ln[i-1] == ' ' || ln[i-1] == '\t' {
				return ln[:i]
			}
		case '/':
			if i+1 < len(ln) && ln[i+1] == '/' {
				return ln[:i]
			}
		}
	}
	return ln
}

// line dispatches one already-trimmed, comment-free AT&T line.
func (s *attState) line(a *Assembler, ln string, lineNo int) error {
	// Label definition: `name:` with nothing after it, or `name: insn`.
	if label, rest, ok := splitLabel(ln); ok && !isRegisterName(label) {
		if err := s.label(a, label); err != nil {
			return err
		}
		if rest == "" {
			return nil
		}
		return s.line(a, rest, lineNo)
	}
	// `name .quad ...` -- GAS allows a bare symbol immediately followed by a data
	// directive with no colon. The leading-dot test matters: without it a line
	// that *is* just a data directive (".long 42") would match too, with the
	// directive's own name taken for a symbol, defining a bogus `.long` label
	// and re-entering here with the operand as if it were code.
	if fields := strings.Fields(ln); len(fields) >= 2 &&
		!strings.HasPrefix(fields[0], ".") && attIsDataDirective(fields[1]) {
		if !isRegisterName(fields[0]) {
			if err := s.label(a, fields[0]); err != nil {
				return err
			}
			return s.line(a, strings.TrimSpace(ln[len(fields[0]):]), lineNo)
		}
	}
	// `symbol = expression` (the `@feat.00 = 0` idiom LLVM emits).
	if eq := strings.Index(ln, "="); eq > 0 && !strings.ContainsAny(ln, "\"'") {
		left := strings.TrimSpace(ln[:eq])
		right := strings.TrimSpace(ln[eq+1:])
		if !isRegisterName(left) && !strings.HasPrefix(left, ".") {
			if v, err := strconv.ParseInt(right, 0, 64); err == nil {
				a.consts[left] = v
				return nil
			}
		}
	}

	fields := strings.Fields(ln)
	if len(fields) == 0 {
		return nil
	}
	head := fields[0]
	if strings.HasPrefix(head, ".") {
		return s.directive(a, head, strings.TrimSpace(ln[len(head):]), ln)
	}
	// goa's own dialect spells the section and export directives without a
	// leading dot (`section .text`, `global main`), and an entry stub written
	// for AssembleATT naturally uses that spelling. Accepting both costs one
	// lookup and removes a trap: without it `section .rdata,"dr"` falls
	// through to the instruction path, where the attribute string is mistaken
	// for operands and a section named `"dr"` appears out of nowhere.
	//
	// `extern` is not listed because pass 1 already consumed every line
	// starting with it, before anything reaches here.
	switch head {
	case "section", "global", "globl":
		return s.directive(a, "."+head, strings.TrimSpace(ln[len(head):]), ln)
	}

	// A real instruction: translate into goa's syntax and reuse the encoder.
	goaLn, ok, err := attTranslate(ln, a.attAlias)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	// Capture the prolog shape for unwind emission, then encode. LLVM's
	// .seh_pushreg/.seh_stackalloc are handled as directives; goa's own
	// pattern matcher only sees the translated push/sub lines, which is
	// exactly the shape it already understands.
	if s.seh != nil {
		s.seh.observe(goaLn)
	}
	return a.processLine(goaLn)
}

// attIsBlockLabel reports whether a dot-prefixed label is local to the
// function it sits in, and so has to be qualified with that function's name to
// stay distinct from an identically-named label elsewhere.
//
// The distinction matters because goa's own `qualify` prefixes *every* dot-name
// with the enclosing global, which is right for the hand-written asm it was
// built for but wrong for LLVM output. GAS has two kinds of dot-label and only
// one of them is function-scoped:
//
//   - .LBB<n>_<m> -- a basic-block label, named after the function number, and
//     only ever branched to from inside that function.
//   - .Ltmp<n>, .Lfunc_begin<n> -- temporaries, also function-scoped.
//
// Everything else -- `.str.0`, `.LCPI0_3`, `.Lswitch` -- is a file-scope
// private name. `.str.0` in particular appears once in .data but is referenced
// from a dozen functions, so qualifying it per-function would make every
// reference dangle.
func attIsBlockLabel(label string) bool {
	if !strings.HasPrefix(label, ".") {
		return false
	}
	rest := label[1:]
	return strings.HasPrefix(rest, "LBB") ||
		strings.HasPrefix(rest, "Ltmp") ||
		strings.HasPrefix(rest, "Lfunc")
}

// label defines a symbol at the current position.
func (s *attState) label(a *Assembler, label string) error {
	// A label ending in ':' has already been stripped by splitLabel.
	if label == "" || strings.HasPrefix(label, ";") {
		return nil
	}
	if attIsBlockLabel(label) {
		a.defineSym(a.qualify(label))
		return nil
	}
	if !strings.HasPrefix(label, ".") {
		a.curGlobal = label
		off := a.curOff()
		a.uwCloseFunc(off, a.cur)
		a.uwCur = &uwFunc{sect: a.cur, start: off}
	}
	a.defineSym(a.qualify(label))
	return nil
}

func isRegisterName(s string) bool {
	_, ok := regIndexMap[strings.TrimPrefix(s, "%")]
	return ok
}

func attIsDataDirective(s string) bool {
	switch s {
	case ".quad", ".long", ".short", ".byte", ".word", ".ascii", ".asciz",
		".string", ".zero", ".space", ".fill", ".value", ".2byte", ".4byte",
		".8byte":
		return true
	}
	return false
}

// directive consumes one GAS pseudo-op. Anything that carries no code and no
// data is acknowledged and dropped, so the instruction stream stays clean.
func (s *attState) directive(a *Assembler, name, rest, ln string) error {
	switch name {
	// ---- symbol bookkeeping -------------------------------------------
	case ".def":
		// `.def @feat.00;` opens a COFF symbol definition. The trailing ';'
		// is GAS's own statement terminator, not part of the name.
		sym := strings.TrimSuffix(strings.TrimSpace(rest), ";")
		sym = strings.TrimSuffix(sym, ";")
		sym = strings.TrimSpace(sym)
		s.pendingDef = sym
		s.inDef = true
		return nil
	case ".scl", ".type", ".endef", ".size", ".ident", ".file", ".version",
		".addrsig", ".addrsig_sym", ".cv_file":
		if name == ".endef" {
			s.inDef = false
			s.pendingDef = ""
		}
		return nil
	case ".globl", ".global":
		// `.globl name` exports a symbol, which goa already does for every
		// label it defines, and the DLL binding for an *undefined* symbol
		// comes from the extern table rather than from here.
		//
		// It also carries goa's own meaning: `global name` is how a source
		// nominates the program entry point, and an entry stub assembled
		// through AssembleATT needs that. LLVM never emits `.globl _start`
		// (it produces relocatable objects), so honouring the directive costs
		// nothing on the machine-generated path and makes hand-written stubs
		// behave the way the rest of goa does.
		if name := strings.TrimSpace(rest); name != "" {
			a.entry = strings.TrimSuffix(strings.Fields(name)[0], ":")
		}
		return nil
	case ".local", ".protected", ".hidden", ".weak", ".private":
		return nil

	// ---- section / layout --------------------------------------------
	case ".section":
		// GAS names the section then quotes its attributes: `.section
		// .rdata,"dr"`. Only the name matters here -- goa decides
		// writability from which section it is, and the flags are advisory
		// metadata LLVM derives from the same knowledge.
		//
		// The cut is on the double quote alone, and the separator in front of
		// it has to go with it. Trimming a character *set* that happens to
		// include the CR of a CRLF line ending would tear ".rdata" into
		// "dr", silently creating a section by that name -- and since a
		// fresh section is what an unknown name falls back to, the file
		// would still assemble, just into the wrong layout.
		//
		// ".rdata" alone would be no better: the name goes into the section
		// table verbatim, and an entry called ".rdata," is what actually
		// ends up there.
		nm := strings.TrimSpace(rest)
		if q := strings.IndexByte(nm, '"'); q >= 0 {
			nm = nm[:q]
		}
		nm = strings.TrimSpace(strings.TrimRight(nm, ","))
		return attNamedSection(a, nm)
	case ".text":
		return attSection(a, ".text", true, false)
	case ".data":
		return attSection(a, ".data", true, false)
	case ".rdata":
		return attSection(a, ".rdata", true, false)
	case ".bss":
		return attSection(a, ".bss", true, true)
	case ".rodata":
		return attSection(a, ".rdata", true, false)
	case ".p2align", ".align", ".balign":
		// Alignment is a promise about where the *next* item lands. goa's
		// sections are byte arrays with no alignment attribute, and the
		// encoder's disp32 rip-relative accesses do not require it, so the
		// padding is not reproduced. It is recorded for diagnostics only.
		_, _ = a, rest
		return nil
	case ".p2fi", ".loc", ".cv_loc":
		return nil

	// ---- SEH unwind ----------------------------------------------------
	case ".seh_proc":
		s.seh = &attSEH{name: strings.TrimSpace(rest)}
		return nil
	case ".seh_endproc":
		s.seh = nil
		return nil
	case ".seh_pushreg":
		if s.seh == nil {
			return nil
		}
		if r, ok := uwRegNum(strings.TrimPrefix(strings.TrimSpace(rest), "%")); ok {
			s.seh.pushes = append(s.seh.pushes, r)
		}
		return nil
	case ".seh_stackalloc":
		if s.seh == nil {
			return nil
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(rest), 0, 64); err == nil {
			s.seh.alloc = int(n)
		}
		return nil
	case ".seh_setframe", ".seh_savereg", ".seh_savexmm", ".seh_setjmpframe",
		".seh_endprologue", ".seh_startepilogue", ".seh_endepilogue",
		".seh_zeroframe", ".seh_popsection":
		// Frame-pointer and stack-pointer bookkeeping. goa derives the same
		// facts from the emitted push/sub sequence, so these are redundant.
		return nil

	// ---- data ----------------------------------------------------------
	case ".quad", ".8byte":
		return attDataDQ(a, rest)
	case ".long", ".4byte":
		return attDataDD(a, rest)
	case ".short", ".2byte", ".word":
		return attDataDW(a, rest)
	case ".byte":
		return attDataDB(a, rest)
	case ".ascii":
		return attDataStr(a, rest, false)
	case ".asciz", ".string":
		return attDataStr(a, rest, true)
	case ".zero", ".space":
		return attDataZero(a, rest)
	case ".fill":
		return attDataFill(a, rest)
	case ".comm":
		return attComm(a, rest)
	case ".value":
		v, err := strconv.ParseInt(strings.TrimSpace(rest), 0, 64)
		if err != nil {
			return fmt.Errorf("bad .value %q", rest)
		}
		a.emitInt64(v)
		return nil
	}
	// Unknown directive: ignore rather than fail. LLVM occasionally emits
	// annotations that carry no encoding meaning, and refusing to assemble a
	// whole program over one of them helps nobody.
	return nil
}

// observe records the pushes and the stack allocation of a translated
// instruction so the function gets an unwind record even when its prolog is
// not the exact shape goa's matcher expects.
func (e *attSEH) observe(goaLn string) {
	if r, ok := uwParsePush(goaLn); ok {
		e.pushes = append(e.pushes, r)
	}
	if n, ok := uwParseSubRsp(goaLn); ok {
		e.alloc = n
	}
}

// attNamedSection switches to a section named by a `.section` directive. GAS
// allows the name to carry a suffix that encodes its purpose
// (`.rdata$.refptr.G_x`, `.text$mn`), and every such fragment belongs to the
// base section -- the suffix exists to let the linker discard or fold the
// group, which a whole-program assembler has no use for.
func attNamedSection(a *Assembler, name string) error {
	// The attribute list is separated by a comma (`.section .rdata,"dr"`), and
	// a name that still carries one would land in the section table verbatim.
	// Trimming here as well as at the call site keeps the guarantee local: no
	// matter which path a section name arrives by, it cannot smuggle a
	// separator through into the image.
	name = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(name), ","))
	if name == "" {
		return nil
	}
	base := name
	if i := strings.Index(base, "$"); i > 0 {
		base = base[:i]
	}
	switch base {
	case ".text", ".data", ".rdata", ".rodata", ".bss":
		return attSection(a, base, true, base == ".bss")
	}
	// An unrecognised section becomes a writable data section of its own, so
	// emitted bytes are never silently dropped.
	return attSection(a, base, true, false)
}

func attSection(a *Assembler, name string, writable, bss bool) error {
	s := a.sectionByName(name)
	if s == nil {
		s = a.newSection(name, writable, name == ".text")
		s.Bss = bss
		a.cur = len(a.sections) - 1
		return nil
	}
	for i, ss := range a.sections {
		if ss == s {
			a.cur = i
		}
	}
	return nil
}

// attParseNumber evaluates an integer literal in a data directive. It accepts
// the full unsigned 64-bit range, because GAS happily writes INT64_MIN as
// 0x8000000000000000 and strconv.ParseInt rejects that as out of range. Without
// the fallback such a value falls through to the symbol branch and the link
// fails with a nonsense "undefined symbol: 0x8000...".
func attParseNumber(tok string) (int64, bool) {
	if v, err := strconv.ParseInt(tok, 0, 64); err == nil {
		return v, true
	}
	if v, err := strconv.ParseUint(tok, 0, 64); err == nil {
		return int64(v), true
	}
	return 0, false
}

// attDataDQ emits 64-bit items (.quad). The operand is usually a symbol
// reference (a relocated pointer) or a small integer, so the bytes are
// reserved and a fixup recorded rather than a value written outright.
func attDataDQ(a *Assembler, rest string) error {
	for _, tok := range attSplitOperands(rest) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if v, ok := attParseNumber(tok); ok {
			a.emitInt64(v)
			continue
		}
		// A symbol: reserve 8 bytes and point them at it. The absolute+wide
		// pair is what a data pointer slot needs -- it is dereferenced
		// directly, so it must hold the full virtual address.
		off := a.curOff()
		a.emitInt64(0)
		a.fixups = append(a.fixups, Fixup{
			sect: a.cur, off: off, sym: a.qualify(tok),
			absolute: true, wide: true, virtual: true,
		})
	}
	return nil
}

// attDataDD emits 32-bit items (.long), including float bit patterns.
func attDataDD(a *Assembler, rest string) error {
	for _, tok := range attSplitOperands(rest) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if v, ok := attParseNumber(tok); ok {
			a.emitInt32(int32(v))
			continue
		}
		if f, err := strconv.ParseFloat(tok, 64); err == nil {
			a.emitInt32(int32(float32bits(f)))
			continue
		}
		// A label difference: `.long .LBB29_14-.LJTI29_0`. This is a switch's
		// jump table, where each entry records how far its case target sits
		// from the table's own base. The two labels are typically in different
		// sections, so the value is not knowable until both are placed -- it
		// needs a relocation against the pair, which Fixup.sym2 carries.
		if lhs, rhs, ok := attSplitSymDiff(tok); ok {
			off := a.curOff()
			a.emitInt32(0)
			a.fixups = append(a.fixups, Fixup{
				sect: a.cur, off: off,
				sym: a.qualify(lhs), sym2: a.qualify(rhs),
			})
			continue
		}
		off := a.curOff()
		a.emitInt32(0)
		a.fixups = append(a.fixups, Fixup{
			sect: a.cur, off: off, sym: a.qualify(tok), absolute: true,
		})
	}
	return nil
}

// attSplitSymDiff recognises a two-symbol difference and returns the two names.
//
// The minus sign is the only thing separating the operands, so the split is
// unambiguous: neither an x86 nor an LLVM-generated label name may contain
// one. The case that matters is a switch jump table, where the right-hand side
// is the table's base label; the linker turns the pair into a real offset once
// both addresses are known.
func attSplitSymDiff(tok string) (lhs, rhs string, ok bool) {
	// Scan from the right so a leading '-' (a negative constant) is not
	// mistaken for the separator.
	i := strings.LastIndex(tok, "-")
	if i <= 0 || i == len(tok)-1 {
		return "", "", false
	}
	lhs, rhs = tok[:i], tok[i+1:]
	if !isSymName(lhs) || !isSymName(rhs) {
		return "", "", false
	}
	return lhs, rhs, true
}

func attDataDW(a *Assembler, rest string) error {
	for _, tok := range attSplitOperands(rest) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if v, ok := attParseNumber(tok); ok {
			a.emitInt16(int16(v))
			continue
		}
		off := a.curOff()
		a.emitInt32(0)
		a.fixups = append(a.fixups, Fixup{
			sect: a.cur, off: off, sym: a.qualify(tok), absolute: true,
		})
	}
	return nil
}

func attDataDB(a *Assembler, rest string) error {
	return a.emitDB(rest)
}

// sectionNames lists the section names in creation order -- diagnostics only.
func (a *Assembler) sectionNames() []string {
	out := make([]string, len(a.sections))
	for i, s := range a.sections {
		out[i] = s.Name
	}
	return out
}

// float32bits returns the IEEE-754 single-precision encoding of f. A .long
// holding a float literal (`.long 0x3f800000` written as `.long 1.0`) must
// store the 32-bit pattern, not the double one.
func float32bits(f float64) uint32 { return math.Float32bits(float32(f)) }

// attDataStr emits string data. GAS quotes with "..." or '...' and, critically,
// distinguishes the NUL-terminated spellings (.asciz, .string) from the raw one
// (.ascii). Forgetting the terminator shifts every following byte of the
// section, which shows up much later as a corrupted pointer or a string that
// runs into the next item -- so the flag is honoured rather than assumed.
func attDataStr(a *Assembler, rest string, nulTerm bool) error {
	for _, tok := range attSplitOperands(rest) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		s := attUnquote(tok)
		if s == "" && tok != `""` {
			// Not a quoted literal -- try a bare integer (a NUL byte).
			if v, err := strconv.ParseInt(tok, 0, 64); err == nil {
				a.emitByte(byte(v))
				continue
			}
			return fmt.Errorf("bad string operand %q", tok)
		}
		a.emitBytes([]byte(s))
		if nulTerm {
			a.emitByte(0)
		}
	}
	return nil
}

// attUnquote resolves a GAS string literal, honouring the C escapes LLVM
// emits (\\, \", \n, \t, \r, \0 and hex/octal forms).
func attUnquote(tok string) string {
	if len(tok) < 2 {
		return ""
	}
	q := tok[0]
	if tok[len(tok)-1] != q {
		return ""
	}
	body := tok[1 : len(tok)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			b.WriteByte(c)
			continue
		}
		i++
		switch body[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '0':
			b.WriteByte(0)
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case '\'':
			b.WriteByte('\'')
		default:
			b.WriteByte('\\')
			b.WriteByte(body[i])
		}
	}
	return b.String()
}

// attDataZero reserves N zero bytes.
//
// Inside .bss this is a bare reservation: the loader zero-fills that memory, so
// writing N zero bytes would only bloat the image. `reserve` advances the
// section's virtual offset while leaving its file bytes empty, which is exactly
// what an uninitialised buffer wants.
func attDataZero(a *Assembler, rest string) error {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return nil
	}
	n, err := strconv.ParseInt(fields[0], 0, 64)
	if err != nil || n < 0 {
		return fmt.Errorf("bad .zero count %q", rest)
	}
	// A second operand is the fill byte; LLVM only ever emits the default 0,
	// which a reservation already provides.
	if a.curSection().Bss {
		a.reserve(int(n))
		return nil
	}
	for i := int64(0); i < n; i++ {
		a.emitByte(0)
	}
	return nil
}

// attDataFill handles `.fill count, size, value`.
func attDataFill(a *Assembler, rest string) error {
	parts := strings.Split(rest, ",")
	if len(parts) < 3 {
		return fmt.Errorf("bad .fill %q", rest)
	}
	count, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 0, 64)
	size, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 0, 64)
	val, err3 := strconv.ParseInt(strings.TrimSpace(parts[2]), 0, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return fmt.Errorf("bad .fill %q", rest)
	}
	for i := int64(0); i < count; i++ {
		for j := int64(0); j < size; j++ {
			a.emitByte(byte(val >> (8 * j)))
		}
	}
	return nil
}

// attComm handles `.comm name, size, align` -- a tentative common-block
// definition. goc's own globals carry initialisers and are emitted as .data,
// so this only needs to reserve the space.
func attComm(a *Assembler, rest string) error {
	parts := strings.Split(rest, ",")
	if len(parts) < 2 {
		return fmt.Errorf("bad .comm %q", rest)
	}
	name := strings.TrimSpace(parts[0])
	size, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 0, 64)
	if err != nil || size < 0 {
		return fmt.Errorf("bad .comm size %q", rest)
	}
	if err := attSection(a, ".bss", true, true); err != nil {
		return err
	}
	a.defineSym(a.qualify(name))
	a.reserve(int(size))
	return nil
}
