package goa

import (
	"fmt"
	"gocld"
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
	memSymOff  int    // K_MEM: extra byte offset added to a symbolic reference
	memBase    int    // K_MEM base register for [base+index*scale+disp]
	memHasBase bool   // K_MEM: operand has a base register
	memIndex   int    // K_MEM index register (-1 = none) for [base+index*scale]
	memScale   int    // K_MEM index scale (1,2,4,8)
	memDisp    int    // K_MEM displacement
	memHasDisp bool   // K_MEM: a displacement was explicitly provided (even if 0)
	memWidth   int    // K_MEM: explicit operand width from byte/word/dword/qword prefix (0 = infer)
	isRip      bool   // K_MEM: true => [rip+sym], false => register-based memory
	sym        string // K_SYM
	memSeg     int    // K_MEM: segment override prefix (segNone/segFS/segGS); emitted before REX
}

// Segment-override prefixes for memory operands. A memory operand may carry one
// of these; the assembler emits the byte as a legacy prefix -- strictly before
// the REX prefix -- so `mov rax, fs:[rbx]` assembles to 64 48 8B 03 (0x64 FS,
// 0x48 REX.W, 0x8B MOV, ModRM). x86-64 uses FS for the Linux TLS base and GS for
// the Windows TEB/TLS pointer.
const (
	segNone = 0
	segFS   = 0x64 // FS segment override
	segGS   = 0x65 // GS segment override
)

type Section struct {
	Name     string
	Writable bool
	Code     bool
	Data     []byte
	// cur is the current virtual offset within the section. For normal sections
	// it tracks len(Data); for a .bss section it advances on resb/resq without
	// any bytes being appended to Data, so the file stays empty while symbol
	// offsets remain correct.
	cur int
	// Bss marks an uninitialised-data section. Its bytes are zero-filled by the
	// loader, never written to the file, and its virtual size is cur (not
	// len(Data), which stays 0).
	Bss bool
	// Unmapped marks a section whose bytes are merged and whose symbols and
	// relocations resolve normally, but which is left out of the finished image.
	//
	// The Win64 unwind sections are the only user. Their contents are small --
	// 12 bytes of .pdata and about 11 of .xdata per function -- but a PE section
	// occupies a whole multiple of FileAlignment (512, the smallest value Windows
	// accepts) in the file, so a program with three functions paid 1024 bytes to
	// store 72 bytes of table, and every image carried at least that. Dropping
	// the sections is what goa's own generator does: it registers no unwind info
	// at all, and the loader copes. goc compiles C, with no C++ exceptions to
	// propagate and no unwinder of its own, so what is lost is the ability to
	// walk the stack during a post-mortem crash dump -- the same loss the native
	// path already accepted, which is why this is a size decision and not a
	// correctness one.
	Unmapped bool
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
	// addend is folded into the computed target: the resolved value is
	// symRVA[sym] + addend. COFF needs it because a relocation against a
	// *section* symbol carries the position within that section in the field's
	// existing contents rather than in the symbol -- the unwind tables are the
	// only place goc sees this. Always 0 for assembler-generated fixups.
	addend int
	// absolute makes the fixup store the resolved address verbatim instead of a
	// displacement. COFF's IMAGE_REL_AMD64_ADDR32NB is such a relocation: the
	// field wants an RVA, not a distance, and applying the usual
	// "target minus where the field sits" arithmetic to it yields a large
	// negative number that the loader rejects.
	absolute bool
	// wide makes an absolute fixup write eight bytes instead of four, for
	// IMAGE_REL_AMD64_ADDR64 -- a pointer-sized slot holding an address.
	wide bool
	// virtual makes an absolute fixup store ImageBase+RVA rather than the RVA
	// alone. The two absolute relocations differ exactly here: an unwind table
	// entry (ADDR32NB) holds an RVA, because the loader adds the base itself,
	// while a data pointer (ADDR64) is dereferenced directly and so has to carry
	// the full address.
	virtual bool
	// sym2 names a second symbol whose address is *subtracted* from the
	// first's. It exists for one shape of code: a switch's jump table, which
	// LLVM emits as a table of label differences --
	//
	//	.long	.LBB29_14-.LJTI29_0
	//
	// meaning "the offset of this case's target from the table's own base".
	// The two labels are usually in different sections (the targets in .text,
	// the table in .rdata), so the value cannot be reduced at assembly time;
	// it only becomes a number once both addresses are known. The relocation
	// is really REL32 against a *pair*, which no single-symbol field can
	// express, so the subtraction is carried here and applied by the linker.
	sym2 string
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
	// Unwind metadata collection (ELF target only). For every function whose
	// prolog is the canonical `push rbp; mov rbp, rsp; push <callee-saves>;
	// sub rsp, N`, we record enough to let the Windows gocrun loader register a
	// per-function RUNTIME_FUNCTION table via RtlAddFunctionTable. Without it
	// the x64 unwinder faults while walking guest frames during a Win32
	// syscall's internal exception dispatch (it has no PE unwind info).
	uwRecs []*uwFunc
	uwCur  *uwFunc
	// attAlias maps symbols that LLVM's AsmPrinter invents onto the ones goa
	// defines. See attDefaultAliases.
	attAlias map[string]string
	// attMode switches label qualification over to GAS's rules. See qualify.
	attMode bool
	// pdataRVA and pdataSize describe the Win64 exception directory: the
	// RUNTIME_FUNCTION array the loader walks during exception dispatch. They
	// stay zero unless an object carrying unwind info was merged in (goa's own
	// assembler emits none).
	pdataRVA  int
	pdataSize int
}

// uwFunc records one function's prolog for unwind-table emission.
type uwFunc struct {
	sect       int   // section index the function lives in (.text)
	start      int   // offset of the function label within the section
	end        int   // offset of the next label (or section end) within sect
	pushes     []int // register numbers pushed, in execution order (incl. rbp)
	alloc      int   // `sub rsp, N` amount (0 if no frame alloc)
	hasProlog  bool  // saw at least `push rbp`
	prologDone bool  // finished capturing the prolog shape
}

// uwRegNum maps an x86-64 register name to its unwind-code register number.
func uwRegNum(name string) (int, bool) {
	switch name {
	case "rax":
		return 0, true
	case "rcx":
		return 1, true
	case "rdx":
		return 2, true
	case "rbx":
		return 3, true
	case "rsp":
		return 4, true
	case "rbp":
		return 5, true
	case "rsi":
		return 6, true
	case "rdi":
		return 7, true
	}
	if len(name) >= 2 && name[0] == 'r' {
		if n, err := strconv.Atoi(name[1:]); err == nil && n >= 8 && n <= 15 {
			return n, true
		}
	}
	return 0, false
}

func uwParsePush(t string) (int, bool) {
	if !strings.HasPrefix(t, "push ") {
		return 0, false
	}
	return uwRegNum(strings.TrimSpace(t[len("push "):]))
}

func uwParseSubRsp(t string) (int, bool) {
	const p = "sub rsp, "
	if !strings.HasPrefix(t, p) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(t[len(p):]))
	if err != nil {
		return 0, false
	}
	return n, true
}

// uwCloseFunc finalizes the in-progress function record (if any) using the
// offset of the label that ends it. When the ending label lives in a different
// section, the function spans to the end of its own section instead.
func (a *Assembler) uwCloseFunc(off, sect int) {
	if a.uwCur == nil {
		return
	}
	if sect == a.uwCur.sect {
		a.uwCur.end = off
	} else {
		a.uwCur.end = len(a.sections[a.uwCur.sect].Data)
	}
	if a.uwCur.hasProlog {
		a.uwRecs = append(a.uwRecs, a.uwCur)
	}
	a.uwCur = nil
}

