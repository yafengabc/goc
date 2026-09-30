package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ---------------------------------------------------------------------------
// Data model
// ---------------------------------------------------------------------------

const (
	K_REG = iota // general purpose 64-bit register
	K_IMM        // immediate integer
	K_MEM        // memory operand ([rip+sym] or [reg])
	K_SYM        // bare symbol (jump/call target, lea target)
)

type Operand struct {
	kind       int
	reg        int    // register index 0..15 (K_REG, K_MEM reg-indirect)
	isByte     bool   // register is an 8-bit register (al/cl/dl/bl/...b)
	is16       bool   // register is a 16-bit register (ax/cx/.../r15w)
	is32       bool   // register is a 32-bit register (eax/ecx/.../r15d)
	isXMM      bool   // register is an XMM register (xmm0..xmm15)
	imm        int64  // K_IMM
	memReg     int    // K_MEM register-indirect register index ([reg] form)
	memSym     string // K_MEM rip-relative symbol ([rip+sym] form)
	memBase    int    // K_MEM base register for [base+index*scale+disp]
	memHasBase bool   // K_MEM: operand has a base register
	memIndex   int    // K_MEM index register (-1 = none) for [base+index*scale]
	memScale   int    // K_MEM index scale (1,2,4,8)
	memDisp    int    // K_MEM displacement
	memHasDisp bool   // K_MEM: a displacement was explicitly provided (even if 0)
	memWidth   int    // K_MEM: explicit operand width from byte/word/dword/qword prefix (0 = infer)
	isRip      bool   // K_MEM: true => [rip+sym], false => register-based memory
	sym        string // K_SYM
}

type Section struct {
	Name     string
	Writable bool
	Code     bool
	Data     []byte
}

type symLoc struct {
	sect int // index into assembler.sections
	off  int // offset within section
}

type Fixup struct {
	sect   int    // section index where the displacement bytes live
	off    int    // offset of the displacement within that section
	sym    string // target symbol (label, data label, or "IAT:name")
	ripAdj int    // extra bytes after the displacement before the true RIP
	// (0 normally; 1 for C6 mov r/m8,imm8 which has a trailing imm8)
	short bool // true => a 1-byte displacement (rel8 short jump), not disp32
}

type Assembler struct {
	sections []*Section
	cur      int // current section index
	syms     map[string]symLoc
	fixups   []Fixup
	exts     map[string]string // extern name -> dll (without .dll)
	consts   map[string]int64
	entry    string
	// subsystem: 3 = console (default), 2 = windows GUI. Driven by the
	// `subsystem windows` directive; a GUI app must not request a console.
	subsystem uint16
	// target selects the output container: targetPE writes a Windows PE32+
	// (with an import table for externs), targetELF writes a static Linux
	// ELF64 where externs become raw syscall stubs.
	target int
	// curGlobal is the most recent non-local label. Labels starting with '.'
	// are local to it (NASM/GAS convention), so two functions may each have
	// a `.Lrec` without their jumps cross-wiring to the other one.
	curGlobal string
	// shortNext asks the next jump for its rel8 form; it is set by the
	// `jmp short label` spelling and cleared once that jump is encoded, so
	// the flag can never leak onto a following instruction.
	shortNext bool
}

// isLocalLabel reports whether a label name is file-local.
func isLocalLabel(s string) bool { return strings.HasPrefix(s, ".") }

// qualify maps a label reference to its real symbol name.
func (a *Assembler) qualify(sym string) string {
	if isLocalLabel(sym) {
		return a.curGlobal + sym
	}
	return sym
}

const (
	subsysWindows = 2 // IMAGE_SUBSYSTEM_WINDOWS_GUI
	subsysConsole = 3 // IMAGE_SUBSYSTEM_WINDOWS_CUI
)

const (
	targetPE = iota
	targetELF
)

// linuxSyscalls maps the syscall names an ELF-target program may declare with
// `extern`. Each becomes a stub that loads the number into rax and does
// `syscall`, so the call site uses the ordinary SysV register arguments
// (rdi, rsi, rdx, r10, r8, r9) with no libc involved.
var linuxSyscalls = map[string]int64{
	"read":          0,
	"write":         1,
	"open":          2,
	"close":         3,
	"lseek":         8,
	"mmap":          9,
	"mprotect":      10,
	"munmap":        11,
	"brk":           12,
	"ioctl":         16,
	"writev":        20,
	"nanosleep":     35,
	"getpid":        39,
	"exit":          60,
	"kill":          62,
	"exit_group":    231,
	"gettimeofday":  96,
	"clock_gettime": 228,
	"unlink":        87,
	"rename":        82,
	// __goclib_rename: the alias goclib's rename() wrapper calls for the raw
	// syscall. The wrapper cannot `extern rename` itself -- the name resolves
	// to its own definition, an infinite recursion (see goclib/file.c).
	"__goclib_rename": 82,
}

func NewAssembler() *Assembler {
	a := &Assembler{
		syms:      map[string]symLoc{},
		exts:      map[string]string{},
		consts:    map[string]int64{},
		subsystem: subsysConsole,
		target:    targetPE,
	}
	// default .text section
	a.newSection(".text", false, true)
	a.cur = 0
	return a
}

// ---------------------------------------------------------------------------
// Register table
// ---------------------------------------------------------------------------

var regIndexMap = map[string]int{
	"rax": 0, "rcx": 1, "rdx": 2, "rbx": 3, "rsp": 4, "rbp": 5, "rsi": 6, "rdi": 7,
	"r8": 8, "r9": 9, "r10": 10, "r11": 11, "r12": 12, "r13": 13, "r14": 14, "r15": 15,
}

// reg32Map / reg16Map map the 32-bit and 16-bit register aliases to the same
// index as their 64-bit counterpart. They let the compiler emit "mov eax,
// [rbp+16]" (a 4-byte load) without a dword prefix, and the assembler infers
// the access width from the register name.
var reg32Map = map[string]int{
	"eax": 0, "ecx": 1, "edx": 2, "ebx": 3, "esp": 4, "ebp": 5, "esi": 6, "edi": 7,
	"r8d": 8, "r9d": 9, "r10d": 10, "r11d": 11, "r12d": 12, "r13d": 13, "r14d": 14, "r15d": 15,
}

var reg16Map = map[string]int{
	"ax": 0, "cx": 1, "dx": 2, "bx": 3, "sp": 4, "bp": 5, "si": 6, "di": 7,
	"r8w": 8, "r9w": 9, "r10w": 10, "r11w": 11, "r12w": 12, "r13w": 13, "r14w": 14, "r15w": 15,
}

func regIndex(s string) (int, bool) {
	v, ok := regIndexMap[s]
	return v, ok
}

// byte registers map to the same index as their 64-bit counterpart.
var byteRegMap = map[string]int{
	"al": 0, "cl": 1, "dl": 2, "bl": 3, "spl": 4, "bpl": 5, "sil": 6, "dil": 7,
	"r8b": 8, "r9b": 9, "r10b": 10, "r11b": 11, "r12b": 12, "r13b": 13, "r14b": 14, "r15b": 15,
}

// xmm register table. Index 0..15 matches the low 3 bits of the XMM register
// number, and the REX.R / REX.B bits extend an XMM just like a GP register.
var xmmRegMap = map[string]int{
	"xmm0": 0, "xmm1": 1, "xmm2": 2, "xmm3": 3, "xmm4": 4, "xmm5": 5, "xmm6": 6, "xmm7": 7,
	"xmm8": 8, "xmm9": 9, "xmm10": 10, "xmm11": 11, "xmm12": 12, "xmm13": 13, "xmm14": 14, "xmm15": 15,
}

// ---------------------------------------------------------------------------
// Section helpers
// ---------------------------------------------------------------------------

func (a *Assembler) newSection(name string, writable, code bool) *Section {
	s := &Section{Name: name, Writable: writable, Code: code}
	a.sections = append(a.sections, s)
	return s
}

