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
	sect   int    // section index where the 4 displacement bytes live
	off    int    // offset of the 4-byte displacement within that section
	sym    string // target symbol (label, data label, or "IAT:name")
	ripAdj int    // extra bytes after the disp32 before the true RIP
	// (0 normally; 1 for C6 mov r/m8,imm8 which has a trailing imm8)
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
	if i := strings.Index(ln, ";"); i >= 0 {
		ln = ln[:i]
	}
	if i := strings.Index(ln, "//"); i >= 0 {
		ln = ln[:i]
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
	// redundant "ptr" keyword. The actual operand size is inferred from the
	// register used (al/bl => 8-bit, rax/rbx => 64-bit), so dropping the
	// keyword is always safe.
	for {
		lower := strings.ToLower(tok)
		stripped := false
		for _, kw := range []string{"byte", "word", "dword", "qword"} {
			if strings.HasPrefix(lower, kw+" ") || strings.HasPrefix(lower, kw+"[") {
				tok = strings.TrimSpace(tok[len(kw):])
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
	if ri, ok := regIndex(tok); ok {
		return Operand{kind: K_REG, reg: ri}, nil
	}
	if strings.HasPrefix(tok, "[") && strings.HasSuffix(tok, "]") {
		inner := strings.TrimSpace(tok[1 : len(tok)-1])
		// RIP-relative: [rip+sym] (case-insensitive "rip")
		innerStripped := strings.ReplaceAll(inner, "RIP+", "")
		innerStripped = strings.ReplaceAll(innerStripped, "rip+", "")
		if innerStripped != inner {
			return Operand{kind: K_MEM, memSym: a.qualify(strings.TrimSpace(innerStripped)), isRip: true, memIndex: -1}, nil
		}
		// Simple register-indirect: [reg]. Normalize it onto memBase (with
		// memScale = 1) so planMem sees a well-formed operand -- leaving
		// memScale at zero makes planMem reject it.
		if !strings.ContainsAny(inner, "+-*") {
			if ri, ok := regIndex(inner); ok {
				return Operand{kind: K_MEM, memReg: ri, memBase: ri, memHasBase: true,
					isRip: false, memIndex: -1, memScale: 1}, nil
			}
			// a bare symbol => RIP-relative data reference ([sym])
			return Operand{kind: K_MEM, memSym: a.qualify(inner), isRip: true, memIndex: -1}, nil
		}
		// Complex: [base+index*scale+disp], [base+disp], [index*scale], ...
		o, err := parseMemInner(inner)
		if err != nil {
			return Operand{}, err
		}
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
func (a *Assembler) encodeMovRegMem(regOp Operand, memOp Operand, store, isByte bool) error {
	regField := regOp.reg
	e, err := a.planMem(regField, memOp)
	if err != nil {
		return err
	}
	// Build REX.
	var opcode byte
	if isByte {
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
	} else {
		if store {
			opcode = 0x89 // MOV r/m, r
		} else {
			opcode = 0x8B // MOV r, r/m
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

func (a *Assembler) encode(mnem string, ops []Operand, ln string) error {
	switch mnem {
	case "ret":
		a.emitByte(0xC3)
		return nil
	case "nop":
		a.emitByte(0x90)
		return nil
	case "int3":
		a.emitByte(0xCC)
		return nil
	case "db":
		return a.emitDB(restOf(ln))
	case "dq":
		return a.emitDQ(restOf(ln))
	case "du":
		return a.emitDU(restOf(ln))
	case "push":
		return a.encodePushPop(0x50, ops, ln)
	case "pop":
		return a.encodePushPop(0x58, ops, ln)
	case "call":
		return a.encodeCall(ops, ln)
	case "jmp":
		return a.encodeJmp(0xE9, ops, ln)
	case "je":
		return a.encodeCond(0x84, ops, ln)
	case "jne":
		return a.encodeCond(0x85, ops, ln)
	case "jl":
		return a.encodeCond(0x8C, ops, ln)
	case "jge":
		return a.encodeCond(0x8D, ops, ln)
	case "jle":
		return a.encodeCond(0x8E, ops, ln)
	case "jg":
		return a.encodeCond(0x8F, ops, ln)
	case "jb":
		return a.encodeCond(0x82, ops, ln)
	case "jae":
		return a.encodeCond(0x83, ops, ln)
	case "jbe":
		return a.encodeCond(0x86, ops, ln)
	case "ja":
		return a.encodeCond(0x87, ops, ln)
	case "js":
		return a.encodeCond(0x88, ops, ln)
	case "jns":
		return a.encodeCond(0x89, ops, ln)
	case "jo":
		return a.encodeCond(0x80, ops, ln)
	case "jno":
		return a.encodeCond(0x81, ops, ln)
	case "lea":
		return a.encodeLea(ops, ln)
	case "mov":
		return a.encodeMov(ops, ln)
	case "add", "sub", "and", "or", "xor", "cmp", "test":
		return a.encodeArith(mnem, ops, ln)
	case "cqo", "cqto":
		// Sign-extend RAX into RDX:RAX, needed before idiv. REX.W + 0x99.
		a.emitByte(0x48)
		a.emitByte(0x99)
		return nil
	case "imul":
		return a.encodeImul(ops, ln)
	case "shl", "sal", "shr", "sar":
		return a.encodeShift(mnem, ops, ln)
	case "idiv":
		return a.encodeUnary(0xF7, 7, ops, ln) // idiv r/m
	case "div":
		return a.encodeUnary(0xF7, 6, ops, ln) // div r/m
	case "inc":
		return a.encodeUnary(0xFF, 0, ops, ln)
	case "dec":
		return a.encodeUnary(0xFF, 1, ops, ln)
	case "neg":
		return a.encodeUnary(0xF7, 3, ops, ln)
	case "syscall":
		// Fast system call (0F 05). Linux: rax = number, args in
		// rdi, rsi, rdx, r10, r8, r9.
		a.emitByte(0x0F)
		a.emitByte(0x05)
		return nil
	case "movsd", "movss", "addsd", "subsd", "mulsd", "divsd", "sqtsd",
		"xorpd", "ucomisd", "cvtsi2sd", "cvttsd2si", "movq":
		return a.encodeSSE(mnem, ops, ln)
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

func (a *Assembler) encodePushPop(base byte, ops []Operand, ln string) error {
	if len(ops) != 1 || ops[0].kind != K_REG {
		return fmt.Errorf("push/pop needs one register: %q", ln)
	}
	r := ops[0].reg
	if r >= 8 {
		a.emitByte(0x41)
	}
	a.emitByte(base + byte(r&7))
	return nil
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
	if o.kind == K_REG {
		// call reg: FF /2 with mod=11 reg field
		a.rexW(0, o.reg)
		a.emitByte(0xFF)
		a.emitByte(modrmRegReg(2, o.reg))
		return nil
	}
	return fmt.Errorf("call: unsupported operand %q", ln)
}

// ---- unconditional jmp -----------------------------------------------------

func (a *Assembler) encodeJmp(op byte, ops []Operand, ln string) error {
	if len(ops) != 1 || ops[0].kind != K_SYM {
		return fmt.Errorf("jmp needs one symbol: %q", ln)
	}
	a.emitByte(op) // E9 rel32
	off := a.curOff()
	a.emitInt32(0)
	a.fixup(off, ops[0].sym)
	return nil
}

// ---- conditional jump (0F 8x rel32) ----------------------------------------

func (a *Assembler) encodeCond(op byte, ops []Operand, ln string) error {
	if len(ops) != 1 || ops[0].kind != K_SYM {
		return fmt.Errorf("conditional jump needs one symbol: %q", ln)
	}
	a.emitByte(0x0F)
	a.emitByte(op)
	off := a.curOff()
	a.emitInt32(0)
	a.fixup(off, ops[0].sym)
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
		} else {
			a.rexW(dst.reg, src.reg)
			a.emitByte(0x8B) // load: reg=dst, rm=src
			a.emitByte(modrmRegReg(dst.reg, src.reg))
		}
		return nil
	}

	// mov reg, [rip+sym]   (64-bit load)
	if dst.kind == K_REG && !dst.isByte && src.kind == K_MEM && src.isRip {
		a.rexW(dst.reg, 0)
		a.emitByte(0x8B)
		a.emitByte(modrmRip(dst.reg))
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, src.memSym)
		return nil
	}

	// mov [rip+sym], reg   (64-bit or 8-bit store)
	if dst.kind == K_MEM && dst.isRip && src.kind == K_REG {
		if src.isByte {
			// 0x88 = mov r/m8, r8: modrm reg field = src -> REX.R (0x44), not REX.B.
			// spl/bpl/sil/dil (4..7) also need a bare REX (0x40) or they decode as ah/ch/dh/bh.
			if src.reg >= 8 {
				a.emitByte(0x44)
			} else if src.reg >= 4 {
				a.emitByte(0x40)
			}
			a.emitByte(0x88) // store r/m8, r8
			a.emitByte(modrmRip(src.reg))
		} else {
			// 0x89 = mov r/m64, r64: modrm reg field = src, rm = RIP.
			a.rexW(src.reg, 0)
			a.emitByte(0x89) // store r/m, r
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

	// mov [base+disp], imm  (64-bit, sign-extended imm32) -- stack slots
	if dst.kind == K_MEM && !dst.isRip && dst.memHasBase && src.kind == K_IMM {
		return a.encodeMovMemImm(dst.memBase, dst.memDisp, src.imm, ln)
	}

	// mov reg, [mem]   (64-bit load, register-based memory)
	if dst.kind == K_REG && !dst.isByte && src.kind == K_MEM && !src.isRip {
		return a.encodeMovRegMem(dst, src, false, false)
	}

	// mov reg8, [mem]  (8-bit load)
	if dst.kind == K_REG && dst.isByte && src.kind == K_MEM && !src.isRip {
		return a.encodeMovRegMem(dst, src, false, true)
	}

	// mov [mem], reg / mov [mem], reg8  (store; byte-ness taken from src)
	if dst.kind == K_MEM && !dst.isRip && src.kind == K_REG {
		return a.encodeMovRegMem(src, dst, true, src.isByte)
	}

	return fmt.Errorf("mov: unsupported operand combination: %q", ln)
}

// encodeMovMemImm emits: mov [base+disp], imm32 (sign-extended to 64-bit via REX.W).
func (a *Assembler) encodeMovMemImm(base, disp int, imm int64, ln string) error {
	rex := byte(0x48)
	if base >= 8 {
		rex |= 0x01
	}
	a.emitByte(rex)
	a.emitByte(0xC7) // mov r/m, imm32
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
	a.emitInt32(int32(imm))
	return nil
}

// ---- arithmetic reg,reg / reg,imm ------------------------------------------

var arithCode = map[string]struct {
	reg byte // opcode for r/m, r form (reg field = src)
	dig byte // /digit for imm form
}{
	"add":  {0x01, 0},
	"sub":  {0x29, 5},
	"and":  {0x21, 4},
	"or":   {0x09, 1},
	"xor":  {0x31, 6},
	"cmp":  {0x39, 7},
	"test": {0x85, 0}, // test r/m, r (no immediate form used here)
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

var shiftDigit = map[string]byte{
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
	"sqtsd":     {0xF2, 0x51, false, false},
	"xorpd":     {0x66, 0x57, false, false},
	"ucomisd":   {0x66, 0x2E, false, false},
	"cvtsi2sd":  {0xF2, 0x2A, true, false},
	"cvttsd2si": {0xF2, 0x2C, true, false},
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

// ---- unary (idiv / inc / dec / neg) ---------------------------------------

func (a *Assembler) encodeUnary(op byte, dig int, ops []Operand, ln string) error {
	if len(ops) != 1 || ops[0].kind != K_REG {
		return fmt.Errorf("unary op needs one register: %q", ln)
	}
	r := ops[0].reg
	a.rexW(0, r)
	a.emitByte(op)
	a.emitByte(modrmRegReg(dig, r))
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