// isLocalLabel reports whether a label name is file-local.
func isLocalLabel(s string) bool { return strings.HasPrefix(s, ".") }

// qualify maps a label reference to its real symbol name.
//
// A dot-prefixed name is local to the enclosing function, so it is qualified
// with that function's name -- which is what keeps two functions' `.Lrec`
// apart. That convention comes from the hand-written asm this assembler was
// built around, where any dot-name is a local label.
//
// LLVM's output breaks it: only `.LBB*`/`.Ltmp*`/`.Lfunc*` are function-scoped,
// while `.str.0`, `.LCPI0_3` and friends are file-scope private names that many
// functions share. Qualifying those would make every reference after the first
// dangle, so in AT&T mode qualify asks attIsBlockLabel instead.
func (a *Assembler) qualify(sym string) string {
	if !isLocalLabel(sym) {
		return sym
	}
	if a.attMode {
		if attIsBlockLabel(sym) {
			return a.curGlobal + sym
		}
		return sym
	}
	return a.curGlobal + sym
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
	"read":                0,
	"write":               1,
	"open":                2,
	"close":               3,
	"lseek":               8,
	"mmap":                9,
	"mprotect":            10,
	"munmap":              11,
	"brk":                 12,
	"ioctl":               16,
	"writev":              20,
	"nanosleep":           35,
	"getpid":              39,
	"exit":                60,
	"kill":                62,
	"exit_group":          231,
	"gettimeofday":        96,
	"__goc_clock_gettime": 228,
	"unlink":              87,
	"rename":              82,
	"__goclib_vfork":      58, // used by system() on Linux
	"__goclib_execve":     59,
	"__goclib_wait4":      61,
	// Directory and metadata calls used by goclib/dir.c. The __goclib_* aliases
	// exist for the same reason as __goclib_rename: the wrapper that carries
	// the C name (stat/mkdir/rmdir) cannot extern that same name, or the stub
	// call would resolve to the wrapper itself.
	"__goclib_stat":       4, // stat
	"__goclib_mkdir":      83,
	"__goclib_rmdir":      84,
	"__goclib_getdents64": 217,
	"__goclib_getcwd":     79, // getcwd
	"__goclib_chmod":      90, // chmod
	"__goclib_access":     21, // access
	"__goclib_fstat":      5,  // fstat
	// __goclib_rename: the alias goclib's rename() wrapper calls for the raw
	// syscall. The wrapper cannot `extern rename` itself -- the name resolves
	// to its own definition, an infinite recursion (see goclib/file.c).
	"__goclib_rename": 82,
	// Threads (goclib/threads.c). __goclib_exit_thread is 60 rather than the
	// plain name `exit` because the C library defines a function called exit;
	// one output cannot carry both symbols. 60 terminates only the calling
	// thread, 231 (exit_group) the whole process -- which is why the library's
	// exit() uses the other one.
	"__goclib_clone":        56,
	"__goclib_futex":        202,
	"__goclib_gettid":       186,
	"__goclib_sched_yield":  24,
	"__goclib_exit_thread":  60,
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
	s := a.curSection()
	s.Data = append(s.Data, b)
	s.cur = len(s.Data)
}
func (a *Assembler) emitBytes(bs []byte) {
	s := a.curSection()
	s.Data = append(s.Data, bs...)
	s.cur = len(s.Data)
}

// emitInt16 writes a 16-bit little-endian value. GAS's `.word` and the 0x66
// operand-size forms both need it, and routing those through emitInt32 would
// put two stray bytes in the instruction stream.
func (a *Assembler) emitInt16(v int16) {
	a.emitByte(byte(v))
	a.emitByte(byte(v >> 8))
}

func (a *Assembler) emitInt32(v int32) {
	s := a.curSection()
	s.Data = append(s.Data, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	s.cur = len(s.Data)
}
func (a *Assembler) emitInt64(v int64) {
	for i := 0; i < 8; i++ {
		a.emitByte(byte(v >> (8 * i)))
	}
}

// reserve grows the current section's virtual offset by n bytes without writing
// any file bytes -- the basis of the BSS resb/resq/resd/resw directives.
func (a *Assembler) reserve(n int) {
	a.curSection().cur += n
}
func (a *Assembler) curOff() int { return a.curSection().cur }

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
// applyFixup patches one recorded relocation into place. target is the resolved
// address of f.sym; sym2, when non-empty, is the address subtracted from it for
// the label-difference form (see Fixup.sym2); base is the address of the section
// holding the field.
func applyFixup(s *Section, f Fixup, target, sym2, base int) error {
	size := 4
	switch {
	case f.short:
		size = 1
	case f.wide:
		size = 8
	}
	if f.off+size > len(s.Data) {
		return fmt.Errorf("fixup out of range for %s", f.sym)
	}
	// An absolute fixup stores the address itself; a relative one stores the
	// distance from the byte after the field (plus any instruction trailer).
	if f.absolute {
		if f.off+size > len(s.Data) {
			return fmt.Errorf("absolute fixup out of range for %s", f.sym)
		}
		addr := uint64(target + f.addend)
		if f.virtual {
			// The preferred load address is above 4GB, so a 32-bit field
			// would truncate it; only the 64-bit form can hold one.
			addr += uint64(gocld.ImageBase)
		}
		for i := 0; i < size; i++ {
			s.Data[f.off+i] = byte(addr >> (8 * i))
		}
		return nil
	}
	if f.sym2 != "" {
		// A jump-table entry: the field wants (sym - sym2), not
		// "sym relative to wherever the field sits". The two live in different
		// sections, so the usual displacement arithmetic is meaningless here --
		// the value is an offset into the table, which happens to be what
		// `base + i*4` lands on because the table's own base is sym2.
		diff := int32(target - sym2)
		for i := 0; i < size; i++ {
			s.Data[f.off+i] = byte(diff >> (8 * i))
		}
		return nil
	}
	disp := int32(target + f.addend - (base + f.off + size + f.ripAdj))
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
	// Close the final function record (spans to the end of its section).
	if a.uwCur != nil {
		a.uwCur.end = len(a.sections[a.uwCur.sect].Data)
		if a.uwCur.hasProlog {
			a.uwRecs = append(a.uwRecs, a.uwCur)
		}
		a.uwCur = nil
	}
	return nil
}

// emitSyscallStubs defines one stub per declared extern when targeting ELF.
// Each stub is `mov rax, <number>; call __goc_syscall; ret`, placed at the
// start of .text. Routing every syscall through the single __goc_syscall
// symbol (defined just below) lets a non-Linux loader substitute a Win32-backed
// translator without CPU emulation or code scanning; on real Linux __goc_syscall
// is just `syscall; ret` so the ELF stays self-contained and native.
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
		// Route every syscall through a single __goc_syscall chokepoint instead
		// of emitting the raw `syscall` instruction. On real Linux the loader
		// (kernel) provides __goc_syscall; in the Windows test harness the
		// gocrun loader rewrites __goc_syscall's body to a Win32-backed
		// translator. Either way the guest runs natively -- no CPU emulation.
		a.emitByte(0xE8) // call rel32
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, "__goc_syscall")
		a.emitByte(0xC3) // ret
	}
	// Single chokepoint for every Linux syscall. Defined here (in .text) so the
	// ELF is self-contained and runs natively on real Linux. The two trailing
	// NOPs leave room for a 5-byte `jmp rel32` patch the Windows loader applies
	// to redirect __goc_syscall to its Win32 translator.
	a.defineSym("__goc_syscall")
	a.emitByte(0x0F) // syscall
	a.emitByte(0x05)
	a.emitByte(0xC3) // ret
	a.emitByte(0x90) // nop (patch padding)
	a.emitByte(0x90) // nop (patch padding)
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
			writable := name == ".data" || name == ".idata" || name == ".tls" || name == ".bss"
			s := a.newSection(name, writable, name == ".text")
			s.Bss = name == ".bss"
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
			a.defineSym(a.qualify(label))
			off := a.curOff()
			// Finalize the previous function record (closes its [start,end)
			// range) and open a fresh one for this global entry label only.
			// Local labels (.Lxxx) are internal branch targets inside the
			// current function and must NOT reset the record, or the function
			// body between two local labels would be left without unwind info.
			a.uwCloseFunc(off, a.cur)
			a.uwCur = &uwFunc{sect: a.cur, start: off}
		} else {
			a.defineSym(a.qualify(label))
		}
		if rest != "" {
			return a.processLine(rest)
		}
		return nil
	}
	// label without colon in front of a data directive: "name db ..." / "name dq ..."
	if fields := strings.Fields(ln); len(fields) >= 2 &&
		(fields[1] == "db" || fields[1] == "dq" || fields[1] == "du" ||
			fields[1] == "resb" || fields[1] == "resw" || fields[1] == "resd" || fields[1] == "resq") {
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
	// Capture the prolog shape so the Windows gocrun loader can register a
	// per-function unwind table. The prolog is `push rbp; mov rbp, rsp;
	// push <callee-saves>; sub rsp, N`. We record the pushed registers and the
	// frame allocation; anything else ends the prolog window.
	if a.uwCur != nil && !a.uwCur.prologDone {
		t := strings.TrimSpace(ln)
		if reg, ok := uwParsePush(t); ok {
			a.uwCur.pushes = append(a.uwCur.pushes, reg)
			a.uwCur.hasProlog = true
		} else if t == "mov rbp, rsp" {
			// frame-pointer setup; no unwind code needed
		} else if n, ok := uwParseSubRsp(t); ok {
			a.uwCur.alloc = n
			a.uwCur.prologDone = true
		} else {
			a.uwCur.prologDone = true
		}
	}
	return a.emitInstr(ln)
}