func (a *Assembler) sectionByName(name string) *Section {
	for _, s := range a.sections {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (a *Assembler) curSection() *Section {
	return a.sections[a.cur]
}

func (a *Assembler) emitByte(b byte) {
	a.curSection().Data = append(a.curSection().Data, b)
}
func (a *Assembler) emitBytes(bs []byte) {
	a.curSection().Data = append(a.curSection().Data, bs...)
}
func (a *Assembler) emitInt32(v int32) {
	a.curSection().Data = append(a.curSection().Data, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
func (a *Assembler) emitInt64(v int64) {
	for i := 0; i < 8; i++ {
		a.emitByte(byte(v >> (8 * i)))
	}
}
func (a *Assembler) curOff() int { return len(a.curSection().Data) }

func (a *Assembler) defineSym(name string) {
	if _, exists := a.syms[name]; exists {
		// ELF syscall stubs are reserved: keep the first (stub) definition so
		// `call exit` from asm always reaches the kernel, even if a C function
		// (e.g. goclib's `void exit(int)`) later defines a label with the same
		// name. C-facing calls to that function are rewritten by the compiler
		// to __goclib_exit, so the C label is dead code and must not win.
		if a.target == targetELF {
			if _, isSyscall := a.exts[name]; isSyscall {
				return
			}
		}
		// redefinition ignored; last wins (labels normally unique)
	}
	a.syms[name] = symLoc{sect: a.cur, off: a.curOff()}
}

func (a *Assembler) fixup(off int, sym string) {
	a.fixups = append(a.fixups, Fixup{sect: a.cur, off: off, sym: sym})
}

// fixupShort records a 1-byte (rel8) displacement for `jmp short label`,
// `je short label` and `jrcxz label`. The displacement still points at the
// instruction that follows the whole thing, but only eight of its bits survive,
// so the linker rejects targets further than +/-127 bytes away.
func (a *Assembler) fixupShort(off int, sym string) {
	a.fixups = append(a.fixups, Fixup{sect: a.cur, off: off, sym: sym, short: true})
}

// applyFixup patches one recorded displacement into s.Data. `target` is the
// resolved address of f.sym and `base` the address of the section holding the
// displacement field; the CPU measures both rel32 and rel8 from the byte that
// follows the displacement (plus any instruction trailer in ripAdj).
func applyFixup(s *Section, f Fixup, target, base int) error {
	size := 4
	if f.short {
		size = 1
	}
	if f.off+size > len(s.Data) {
		return fmt.Errorf("fixup out of range for %s", f.sym)
	}
	disp := int32(target - (base + f.off + size + f.ripAdj))
	if f.short && (disp < -128 || disp > 127) {
		return fmt.Errorf("short jump to %s is %d bytes away (limit +/-127)", f.sym, disp)
	}
	for i := 0; i < size; i++ {
		s.Data[f.off+i] = byte(disp >> (8 * i))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Top level
// ---------------------------------------------------------------------------

func (a *Assembler) Assemble(src string) error {
	lines := strings.Split(src, "\n")

	// Pass 1: collect externs so `call` knows what is external.
	for _, ln := range lines {
		ln = stripComment(ln)
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "extern ") {
			if err := a.parseExtern(t); err != nil {
				return err
			}
		}
	}

	// ELF targets have no DLL imports: every extern must name a Linux
	// syscall, and we emit a stub for it so `call write` just works.
	if a.target == targetELF {
		if err := a.emitSyscallStubs(); err != nil {
			return err
		}
	}

	// Pass 2: emit.
	for _, ln := range lines {
		ln = stripComment(ln)
		if err := a.processLine(ln); err != nil {
			return err
		}
	}
	return nil
}

// emitSyscallStubs defines one stub per declared extern when targeting ELF.
// Each stub is `mov rax, <number>; syscall; ret`, placed at the start of
// .text, so `call write` reaches the kernel with the SysV argument registers
// (rdi, rsi, rdx, ...) untouched.
func (a *Assembler) emitSyscallStubs() error {
	names := make([]string, 0, len(a.exts))
	for n := range a.exts {
		names = append(names, n)
	}
	sort.Strings(names)

	prev := a.cur
	a.cur = 0 // .text is always section 0
	for _, n := range names {
		num, ok := linuxSyscalls[n]
		if !ok {
			a.cur = prev
			return fmt.Errorf("extern %q: not a Linux syscall known to goa; an ELF target has no DLL imports", n)
		}
		a.defineSym(n)
		if err := a.encode("mov", []Operand{
			{kind: K_REG, reg: 0},   // rax
			{kind: K_IMM, imm: num}, // syscall number
		}, "mov rax, "+n); err != nil {
			a.cur = prev
			return err
		}
		a.emitByte(0x0F) // syscall
		a.emitByte(0x05)
		a.emitByte(0xC3) // ret
	}
	a.cur = prev
	return nil
}

func stripComment(ln string) string {
	// ';' and '//' end the line anywhere. '#' only when it is the first
	// non-space character (so 0x... hex literals stay intact).
	// The scan is quote-aware: a ';' or "//" inside a string literal (e.g.
	// `LC0 db ",;.", 0` or `db "path//x", 0`) is data, not a comment.
	// A backslash inside a string skips the next char so an escaped quote
	// ("a \" b") does not close the string early.
	inStr := byte(0)
	for i := 0; i < len(ln); i++ {
		c := ln[i]
		if inStr != 0 {
			if c == '\\' && i+1 < len(ln) {
				i++ // escaped char belongs to the string
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inStr = c
			continue
		}
		if c == ';' || (c == '/' && i+1 < len(ln) && ln[i+1] == '/') {
			ln = ln[:i]
			break
		}
	}
	t := strings.TrimLeft(ln, " \t")
	if strings.HasPrefix(t, "#") {
		return ""
	}
	return ln
}

func (a *Assembler) parseExtern(t string) error {
	// extern Name, dll   (dll optional -> defaults to kernel32)
	rest := strings.TrimSpace(t[len("extern "):])
	parts := strings.SplitN(rest, ",", 2)
	name := strings.TrimSpace(parts[0])
	dll := "kernel32"
	if len(parts) == 2 {
		dll = strings.TrimSpace(parts[1])
	}
	if !strings.HasSuffix(dll, ".dll") {
		dll += ".dll"
	}
	a.exts[name] = dll
	return nil
}

// splitLabel looks for a "name:" prefix, ignoring colons inside string
// literals -- otherwise `LC0 db "note: x", 0` would be parsed as a label
// named `LC0 db "note`. A label is a bare identifier: no spaces or quotes.
func splitLabel(ln string) (label, rest string, ok bool) {
	inStr := byte(0)
	for i := 0; i < len(ln); i++ {
		c := ln[i]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inStr = c
			continue
		}
		if c != ':' {
			continue
		}
		name := strings.TrimSpace(ln[:i])
		if name == "" || strings.ContainsAny(name, " \t\"'") {
			return "", "", false
		}
		return name, strings.TrimSpace(ln[i+1:]), true
	}
	return "", "", false
}

func (a *Assembler) processLine(ln string) error {
	ln = strings.TrimSpace(ln)
	if ln == "" {
		return nil
	}
	if strings.HasPrefix(ln, "section ") {
		name := strings.TrimSpace(ln[len("section "):])
		if s := a.sectionByName(name); s != nil {
			for i, ss := range a.sections {
				if ss == s {
					a.cur = i
				}
			}
		} else {
			writable := name == ".data" || name == ".idata"
			s := a.newSection(name, writable, name == ".text")
			a.cur = len(a.sections) - 1
			_ = s
		}
		return nil
	}
	if strings.HasPrefix(ln, "global ") {
		a.entry = strings.TrimSpace(ln[len("global "):])
		return nil
	}
	if strings.HasPrefix(ln, "extern ") {
		return nil // already handled in pass 1
	}
	if strings.HasPrefix(ln, "subsystem ") {
		v := strings.TrimSpace(ln[len("subsystem "):])
		switch v {
		case "windows", "gui", "windowsgui":
			a.subsystem = subsysWindows
		case "console", "cui":
			a.subsystem = subsysConsole
		default:
			return fmt.Errorf("unknown subsystem %q (want windows or console)", v)
		}
		return nil
	}
	// constant:  name = number
	if eq := strings.Index(ln, "="); eq > 0 && !strings.Contains(ln, "\"") && !strings.Contains(ln, "'") {
		left := strings.TrimSpace(ln[:eq])
		right := strings.TrimSpace(ln[eq+1:])
		if _, isReg := regIndexMap[left]; !isReg {
			if v, err := strconv.ParseInt(right, 0, 64); err == nil {
				a.consts[left] = v
				return nil
			}
		}
	}
	// label?
	if label, rest, ok := splitLabel(ln); ok {
		if !isLocalLabel(label) {
			a.curGlobal = label
		}
		a.defineSym(a.qualify(label))
		if rest != "" {
			return a.processLine(rest)
		}
		return nil
	}
	// label without colon in front of a data directive: "name db ..." / "name dq ..."
	if fields := strings.Fields(ln); len(fields) >= 2 &&
		(fields[1] == "db" || fields[1] == "dq" || fields[1] == "du") {
		label := fields[0]
		if _, isReg := regIndexMap[label]; !isReg {
			if !isLocalLabel(label) {
				a.curGlobal = label
			}
			a.defineSym(a.qualify(label))
			rest := strings.TrimSpace(ln[len(label):])
			return a.processLine(rest)
		}
	}
	return a.emitInstr(ln)
}

// ---------------------------------------------------------------------------
// Operand parsing
// ---------------------------------------------------------------------------

func (a *Assembler) parseOperand(tok string) (Operand, error) {
	tok = strings.TrimSpace(tok)
	// Strip NASM-style size prefixes (byte/word/dword/qword) and the
	// redundant "ptr" keyword. The actual operand size is usually inferred
	// from the register used (al/bl => 8-bit, rax/rbx => 64-bit), so for
	// byte/qword dropping the keyword is safe. But an explicit prefix on a
	// *memory* operand (e.g. "mov dword [rbp-8], eax" style, written by the
	// compiler with 64-bit GPRs) is the only way to request a narrower
	// access, so it is recorded in memWidth instead of being discarded.
	sizeKw := 0
	for {
		lower := strings.ToLower(tok)
		stripped := false
		for i, kw := range []string{"byte", "word", "dword", "qword"} {
			if strings.HasPrefix(lower, kw+" ") || strings.HasPrefix(lower, kw+"[") {
				tok = strings.TrimSpace(tok[len(kw):])
				sizeKw = []int{1, 2, 4, 8}[i]
				stripped = true
				break
			}
		}
		if !stripped {
			if strings.HasPrefix(lower, "ptr ") {
				tok = strings.TrimSpace(tok[4:])
			} else {
				break
			}
		}
		if stripped {
			continue
		}
		break
	}
	if tok == "" {
		return Operand{}, fmt.Errorf("empty operand")
	}
	if ri, ok := byteRegMap[tok]; ok {
		return Operand{kind: K_REG, reg: ri, isByte: true}, nil
	}
	if ri, ok := xmmRegMap[tok]; ok {
		return Operand{kind: K_REG, reg: ri, isXMM: true}, nil
	}
	if ri, ok := reg16Map[tok]; ok {
		return Operand{kind: K_REG, reg: ri, is16: true}, nil
	}
	if ri, ok := reg32Map[tok]; ok {
		return Operand{kind: K_REG, reg: ri, is32: true}, nil
	}
	if ri, ok := regIndex(tok); ok {
		return Operand{kind: K_REG, reg: ri}, nil
	}
	if strings.HasPrefix(tok, "[") && strings.HasSuffix(tok, "]") {
		inner := strings.TrimSpace(tok[1 : len(tok)-1])
		// RIP-relative: [rip+sym] (case-insensitive "rip")
		innerStripped := strings.ReplaceAll(inner, "RIP+", "")
		innerStripped = strings.ReplaceAll(innerStripped, "rip+", "")
		if innerStripped != inner {
			return Operand{kind: K_MEM, memSym: a.qualify(strings.TrimSpace(innerStripped)), isRip: true, memIndex: -1, memWidth: sizeKw}, nil
		}
		// Simple register-indirect: [reg]. Normalize it onto memBase (with
		// memScale = 1) so planMem sees a well-formed operand -- leaving
		// memScale at zero makes planMem reject it.
		if !strings.ContainsAny(inner, "+-*") {
			if ri, ok := regIndex(inner); ok {
				return Operand{kind: K_MEM, memReg: ri, memBase: ri, memHasBase: true,
					isRip: false, memIndex: -1, memScale: 1, memWidth: sizeKw}, nil
			}
			// a bare symbol => RIP-relative data reference ([sym])
			return Operand{kind: K_MEM, memSym: a.qualify(inner), isRip: true, memIndex: -1, memWidth: sizeKw}, nil
		}
		// Complex: [base+index*scale+disp], [base+disp], [index*scale], ...
		o, err := parseMemInner(inner)
		if err != nil {
			return Operand{}, err
		}
		o.memWidth = sizeKw
		return o, nil
	}
	// immediate (literal or constant name)
	if v, ok := a.consts[tok]; ok {
		return Operand{kind: K_IMM, imm: v}, nil
	}
	if tok == "rip" {
		return Operand{kind: K_SYM, sym: "rip"}, nil
	}
	if (tok[0] >= '0' && tok[0] <= '9') || tok[0] == '-' || tok[0] == '+' || strings.HasPrefix(tok, "0x") || strings.HasPrefix(tok, "0X") {
		v, err := strconv.ParseInt(tok, 0, 64)
		if err != nil {
			return Operand{}, fmt.Errorf("bad immediate %q", tok)
		}
		return Operand{kind: K_IMM, imm: v}, nil
	}
	return Operand{kind: K_SYM, sym: a.qualify(tok)}, nil
}

// ---- memory operand parsing (SIB: [base+index*scale+disp]) ----------------

type memTerm struct {
	neg  bool
	text string
}

// splitMemTerms splits a memory-inner string on '+'/'-' while keeping the
// sign of each term. e.g. "r11+r10*4-8" -> [{+,r11},{+,r10*4},{-,8}].
func splitMemTerms(s string) []memTerm {
	var out []memTerm
	var cur strings.Builder
	sign := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, memTerm{neg: sign, text: cur.String()})
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '+':
			flush()
			sign = false
		case '-':
			flush()
			sign = true
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// parseMemInner parses a non-RIP memory operand into a normalized Operand.
// Supported forms: [reg], [base+disp], [base-disp], [base+index],
// [base+index*scale], [base+index*scale+disp], [index*scale], [index*scale+disp].
func parseMemInner(inner string) (Operand, error) {
	o := Operand{kind: K_MEM, isRip: false, memIndex: -1, memScale: 1}
	terms := splitMemTerms(inner)
	for _, t := range terms {
		term := strings.TrimSpace(t.text)
		if term == "" {
			continue
		}
		if strings.Contains(term, "*") {
			parts := strings.SplitN(term, "*", 2)
			idxName := strings.TrimSpace(parts[0])
			sc, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return Operand{}, fmt.Errorf("bad scale in %q", term)
			}
			ri, ok := regIndex(idxName)
			if !ok {
				return Operand{}, fmt.Errorf("bad index register %q", idxName)
			}
			o.memIndex = ri
			o.memScale = sc
			continue
		}
		if ri, ok := regIndex(term); ok {
			if !o.memHasBase {
				o.memBase = ri
				o.memHasBase = true
			} else {
				// second register => index
				o.memIndex = ri
			}
			continue
		}
		v, err := strconv.ParseInt(term, 0, 64)
		if err != nil {
			return Operand{}, fmt.Errorf("bad memory term %q", term)
		}
		if t.neg {
			v = -v
		}
		o.memDisp += int(v)
		o.memHasDisp = true
	}
	return o, nil
}

// ---- SIB/ModRM planning + emission ---------------------------------------

type memEnc struct {
	modrm    byte
	sib      byte
	hasSIB   bool
	disp     int
	dispSize int // 0, 1, or 4
	rexR     bool
	rexX     bool
	rexB     bool
}

// planMem computes the ModRM (+SIB+displacement) bytes for a register-based
// (non-RIP) memory operand whose ModRM.reg field is `regField`. It returns the
// planned encoding plus the REX.R/X/B bits that must be ORed into the REX prefix.
func (a *Assembler) planMem(regField int, mem Operand) (memEnc, error) {
	var e memEnc
	e.rexR = regField >= 8

	base := mem.memBase
	hasBase := mem.memHasBase
	hasIdx := mem.memIndex >= 0
	scale := mem.memScale
	disp := mem.memDisp
	hasDisp := mem.memHasDisp

	if scale != 1 && scale != 2 && scale != 4 && scale != 8 {
		return e, fmt.Errorf("bad scale %d (must be 1,2,4,8)", scale)
	}

	// base == rbp/r13 (5/13) and no-base both force a displacement.
	baseIsBP := hasBase && (base == 5 || base == 13)
	needDisp := hasDisp
	if baseIsBP {
		needDisp = true
	}

	// Determine whether a SIB byte is required.
	useSIB := hasIdx || base == 4 || base == 12 || (!hasBase && !hasIdx)
	// Special: absolute [disp] (no base, no index) also needs SIB with no-base.
	if !hasBase && !hasIdx {
		useSIB = true
	}

	// Choose mod (and displacement size).
	switch {
	case !needDisp && disp == 0 && !baseIsBP && !useSIB:
		e.modrm = 0x00
	case !needDisp && disp == 0 && !baseIsBP && useSIB:
		// SIB but no displacement (e.g. [rax+rbx])
		e.modrm = 0x00
	case disp >= -128 && disp <= 127:
		e.modrm = 0x40
		e.disp = disp
		e.dispSize = 1
	default:
		e.modrm = 0x80
		e.disp = disp
		e.dispSize = 4
	}

	// reg field
	e.modrm |= byte((regField & 7) << 3)

	if useSIB {
		e.modrm |= 0x04 // rm = 100 => SIB follows
		e.hasSIB = true
		var sib byte
		scaleBits := 0
		switch scale {
		case 1:
			scaleBits = 0
		case 2:
			scaleBits = 1
		case 4:
			scaleBits = 2
		case 8:
			scaleBits = 3
		}
		sib |= byte(scaleBits << 6)
		if hasIdx {
			sib |= byte((mem.memIndex & 7) << 3)
			e.rexX = mem.memIndex >= 8
		} else {
			sib |= 0x20 // index = 100 (none)
		}
		if hasBase {
			sib |= byte(base & 7)
			e.rexB = base >= 8
		} else {
			sib |= 0x05 // base = 101 (none) => disp32 required
			// force a 4-byte displacement
			e.dispSize = 4
			e.modrm = (e.modrm & 0x3F) | 0x80
			e.disp = disp
		}
		e.sib = sib
	} else {
		// no SIB: rm = base register (or 101 for rbp with disp)
		e.modrm |= byte(base & 7)
		e.rexB = base >= 8
	}
	return e, nil
}

func (a *Assembler) emitMemEnc(e memEnc) {
	a.emitByte(e.modrm)
	if e.hasSIB {
		a.emitByte(e.sib)
	}
	switch e.dispSize {
	case 0:
		// no displacement
	case 1:
		a.emitByte(byte(int8(e.disp)))
	case 4:
		a.emitInt32(int32(e.disp))
	}
}

// encodeMovRegMem emits mov between a register and a register-based memory
// operand (no RIP-relative / no symbol fixup). store=true => mem <- reg.
// width is 1 (byte), 4 (dword), or 8 (qword).
func (a *Assembler) encodeMovRegMem(regOp Operand, memOp Operand, store bool, width int) error {
	regField := regOp.reg
	e, err := a.planMem(regField, memOp)
	if err != nil {
		return err
	}
	// Build REX.
	var opcode byte
	if width == 1 {
		if store {
			opcode = 0x88 // MOV r/m8, r8
		} else {
			opcode = 0x8A // MOV r8, r/m8
		}
		var rex byte = 0x40
		if e.rexR {
			rex |= 0x04
		}
		if e.rexX {
			rex |= 0x02
		}
		if e.rexB {
			rex |= 0x01
		}
		// spl/bpl/sil/dil (ModRM.reg 4..7) require a REX prefix: without it they
		// decode as ah/ch/dh/bh. Only al/cl/dl/bl (0..3) may omit the REX byte.
		if rex != 0x40 || (regField >= 4 && regField <= 7) {
			a.emitByte(rex)
		}
	} else if width == 2 || width == 4 {
		// 16-bit / 32-bit: no REX.W. The 0x66 operand-size prefix selects
		// 16 bits; 32-bit needs no prefix. REX is only emitted when a high
		// register (8..15) appears in the R/X/B slot.
		if width == 2 {
			a.emitByte(0x66)
		}
		if store {
			opcode = 0x89 // MOV r/m16|32, r16|32
		} else {
			opcode = 0x8B // MOV r16|32, r/m16|32
		}
		var rex byte = 0x40
		if e.rexR {
			rex |= 0x04
		}
		if e.rexX {
			rex |= 0x02
		}
		if e.rexB {
			rex |= 0x01
		}
		if rex != 0x40 {
			a.emitByte(rex)
		}
	} else {
		if store {
			opcode = 0x89 // MOV r/m64, r64
		} else {
			opcode = 0x8B // MOV r64, r/m64
		}
		rex := byte(0x48)
		if e.rexR {
			rex |= 0x04
		}
		if e.rexX {
			rex |= 0x02
		}
		if e.rexB {
			rex |= 0x01
		}
		a.emitByte(rex)
	}
	a.emitByte(opcode)
	a.emitMemEnc(e)
	return nil
}

// ---------------------------------------------------------------------------
// Instruction emission
// ---------------------------------------------------------------------------

func (a *Assembler) emitInstr(ln string) error {
	// split mnemonic / operands
	var mnem string
	var rest string
	if sp := strings.IndexAny(ln, " \t"); sp >= 0 {
		mnem = strings.TrimSpace(ln[:sp])
		rest = strings.TrimSpace(ln[sp+1:])
	} else {
		mnem = ln
	}
	// Data directives are not instructions: route them straight to the
	// byte/quad emitter without operand parsing (operands may be
	// "32 dup(0)", strings, char literals, etc.).
	if mnem == "db" {
		return a.emitDB(rest)
	}
	if mnem == "dq" {
		return a.emitDQ(rest)
	}
	if mnem == "du" {
		return a.emitDU(rest)
	}
	// `jmp short label` / `jne short label` ask for the two-byte rel8 form.
	// `short` is not an operand, so it is stripped here and handed to encode
	// via a one-shot flag rather than reaching the operand parser.
	if restLC := strings.ToLower(rest); strings.HasPrefix(restLC, "short ") {
		a.shortNext = true
		rest = strings.TrimSpace(rest[6:])
	}
	var ops []Operand
	if rest != "" {
		for _, p := range strings.Split(rest, ",") {
			o, err := a.parseOperand(p)
			if err != nil {
				return fmt.Errorf("line %q: %v", ln, err)
			}
			ops = append(ops, o)
		}
	}
	return a.encode(mnem, ops, ln)
}

// rexW emits the REX prefix for a 64-bit operation. regIdx / rmIdx select the
// R (reg field) and B (rm/base field) extension bits; we never use an index
// register so REX.X stays 0.
func (a *Assembler) rexW(regIdx, rmIdx int) {
	v := byte(0x48)
	if regIdx >= 8 {
		v |= 0x04
	}
	if rmIdx >= 8 {
		v |= 0x01
	}
	a.emitByte(v)
}

// modrmRegReg builds mod=11 (register-direct) ModRM: reg field = r, rm field = m.
func modrmRegReg(r, m int) byte {
	return byte(0xC0 | ((r & 7) << 3) | (m & 7))
}

// modrmRip builds mod=00, rm=101 (RIP-relative) ModRM with given reg field.
func modrmRip(r int) byte {
	return byte(0x05 | ((r & 7) << 3))
}

// regWidth reports the access width in bytes implied by a register operand:
// 1 for al/spl, 2 for ax, 4 for eax, 8 for rax.
func regWidth(o Operand) int {
	switch {
	case o.isByte:
		return 1
	case o.is16:
		return 2
	case o.is32:
		return 4
	default:
		return 8
	}
}

// rexByte assembles a REX prefix for the given operand width and ModRM
// extension bits, and reports whether the byte actually has to be emitted.
//
// Two rules force the byte out even when no extension bit is set: a 64-bit
// operand size always needs REX.W, and an 8-bit form naming spl/bpl/sil/dil
// (register codes 4..7) needs a REX or it decodes as ah/ch/dh/bh.
func rexByte(width, regField, rmReg int, rr, rx, rb bool) (byte, bool) {
	v := byte(0x40)
	if width == 8 {
		v |= 0x08
	}
	if rr {
		v |= 0x04
	}
	if rx {
		v |= 0x02
	}
	if rb {
		v |= 0x01
	}
	must := width == 8 || rr || rx || rb
	if width == 1 && rmReg >= 4 && rmReg <= 7 {
		must = true // spl/bpl/sil/dil in the rm field without REX => ah/ch/dh/bh
	}
	if width == 1 && regField >= 4 && regField <= 7 {
		must = true
	}
	return v, must
}

// emitOpRM emits a full r/m instruction and takes care of every operand form
// goa understands:
//
//	reg,reg -> prefixes [REX] [0F] op ModRM(mod=11)
//	reg,mem -> prefixes [REX] [0F] op ModRM [SIB] [disp]
//	reg,[rip+sym] -> ... same, with a disp32 left for the linker to patch
//
// `rm` is always the r/m operand (the ModRM.rm field); if it is K_MEM with an
// explicit width prefix and no other width is known, the caller should have
// resolved that already. `tail`, when non-nil, runs after the r/m bytes to emit
// an instruction trailer (the imm8 of `bt r/m, 3`); with a RIP-relative rm it
// runs after the patched disp32, which is exactly where the trailer belongs.
func (a *Assembler) emitOpRM(width int, prefixes []byte, twoByte bool, op byte, regField int, rm Operand, tail func()) error {
	// legacy prefixes (0x66 / 0xF2 / 0xF3) always precede REX
	emitPre := func() {
		a.emitBytes(prefixes)
		if width == 2 {
			a.emitByte(0x66) // operand-size prefix => 16-bit
		}
	}
	switch rm.kind {
	case K_REG:
		emitPre()
		if v, ok := rexByte(width, regField, rm.reg, regField >= 8, false, rm.reg >= 8); ok {
			a.emitByte(v)
		}
		if twoByte {
			a.emitByte(0x0F)
		}
		a.emitByte(op)
		a.emitByte(modrmRegReg(regField, rm.reg))
		if tail != nil {
			tail()
		}
		return nil
	case K_MEM:
		if rm.isRip {
			emitPre()
			if v, ok := rexByte(width, regField, -1, regField >= 8, false, false); ok {
				a.emitByte(v)
			}
			if twoByte {
				a.emitByte(0x0F)
			}
			a.emitByte(op)
			a.emitByte(modrmRip(regField))
			off := a.curOff()
			a.emitInt32(0)
			a.fixup(off, rm.memSym)
			if tail != nil {
				tail()
			}
			return nil
		}
		e, err := a.planMem(regField, rm)
		if err != nil {
			return err
		}
		emitPre()
		if v, ok := rexByte(width, regField, -1, e.rexR, e.rexX, e.rexB); ok {
			a.emitByte(v)
		}
		if twoByte {
			a.emitByte(0x0F)
		}
		a.emitByte(op)
		a.emitMemEnc(e)
		if tail != nil {
			tail()
		}
		return nil
	}
	return fmt.Errorf("unsupported operand form")
}

// memSrcWidth resolves how many bytes a memory source operand transfers. An
// explicit byte/word/dword/qword prefix wins; otherwise `fallback` (normally
// the width of the other, register, operand) decides.
func memSrcWidth(m Operand, fallback int) int {
	if m.memWidth != 0 {
		return m.memWidth
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Condition codes
// ---------------------------------------------------------------------------

// ccTable numbers the sixteen x86 condition codes. The whole point of keeping
// them in one place is that every conditional instruction family derives its
// opcode by simple addition:
//
//	j<cc> rel32     0F 80+cc
//	set<cc> r/m8    0F 90+cc /0
//	cmov<cc> r,r/m  0F 40+cc /r
var ccTable = map[string]int{
	"o": 0, "no": 1,
	"b": 2, "ae": 3,
	"e": 4, "ne": 5,
	"be": 6, "a": 7,
	"s": 8, "ns": 9,
	"p": 10, "np": 11,
	"l": 12, "ge": 13,
	"le": 14, "g": 15,
}

// ccAlias holds every classic synonym for a condition code, keyed by what
// follows the `j` / `set` / `cmov` prefix: jz == je, jc == jb, jnle == jg, ...
var ccAlias = map[string]string{
	"z": "e", "nz": "ne",
	"c": "b", "nae": "b", "nb": "ae", "nc": "ae",
	"na": "be", "nbe": "a",
	"pe": "p", "po": "np",
	"nge": "l", "nl": "ge", "ng": "le", "nle": "g",
}

// ccOf resolves the condition-code suffix of a j/set/cmov mnemonic to 0..15.
func ccOf(suffix string) (int, bool) {
	if n, ok := ccTable[suffix]; ok {
		return n, true
	}
	if canon, ok := ccAlias[suffix]; ok {
		return ccTable[canon], true
	}
	return 0, false
}

func (a *Assembler) encode(mnem string, ops []Operand, ln string) error {
	// The `short` flag belongs to exactly this instruction, so drop it on the
	// way out whatever path we take -- otherwise a stray `jmp short` would
	// silently shrink the next ordinary jump too.
	defer func() { a.shortNext = false }()
	switch mnem {
	case "ret", "retn", "retq":
		// Plain `ret` is C3; `ret N` additionally pops N bytes of arguments
		// (C2 imm16), the form a callee-cleanup convention returns with.
		if len(ops) == 0 {
			a.emitByte(0xC3)
			return nil
		}
		if len(ops) == 1 && ops[0].kind == K_IMM {
			a.emitByte(0xC2)
			a.emitByte(byte(uint16(ops[0].imm)))
			a.emitByte(byte(uint16(ops[0].imm) >> 8))
			return nil
		}
		return fmt.Errorf("ret takes no operand or an immediate: %q", ln)
	case "leave":
		a.emitByte(0xC9)
		return nil
	case "nop":
		a.emitByte(0x90)
		return nil
	case "pause":
		// PAUSE is the spin-loop hint, spelled F3 90: identical to `rep nop`
		// but always written as one instruction.
		a.emitBytes([]byte{0xF3, 0x90})
		return nil
	case "hlt":
		a.emitByte(0xF4)
		return nil
	case "ud2":
		a.emitBytes([]byte{0x0F, 0x0B})
		return nil
	case "int3":
		a.emitByte(0xCC)
		return nil
	case "cpuid":
		a.emitBytes([]byte{0x0F, 0xA2})
		return nil
	case "rdtsc":
		a.emitBytes([]byte{0x0F, 0x31})
		return nil
	case "mfence", "lfence", "sfence":
		// The three barriers share group 0F AE and differ only in the ModRM
		// byte, which encodes a fully register-direct operand (/5, /2, /7
		// are unused here but the byte values are what the CPU matches).
		switch mnem {
		case "mfence":
			a.emitBytes([]byte{0x0F, 0xAE, 0xF0})
		case "lfence":
			a.emitBytes([]byte{0x0F, 0xAE, 0xE8})
		default:
			a.emitBytes([]byte{0x0F, 0xAE, 0xF8})
		}
		return nil
	case "db":
		return a.emitDB(restOf(ln))
	case "dq":
		return a.emitDQ(restOf(ln))
	case "du":
		return a.emitDU(restOf(ln))
	case "push", "pop":
		return a.encodePushPop(mnem, ops, ln)
	case "call":
		return a.encodeCall(ops, ln)
	case "jmp":
		return a.encodeJmp(ops, ln)
	case "lea":
		return a.encodeLea(ops, ln)
	case "mov":
		return a.encodeMov(ops, ln)
	case "add", "sub", "adc", "sbb", "and", "or", "xor", "cmp", "test":
		return a.encodeArith(mnem, ops, ln)
	case "cqo", "cqto":
		// Sign-extend RAX into RDX:RAX (64-bit), needed before idiv.
		a.emitBytes([]byte{0x48, 0x99})
		return nil
	case "cdq", "cltd":
		// Same idea one size down: EAX -> EDX:EAX.
		a.emitByte(0x99)
		return nil
	case "cdqe", "cltq":
		// Sign-extend EAX into RAX (32 -> 64 bits).
		a.emitBytes([]byte{0x48, 0x98})
		return nil
	case "cwde", "cwtl":
		// Sign-extend AX into EAX (16 -> 32 bits).
		a.emitByte(0x98)
		return nil
	case "imul":
		return a.encodeImul(ops, ln)
	case "shl", "sal", "shr", "sar", "rol", "ror", "rcl", "rcr":
		return a.encodeShift(mnem, ops, ln)
	case "idiv":
		return a.encodeGrp(0xF7, 7, ops, ln)
	case "div":
		return a.encodeGrp(0xF7, 6, ops, ln)
	case "mul":
		return a.encodeGrp(0xF7, 4, ops, ln)
	case "not":
		return a.encodeGrp(0xF7, 2, ops, ln)
	case "neg":
		return a.encodeGrp(0xF7, 3, ops, ln)
	case "inc":
		return a.encodeGrp(0xFF, 0, ops, ln)
	case "dec":
		return a.encodeGrp(0xFF, 1, ops, ln)
	case "movzx", "movsx", "movsxd", "movslq", "movzbl", "movzbw", "movzbq",
		"movzwl", "movzwq", "movsbl", "movsbw", "movsbq", "movswl", "movswq":
		return a.encodeMovExtend(mnem, ops, ln)
	case "xchg":
		return a.encodeXchg(ops, ln)
	case "bt", "bts", "btr", "btc":
		return a.encodeBit(mnem, ops, ln)
	case "bswap":
		return a.encodeBswap(ops, ln)
	case "jrcxz", "jecxz":
		return a.encodeJcxz(mnem, ops, ln)
	case "syscall":
		// Fast system call (0F 05). Linux: rax = number, args in
		// rdi, rsi, rdx, r10, r8, r9.
		a.emitByte(0x0F)
		a.emitByte(0x05)
		return nil
	case "movsd", "movss", "addsd", "subsd", "mulsd", "divsd", "sqrtsd",
		"xorpd", "ucomisd", "cvtsi2sd", "cvttsd2si", "cvtss2sd", "cvtsd2ss", "movq":
		return a.encodeSSE(mnem, ops, ln)
	}

	// Everything conditional is derived from the sixteen condition codes:
	// j<cc> rel8/32, set<cc> r/m8, cmov<cc> r, r/m. Resolving them here keeps
	// the 100+ spellings (jz/je/jnbe/ja, sete/setz, cmovle/cmovng, ...) in one
	// table instead of one case arm each.
	switch {
	case strings.HasPrefix(mnem, "set"):
		if cc, ok := ccOf(mnem[3:]); ok {
			return a.encodeSetCC(cc, ops, ln)
		}
	case strings.HasPrefix(mnem, "cmov"):
		if cc, ok := ccOf(mnem[4:]); ok {
			return a.encodeCmovCC(cc, ops, ln)
		}
	case strings.HasPrefix(mnem, "j"):
		if cc, ok := ccOf(mnem[1:]); ok {
			return a.encodeCond(cc, ops, ln)
		}
	}
	return fmt.Errorf("unknown instruction: %q (line %q)", mnem, ln)
}

func restOf(ln string) string {
	if sp := strings.IndexAny(ln, " \t"); sp >= 0 {
		return strings.TrimSpace(ln[sp+1:])
	}
	return ""
}

// ---- db / dq ---------------------------------------------------------------

func (a *Assembler) emitDB(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}
	for _, tok := range splitTopLevel(rest, ',') {
		t := strings.TrimSpace(tok)
		if t == "" {
			continue
		}
		// dup support: "N dup(value)"  (value may be a number or 'c'/string)
		if i := strings.Index(t, "dup"); i > 0 {
			cntStr := strings.TrimSpace(t[:i])
			if cnt, err := strconv.Atoi(cntStr); err == nil {
				open := strings.Index(t[i:], "(")
				close := strings.LastIndex(t[i:], ")")
				if open >= 0 && close > open {
					inner := strings.TrimSpace(t[i+open+1 : i+close])
					if strings.HasPrefix(inner, "'") && len(inner) >= 3 && inner[len(inner)-1] == '\'' {
						b := byte(inner[1])
						for k := 0; k < cnt; k++ {
							a.emitByte(b)
						}
					} else if strings.HasPrefix(inner, "\"") {
						s, _ := strconv.Unquote(inner)
						for k := 0; k < cnt; k++ {
							a.emitBytes([]byte(s))
						}
					} else if v, e2 := strconv.ParseInt(inner, 0, 64); e2 == nil {
						for k := 0; k < cnt; k++ {
							a.emitByte(byte(v))
						}
					}
					continue
				}
			}
		}
		if strings.HasPrefix(t, "\"") {
			s, err := strconv.Unquote(t)
			if err != nil {
				// handle manual escapes minimally
				s = unescape(t)
			}
			a.emitBytes([]byte(s))
		} else if strings.HasPrefix(t, "'") && len(t) >= 3 && t[len(t)-1] == '\'' {
			a.emitByte(byte(t[1]))
		} else {
			v, err := strconv.ParseInt(t, 0, 64)
			if err != nil {
				return fmt.Errorf("bad db operand %q", t)
			}
			a.emitByte(byte(v))
		}
	}
	return nil
}

func (a *Assembler) emitDQ(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}
	for _, tok := range splitTopLevel(rest, ',') {
		t := strings.TrimSpace(tok)
		if t == "" {
			continue
		}
		v, err := strconv.ParseInt(t, 0, 64)
		if err == nil {
			a.emitInt64(v)
			continue
		}
		// floating-point literal -> IEEE-754 double bits (little-endian)
		f, err2 := strconv.ParseFloat(t, 64)
		if err2 == nil {
			a.emitInt64(int64(math.Float64bits(f)))
			continue
		}
		return fmt.Errorf("bad dq operand %q", t)
	}
	return nil
}

// emitDU defines UTF-16LE data ("du" = define unicode). Accepts string
// literals and 16-bit numbers, and always appends a NUL terminator so a label
// on the directive can be handed straight to a Wide-char Win32 API.
func (a *Assembler) emitDU(rest string) error {
	rest = strings.TrimSpace(rest)
	for _, tok := range splitTopLevel(rest, ',') {
		t := strings.TrimSpace(tok)
		if t == "" {
			continue
		}
		if t[0] == '"' || t[0] == '\'' {
			s, err := strconv.Unquote(t)
			if err != nil {
				return fmt.Errorf("bad du string %q", t)
			}
			a.emitUTF16(s)
			continue
		}
		v, err := strconv.ParseInt(t, 0, 64)
		if err != nil {
			return fmt.Errorf("bad du operand %q", t)
		}
		a.emitByte(byte(v))
		a.emitByte(byte(v >> 8))
	}
	// NUL-terminate so the label can be passed straight to a Wide API.
	// (emitUTF16("") would emit nothing, so write the two zero bytes directly.)
	a.emitByte(0)
	a.emitByte(0)
	return nil
}

func (a *Assembler) emitUTF16(s string) {
	for _, u := range utf16.Encode([]rune(s)) {
		a.emitByte(byte(u))
		a.emitByte(byte(u >> 8))
	}
}

func unescape(s string) string {
	// strip surrounding quotes (single or double) then process escapes
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		s = s[1 : len(s)-1]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '0':
				b.WriteByte(0)
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case '\'':
				b.WriteByte('\'')
			default:
				b.WriteByte(s[i])
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// ---- push / pop ------------------------------------------------------------

// encodePushPop assembles push/pop in every form goa understands:
//
//	push/pop r64        50+rd / 58+rd   (0x66 prefix for the 16-bit form)
//	push imm           6A ib / 68 id    (sign-extended to 64 bits)
//	push [mem]         FF /6            (64-bit memory read)
//	pop  [mem]         8F /0
func (a *Assembler) encodePushPop(mnem string, ops []Operand, ln string) error {
	if len(ops) != 1 {
		return fmt.Errorf("%s needs exactly one operand: %q", mnem, ln)
	}
	o := ops[0]
	push := mnem == "push"
	switch {
	case push && o.kind == K_IMM:
		switch {
		case o.imm >= -128 && o.imm <= 127:
			a.emitByte(0x6A)
			a.emitByte(byte(int8(o.imm)))
		case o.imm >= -2147483648 && o.imm <= 2147483647:
			a.emitByte(0x68)
			a.emitInt32(int32(o.imm))
		default:
			return fmt.Errorf("push: immediate %d does not fit 32 bits: %q", o.imm, ln)
		}
		return nil
	case o.kind == K_REG:
		if o.is16 {
			a.emitByte(0x66) // operand-size prefix => 16-bit push/pop
		}
		if o.reg >= 8 {
			a.emitByte(0x41)
		}
		if push {
			a.emitByte(byte(0x50 + (o.reg & 7)))
		} else {
			a.emitByte(byte(0x58 + (o.reg & 7)))
		}
		return nil
	case o.kind == K_MEM:
		if push {
			return a.emitOpRM(8, nil, false, 0xFF, 6, o, nil)
		}
		return a.emitOpRM(8, nil, false, 0x8F, 0, o, nil)
	case o.kind == K_SYM:
		return fmt.Errorf("%s needs `word [rip+sym]` here, not a bare symbol: %q", mnem, ln)
	}
	return fmt.Errorf("%s: unsupported operand %q", mnem, ln)
}

// ---- call ------------------------------------------------------------------

func (a *Assembler) encodeCall(ops []Operand, ln string) error {
	if len(ops) != 1 {
		return fmt.Errorf("call needs one operand: %q", ln)
	}
	o := ops[0]
	if o.kind == K_SYM {
		if _, isExt := a.exts[o.sym]; isExt && a.target == targetPE {
			// indirect call through IAT: FF /2 with RIP-relative disp
			a.emitByte(0xFF)
			a.emitByte(0x15) // mod=00, reg=/2, rm=101
			off := a.curOff()
			a.emitInt32(0)
			a.fixup(off, "IAT:"+o.sym)
			return nil
		}
		// direct near call E8 rel32
		a.emitByte(0xE8)
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, o.sym)
		return nil
	}
	if o.kind == K_REG || o.kind == K_MEM {
		// call reg / call [mem] : FF /2
		return a.emitOpRM(8, nil, false, 0xFF, 2, o, nil)
	}
	return fmt.Errorf("call: unsupported operand %q", ln)
}

// ---- unconditional jmp -----------------------------------------------------

// encodeJmp handles the three jump forms: `jmp label` (E9 rel32, or EB rel8
// when written `jmp short label`) and the indirect `jmp reg` / `jmp [mem]`
// (FF /4), which is what a function-pointer dispatch compiles to.
func (a *Assembler) encodeJmp(ops []Operand, ln string) error {
	if len(ops) != 1 {
		return fmt.Errorf("jmp needs one operand: %q", ln)
	}
	o := ops[0]
	if o.kind == K_SYM {
		if a.shortNext {
			a.emitByte(0xEB) // jmp rel8
			off := a.curOff()
			a.emitByte(0)
			a.fixupShort(off, o.sym)
			return nil
		}
		a.emitByte(0xE9) // jmp rel32
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, o.sym)
		return nil
	}
	if o.kind == K_REG || o.kind == K_MEM {
		return a.emitOpRM(8, nil, false, 0xFF, 4, o, nil)
	}
	return fmt.Errorf("jmp: unsupported operand: %q", ln)
}

// ---- conditional jumps -----------------------------------------------------

// encodeCond emits `j<cc> label`. The 0F 80+cc rel32 form is the default
// because it always reaches; `j<cc> short label` asks for the two-byte
// 70+cc rel8 form, which the linker rejects beyond +/-127 bytes.
func (a *Assembler) encodeCond(cc int, ops []Operand, ln string) error {
	if len(ops) != 1 || ops[0].kind != K_SYM {
		return fmt.Errorf("conditional jump needs one label: %q", ln)
	}
	if a.shortNext {
		a.emitByte(byte(0x70 + cc))
		off := a.curOff()
		a.emitByte(0)
		a.fixupShort(off, ops[0].sym)
		return nil
	}
	a.emitByte(0x0F)
	a.emitByte(byte(0x80 + cc))
	off := a.curOff()
	a.emitInt32(0)
	a.fixup(off, ops[0].sym)
	return nil
}

// encodeJcxz emits `jrcxz label` / `jecxz label` (E3 rel8) -- "jump if
// (r)cx is zero", the instruction strlen-style scan loops end with. No wide
// variant exists, so this is always a short jump. jecxz takes the 0x67
// address-size prefix, which restricts the counter to ecx in 64-bit code.
func (a *Assembler) encodeJcxz(mnem string, ops []Operand, ln string) error {
	if len(ops) != 1 || ops[0].kind != K_SYM {
		return fmt.Errorf("%s needs one label: %q", mnem, ln)
	}
	if mnem == "jecxz" {
		a.emitByte(0x67)
	}
	a.emitByte(0xE3)
	off := a.curOff()
	a.emitByte(0)
	a.fixupShort(off, ops[0].sym)
	return nil
}

// ---- lea reg, [rip+sym] ----------------------------------------------------

func (a *Assembler) encodeLea(ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("lea needs 2 operands: %q", ln)
	}
	dst, mem := ops[0], ops[1]
	if dst.kind != K_REG {
		return fmt.Errorf("lea dst must be register: %q", ln)
	}
	// RIP-relative: lea reg, [rip+sym]
	if mem.kind == K_MEM && mem.isRip {
		sym := mem.memSym
		a.rexW(dst.reg, 0)
		a.emitByte(0x8D)
		a.emitByte(modrmRip(dst.reg))
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, sym)
		return nil
	}
	// bare symbol: lea reg, sym  (RIP-relative)
	if mem.kind == K_SYM {
		sym := mem.sym
		a.rexW(dst.reg, 0)
		a.emitByte(0x8D)
		a.emitByte(modrmRip(dst.reg))
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, sym)
		return nil
	}
	// register-based memory: lea reg, [base+index*scale+disp]
	if mem.kind == K_MEM && !mem.isRip {
		e, err := a.planMem(dst.reg, mem)
		if err != nil {
			return err
		}
		rex := byte(0x48)
		if e.rexR {
			rex |= 0x04
		}
		if e.rexX {
			rex |= 0x02
		}
		if e.rexB {
			rex |= 0x01
		}
		a.emitByte(rex)
		a.emitByte(0x8D)
		a.emitMemEnc(e)
		return nil
	}
	return fmt.Errorf("lea: unsupported source %q", ln)
}