// ---------------------------------------------------------------------------
// Operand parsing
// ---------------------------------------------------------------------------

func (a *Assembler) parseOperand(tok string) (Operand, error) {
	tok = strings.TrimSpace(tok)
	// Segment-override prefix: fs:[...] / gs:[...] (also fs:dword [...]).
	// Only a memory operand may carry one, so require a '[' after the colon.
	var memSeg int
	switch {
	case strings.HasPrefix(tok, "fs:") && strings.Contains(tok, "["):
		memSeg, tok = segFS, strings.TrimSpace(tok[3:])
	case strings.HasPrefix(tok, "gs:") && strings.Contains(tok, "["):
		memSeg, tok = segGS, strings.TrimSpace(tok[3:])
	}
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
			// The address may reach into the object (`[rip+G_x+4]`), which
			// LLVM emits for every field past the first. Stripping the "rip+"
			// prefix alone would leave "G_x+4" looking like one undefined
			// symbol, so the offset is split back off here.
			sym, off := innerStripped, 0
			if base, o2, ok := splitSymbolOffset(strings.TrimSpace(innerStripped)); ok {
				sym, off = base, o2
			}
			return Operand{kind: K_MEM, memSym: a.qualify(strings.TrimSpace(sym)), memSymOff: off,
				isRip: true, memIndex: -1, memScale: 1, memWidth: sizeKw, memSeg: memSeg}, nil
		}
		// Simple register-indirect: [reg]. Normalize it onto memBase (with
		// memScale = 1) so planMem sees a well-formed operand -- leaving
		// memScale at zero makes planMem reject it.
		if !strings.ContainsAny(inner, "+-*") {
			if ri, ok := regIndex(inner); ok {
				return Operand{kind: K_MEM, memReg: ri, memBase: ri, memHasBase: true,
					isRip: false, memIndex: -1, memScale: 1, memWidth: sizeKw, memSeg: memSeg}, nil
			}
			// a bare symbol => RIP-relative data reference ([sym])
			return Operand{kind: K_MEM, memSym: a.qualify(inner), isRip: true, memIndex: -1, memWidth: sizeKw, memSeg: memSeg}, nil
		}
		// Complex: [base+index*scale+disp], [base+disp], [index*scale], ...
		o, err := parseMemInner(inner)
		if err != nil {
			return Operand{}, err
		}
		// A symbolic reference found among the terms needs the assembler's
		// label-scoping rules applied, exactly as the bare `[sym]` case above
		// does.
		if o.memSym != "" {
			o.memSym = a.qualify(o.memSym)
		}
		o.memWidth = sizeKw
		o.memSeg = memSeg
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
		if err == nil {
			if t.neg {
				v = -v
			}
			o.memDisp += int(v)
			o.memHasDisp = true
			continue
		}
		// A symbol inside the brackets, possibly carrying an offset: `[rip+G_x]`,
		// or `[G_x+4]` / `[rip+G_x-8]` for one that reaches into the object.
		// The offset cannot live in memDisp -- that field is the
		// *displacement from the base register*, and mixing the two would
		// silently drop one of them -- so it is kept alongside the symbol and
		// added back when the reference is resolved.
		name := term
		if rest := strings.TrimPrefix(name, "rip"); rest != name && strings.HasPrefix(rest, "+") {
			name = rest[1:]
		}
		if base, off, ok := splitSymbolOffset(name); ok {
			o.memSym = base
			o.memSymOff = off
			o.isRip = true
			continue
		}
		if isSymName(name) {
			o.memSym = name
			o.isRip = true
			continue
		}
		return Operand{}, fmt.Errorf("bad memory term %q", term)
	}
	return o, nil
}

// splitSymbolOffset separates a trailing signed offset from a symbol name:
// `G_gm+4` -> ("G_gm", 4), `.LCPI0_3-8` -> (".LCPI0_3", -8).
//
// The sign is what makes this unambiguous. A name may legitimately end in
// digits -- `.L2`, `G_v2` -- but it can never end in "+4" or "-8", because
// those characters cannot appear in an identifier. Scanning back over the
// digits and requiring a sign in front of them therefore never splits a real
// symbol in half.
func splitSymbolOffset(s string) (name string, off int, ok bool) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) || i == 0 {
		return "", 0, false // no trailing digits, or nothing before them
	}
	sign := s[i-1]
	if sign != '+' && sign != '-' {
		return "", 0, false
	}
	name = s[:i-1]
	if !isSymName(name) {
		return "", 0, false
	}
	v, err := strconv.ParseInt(s[i-1:], 0, 64)
	if err != nil {
		return "", 0, false
	}
	return name, int(v), true
}

// isSymName reports whether a token looks like a plain identifier rather than
// a register, an immediate, or a malformed term. LLVM's private data labels
// (`.str.0`, `.LCPI0_3`) and C globals (`G_count`) both qualify.
func isSymName(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if !(c == '_' || c == '.' || c == '@' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '_' || c == '.' || c == '$' || c == '@' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
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
	case !hasBase && !hasIdx:
		// Absolute [disp32]: mod=00 with a SIB whose base=101 supplies the
		// disp32. (mod=10 here would wrongly decode as [rbp+disp32].)
		e.modrm = 0x00
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
			sib |= 0x05 // base = 101 (none) => disp32 follows (mod stays 00)
			e.dispSize = 4
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
	// `lock` is the legacy LOCK# prefix (F0). It makes the read-modify-write
	// instruction that follows atomic -- which is how C11 _Atomic operations
	// are built. It must precede REX and the opcode, so it is emitted here and
	// the rest of the line is assembled as an ordinary instruction.
	if strings.EqualFold(mnem, "lock") {
		if rest == "" {
			return fmt.Errorf("line %q: 'lock' must be followed by an instruction", ln)
		}
		a.emitByte(0xF0)
		return a.emitInstr(rest)
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
	// Emit any segment-override prefix (FS/GS) as a legacy prefix -- strictly
	// before REX and opcode. At most one operand can be a memory reference, and
	// the prefix applies to it. (The syscall-stub path in emitSyscallStubs does
	// not use memory operands with a segment, so it needs no handling here.)
	for _, op := range ops {
		if op.kind == K_MEM && op.memSeg != segNone {
			a.emitByte(byte(op.memSeg))
			break
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

// emitArithRex emits the REX prefix for a 16/32/64-bit arithmetic op whose
// width comes from its register operands. W is set only for 64-bit operands: a
// 32-bit form must NOT get REX.W, or the CPU silently promotes it to 64-bit
// (e.g. `cmp r10d, eax` becomes `cmp r10, rax`). Under the int carry model a
// materialized int has high 32 bits = 0, so a 64-bit signed compare of
// 0x00000000FFFFFFFF (int -1) against 1 sees 4294967295 >= 1 and loops
// forever; and `sub r10d, eax` as a 64-bit sub pollutes the high bits that the
// zero-extension invariant depends on. 0x66 (16-bit) precedes REX like always.
func (a *Assembler) emitArithRex(width int, regIdx, rmIdx int) {
	if width == 2 {
		a.emitByte(0x66)
	}
	v := byte(0x40)
	if width == 8 {
		v |= 0x08
	}
	if regIdx >= 8 {
		v |= 0x04
	}
	if rmIdx >= 8 {
		v |= 0x01
	}
	if v != 0x40 {
		a.emitByte(v)
	}
}

// arithWidth returns the operation width in bytes for a reg-reg arithmetic op:
// either operand being 32-bit (eax/r10d) makes the whole op 32-bit, either
// being 16-bit makes it 16-bit, otherwise it is 64-bit. Valid code pairs
// same-width operands; the rule mirrors encodeMov's.
func arithWidth(dst, src Operand) int {
	if dst.is32 || src.is32 {
		return 4
	}
	if dst.is16 || src.is16 {
		return 2
	}
	return 8
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
			if tail != nil {
				// The trailer (e.g. the imm8 of "bt [rip+sym], imm8")
				// follows the disp32, so the true RIP is off+4+trailer
				// bytes. Without ripAdj the rel32 was 1 byte short and the
				// fixup pointed the CPU at the immediate's own byte.
				a.fixups = append(a.fixups, Fixup{sect: a.cur, off: off, sym: rm.memSym, ripAdj: 1})
			} else {
				a.fixup(off, rm.memSym)
			}
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
	case "resb":
		return a.emitRes(1, restOf(ln))
	case "resw":
		return a.emitRes(2, restOf(ln))
	case "resd":
		return a.emitRes(4, restOf(ln))
	case "resq":
		return a.emitRes(8, restOf(ln))
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
	case "xadd":
		return a.encodeXadd(ops, ln)
	case "cmpxchg":
		return a.encodeCmpxchg(ops, ln)
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
		"xorpd", "ucomisd", "comisd", "cvtsi2sd", "cvtsi2ss", "cvttsd2si",
		"cvtss2sd", "cvtsd2ss", "minsd", "minss", "maxsd", "maxss",
		"andpd", "andps", "orpd", "orps", "pand", "pandn", "por",
		"andnpd", "andnps", "pxor", "xorps", "movmskpd", "movmskps",
		"cvttpd2dq", "cvtpd2dq", "cvtdq2pd",
		"addss", "subss", "mulss", "divss", "sqrtss", "rcpss", "rsqrtss",
		"cmpeqsd", "cmpltsd", "cmplesd", "cmpunordsd", "cmpneqsd",
		"cmpnltsd", "cmpnlesd", "cmpordsd",
		"cmpeqss", "cmpltss", "cmpless", "cmpunordss", "cmpneqss",
		"cmpnltss", "cmpnless", "cmpordss",
		"movq", "movd", "movaps", "movapd", "movdqa":
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

// emitRes reserves count*unit bytes of uninitialised space (the resb/resw/resd/
// resq directives). No file bytes are written; only the section's virtual offset
// advances, so a .bss section shrinks the on-disk image while keeping
// zero-filled memory at the right address.
func (a *Assembler) emitRes(unit int, rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}
	count, err := a.parseCount(rest)
	if err != nil {
		return err
	}
	if count < 0 {
		return fmt.Errorf("negative reserve count %q", rest)
	}
	a.reserve(unit * int(count))
	return nil
}

// parseCount evaluates a small integer expression for the res* directives:
// integers, named constants (a.consts), and '*' products such as `4*1024`.
func (a *Assembler) parseCount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if v, ok := a.consts[s]; ok {
		return v, nil
	}
	// product of factors separated by '*'
	if strings.Contains(s, "*") {
		var total int64 = 1
		for _, p := range strings.Split(s, "*") {
			f, err := a.parseCount(strings.TrimSpace(p))
			if err != nil {
				return 0, err
			}
			total *= f
		}
		return total, nil
	}
	return strconv.ParseInt(s, 0, 64)
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
		// Width-aware mov reg, imm: `mov eax, -2147483648` must emit the 32-bit
		// B8+rd imm32 form (zero-extending rax to the materialized-int pattern),
		// NOT REX.W + imm64, which would sign-extend into 0xFFFFFFFF80000000
		// and corrupt the materialized invariant. The same rule applies to a
		// 0x66-prefixed imm16. Only a full-width (64-bit) destination takes the
		// REX.W + imm64 form.
		if dst.is16 {
			a.emitByte(0x66)
			if dst.reg >= 8 {
				a.emitByte(0x41) // REX.B for r8w-r15w
			}
			a.emitByte(byte(0xB8 | (dst.reg & 7)))
			v := int16(src.imm)
			a.emitByte(byte(v))
			a.emitByte(byte(v >> 8))
			return nil
		}
		if dst.is32 {
			if dst.reg >= 8 {
				a.emitByte(0x41) // REX.B for r8d-r15d
			}
			a.emitByte(byte(0xB8 | (dst.reg & 7)))
			a.emitInt32(int32(src.imm))
			return nil
		}
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

	// mov [rip+sym], imm -- a RIP-relative immediate store. Used both for tiny
	// globals (the byte form) and for ordinary 4- and 8-byte ones, which is
	// what LLVM emits when it initialises a global's slot: `movl $301,
	// G_gm+28(%rip)`. The immediate trails the disp32, so the recorded RIP is
	// one byte further along than a plain disp32 field would be.
	if dst.kind == K_MEM && dst.isRip && src.kind == K_IMM {
		width := dst.memWidth
		if width == 0 {
			// No size keyword: match the immediate's own magnitude the way
			// the byte-only form always did, so an existing caller writing
			// `mov [rip+x], 5` keeps producing a C6 byte store.
			if src.imm >= -128 && src.imm <= 255 {
				width = 1
			} else {
				width = 8
			}
		}
		if width == 2 {
			a.emitByte(0x66)
		}
		if width == 8 {
			a.emitByte(0x48)
		}
		if width == 1 {
			a.emitByte(0xC6) // mov r/m8, imm8 (reg field /0)
		} else {
			a.emitByte(0xC7) // mov r/m, imm32/imm16
		}
		a.emitByte(modrmRip(0))
		off := a.curOff()
		a.emitInt32(0)
		trailer := 1
		if width != 1 {
			trailer = 4
		}
		a.fixups = append(a.fixups, Fixup{sect: a.cur, off: off, sym: dst.memSym, ripAdj: trailer})
		switch width {
		case 1:
			a.emitByte(byte(src.imm))
		case 2:
			a.emitInt16(int16(src.imm))
		default:
			a.emitInt32(int32(src.imm))
		}
		return nil
	}

	// mov [base+disp], imm  (64-bit sign-extended imm32, or 32-bit imm32
	// when prefixed `dword`) -- stack slots / struct member stores.
	if dst.kind == K_MEM && !dst.isRip && dst.memHasBase && src.kind == K_IMM {
		width := dst.memWidth
		if width == 0 {
			width = 8
		}
		return a.encodeMovMemImm(dst, src.imm, width, ln)
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
// encodeMovMemImm emits: mov [base+idx*scale+disp], imm (imm16 under a 0x66
// prefix for width==2, imm32 -- sign-extended to 64 bits by REX.W -- for
// width==8, plain imm32 for width==4). The full memory operand is passed so
// an index register and scale survive encoding -- the previous base+disp
// signature dropped the index, so "mov [rax+rbx*4], 10" was silently encoded
// as "mov [rax], 10".
func (a *Assembler) encodeMovMemImm(mem Operand, imm int64, width int, ln string) error {
	if width == 2 {
		a.emitByte(0x66) // operand-size prefix => 16-bit immediate
	}
	// planMem computes ModRM/SIB/displacement for the memory operand and the
	// REX.X/B bits for its index/base registers. The ModRM.reg field is /0
	// for both the C6 (imm8) and C7 (imm32/imm16) forms.
	enc, err := a.planMem(0, mem)
	if err != nil {
		return err
	}
	rex := byte(0x40)
	if width == 8 {
		rex = 0x48
	}
	if enc.rexX {
		rex |= 0x02
	}
	if enc.rexB {
		rex |= 0x01
	}
	if width == 8 || enc.rexX || enc.rexB {
		a.emitByte(rex)
	}
	if width == 1 {
		// mov r/m8, imm8 -- the byte-size form uses opcode C6, NOT C7.
		// C7 is "mov r/m, imm32/imm16" and would zero a whole 4-byte (or
		// 2-byte under 0x66) word. The store at address N writes bytes
		// N, N+1, N+2, N+3, so a C7 here clobbers the adjacent higher
		// stack slot (one byte up) and silently corrupts a neighbour
		// narrow local -- the #81 _Bool/char zero-init bug.
		a.emitByte(0xC6)
	} else {
		a.emitByte(0xC7) // mov r/m, imm32 (or imm16 under 0x66)
	}
	a.emitByte(enc.modrm)
	if enc.hasSIB {
		a.emitByte(enc.sib)
	}
	switch {
	case enc.dispSize == 0:
		// no displacement
	case enc.dispSize == 1:
		a.emitByte(byte(int8(enc.disp)))
	default:
		a.emitInt32(int32(enc.disp))
	}
	switch {
	case width == 1:
		a.emitByte(byte(imm)) // imm8 for the C6 byte-store form
	case width == 2:
		a.emitByte(byte(int16(imm)))
		a.emitByte(byte(int16(imm) >> 8))
	default:
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
	// A memory destination is legal only where the encoding actually has an
	// r/m field to put it in: the immediate forms (81 /digit) and the
	// register-to-memory forms below. The register-to-register form needs
	// dst.reg and rejects it there. LLVM emits `addq %r14, 64(%r15)` and
	// `cmpl $0, 12(%rcx)` constantly, so this cannot be a blanket rejection.
	if dst.kind != K_REG && dst.kind != K_MEM {
		return fmt.Errorf("%s dst must be register or memory: %q", mnem, ln)
	}

	if src.kind == K_REG && dst.kind == K_REG {
		// opcode is "op r/m64, r64": modrm reg field = src, rm field = dst.
		// REX.R extends the reg field (src), REX.B extends the rm field (dst).
		// Width comes from the operands: `cmp r10d, eax` must encode as a
		// 32-bit cmp (REX.W=0), not the 64-bit form rexW used to force.
		a.emitArithRex(arithWidth(dst, src), src.reg, dst.reg)
		a.emitByte(c.reg)
		a.emitByte(modrmRegReg(src.reg, dst.reg)) // reg field = src, rm = dst
		return nil
	}
	if src.kind == K_IMM && mnem == "test" {
		// test r/m, imm has its own opcodes: F6 /0 for imm8 and F7 /0 for
		// imm32 (imm16 under 0x66). The 83/81 group the other arithmetic
		// ops share decodes /0 as ADD, so the old code turned
		// "test eax, 1" into "add eax, 1" -- silently corrupting the
		// register and the flags it was supposed to probe.
		if src.imm >= -128 && src.imm <= 127 {
			a.emitArithRex(regWidth(dst), 0, dst.reg)
			a.emitByte(0xF6)
			a.emitByte(modrmRegReg(0, dst.reg))
			a.emitByte(byte(int8(src.imm)))
		} else {
			a.emitArithRex(regWidth(dst), 0, dst.reg)
			a.emitByte(0xF7)
			a.emitByte(modrmRegReg(0, dst.reg))
			a.emitInt32(int32(src.imm))
		}
		return nil
	}
	if src.kind == K_IMM {
		// Immediate into memory: op r/m, imm -- 83 /digit ib (sign-extended
		// imm8) or 81 /digit id. Distinct from the register form above only in
		// that the r/m field names a memory location instead of a register, so
		// the ModRM and the following immediate bytes have to be planned
		// through planMem. LLVM emits this constantly (`addl $1000, -12(%rbp)`,
		// `cmpl $0, 12(%rcx)`) to materialise a value or test a field, so
		// rejecting it would make most real compiler output unassemblable.
		if dst.kind == K_MEM {
			width := dst.memWidth
			if width == 0 {
				width = 8
			}
			// 8-bit immediate into memory uses opcode 80 with the sign-extended
			// imm8 (F6 /0 is the register-only "test" form and must not be
			// reused here). `cmpb $37, 1(%r8)` -- comparing one byte of a
			// buffer against a character constant -- is how every string
			// routine in goclib tests its input, so it has to work.
			if dst.isRip {
				if width == 1 {
					a.emitByte(0x80)
					a.emitByte(modrmRip(int(c.dig)))
					off := a.curOff()
					a.emitInt32(0)
					a.fixups = append(a.fixups, Fixup{sect: a.cur, off: off, sym: dst.memSym, ripAdj: 1})
					a.emitByte(byte(src.imm))
					return nil
				}
				if width == 2 {
					a.emitByte(0x66)
				}
				if width == 8 {
					a.emitByte(0x48)
				}
				if src.imm >= -128 && src.imm <= 127 && width != 2 {
					a.emitByte(0x83)
					a.emitByte(modrmRip(int(c.dig)))
					off := a.curOff()
					a.emitInt32(0)
					a.fixups = append(a.fixups, Fixup{sect: a.cur, off: off, sym: dst.memSym, ripAdj: 1})
					a.emitByte(byte(int8(src.imm)))
					return nil
				}
				a.emitByte(0x81)
				a.emitByte(modrmRip(int(c.dig)))
				off := a.curOff()
				a.emitInt32(0)
				a.fixups = append(a.fixups, Fixup{sect: a.cur, off: off, sym: dst.memSym, ripAdj: 4})
				a.emitInt32(int32(src.imm))
				return nil
			}
			enc, err := a.planMem(int(c.dig), dst)
			if err != nil {
				return err
			}
			// REX carries only the memory operand's extension bits here (the
			// reg field is a group number that never needs extending) plus W
			// for the 64-bit form.
			rex := byte(0x40)
			if width == 8 {
				rex = 0x48
			}
			if enc.rexX {
				rex |= 0x02
			}
			if enc.rexB {
				rex |= 0x01
			}
			if rex != 0x40 {
				a.emitByte(rex)
			}
			if width == 1 {
				// 80 /digit ib -- the only encoding of an 8-bit immediate into
				// a memory operand.
				a.emitByte(0x80)
				a.emitMemEnc(enc)
				a.emitByte(byte(src.imm))
				return nil
			}
			if width == 2 {
				a.emitByte(0x66) // 16-bit operands take an imm16, not an imm8
			}
			if src.imm >= -128 && src.imm <= 127 && width != 2 {
				a.emitByte(0x83)
				a.emitMemEnc(enc)
				a.emitByte(byte(int8(src.imm)))
				return nil
			}
			a.emitByte(0x81)
			a.emitMemEnc(enc)
			a.emitInt32(int32(src.imm))
			return nil
		}
		if src.imm >= -128 && src.imm <= 127 {
			a.emitArithRex(regWidth(dst), 0, dst.reg)
			a.emitByte(0x83)
			a.emitByte(modrmRegReg(int(c.dig), dst.reg))
			a.emitByte(byte(int8(src.imm)))
		} else {
			a.emitArithRex(regWidth(dst), 0, dst.reg)
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
	// One-operand form: the unsigned multiply into rdx:rax. AT&T spells it
	// `imulq %rdx` (one operand, rax implied); goa's own source always writes
	// the two-operand `mul`. Only the width differs between the signed and
	// unsigned variants, and neither operand form encodes it, so both share
	// F6/F7 /5.
	if len(ops) == 1 && (ops[0].kind == K_REG || ops[0].kind == K_MEM) {
		o := ops[0]
		width := 8
		if o.kind == K_REG {
			if regWidth(o) != 8 {
				// 32-bit multiply: same opcode, no REX.W.
				width = 4
			}
		} else if o.memWidth != 0 {
			width = o.memWidth
		}
		if width == 1 {
			return fmt.Errorf("imul: 8-bit multiply is not defined: %q", ln)
		}
		rex := byte(0x48)
		if width != 8 {
			rex = 0x40
		}
		if o.kind == K_MEM {
			enc, err := a.planMem(5, o)
			if err != nil {
				return err
			}
			if enc.rexX {
				rex |= 0x02
			}
			if enc.rexB {
				rex |= 0x01
			}
			if rex != 0x40 {
				a.emitByte(rex)
			}
			if width == 2 {
				a.emitByte(0x66)
			}
			a.emitByte(0xF7)
			a.emitMemEnc(enc)
			return nil
		}
		if rex != 0x40 {
			a.emitByte(rex)
		}
		if width == 2 {
			a.emitByte(0x66)
		}
		a.emitByte(0xF7)
		a.emitByte(modrmRegReg(5, o.reg))
		return nil
	}
	// Three-operand form: imul dst, src, imm. AT&T and Intel agree on the
	// operand order here (destination first either way), so this reaches the
	// encoder identically from both syntaxes -- which is what lets a whole
	// LLVM .s file drop straight in. Constant-folding compilers emit this
	// constantly (`imulq $1374389535, %r8, %r9`).
	if len(ops) == 3 && ops[0].kind == K_REG && ops[2].kind == K_IMM {
		dst, src, imm := ops[0], ops[1], ops[2].imm
		w := arithWidth(dst, src)
		// 69 /r id -- dst = src * imm32. Unlike the two-operand immediate
		// form there is a genuine source register, so the destination does
		// not have to occupy both the reg and rm fields.
		a.emitArithRex(w, dst.reg, src.reg)
		a.emitByte(0x69)
		a.emitByte(modrmRegReg(dst.reg, src.reg))
		a.emitInt32(int32(imm))
		return nil
	}
	if len(ops) != 2 || ops[0].kind != K_REG {
		return fmt.Errorf("imul needs a register destination: %q", ln)
	}
	dst := ops[0]
	if ops[1].kind == K_REG {
		// imul r32, r/m32 : 0F AF /r  (reg=dst, rm=src). Width-aware: imul
		// eax, r10d must not get REX.W (would multiply full 64-bit rax).
		a.emitArithRex(arithWidth(dst, ops[1]), dst.reg, ops[1].reg)
		a.emitByte(0x0F)
		a.emitByte(0xAF)
		a.emitByte(modrmRegReg(dst.reg, ops[1].reg))
		return nil
	}
	if ops[1].kind == K_IMM {
		// imul r32, imm : 6B /r ib (sign-extended imm8) or 69 /r id (imm32).
		// The destination register occupies BOTH the reg and rm fields of the
		// ModRM, so when dst.reg >= 8 we must set REX.R (reg field) AND REX.B
		// (rm field) — rexW(dst, dst) did that but always forced 64-bit; the
		// 32-bit form (imul r11d, 4) must stay REX.W=0.
		imm := ops[1].imm
		if imm >= -128 && imm <= 127 {
			a.emitArithRex(regWidth(dst), dst.reg, dst.reg)
			a.emitByte(0x6B)
			a.emitByte(modrmRegReg(dst.reg, dst.reg))
			a.emitByte(byte(int8(imm)))
		} else {
			a.emitArithRex(regWidth(dst), dst.reg, dst.reg)
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
	if len(ops) == 1 && ops[0].kind == K_REG {
		// One-operand form: shift by CL. AT&T writes it as a single operand
		// (`shrl %eax`) because the count register is implied, while the Intel
		// spelling is explicit (`shr eax, cl`). Both are the same D2/D3
		// encoding, and goc's own code generator emits the explicit form, so
		// the pair has to coexist.
		dst := ops[0]
		a.emitShiftRex(dst, int(dig))
		a.emitByte(0xD3) // /digit, count in CL
		a.emitByte(modrmRegReg(int(dig), dst.reg))
		return nil
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
			a.emitShiftRex(dst, int(dig))
			a.emitByte(0xD1)
			a.emitByte(modrmRegReg(int(dig), dst.reg))
		} else {
			a.emitShiftRex(dst, int(dig))
			a.emitByte(0xC1)
			a.emitByte(modrmRegReg(int(dig), dst.reg))
			a.emitByte(byte(c))
		}
	case ops[1].kind == K_REG && ops[1].isByte && ops[1].reg == 1: // cl
		a.emitShiftRex(dst, int(dig))
		a.emitByte(0xD3)
		a.emitByte(modrmRegReg(int(dig), dst.reg))
	default:
		return fmt.Errorf("%s: unsupported shift count: %q", mnem, ln)
	}
	return nil
}

// emitShiftRex emits the REX prefix for a shift whose destination is dst and
// whose ModRM reg field carries the shift group digit dig. Width-aware: a
// 32-bit destination must NOT get REX.W, or `sar eax, cl` silently operates on
// 64-bit rax (sign bit = bit 63). For a materialized int in eax the high 32
// bits are 0, so such a 64-bit arithmetic shift degenerates into a logical one
// (negative `a >> n` came out as a huge positive). The old 64-bit shifts
// (shl/sar rax, ...) keep REX.W=1 unchanged.
func (a *Assembler) emitShiftRex(dst Operand, dig int) {
	rex, must := rexByte(regWidth(dst), dig, dst.reg, false, false, dst.reg >= 8)
	if must {
		a.emitByte(rex)
	}
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
	// The packed double<->int32 conversions, all 0F E6 with the prefix
	// selecting the direction: 66 truncating pd->dq, F2 rounding pd->dq,
	// F3 dq->pd. goclib's printf reaches these whenever a double has to be
	// printed as an integer.
	"cvttpd2dq": {0x66, 0xE6, false, false},
	"cvtpd2dq":  {0xF2, 0xE6, false, false},
	"cvtdq2pd":  {0xF3, 0xE6, false, false},
	// The single-precision counterparts, same opcodes with F3 in place of
	// F2. goc keeps `float` as a distinct C type, so a float expression
	// reaches these even though the value is computed in double precision
	// internally.
	"addss":   {0xF3, 0x58, false, false},
	"subss":   {0xF3, 0x5C, false, false},
	"mulss":   {0xF3, 0x59, false, false},
	"divss":   {0xF3, 0x5E, false, false},
	"sqrtss":  {0xF3, 0x51, false, false},
	"rcpss":   {0xF3, 0x53, false, false},
	"rsqrtss": {0xF3, 0x52, false, false},
	// Single <-> double conversions. Both are XMM-dst with an XMM or m32/m64
	// source, which is exactly the generic two-operand shape above.
	"cvtss2sd": {0xF3, 0x5A, false, false},
	"cvtsd2ss": {0xF2, 0x5A, false, false},
	// Aligned 128-bit moves. LLVM emits these instead of the unaligned
	// movsd/movupd pair whenever it can prove the operand's alignment (an
	// alloca with an align, or a struct member of known type), which for
	// straight-line copies of doubles is the common case. Same encoding as
	// movsd but with a 66/F3 prefix selecting packed/scalar width.
	"movaps": {0x00, 0x28, false, false},
	"movapd": {0x66, 0x28, false, false},
	// Aligned 128-bit *integer* move, 66 0F 6F (load) / 66 0F 7F (store).
	// LLVM prefers it over a pair of movq when moving a whole 16-byte
	// aggregate such as a small struct or vector, which is how a struct
	// assignment lowers once the type is over-aligned.
	"movdqa": {0x66, 0x6F, false, false},
	// min/max. goclib implements fmin/fmax on top of these, and once the
	// inlining pass has run they are the only floating-point compare left in
	// an otherwise integer-only instruction stream. 5F is MINSD/MINSS (dest
	// keeps the second source on a tie or on NaN) and 5F+1 is the MAX pair;
	// both are 0F 5x with F2 selecting the double form.
	"minsd": {0xF2, 0x5D, false, false},
	"minss": {0xF3, 0x5D, false, false},
	"maxsd": {0xF2, 0x5F, false, false},
	"maxss": {0xF3, 0x5F, false, false},
	// The cmp<cc>sd family: ucomisd is 2E, comisd is 2F.
	"comisd": {0x66, 0x2F, false, false},
	// The packed bitwise trio, 66 0F 54/55/56. goclib's fabs is `andpd` with
	// a sign-bit mask and copysign is `andpd`/`xorpd` over it, so these are
	// how absolute value reaches the hardware.
	"andpd": {0x66, 0x54, false, false},
	"andps": {0x00, 0x54, false, false},
	"orpd":  {0x66, 0x56, false, false},
	"orps":  {0x00, 0x56, false, false},
	// andnpd is (NOT src1) AND src2 -- the primitive fmin/fmax are built from,
	// since the SSE min/max instructions have NaN semantics that do not match C.
	"andnpd": {0x66, 0x55, false, false},
	"andnps": {0x00, 0x55, false, false},
	"xorps":  {0x00, 0x57, false, false},
	"pxor":   {0x66, 0xEF, false, false},
	"pand":   {0x66, 0xDB, false, false},
	"pandn":  {0x66, 0xDF, false, false},
	"por":    {0x66, 0xEB, false, false},
}

// emitSSEPrefix emits an SSE mandatory prefix, or nothing when there is none.
// The two-operand 0F 28/29 forms (movaps, movapd) carry a 66 prefix only for
// the packed variant; the single-precision one has no prefix at all, and
// emitting a stray 0x00 there would shift the whole instruction by a byte.
func (a *Assembler) emitSSEPrefix(p byte) {
	if p != 0 {
		a.emitByte(p)
	}
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
	if mnem == "movq" || mnem == "movd" {
		return a.encodeMovQ(mnem, ops, ln)
	}
	if _, ok := sseCmpPred[mnem]; ok {
		return a.encodeSSECmp(mnem, ops, ln)
	}
	if mnem == "movmskpd" || mnem == "movmskps" {
		return a.encodeSSEMaskToGP(mnem, ops, ln)
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
		a.emitSSEPrefix(spec.prefix)
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

	a.emitSSEPrefix(spec.prefix)
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

// encodeMovQ implements the GP <-> XMM moves in both their 32- and 64-bit
// forms -- `movd` and `movq`, which differ only in whether REX.W is present:
//
//	movd xmm, r32   => 66 0F 6E       movq xmm, r64  => 66 REX.W 0F 6E
//	movd r32, xmm   => 66 0F 7E       movq r64, xmm  => 66 REX.W 0F 7E
//
// It also covers the memory halves, which LLVM spells with the same mnemonics
// but different prefixes:
//
//	movd xmm, m32   => 66 0F 6E /r
//	movq xmm, m64   => 66 REX.W 0F 6E /r
//	movd m32, xmm   => 66 0F 7E /r
//	movq m64, xmm   => F3 0F 7E /r      (note: no REX.W, no 66)
//
// The asymmetry in the last line is not a typo -- it is what the hardware
// specifies. The 64-bit load form has no dedicated opcode, so it borrows the
// single-precision store encoding with F3 in front of it. Getting the prefixes
// wrong produces a valid-looking instruction that moves the wrong bytes, which
// is why every case is spelled out rather than folded into the register path.
func (a *Assembler) encodeMovQ(mnem string, ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("movq needs 2 operands: %q", ln)
	}
	dst, src := ops[0], ops[1]
	// movd moves 32 bits and carries no REX.W; movq moves 64 and requires it.
	// Getting this backwards is silent corruption rather than a trap: the
	// 64-bit form of `movd xmm, eax` would zero-extend into rax and clobber
	// whatever was there.
	w := mnem != "movd"
	if dst.isXMM && src.kind == K_MEM {
		// 66 [REX.W] 0F 6E /r -- load into the low quadword.
		a.emitByte(0x66)
		if w {
			a.emitSSE(true, dst.reg, 0)
		} else {
			a.emitSSE(false, dst.reg, 0)
		}
		a.emitByte(0x0F)
		a.emitByte(0x6E)
		return a.emitModRMMem(dst.reg, src)
	}
	if src.isXMM && dst.kind == K_MEM {
		// movq stores with F3 and no REX.W; movd uses the 66-prefixed 7E.
		if w {
			a.emitByte(0xF3)
			a.emitSSE(false, src.reg, 0)
		} else {
			a.emitByte(0x66)
			a.emitSSE(false, src.reg, 0)
		}
		a.emitByte(0x0F)
		a.emitByte(0x7E)
		return a.emitModRMMem(src.reg, dst)
	}
	if dst.isXMM && src.kind == K_REG && !src.isXMM {
		a.emitByte(0x66)
		a.emitSSE(w, dst.reg, src.reg)
		a.emitByte(0x0F)
		a.emitByte(0x6E)
		a.emitByte(modrmRegReg(dst.reg, src.reg))
		return nil
	}
	if src.isXMM && dst.kind == K_REG && !dst.isXMM {
		a.emitByte(0x66)
		a.emitSSE(w, src.reg, dst.reg)
		a.emitByte(0x0F)
		a.emitByte(0x7E)
		a.emitByte(modrmRegReg(src.reg, dst.reg))
		return nil
	}
	// XMM <-> XMM with the movq mnemonic is a plain 128-bit move in LLVM's
	// output; fall back to the SSE spec so `movaps`-style handling applies.
	if dst.isXMM && src.isXMM {
		a.emitByte(0xF3)
		a.emitSSE(false, dst.reg, src.reg)
		a.emitByte(0x0F)
		a.emitByte(0x7E)
		a.emitByte(modrmRegReg(dst.reg, src.reg))
		return nil
	}
	return fmt.Errorf("movq: unsupported operands: %q", ln)
}

// emitModRMMem emits the ModRM byte and any displacement for an SSE
// instruction whose rm field is a memory operand, recording a fixup when the
// reference is RIP-relative.
func (a *Assembler) emitModRMMem(reg int, mem Operand) error {
	if mem.isRip {
		a.emitByte(modrmRip(reg))
		off := a.curOff()
		a.emitInt32(0)
		a.fixup(off, mem.memSym)
		return nil
	}
	e, err := a.planMem(reg, mem)
	if err != nil {
		return err
	}
	a.emitMemEnc(e)
	return nil
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

// sseCmpPred maps the packed-SSE comparison family onto the predicate
// immediate that CMPSD/CMPSS (0F C2 /r ib) carries. The eight predicates are
// fixed by the ISA and shared by every condition:
//
//	0 EQ   1 LT   2 LE   3 UNORD   4 NEQ   5 NLT   6 NLE   7 ORD
//
// goclib lowers every floating-point `<`, `<=`, `>` and `>=` onto these rather
// than onto ucomisd plus a branch, because the comparison result is a mask the
// following andpd/xorpd consumes directly.
var sseCmpPred = map[string]byte{
	"cmpeqsd": 0, "cmpltsd": 1, "cmplesd": 2, "cmpunordsd": 3,
	"cmpneqsd": 4, "cmpnltsd": 5, "cmpnlesd": 6, "cmpordsd": 7,

	"cmpeqss": 0, "cmpltss": 1, "cmpless": 2, "cmpunordss": 3,
	"cmpneqss": 4, "cmpnltss": 5, "cmpnless": 6, "cmpordss": 7,
}

// encodeSSECmp emits the cmp<cond><sd|ss> family: F2/F3 0F C2 /r ib, where the
// ModRM holds the two sources and the trailing byte selects the predicate.
func (a *Assembler) encodeSSECmp(mnem string, ops []Operand, ln string) error {
	pred, ok := sseCmpPred[mnem]
	if !ok {
		return fmt.Errorf("unknown SSE compare: %q", mnem)
	}
	if len(ops) != 2 {
		return fmt.Errorf("%s needs 2 operands: %q", mnem, ln)
	}
	// F3 selects the single-precision form; everything else here is double.
	prefix := byte(0xF2)
	if strings.HasSuffix(mnem, "ss") {
		prefix = 0xF3
	}
	var xmmOp, other Operand
	if ops[0].isXMM {
		xmmOp, other = ops[0], ops[1]
	} else {
		xmmOp, other = ops[1], ops[0]
	}
	a.emitByte(prefix)
	if other.kind == K_MEM {
		if other.isRip {
			a.emitSSE(false, xmmOp.reg, 0)
			a.emitByte(0x0F)
			a.emitByte(0xC2)
			a.emitByte(modrmRip(xmmOp.reg))
			off := a.curOff()
			a.emitInt32(0)
			a.fixup(off, other.memSym)
			a.emitByte(pred)
			return nil
		}
		e, err := a.planMem(xmmOp.reg, other)
		if err != nil {
			return err
		}
		a.emitSSEmem(false, e)
		a.emitByte(0x0F)
		a.emitByte(0xC2)
		a.emitMemEnc(e)
		a.emitByte(pred)
		return nil
	}
	a.emitSSE(false, xmmOp.reg, other.reg)
	a.emitByte(0x0F)
	a.emitByte(0xC2)
	a.emitByte(modrmRegReg(xmmOp.reg, other.reg))
	a.emitByte(pred)
	return nil
}

// encodeSSEMaskToGP implements movmskpd/movmskps: 66 0F 50 /r and 0F 50 /r.
// It copies the *sign bits* of the packed lanes into a GP register, one bit
// per lane, and discards the rest -- the standard way to turn a SIMD
// comparison result back into a branch condition without going through memory.
// Unlike every other SSE form here the GP register is the reg field and the
// XMM source is rm, so the operand roles are reversed.
func (a *Assembler) encodeSSEMaskToGP(mnem string, ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("%s needs 2 operands: %q", mnem, ln)
	}
	gp, src := ops[0], ops[1]
	if gp.isXMM || (src.kind != K_REG && src.kind != K_MEM) {
		return fmt.Errorf("%s needs a GP destination: %q", mnem, ln)
	}
	if strings.HasSuffix(mnem, "pd") {
		a.emitByte(0x66)
	}
	if src.kind == K_MEM {
		if src.isRip {
			a.emitSSE(false, gp.reg, 0)
			a.emitByte(0x0F)
			a.emitByte(0x50)
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
		a.emitSSEmem(false, e)
		a.emitByte(0x0F)
		a.emitByte(0x50)
		a.emitMemEnc(e)
		return nil
	}
	a.emitSSE(false, gp.reg, src.reg)
	a.emitByte(0x0F)
	a.emitByte(0x50)
	a.emitByte(modrmRegReg(gp.reg, src.reg))
	return nil
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

// ---- xadd / cmpxchg ------------------------------------------------------

// encodeXadd emits `xadd dst, src` (0F C0 /r for 8-bit, 0F C1 /r otherwise):
// it swaps dst and src and writes their sum to dst. With a LOCK prefix it is
// the canonical "fetch and add", which is how atomic_fetch_add is lowered:
// the register comes away with the old value and memory with the new one.
func (a *Assembler) encodeXadd(ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("xadd needs 2 operands: %q", ln)
	}
	dst, src := ops[0], ops[1]
	if src.kind != K_REG {
		return fmt.Errorf("xadd src must be a register: %q", ln)
	}
	if dst.kind != K_REG && dst.kind != K_MEM {
		return fmt.Errorf("xadd dst must be a register or memory: %q", ln)
	}
	width := regWidth(src)
	op := byte(0xC1)
	if width == 1 {
		op = 0xC0
	}
	return a.emitOpRM(width, nil, true, op, src.reg, dst, nil)
}

// encodeCmpxchg emits `cmpxchg dst, src` (0F B0 /r for 8-bit, 0F B1 /r
// otherwise): it compares AL/AX/EAX/RAX with dst and, when they are equal,
// stores src into dst and sets ZF; otherwise it loads dst into the accumulator
// and clears ZF. Under LOCK this is the compare-and-swap every atomic
// exchange and CAS loop is built from.
func (a *Assembler) encodeCmpxchg(ops []Operand, ln string) error {
	if len(ops) != 2 {
		return fmt.Errorf("cmpxchg needs 2 operands: %q", ln)
	}
	dst, src := ops[0], ops[1]
	if src.kind != K_REG {
		return fmt.Errorf("cmpxchg src must be a register: %q", ln)
	}
	if dst.kind != K_REG && dst.kind != K_MEM {
		return fmt.Errorf("cmpxchg dst must be a register or memory: %q", ln)
	}
	width := regWidth(src)
	op := byte(0xB1)
	if width == 1 {
		op = 0xB0
	}
	return a.emitOpRM(width, nil, true, op, src.reg, dst, nil)
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