// ---- mov -------------------------------------------------------------------

func (a *Assembler) encodeMov(ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("mov needs 2 operands: %q", ln)
	}
	dst, src := ops[0], ops[1]

	// mov reg, imm64
	if dst.kind == K_REG && src.kind == K_IMM {
		if dst.reg >= 8 {
			a.emitByte(0x48 | 0x01)
		} else {
			a.emitByte(0x48)
		}
		a.emitByte(byte(0xB8 | (dst.reg & 7)))
		a.emitInt64(src.imm)
		return nil
	}

	// mov reg, reg  (8-bit if either operand is a byte register)
	if dst.kind == K_REG && src.kind == K_REG {
		if dst.isByte || src.isByte {
			// REX must precede the opcode. reg field = dst (REX.R), rm = src (REX.B).
			// spl/bpl/sil/dil (4..7) also need a REX prefix or they decode as ah/ch/dh/bh.
			needRex := dst.reg >= 8 || src.reg >= 8 ||
				(dst.reg >= 4 && dst.reg <= 7) || (src.reg >= 4 && src.reg <= 7)
			if needRex {
				rex := byte(0x40)
				if dst.reg >= 8 {
					rex |= 0x04
				}
				if src.reg >= 8 {
					rex |= 0x01
				}
				a.emitByte(rex)
			}
			a.emitByte(0x8A) // mov r8, r/m8 (mod=11)
			a.emitByte(modrmRegReg(dst.reg, src.reg))
			return nil
		}
		// 16-bit or 32-bit register move: no REX.W, no operand-size prefix for
		// 32-bit; a 0x66 prefix selects 16 bits. REX is only needed when a high
		// register (8..15) appears.
		if dst.is16 || src.is16 || dst.is32 || src.is32 {
			needRex := dst.reg >= 8 || src.reg >= 8
			if needRex {
				rex := byte(0x40)
				if dst.reg >= 8 {
					rex |= 0x04
				}
				if src.reg >= 8 {
					rex |= 0x01
				}
				a.emitByte(rex)
			}
			if dst.is16 || src.is16 {
				a.emitByte(0x66) // operand-size prefix => 16-bit
			}
			a.emitByte(0x8B) // load: reg=dst, rm=src (same opcode for 16/32)
			a.emitByte(modrmRegReg(dst.reg, src.reg))
			return nil
		}
		a.rexW(dst.reg, src.reg)
		a.emitByte(0x8B) // load: reg=dst, rm=src
		a.emitByte(modrmRegReg(dst.reg, src.reg))
		return nil
	}

	// mov reg, [rip+sym]   (load; width from `dword`/`word` prefix or 64-bit)
	if dst.kind == K_REG && !dst.isByte && src.kind == K_MEM && src.isRip {
		width := src.memWidth
		if width == 0 {
			width = 8
		}
		if width == 2 {
			a.emitByte(0x66) // operand-size prefix => 16-bit
		}
		if width == 8 {
			a.rexW(dst.reg, 0)
		} else if dst.reg >= 8 {
			a.emitByte(0x44) // REX.R only
		}
		a.emitByte(0x8B)
		a.emitByte(modrmRip(dst.reg))
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, src.memSym)
		return nil
	}

	// mov [rip+sym], reg   (store; width from `dword`/`word` prefix, `byte`
	// register, or 64-bit)
	if dst.kind == K_MEM && dst.isRip && src.kind == K_REG {
		width := dst.memWidth
		if width == 0 {
			if src.isByte {
				width = 1
			} else {
				width = 8
			}
		}
		if width == 1 {
			// 0x88 = mov r/m8, r8: only REX.R (0x44) or the spl/bpl/sil/dil
			// bare REX (0x40) is needed.
			if src.reg >= 8 {
				a.emitByte(0x44)
			} else if src.reg >= 4 {
				a.emitByte(0x40)
			}
			a.emitByte(0x88)
			a.emitByte(modrmRip(src.reg))
		} else {
			if width == 2 {
				a.emitByte(0x66)
			}
			if width == 8 {
				a.rexW(src.reg, 0)
			} else if src.reg >= 8 {
				a.emitByte(0x44) // REX.R only
			}
			a.emitByte(0x89) // store r/m16|32|64, r16|32|64
			a.emitByte(modrmRip(src.reg))
		}
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, dst.memSym)
		return nil
	}

	// mov reg8, [rip+sym]  (8-bit load)
	if dst.kind == K_REG && dst.isByte && src.kind == K_MEM && src.isRip {
		// 0x8A = mov r8, r/m8: modrm reg field = dst -> REX.R (0x44).
		// spl/bpl/sil/dil (4..7) also need a bare REX (0x40) or they decode as ah/ch/dh/bh.
		if dst.reg >= 8 {
			a.emitByte(0x44)
		} else if dst.reg >= 4 {
			a.emitByte(0x40)
		}
		a.emitByte(0x8A)
		a.emitByte(modrmRip(dst.reg))
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, src.memSym)
		return nil
	}

	// mov [rip+sym], imm  (only supported when imm fits a byte, for tiny vars)
	if dst.kind == K_MEM && dst.isRip && src.kind == K_IMM {
		if src.imm < -128 || src.imm > 255 {
			return fmt.Errorf("mov [mem], imm: only byte immediates supported: %q", ln)
		}
		a.emitByte(0xC6) // mov r/m8, imm8 (reg field /0)
		a.emitByte(modrmRip(0))
		off := a.curOff()
		a.emitInt32(0)
		// The imm8 follows the disp32, so the true RIP is off+4+1.
		a.fixups = append(a.fixups, Fixup{sect: a.cur, off: off, sym: dst.memSym, ripAdj: 1})
		a.emitByte(byte(src.imm)) // the immediate follows the disp32
		return nil
	}

	// mov [base+disp], imm  (64-bit sign-extended imm32, or 32-bit imm32
	// when prefixed `dword`) -- stack slots / struct member stores.
	if dst.kind == K_MEM && !dst.isRip && dst.memHasBase && src.kind == K_IMM {
		width := dst.memWidth
		if width == 0 {
			width = 8
		}
		return a.encodeMovMemImm(dst.memBase, dst.memDisp, src.imm, width, ln)
	}

	// mov reg, [mem]   (load; width from `dword`/`word`/`byte` prefix, or
	// inferred from the destination register width)
	if dst.kind == K_REG && !dst.isByte && src.kind == K_MEM && !src.isRip {
		width := src.memWidth
		if width == 0 {
			if dst.is16 {
				width = 2
			} else if dst.is32 {
				width = 4
			} else {
				width = 8
			}
		}
		return a.encodeMovRegMem(dst, src, false, width)
	}

	// mov reg8, [mem]  (8-bit load)
	if dst.kind == K_REG && dst.isByte && src.kind == K_MEM && !src.isRip {
		width := src.memWidth
		if width == 0 {
			width = 1
		}
		return a.encodeMovRegMem(dst, src, false, width)
	}

	// mov [mem], reg / mov [mem], reg8  (store; width from `dword`/`word`/`byte`
	// prefix, or inferred from the source register width)
	if dst.kind == K_MEM && !dst.isRip && src.kind == K_REG {
		width := dst.memWidth
		if width == 0 {
			if src.isByte {
				width = 1
			} else if src.is16 {
				width = 2
			} else if src.is32 {
				width = 4
			} else {
				width = 8
			}
		}
		return a.encodeMovRegMem(src, dst, true, width)
	}

	return fmt.Errorf("mov: unsupported operand combination: %q", ln)
}

// encodeMovMemImm emits: mov [base+disp], imm (imm16 under a 0x66 prefix for
// width==2, imm32 -- sign-extended to 64 bits by REX.W -- for width==8, plain
// imm32 for width==4).
func (a *Assembler) encodeMovMemImm(base, disp int, imm int64, width int, ln string) error {
	if width == 2 {
		a.emitByte(0x66) // operand-size prefix => 16-bit immediate
	}
	rex := byte(0x40)
	if width == 8 {
		rex = 0x48
	}
	if base >= 8 {
		rex |= 0x01
	}
	if width == 8 || base >= 8 {
		a.emitByte(rex)
	}
	a.emitByte(0xC7) // mov r/m, imm32 (or imm16 under 0x66)
	useSIB := base == 4 || base == 12
	var modrm byte
	if !useSIB {
		switch {
		case disp == 0:
			modrm = byte(0x00 | (base & 7))
		case disp >= -128 && disp <= 127:
			modrm = byte(0x40 | (base & 7))
		default:
			modrm = byte(0x80 | (base & 7))
		}
	} else {
		switch {
		case disp == 0:
			modrm = 0x04
		case disp >= -128 && disp <= 127:
			modrm = 0x44
		default:
			modrm = 0x84
		}
	}
	a.emitByte(modrm)
	if useSIB {
		a.emitByte(0x24) // scale 0, no index, base = rsp/r12
	}
	switch {
	case disp == 0:
		// no displacement
	case disp >= -128 && disp <= 127:
		a.emitByte(byte(int8(disp)))
	default:
		a.emitInt32(int32(disp))
	}
	if width == 2 {
		a.emitByte(byte(int16(imm)))
		a.emitByte(byte(int16(imm) >> 8))
	} else {
		a.emitInt32(int32(imm))
	}
	return nil
}

// ---- arithmetic reg,reg / reg,imm / reg,[mem] ------------------------------

var arithCode = map[string]struct {
	reg   byte // opcode for r/m, r form (reg field = src)
	dig   byte // /digit for imm form
	load  byte // opcode for r, r/m form (reg field = dst, rm = [mem]; 16/32/64-bit)
	load8 byte // 8-bit r, r/m form
}{
	"add":  {0x01, 0, 0x03, 0x02},
	"sub":  {0x29, 5, 0x2B, 0x2A},
	"adc":  {0x11, 2, 0x13, 0x12}, // with carry -- multi-precision add
	"sbb":  {0x19, 3, 0x1B, 0x1A}, // with borrow -- multi-precision subtract
	"and":  {0x21, 4, 0x23, 0x22},
	"or":   {0x09, 1, 0x0B, 0x0A},
	"xor":  {0x31, 6, 0x33, 0x32},
	"cmp":  {0x39, 7, 0x3B, 0x3A},
	"test": {0x85, 0, 0x85, 0x84}, // test r/m, r — symmetric, same opcode
}

func (a *Assembler) encodeArith(mnem string, ops []Operand, ln string) error {
	c, ok := arithCode[mnem]
	if !ok {
		return fmt.Errorf("bad arithmetic: %q", mnem)
	}
	if len(ops) != 2 {
		return fmt.Errorf("%s needs 2 operands: %q", mnem, ln)
	}
	dst, src := ops[0], ops[1]
	if dst.kind != K_REG {
		return fmt.Errorf("%s dst must be register: %q", mnem, ln)
	}

	if src.kind == K_REG {
		// opcode is "op r/m64, r64": modrm reg field = src, rm field = dst.
		// REX.R extends the reg field (src), REX.B extends the rm field (dst).
		a.rexW(src.reg, dst.reg)
		a.emitByte(c.reg)
		a.emitByte(modrmRegReg(src.reg, dst.reg)) // reg field = src, rm = dst
		return nil
	}
	if src.kind == K_IMM {
		if src.imm >= -128 && src.imm <= 127 {
			a.rexW(0, dst.reg)
			a.emitByte(0x83)
			a.emitByte(modrmRegReg(int(c.dig), dst.reg))
			a.emitByte(byte(int8(src.imm)))
		} else {
			a.rexW(0, dst.reg)
			a.emitByte(0x81)
			a.emitByte(modrmRegReg(int(c.dig), dst.reg))
			a.emitInt32(int32(src.imm))
		}
		return nil
	}
	if src.kind == K_MEM {
		// op r, r/m with rm = memory (load direction, e.g. "add eax, [rbp-16]").
		// reg field = dst, rm field = the memory operand. Width comes from an
		// explicit byte/word/dword/qword prefix, else the destination register.
		width := src.memWidth
		if width == 0 {
			switch {
			case dst.isByte:
				width = 1
			case dst.is16:
				width = 2
			case dst.is32:
				width = 4
			default:
				width = 8
			}
		}

		op := c.load
		if width == 1 {
			op = c.load8
		}

		if src.isRip {
			// [rip+sym]: modrmRip encodes the reg field = dst, rm = RIP-relative.
			if width == 1 {
				// spl/bpl/sil/dil (reg 4..7) need a bare REX or they decode as
				// ah/ch/dh/bh; 8..15 need REX.R.
				if dst.reg >= 8 {
					a.emitByte(0x44)
				} else if dst.reg >= 4 {
					a.emitByte(0x40)
				}
				a.emitByte(op)
				a.emitByte(modrmRip(dst.reg))
			} else {
				if width == 2 {
					a.emitByte(0x66)
				}
				if width == 8 {
					a.rexW(dst.reg, 0)
				} else if dst.reg >= 8 {
					a.emitByte(0x44) // REX.R only
				}
				a.emitByte(op)
				a.emitByte(modrmRip(dst.reg))
			}
			off := a.curOff()
			a.emitInt32(0)
			a.fixup(off, src.memSym)
			return nil
		}

		// Register-based [base+index*scale+disp].
		e, err := a.planMem(dst.reg, src)
		if err != nil {
			return err
		}
		switch width {
		case 1:
			var rex byte = 0x40
			if e.rexR {
				rex |= 0x04
			}
			if e.rexX {
				rex |= 0x02
			}
			if e.rexB {
				rex |= 0x01
			}
			// spl/bpl/sil/dil (reg 4..7) require a REX prefix.
			if rex != 0x40 || (dst.reg >= 4 && dst.reg <= 7) {
				a.emitByte(rex)
			}
		case 2, 4:
			if width == 2 {
				a.emitByte(0x66)
			}
			var rex byte = 0x40
			if e.rexR {
				rex |= 0x04
			}
			if e.rexX {
				rex |= 0x02
			}
			if e.rexB {
				rex |= 0x01
			}
			if rex != 0x40 {
				a.emitByte(rex)
			}
		default: // 8
			rex := byte(0x48)
			if e.rexR {
				rex |= 0x04
			}
			if e.rexX {
				rex |= 0x02
			}
			if e.rexB {
				rex |= 0x01
			}
			a.emitByte(rex)
		}
		a.emitByte(op)
		a.emitMemEnc(e)
		return nil
	}
	return fmt.Errorf("%s: unsupported src: %q", mnem, ln)
}

// ---- imul reg, reg ---------------------------------------------------------

func (a *Assembler) encodeImul(ops []Operand, ln string) error {
	if len(ops) != 2 || ops[0].kind != K_REG {
		return fmt.Errorf("imul needs a register destination: %q", ln)
	}
	dst := ops[0]
	if ops[1].kind == K_REG {
		// imul r64, r64 : 0F AF /r  (reg=dst, rm=src)
		a.rexW(dst.reg, ops[1].reg)
		a.emitByte(0x0F)
		a.emitByte(0xAF)
		a.emitByte(modrmRegReg(dst.reg, ops[1].reg))
		return nil
	}
	if ops[1].kind == K_IMM {
		// imul r64, imm : 6B /r ib (sign-extended imm8) or 69 /r id (imm32).
		// The destination register occupies BOTH the reg and rm fields of the
		// ModRM, so when dst.reg >= 8 we must set REX.R (reg field) AND REX.B
		// (rm field) — rexW(dst, dst) does exactly that.
		imm := ops[1].imm
		if imm >= -128 && imm <= 127 {
			a.rexW(dst.reg, dst.reg)
			a.emitByte(0x6B)
			a.emitByte(modrmRegReg(dst.reg, dst.reg))
			a.emitByte(byte(int8(imm)))
		} else {
			a.rexW(dst.reg, dst.reg)
			a.emitByte(0x69)
			a.emitByte(modrmRegReg(dst.reg, dst.reg))
			a.emitInt32(int32(imm))
		}
		return nil
	}
	return fmt.Errorf("imul: unsupported operands: %q", ln)
}

// ---- shift reg, imm / reg, cl ---------------------------------------------

// shiftDigit maps each rotate/shift onto its group number in opcodes D0-D3 /
// C0-C1. The four rotations round out the family: rol/ror are what a checksum
// loop needs, rcl/rcr carry the bit through CF for multi-precision shifts.
var shiftDigit = map[string]byte{
	"rol": 0,
	"ror": 1,
	"rcl": 2,
	"rcr": 3,
	"shl": 4, "sal": 4,
	"shr": 5,
	"sar": 7,
}

func (a *Assembler) encodeShift(mnem string, ops []Operand, ln string) error {
	dig, ok := shiftDigit[mnem]
	if !ok {
		return fmt.Errorf("bad shift: %q", mnem)
	}
	if len(ops) != 2 || ops[0].kind != K_REG {
		return fmt.Errorf("%s needs a register and a shift count: %q", mnem, ln)
	}
	dst := ops[0]
	switch {
	case ops[1].kind == K_IMM:
		c := ops[1].imm
		if c < 0 || c > 63 {
			return fmt.Errorf("%s count %d out of range [0,63]: %q", mnem, c, ln)
		}
		if c == 1 {
			a.rexW(0, dst.reg)
			a.emitByte(0xD1)
			a.emitByte(modrmRegReg(int(dig), dst.reg))
		} else {
			a.rexW(0, dst.reg)
			a.emitByte(0xC1)
			a.emitByte(modrmRegReg(int(dig), dst.reg))
			a.emitByte(byte(c))
		}
	case ops[1].kind == K_REG && ops[1].isByte && ops[1].reg == 1: // cl
		a.rexW(0, dst.reg)
		a.emitByte(0xD3)
		a.emitByte(modrmRegReg(int(dig), dst.reg))
	default:
		return fmt.Errorf("%s: unsupported shift count: %q", mnem, ln)
	}
	return nil
}

// ---- SSE2 (scalar double / quadword moves) --------------------------------

// sseSpec describes a two-operand SSE2 instruction. `prefix` is the legacy
// prefix (0x66 / 0xF2 / 0xF3); `op` is the opcode byte after 0F; `w` asks for
// REX.W (needed by the movq/cvtsi2sd/cvttsd2si conversions). `sdx` marks
// movsd/movss, whose store form (xmm -> mem) uses op|1.
var sseSpec = map[string]struct {
	prefix byte
	op     byte
	w      bool
	sdx    bool
}{
	"movsd":     {0xF2, 0x10, false, true},
	"movss":     {0xF3, 0x10, false, true},
	"addsd":     {0xF2, 0x58, false, false},
	"subsd":     {0xF2, 0x5C, false, false},
	"mulsd":     {0xF2, 0x59, false, false},
	"divsd":     {0xF2, 0x5E, false, false},
	"sqrtsd":    {0xF2, 0x51, false, false},
	"xorpd":     {0x66, 0x57, false, false},
	"ucomisd":   {0x66, 0x2E, false, false},
	"cvtsi2sd":  {0xF2, 0x2A, true, false},
	"cvttsd2si": {0xF2, 0x2C, true, false},
	// Single <-> double conversions. Both are XMM-dst with an XMM or m32/m64
	// source, which is exactly the generic two-operand shape above.
	"cvtss2sd": {0xF3, 0x5A, false, false},
	"cvtsd2ss": {0xF2, 0x5A, false, false},
}

// emitSSE emits the REX prefix for an SSE instruction: W + R (reg field) + B
// (rm field). No X bit -- XMM instructions never use an index register.
func (a *Assembler) emitSSE(w bool, regExt, rmExt int) {
	v := byte(0x40)
	if w {
		v |= 0x08
	}
	if regExt >= 8 {
		v |= 0x04
	}
	if rmExt >= 8 {
		v |= 0x01
	}
	if v != 0x40 {
		a.emitByte(v)
	}
}

// emitSSEmem is like emitSSE but takes the extension bits from a planned
// memory encoding (so REX.R/B come from the XMM reg field and the base/index).
func (a *Assembler) emitSSEmem(w bool, e memEnc) {
	v := byte(0x40)
	if w {
		v |= 0x08
	}
	if e.rexR {
		v |= 0x04
	}
	if e.rexX {
		v |= 0x02
	}
	if e.rexB {
		v |= 0x01
	}
	if v != 0x40 {
		a.emitByte(v)
	}
}

func (a *Assembler) encodeSSE(mnem string, ops []Operand, ln string) error {
	if mnem == "movq" {
		return a.encodeMovQ(ops, ln)
	}
	spec, ok := sseSpec[mnem]
	if !ok {
		return fmt.Errorf("unknown SSE: %q", mnem)
	}
	if len(ops) != 2 {
		return fmt.Errorf("%s needs 2 operands: %q", mnem, ln)
	}
	// cvttsd2si is the one SSE2 form whose destination is a GP register: the
	// GP dst lives in ModRM.reg and the XMM/mem source in ModRM.rm. The
	// generic path below assumes the XMM operand is always the reg field,
	// which would swap the two and silently produce e.g. "cvttsd2si rax,xmm1"
	// for "cvttsd2si rcx,xmm0".
	if mnem == "cvttsd2si" {
		gp, src := ops[0], ops[1]
		if gp.isXMM || src.kind != K_REG && src.kind != K_MEM {
			return fmt.Errorf("cvttsd2si needs GP dst: %q", ln)
		}
		a.emitByte(spec.prefix)
		if src.kind == K_MEM {
			if src.isRip {
				a.emitSSE(spec.w, gp.reg, 0)
				a.emitByte(0x0F)
				a.emitByte(spec.op)
				a.emitByte(modrmRip(gp.reg))
				off := a.curOff()
				a.emitInt32(0)
				a.fixup(off, src.memSym)
				return nil
			}
			e, err := a.planMem(gp.reg, src)
			if err != nil {
				return err
			}
			a.emitSSEmem(spec.w, e)
			a.emitByte(0x0F)
			a.emitByte(spec.op)
			a.emitMemEnc(e)
			return nil
		}
		a.emitSSE(spec.w, gp.reg, src.reg)
		a.emitByte(0x0F)
		a.emitByte(spec.op)
		a.emitByte(modrmRegReg(gp.reg, src.reg))
		return nil
	}
	// The XMM operand is always the ModRM reg field; the other operand is the
	// rm field (a GP register, an XMM, or memory).
	var xmmOp, other Operand
	if ops[0].isXMM {
		xmmOp, other = ops[0], ops[1]
	} else {
		xmmOp, other = ops[1], ops[0]
	}
	op := spec.op
	if spec.sdx && xmmOp != ops[0] {
		op |= 1 // movsd/movss store form: xmm is the source, mem is the dest
	}

	// REX planning for the rm operand (memory uses the planned extension bits).
	var e memEnc
	if other.kind == K_MEM && !other.isRip {
		var err error
		e, err = a.planMem(xmmOp.reg, other)
		if err != nil {
			return err
		}
	}

	a.emitByte(spec.prefix)
	if other.kind == K_MEM && !other.isRip {
		a.emitSSEmem(spec.w, e)
	} else {
		a.emitSSE(spec.w, xmmOp.reg, other.reg)
	}
	a.emitByte(0x0F)
	a.emitByte(op)

	if other.kind == K_MEM {
		if other.isRip {
			a.emitByte(modrmRip(xmmOp.reg))
			off := a.curOff()
			a.emitInt32(0)
			a.fixup(off, other.memSym)
		} else {
			a.emitMemEnc(e)
		}
	} else {
		a.emitByte(modrmRegReg(xmmOp.reg, other.reg))
	}
	return nil
}

// encodeMovQ implements the two GP <-> XMM moves:
//
//	movq xmm, r64   => 66 REX.W 0F 6E   (reg=xmm, rm=r64)   GP -> XMM
//	movq r64, xmm   => 66 REX.W 0F 7E   (reg=xmm, rm=r64)   XMM -> GP
func (a *Assembler) encodeMovQ(ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("movq needs 2 operands: %q", ln)
	}
	dst, src := ops[0], ops[1]
	if dst.isXMM && src.kind == K_REG && !src.isXMM {
		a.emitByte(0x66)
		a.emitSSE(true, dst.reg, src.reg)
		a.emitByte(0x0F)
		a.emitByte(0x6E)
		a.emitByte(modrmRegReg(dst.reg, src.reg))
		return nil
	}
	if src.isXMM && dst.kind == K_REG && !dst.isXMM {
		a.emitByte(0x66)
		a.emitSSE(true, src.reg, dst.reg)
		a.emitByte(0x0F)
		a.emitByte(0x7E)
		a.emitByte(modrmRegReg(src.reg, dst.reg))
		return nil
	}
	return fmt.Errorf("movq: unsupported operands: %q", ln)
}

// ---- group-encoded unary ops (idiv / div / mul / neg / not / inc / dec) ----
//
// All of these share one shape: an opcode byte whose ModRM.reg field carries a
// group number (/0 ../7) and whose ModRM.rm field names a single operand.
// That operand may now be memory, not just a register, so `inc qword [rbp-8]`
// updates a stack slot in place.
func (a *Assembler) encodeGrp(op byte, dig int, ops []Operand, ln string) error {
	if len(ops) != 1 {
		return fmt.Errorf("unary op needs exactly one operand: %q", ln)
	}
	o := ops[0]
	var width int
	switch o.kind {
	case K_REG:
		width = regWidth(o)
	case K_MEM:
		width = memSrcWidth(o, 8)
	default:
		return fmt.Errorf("unary op needs a register or memory operand: %q", ln)
	}
	return a.emitOpRM(width, nil, false, op, dig, o, nil)
}

// ---- movzx / movsx / movsxd ------------------------------------------------

// movextAT holds the GAS/AT&T spellings of the widening moves, whose names
// encode both widths: movzXX = zero-extend, movsXX = sign-extend, the first
// trailing letter being the source (b=byte, w=word, l=dword) and the last the
// destination (w=word, l=dword, q=qword). Hand-written Linux asm spells them
// this way, and the ELF target exists to consume hand-written Linux asm.
var movextAT = map[string][2]int{
	"movzbl": {1, 4}, "movzbw": {1, 2}, "movzbq": {1, 8},
	"movzwl": {2, 4}, "movzwq": {2, 8},
	"movsbl": {1, 4}, "movsbw": {1, 2}, "movsbq": {1, 8},
	"movswl": {2, 4}, "movswq": {2, 8},
	"movslq": {4, 8},
}

// encodeMovExtend assembles the whole zero/sign-extension family. The rule is
// that the *source* width chooses the opcode and the *destination* width
// chooses REX.W / 0x66:
//
//	8-bit src   0F B6/BE /r
//	16-bit src  0F B7/BF /r
//	32-bit src  REX.W 63 /r   (movsxd / movslq only -- into a 64-bit register)
func (a *Assembler) encodeMovExtend(mnem string, ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("%s needs 2 operands: %q", mnem, ln)
	}
	dst, src := ops[0], ops[1]
	if dst.kind != K_REG || dst.isByte {
		return fmt.Errorf("%s dst must be a 16/32/64-bit register: %q", mnem, ln)
	}
	dw := regWidth(dst)
	signed := strings.HasPrefix(mnem, "movs")

	// Explicit GAS widths (movzbl, movswq, ...) override both directions; the
	// register would otherwise have to be the right size to say anything.
	if w, ok := movextAT[mnem]; ok {
		return a.emitMovext(dst, src, w[0], w[1], signed, ln)
	}

	var sw int
	switch src.kind {
	case K_REG:
		sw = regWidth(src)
	case K_MEM:
		sw = memSrcWidth(src, 0)
	default:
		return fmt.Errorf("%s: src must be a register or memory: %q", mnem, ln)
	}
	if sw == 0 {
		return fmt.Errorf("%s: memory source needs a byte/word prefix: %q", mnem, ln)
	}
	return a.emitMovext(dst, src, sw, dw, signed, ln)
}

// emitMovext emits one widening move given both operand widths.
func (a *Assembler) emitMovext(dst, src Operand, sw, dw int, signed bool, ln string) error {
	// movsxd: 32 -> 64 bits. Its own opcode (REX.W 63 /r) and its own
	// restriction that the destination has to be a full 64-bit register.
	if sw == 4 {
		if !signed || dw != 8 {
			return fmt.Errorf("only movsxd may widen a dword source (got %d->%d): %q", sw, dw, ln)
		}
		return a.emitOpRM(8, nil, false, 0x63, dst.reg, src, nil)
	}
	if dw < sw {
		return fmt.Errorf("cannot widen a %d-byte source into a %d-byte register: %q", sw, dw, ln)
	}
	var op byte
	switch {
	case sw == 1 && !signed:
		op = 0xB6 // movzx r, r/m8
	case sw == 1:
		op = 0xBE // movsx r, r/m8
	case sw == 2 && !signed:
		op = 0xB7 // movzx r, r/m16
	default:
		op = 0xBF // movsx r, r/m16
	}
	return a.emitOpRM(dw, nil, true, op, dst.reg, src, nil)
}

// ---- setcc -----------------------------------------------------------------

// encodeSetCC emits `set<cc> r/m8`: it writes 1 or 0 according to the flags,
// which is how a C comparison becomes a plain 0/1 int. The target is 8 bits
// wide and must be named as such (al, sil, `byte [rbp-8]`).
func (a *Assembler) encodeSetCC(cc int, ops []Operand, ln string) error {
	if len(ops) != 1 {
		return fmt.Errorf("setcc needs one operand: %q", ln)
	}
	o := ops[0]
	if o.kind == K_REG && !o.isByte {
		return fmt.Errorf("setcc target must be an 8-bit register: %q", ln)
	}
	if o.kind != K_REG && o.kind != K_MEM {
		return fmt.Errorf("setcc target must be a byte register or memory: %q", ln)
	}
	return a.emitOpRM(1, nil, true, byte(0x90+cc), 0, o, nil)
}

// ---- cmovcc ----------------------------------------------------------------

// encodeCmovCC emits `cmov<cc> dst, src` -- the conditional move a compiler
// prefers over a branch when the body is a single assignment ("x = cond ? a :
// b" with no side effects). Width follows the destination register.
func (a *Assembler) encodeCmovCC(cc int, ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("cmov needs 2 operands: %q", ln)
	}
	dst := ops[0]
	if dst.kind != K_REG {
		return fmt.Errorf("cmov dst must be a register: %q", ln)
	}
	return a.emitOpRM(regWidth(dst), nil, true, byte(0x40+cc), dst.reg, ops[1], nil)
}

// ---- xchg ------------------------------------------------------------------

// encodeXchg emits `xchg dst, src` (86/87 /r; 8-bit uses 86). The operation is
// symmetric so the operand order does not matter to the encoding, but it does
// carry an implicit lock with memory operands -- exactly why an atomic
// flag flip can be written as one instruction.
func (a *Assembler) encodeXchg(ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("xchg needs 2 operands: %q", ln)
	}
	dst, src := ops[0], ops[1]
	if dst.kind != K_REG {
		return fmt.Errorf("xchg dst must be a register: %q", ln)
	}
	width := regWidth(dst)
	op := byte(0x87)
	if width == 1 {
		op = 0x86
	}
	return a.emitOpRM(width, nil, false, op, dst.reg, src, nil)
}

// ---- bit test family -------------------------------------------------------

// bitRegOp gives the register-count form of each bit instruction; the ModRM.reg
// field then holds the *bit number* register and ModRM.rm the bit string.
var bitRegOp = map[string]byte{
	"bt":  0xA3, // bit test
	"bts": 0xAB, // bit test and set
	"btr": 0xB3, // bit test and reset
	"btc": 0xBB, // bit test and complement
}

// bitImmDig gives the immediate form, all sharing opcode 0F BA with only the
// ModRM.reg field (and the trailing imm8 bit number) telling them apart.
var bitImmDig = map[string]int{
	"bt": 4, "bts": 5, "btr": 6, "btc": 7,
}

// encodeBit assembles `bt/bts/btr/btc bitstring, index`, where the index is
// either a register or an 8-bit immediate. These one instruction-level flags
// are what a lock-free bitmask uses to claim a slot.
func (a *Assembler) encodeBit(mnem string, ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("%s needs 2 operands: %q", mnem, ln)
	}
	rmOp, cnt := ops[0], ops[1]
	width := 8
	if rmOp.kind == K_REG {
		width = regWidth(rmOp)
	} else if rmOp.kind == K_MEM {
		width = memSrcWidth(rmOp, 8)
	} else {
		return fmt.Errorf("%s: first operand must be a register or memory: %q", mnem, ln)
	}
	switch {
	case cnt.kind == K_REG:
		return a.emitOpRM(width, nil, true, bitRegOp[mnem], cnt.reg, rmOp, nil)
	case cnt.kind == K_IMM:
		if cnt.imm < 0 || cnt.imm > 255 {
			return fmt.Errorf("%s: bit index %d does not fit 8 bits: %q", mnem, cnt.imm, ln)
		}
		imm := cnt.imm
		return a.emitOpRM(width, nil, true, 0xBA, bitImmDig[mnem], rmOp, func() {
			a.emitByte(byte(imm))
		})
	}
	return fmt.Errorf("%s: bit index must be a register or immediate: %q", mnem, ln)
}

// ---- bswap -----------------------------------------------------------------

// encodeBswap emits `bswap reg` (0F C8+rd), the byte-order reversal that turns
// a big-endian wire value into a little-endian register. Only 32- and 64-bit
// registers may be named; bswap ax is not encodable.
func (a *Assembler) encodeBswap(ops []Operand, ln string) error {
	if len(ops) != 1 || ops[0].kind != K_REG {
		return fmt.Errorf("bswap needs one register: %q", ln)
	}
	r := ops[0].reg
	if ops[0].is16 || ops[0].isByte {
		return fmt.Errorf("bswap needs a 32/64-bit register: %q", ln)
	}
	// The default operand size here is 32 bits, so `bswap eax` needs no REX
	// while `bswap rax` needs REX.W to reverse all eight bytes. There is only
	// ever ONE REX byte, so the REX.B that extends r8-r15 folds into it rather
	// than being emitted separately (emitting 48 41 would leave 0x41 to be
	// decoded as part of the instruction).
	rex := byte(0)
	if !ops[0].is32 {
		rex = 0x48
	}
	if r >= 8 {
		if rex == 0 {
			rex = 0x40 // REX always has 0x40 as its base, even for a lone REX.B
		}
		rex |= 0x01
	}
	if rex != 0 {
		a.emitByte(rex)
	}
	a.emitByte(0x0F)
	a.emitByte(byte(0xC8 + (r & 7)))
	return nil
}

// ---------------------------------------------------------------------------
// string helpers
// ---------------------------------------------------------------------------

// splitTopLevel splits on commas that are NOT inside quotes.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	var cur strings.Builder
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				// A backslash escapes whatever follows it, so the quote in
				// "\"" does not end the string. Skipping escapes here keeps
				// the quoting state in step with the bytes: without it, a db
				// operand holding something JSON-ish -- "a\\\\\"b,c" -- ended
				// its string at the wrong place and the comma inside it was
				// split off as a separate operand.
				i++
				cur.WriteByte(s[i])
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inStr = c
			cur.WriteByte(c)
			continue
		}
		if c == sep {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
