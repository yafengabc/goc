package main

import (
	"embed"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CG emits x86-64 assembly in the Intel syntax that goa (our own assembler)
// understands. No gcc anywhere in the pipeline: goc -> .asm -> goa -> .exe.
//
// The generated code follows the Windows x64 ABI: integer args in RCX/RDX/R8/R9,
// a 32-byte shadow space reserved by the caller, and RSP kept 16-byte aligned
// at every call site.
// varInfo records a variable's home: either a stack slot (off != 0) or a
// callee-save register (reg != ""). Doubles and address-taken / array locals
// always live on the stack; small int locals may be cached in a register.
type varInfo struct {
	off int
	typ *Type
	reg string
}

type CG struct {
	insts      []Inst // the function-body instruction stream (see Inst)
	opt        int    // optimisation level from -O; 0 keeps the legacy output
	strs       []StrLit
	strLab     map[*StrLit]string
	doubles    []float64
	doubleLab  map[float64]string
	label      int
	vars       map[string]varInfo // per-function: param = +off, local = -off
	localBytes int                // bytes consumed by stack-resident locals (incl. array padding)
	regArea    int                // bytes reserved just below rbp for saved callee-save regs
	globals    map[string]bool    // names of program-level (global/static) variables
	globalLab  map[string]string  // name -> .data label for a global variable
	globalTyp  map[string]*Type   // name -> declared type of a global variable
	// globalStrInits records every pointer slot initialised by a string literal
	// -- top-level "char *p = "str"", a static local, or a char* member nested
	// anywhere inside a braced initialiser. The pointer value cannot live in
	// .data as a relocation (goa has none), so the entry stub writes each one
	// at startup with `lea rax,[rip+<global>]; lea rdx,[rip+<str>];
	// mov [rax+<off>],rdx` -- glab is the .data label of the object, off the
	// byte offset of the pointer slot inside it, slab the .rdata label of the
	// string constant.
	globalStrInits []struct {
		glab, slab string
		off        int
	}
	// Static locals: a "static int x;" inside a function gets a unique .data
	// label (collision-free even when two functions name their static "x") and
	// persists across calls. c.staticVars maps the source name to that label
	// for the function currently being generated; c.staticList accumulates every
	// static local across all functions so emitAssembly can lay them out in
	// .data. staticSeq gives each a unique label.
	staticVars map[string]string
	staticList []staticEmit
	staticSeq  int
	usedRegs   []string        // callee-save registers actually used as local homes
	tmpDepth   int             // live expression-temporary slots
	funcs      map[string]bool // user-defined functions (by name)
	funcDefs   map[string]*FuncDecl
	calls      map[string]bool // functions called that are not defined here
	need       map[string]bool // goclib functions this program actually uses
	// Built-in C library (clibCStore): needed C functions are emitted through
	// genFunc (which marks more needs, so Gen iterates to a fixpoint), and the
	// library's file-scope variables join the .data pool -- but only those the
	// program actually references (libGlobUsed).
	libEmitted   map[string]bool
	libGlobNames map[string]bool
	libGlobUsed  map[string]bool
	libGlobals   []*DeclStmt
	linux        bool              // true -> SysV ABI + ELF output
	curRet       *Type             // return type of the function being generated
	curParam     []*Type           // parameter types of the current function
	resTyp       CType             // type of the value left by the last genExprT
	resSigned    bool              // signedness of the last genExprT result (int-class only)
	resW         int               // semantic width of the last genExprT result: 1/2/4 (int-class), 8 (long/pointer/double)
	lvBitWidth   int               // bit width of the bit-field lvalue addressed by the last genLValue (0 = not a bit-field)
	lvBitOff     int               // bit offset of that bit-field within its storage unit
	lvBitUnit    int               // storage-unit size in bytes (1/2/4/8) of that bit-field's base type
	lvBitSigned  bool              // signedness of that bit-field's base type (for sign extension)
	tmpSgn       []bool            // signedness of each expression-temporary slot
	loops        []loopLabels      // active loop targets for break/continue
	breaks       []string          // active break targets: innermost loop or switch, last
	swDepth      int               // how many switch statements enclose the code being emitted
	swSlots      int               // switch value slots reserved in this frame (max nesting)
	curFn        string            // name of the function being generated (label mangling)
	labels       map[string]string // C label name -> assembly label, per function
	saveBaseOff  int               // rbp offset of the variadic save area (0 if none)
	nFixed       int               // number of named params before "..." in the current fn
	sretSlot     int               // rbp offset of this function's hidden sret-pointer slot (0 = returns a scalar)
	resStruct    bool              // the last call returned a struct; its value is in a tmp result buffer
	resStructSz  int               // size in bytes of that struct
	resStructK   int               // tmpSlot index of the first result-buffer slot
	resStructSl  int               // number of tmp slots occupied by the result buffer
}

// loopLabels records the break/continue targets of the innermost loop.
type loopLabels struct {
	breakLbl string
	contLbl  string
}

// staticEmit pairs a static-local declaration with the unique .data label it is
// laid out under. Accumulated in c.staticList and emitted by emitAssembly.
type staticEmit struct {
	lab string
	d   *DeclStmt
}

// argRegs returns the integer argument registers for the target ABI.
func (c *CG) argRegs() []string {
	if c.linux {
		return []string{"rdi", "rsi", "rdx", "rcx", "r8", "r9"} // SysV AMD64
	}
	return []string{"rcx", "rdx", "r8", "r9"} // Windows x64
}

// argXMM returns the XMM argument registers for the target ABI.
func (c *CG) argXMM() []string {
	if c.linux {
		return []string{"xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5", "xmm6", "xmm7"}
	}
	return []string{"xmm0", "xmm1", "xmm2", "xmm3"}
}

// stackArgOff returns the rsp offset of the k-th stack argument (0-based),
// after the caller has already reserved the space with `sub rsp, extra`.
func (c *CG) stackArgOff(k int) int {
	if c.linux {
		return 8 * k // SysV has no shadow space
	}
	return 32 + 8*k // Windows: above the 32-byte shadow space
}

// shadowSpace is the caller-owned scratch area the ABI requires.
func (c *CG) shadowSpace() int {
	if c.linux {
		return 0
	}
	return 32
}

// maxArgs is the number of arguments goc can pass to any call. The first
// len(argRegs) go in the integer argument registers (4 on Windows x64,
// 6 on SysV AMD64); the rest are spilled onto the stack. 16 covers the
// longest Win32 API (CreateWindowExA, 12 args) plus the shadow-space
// margin Windows needs, and the variadic save area / stack-copy loops in
// the prologue follow this constant automatically.
const maxArgs = 16

// scratchSlots is the number of 8-byte stack slots reserved for spilling
// the left operand of nested binary expressions. 32 is plenty for toy code.
const scratchSlots = 32

// externDLL resolves an imported symbol to the DLL that exports it. The
// ownership table lives in goclib/win32.def (embedded, parsed by
// loadWin32Def), so adding a Windows API is a one-line data change -- no Go
// source edit, no recompile. Anything not listed there is a hard error rather
// than a silent guess.
//
// The C library itself (printf, strlen, malloc, ...) is implemented in
// portable C (goclib/goclib.c) on top of kernel32 (Windows) or syscalls
// (Linux), and goc compiles it into every program at start-up. There is no
// msvcrt anywhere in the pipeline.
var externDLL = loadWin32Def()

// loadWin32Def parses the embedded goclib/win32.def table. A "# <dll>" line
// starts a group; every following bare line is an exported function name of
// that DLL. ";" comments and blanks are ignored. This is the single source of
// truth for the "extern Name, dll" imports Gen emits for the PE target.
func loadWin32Def() map[string]string {
	b, err := goclibDefFS.ReadFile("goclib/win32.def")
	if err != nil {
		panic("goc: goclib/win32.def missing from embedded FS: " + err.Error())
	}
	m := map[string]string{}
	dll := ""
	for _, ln := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, ";") {
			continue
		}
		if strings.HasPrefix(t, "#") {
			dll = strings.TrimSpace(strings.TrimPrefix(t, "#"))
			continue
		}
		if dll == "" {
			panic("goc: win32.def: function " + t + " appears before any '# dll' group")
		}
		if _, dup := m[t]; dup {
			panic("goc: win32.def: duplicate function " + t)
		}
		m[t] = dll
	}
	return m
}

// externLinux lists the syscall names an ELF-target program may reach for.
// goa turns each into a `mov rax,N; syscall; ret` stub, so the C code never
// links against a library.
var externLinux = map[string]bool{
	"read": true, "write": true, "open": true, "close": true,
	"lseek": true, "mmap": true, "munmap": true, "brk": true, "ioctl": true,
	"writev": true, "nanosleep": true, "getpid": true, "kill": true,
	"exit": true, "exit_group": true, "gettimeofday": true, "clock_gettime": true,
}

// ---------------------------------------------------------------------------
// goclib: the C library
// ---------------------------------------------------------------------------
//
// The library is plain C (goclib/goclib.c plus the headers it includes) that
// goc compiles at start-up exactly like a user program, once per target.
// Functions land in the output only when a program actually calls them (plus
// their transitive callees), so a hello-world does not pay for malloc.

//go:embed goclib/*.def
var goclibDefFS embed.FS

//go:embed goclib/*.c
var goclibCFS embed.FS

func init() {
	// Compile the built-in C library for both targets. A failure is reported
	// through goclibErr when Gen runs.
	clibCWin, clibCErr = buildClibC(false)
	if clibCErr == nil {
		clibCLinux, clibCErr = buildClibC(true)
	}
	goclibErr = clibCErr
}

// ---------------------------------------------------------------------------
// goclib in C: the embedded library is compiled by goc itself
// ---------------------------------------------------------------------------
//
// goclib.c (plus the headers it includes) is a plain C translation unit that
// goc compiles at start-up exactly like a user program. Every function the
// library defines is emitted through the regular code generator (genFunc) when
// a program needs it, so the C source is the single implementation of the
// built-in library.
//
// Function implementations may live either in a declaration header or in a .c
// file: the umbrella goclib.h is processed as its own translation unit first
// (collecting any definitions placed in the headers it includes), then every
// goclib/*.c. A later definition of the same name wins, so a .c definition
// overrides a header one and duplicates never reach the linker.

// clibCProgram is the compiled built-in library for one target.
type clibCProgram struct {
	funcs   map[string]*FuncDecl // defined functions, by name
	order   []string             // definition order, for stable output
	protos  []*FuncDecl          // prototypes declared by the library headers
	globals []*DeclStmt          // file-scope variables (e.g. rand_state)
}

var (
	clibCWin   *clibCProgram
	clibCLinux *clibCProgram
	clibCErr   error
	goclibErr  error
)

// clibCStore picks the compiled C library for a target.
func clibCStore(linux bool) *clibCProgram {
	if linux {
		return clibCLinux
	}
	return clibCWin
}

// buildClibC compiles the embedded goclib sources into a Program for one
// target. The umbrella header goes first (its includes pull in the standard
// headers, so definitions placed there are collected too), then the .c files
// in name order. The library has no main(), so that one checker diagnostic is
// expected and filtered; anything else is a hard error -- the library must
// compile for every program.
func buildClibC(linux bool) (*clibCProgram, error) {
	lib := &clibCProgram{funcs: map[string]*FuncDecl{}}
	compile := func(name, src string) error {
		toks, err := PreprocessTarget(src, "goclib/"+name, linux)
		if err != nil {
			return fmt.Errorf("goclib/%s: %v", name, err)
		}
		prog, err := Parse(toks)
		if err != nil {
			return fmt.Errorf("goclib/%s: %v", name, err)
		}
		for _, e := range Check(prog) {
			if strings.Contains(e.Error(), "program has no main()") {
				continue // the library is not a program
			}
			return fmt.Errorf("goclib/%s: %v", name, e)
		}
		lib.protos = append(lib.protos, prog.Prototypes...)
		for _, g := range prog.Globals {
			lib.globals = append(lib.globals, g)
		}
		// Definitions: later files win over earlier ones (a .c definition
		// overrides a header definition of the same name).
		for _, f := range prog.Funcs {
			if _, dup := lib.funcs[f.Name]; !dup {
				lib.order = append(lib.order, f.Name)
			}
			lib.funcs[f.Name] = f
		}
		return nil
	}
	if b, err := goclibHeaders.ReadFile("goclib/goclib.h"); err != nil {
		return nil, err
	} else if err := compile("goclib.h", string(b)); err != nil {
		return nil, err
	}
	entries, err := goclibCFS.ReadDir("goclib")
	if err != nil {
		return nil, err
	}
	var cfiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".c") {
			cfiles = append(cfiles, e.Name())
		}
	}
	sort.Strings(cfiles)
	for _, f := range cfiles {
		b, err := goclibCFS.ReadFile("goclib/" + f)
		if err != nil {
			return nil, err
		}
		if err := compile(f, string(b)); err != nil {
			return nil, err
		}
	}
	return lib, nil
}

// goclibNames lists the public (non-internal) built-in library functions, for
// error messages.
func goclibNames(linux bool) []string {
	var out []string
	if lib := clibCStore(linux); lib != nil {
		for _, n := range lib.order {
			if strings.HasPrefix(n, "__") {
				continue
			}
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (c *CG) newLabel(prefix string) string {
	c.label++
	return fmt.Sprintf(".L%s%d", prefix, c.label)
}

// tmpSlot returns the rbp offset of the k-th (1-indexed) expression
// temporary. These live in the function frame, above the locals, and are
// used to spill the left operand of a binary expression without touching
// RSP (so 16-byte stack alignment at calls is preserved).
func (c *CG) tmpSlot(k int) int {
	return -(c.regArea + c.localBytes + 8*k)
}

// swSlot returns the rbp offset of the k-th (1-indexed) switch value slot.
// A switch evaluates its controlling expression once and keeps it here while
// the case comparisons and the bodies run, so a nested expression temporary
// (tmpSlot) can never clobber it. These slots sit below the expression
// temporaries and above the variadic save area.
func (c *CG) swSlot(k int) int {
	return -(c.regArea + c.localBytes + 8*scratchSlots + 8*k)
}

// slotWidth returns the byte width used to load/store a variable/value of type t
// as a scalar in rax: char=1, short=2, int=4, long/pointer=8. Aggregates and
// doubles are handled by their own paths.
func (c *CG) slotWidth(t *Type) int {
	if t == nil {
		return 8
	}
	switch t.Kind {
	case KInt:
		// A goc "int" is an 8-byte value slot: in goc's C subset, pointers are
		// stored in int-typed variables, so the slot must be wide enough to
		// hold a 64-bit address. char (1) and short (2) stay genuinely narrow
		// because they never hold pointers. 32-bit signed/unsigned *arithmetic*
		// semantics are still enforced at operation sites (see genBinary), not
		// here.
		if t.Width == 4 {
			return 8
		}
		if t.Width < 1 {
			return 1
		}
		return t.Width
	case KBool:
		return 1 // bool stored as 1 byte
	case KFloat:
		// A float scalar really is 4 bytes in memory: it is stored as an IEEE
		// single and widened to double on every read. (A double stays 8.)
		return 4
	case KPtr, KFunc, KDouble:
		return 8
	case KStruct, KUnion:
		if t.Size != 0 {
			return t.Size
		}
	}
	return 8
}

// extendInt sign/zero-extends a value already in rax (loaded as `width` bytes)
// to a full 64-bit value. Signedness controls which direction the extension
// takes. width 8 (or any non-narrow value) is a no-op.
func (c *CG) extendInt(width int, signed bool) {
	switch width {
	case 1: // char or bool
		if signed {
			c.emit("shl rax, 56")
			c.emit("sar rax, 56")
		} else {
			c.emit("and rax, 0xff")
		}
	case 2: // short
		if signed {
			c.emit("shl rax, 48")
			c.emit("sar rax, 48")
		} else {
			c.emit("and rax, 0xffff")
		}
	case 4: // int
		if signed {
			c.emit("shl rax, 32")
			c.emit("sar rax, 32")
		}
		// case 8 for long/pointer is no-op, fall through
	}
}

// canonInt canonicalises the 64-bit value in rax as a 32-bit integer: signed
// ints are sign-extended (shl/sar 32), unsigned ints are zero-extended
// (shl/shr 32). It must be applied ONLY at operation sites whose semantic
// result width is 4 bytes (see promotedArith) -- never at variable loads and
// never to 8-byte results (long/unsigned long/pointer arithmetic would be
// wrongly truncated). Callers gate it on resW == 4.
func (c *CG) canonInt(signed bool) {
	if signed {
		c.emit("shl rax, 32")
		c.emit("sar rax, 32")
	} else {
		c.emit("shl rax, 32")
		c.emit("shr rax, 32")
	}
}

// promotedArith implements the C usual arithmetic conversions for the
// int-class operands goc supports (char/short/int/long, signed/unsigned). It
// returns the semantic width and signedness of the promoted common type that
// an arithmetic/bitwise/shift/comparison operation operates on:
//
//   - char/short operands (width 1/2) are first promoted to int (width 4,
//     signed), because int can represent every value they can hold -- this is
//     what makes "(unsigned char)a - (unsigned char)b" a signed-int operation.
//   - of two differently-ranked operands the narrower is converted to the
//     wider type, so the result takes the wider operand's signedness (e.g.
//     unsigned long * int is unsigned long; int + long is signed long).
//   - of two same-ranked operands, unsigned wins over signed (int + unsigned
//     int is unsigned int).
func promotedArith(leftW int, leftS bool, rightW int, rightS bool) (int, bool) {
	lw, rs := leftW, leftS
	rw, ls := rightW, rightS
	if lw < 4 {
		lw, rs = 4, true
	}
	if rw < 4 {
		rw, ls = 4, true
	}
	if lw == rw {
		// Same rank: unsigned beats signed (both must be signed for a signed result).
		return lw, rs && ls
	}
	if lw > rw {
		return lw, rs
	}
	return rw, ls
}

// semWOf returns the semantic width a value of type t carries in expressions:
// char=1, short=2, int=4, long/pointer/double=8. Unlike typeWidth it is
// already correct for pointer-typed values.
func (c *CG) semWOf(t *Type) int {
	if t == nil {
		return 8
	}
	if t.Kind == KInt {
		if t.Width < 1 {
			return 1
		}
		return t.Width
	}
	if t.Kind == KBool {
		return 1
	}
	return 8
}

// instKind tells optimisation passes what a body line is, and therefore what
// they may legally do with it.
type instKind uint8

const (
	instInstr instKind = iota // "\tmov ..." etc. -- a real instruction
	instLabel                 // "L3:" -- a branch target; never move code across one
	instRaw                   // verbatim passthrough (inline __asm lines)
)

// Inst is one line of the generated function-body assembly. Text is the exact
// line as it will print, without the trailing newline: the legacy pipeline
// wrote these very strings straight into a strings.Builder, so rendering the
// stream with printASM reproduces the old output byte for byte. Structured
// operands arrive with the passes that need them; the stream's value today is
// that label boundaries and inline-asm regions are explicit.
type Inst struct {
	Kind instKind
	Text string
}

// printASM renders the instruction stream as goa-ready assembly text. Every
// line is printed exactly as stored, newline-terminated: with no optimisation
// pass in between, the result is byte-for-byte what the strings.Builder
// pipeline produced. Passes that rewrite the stream must keep lines in order
// and never move an instruction across an instLabel.
func printASM(insts []Inst) string {
	var b strings.Builder
	for _, in := range insts {
		b.WriteString(in.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// emit appends one indented instruction line to the body stream.
func (c *CG) emit(format string, a ...any) {
	c.insts = append(c.insts, Inst{Kind: instInstr, Text: "\t" + fmt.Sprintf(format, a...)})
}

// line appends a verbatim body line that is not an instruction: a label, an
// inline-__asm passthrough line, and similar. A trailing colon marks the line
// a label, so a pass can see a branch-target boundary without parsing asm.
func (c *CG) line(s string) {
	s = strings.TrimSuffix(s, "\n")
	k := instRaw
	if strings.HasSuffix(s, ":") {
		k = instLabel
	}
	c.insts = append(c.insts, Inst{Kind: k, Text: s})
}

// loadGlobal loads the value of a program-level variable (true global or static
// local) whose .data label is lab and whose declared type is gt, leaving the
// result in rax (int-class) or xmm0 (float). Arrays decay to a pointer to
// element 0, mirroring local arrays; whole aggregates cannot be loaded and
// yield an error (consumers must go through genLValue instead).
func (c *CG) loadGlobal(lab string, gt *Type) (CType, error) {
	if gt != nil && gt.IsArray() {
		c.emit("lea rax, [rip+%s]", lab)
		c.resTyp = TInt
		c.resSigned = false
		c.resW = 8
		return TInt, nil
	}
	if isAgg(gt) {
		// A whole struct/union value cannot be loaded into rax; consumers
		// must go through genLValue (see structSrcAddr).
		return TInt, fmt.Errorf("cannot load struct/union value at %q directly", lab)
	}
	if gt != nil && gt.IsFloating() {
		// A float global is stored as a 4-byte single and widened on the way
		// in, exactly like a float local.
		if gt.Kind == KFloat {
			c.emit("movss xmm0, [rip+%s]", lab)
			c.emit("cvtss2sd xmm0, xmm0")
		} else {
			c.emit("movsd xmm0, [rip+%s]", lab)
		}
		c.resTyp = TDouble
		c.resSigned = false
		c.resW = 8
		return TDouble, nil
	}
	c.emit("mov rax, [rip+%s]", lab)
	c.resTyp = TInt
	c.resSigned = gt != nil && gt.Kind == KInt && gt.Signed
	c.resW = c.semWOf(gt)
	return TInt, nil
}

// loadVar emits code that loads variable vi's value into rax (int) or xmm0
// (double), and records the resulting type in c.resTyp.
// movsd (not movq) is used for the XMM <-> memory moves: goa only knows the
// GP <-> XMM forms of movq.
func (c *CG) loadVar(vi varInfo) {
	if vi.reg != "" {
		// Register-cached int local: value is already in the callee-save.
		c.emit("mov rax, %s", vi.reg)
		// Only narrow types (char/short) are extended here: their cached
		// value carries the raw low 8/16 bits, and extending recovers the
		// canonical (promoted) int. A plain int (width 4) must load the full
		// 64 bits unchanged -- int variables legitimately hold 64-bit
		// pointers ("int s = \"hello\"; strchr(s, 'e')") that a 32-bit
		// sign/zero-extension would truncate. Int canonicalisation happens at
		// operation sites (genBinary/genUnary/genIncDec) and in CastExpr.
		if vi.typ != nil && vi.typ.Kind == KInt && vi.typ.Width < 4 {
			c.extendInt(vi.typ.Width, vi.typ.Signed)
		}
		c.resTyp = TInt
		c.resSigned = vi.typ != nil && vi.typ.Kind == KInt && vi.typ.Signed
		c.resW = c.semWOf(vi.typ)
		return
	}
	if vi.typ != nil && vi.typ.IsFloating() {
		// A float scalar is stored as a 4-byte IEEE single; widen it to the
		// double every expression carries. Doubles load unchanged.
		if vi.typ.Kind == KFloat {
			c.emit("movss xmm0, [rbp%+d]", vi.off)
			c.emit("cvtss2sd xmm0, xmm0")
		} else {
			c.emit("movsd xmm0, [rbp%+d]", vi.off)
		}
		c.resTyp = TDouble
		c.resSigned = false
		c.resW = 8
		return
	}
	w := c.slotWidth(vi.typ)
	signed := vi.typ != nil && vi.typ.Kind == KInt && vi.typ.Signed
	switch w {
	case 1:
		c.emit("xor rax, rax")
		c.emit("mov al, [rbp%+d]", vi.off)
		c.extendInt(1, signed)
	case 2:
		c.emit("mov eax, [rbp%+d]", vi.off) // low 16 bits hold the value
		c.extendInt(2, signed)
	default:
		// int (8-byte slot) and pointers load full 64 bits. We deliberately do
		// NOT sign-extend here: that would truncate 64-bit pointers that live
		// in int variables. 32-bit signed/unsigned arithmetic canonicalisation
		// is applied at operation sites instead (genBinary).
		c.emit("mov rax, [rbp%+d]", vi.off)
	}
	c.resTyp = TInt
	c.resSigned = signed
	c.resW = c.semWOf(vi.typ)
}

// storeVar emits code that stores the value currently in rax (int) or xmm0
// (double) into variable vi's slot or home register.
func (c *CG) storeVar(vi varInfo) {
	if vi.reg != "" {
		// Register-cached int local: keep it in the callee-save (which
		// survives function calls, so no spill is needed).
		if vi.typ != nil && vi.typ.Kind == KBool {
			c.normalizeBool()
		}
		c.emit("mov %s, rax", vi.reg)
		return
	}
	if vi.typ != nil && vi.typ.IsFloating() {
		// Narrow the double in xmm0 down to a 4-byte IEEE single for float
		// storage; doubles store unchanged.
		if vi.typ.Kind == KFloat {
			c.emit("cvtsd2ss xmm0, xmm0")
			c.emit("movss [rbp%+d], xmm0", vi.off)
		} else {
			c.emit("movsd [rbp%+d], xmm0", vi.off)
		}
		return
	}
	if vi.typ != nil && vi.typ.Kind == KBool {
		c.normalizeBool()
	}
	switch c.slotWidth(vi.typ) {
	case 1:
		c.emit("mov byte [rbp%+d], al", vi.off)
	case 2:
		c.emit("mov word [rbp%+d], rax", vi.off)
	default:
		// int (8-byte slot) and pointers store the full 64-bit value so a
		// 64-bit address survives in an int-typed variable.
		c.emit("mov [rbp%+d], rax", vi.off)
	}
}

// ensureType converts the value currently in rax/xmm0 to the requested type
// (if they differ) and updates c.resTyp.
func (c *CG) ensureType(want CType) error {
	if c.resTyp == want {
		return nil
	}
	switch {
	case c.resTyp == TDouble && want == TInt:
		c.emit("cvttsd2si rax, xmm0")
		c.resTyp = TInt
	case c.resTyp == TInt && want == TDouble:
		c.emit("cvtsi2sd xmm0, rax")
		c.resTyp = TDouble
	default:
		return fmt.Errorf("type mismatch: cannot convert %v to %v", c.resTyp, want)
	}
	return nil
}

// genExpr is the entry point for emitting an expression. The integer/double
// type of the result is returned so callers can route it to the right
// register.
func (c *CG) genExprT(e Expr) (CType, error) {
	switch n := e.(type) {
	case *NumLit:
		if n.Kind == TDouble {
			lab, ok := c.doubleLab[n.Fval]
			if !ok {
				lab = fmt.Sprintf("LD%dx", len(c.doubles))
				c.doubles = append(c.doubles, n.Fval)
				c.doubleLab[n.Fval] = lab
			}
			c.emit("movsd xmm0, [rip+%s]", lab)
			c.resTyp = TDouble
			return TDouble, nil
		}
		c.emit("mov rax, %d", n.Val)
		c.resTyp = TInt
		c.resSigned = true
		// An unsuffixed decimal literal is an int when it fits in 32 bits;
		// larger values are long (8 bytes) and must not be truncated later.
		c.resW = 4
		if n.Val > 0x7fffffff || n.Val < -0x80000000 {
			c.resW = 8
		}
		return TInt, nil
	case *StrLit:
		lab, ok := c.strLab[n]
		if !ok {
			lab = fmt.Sprintf("LC%d", len(c.strs))
			c.strs = append(c.strs, *n)
			c.strLab[n] = lab
		}
		c.emit("lea rax, [rip+%s]", lab)
		c.resTyp = TInt
		c.resSigned = false
		c.resW = 8 // a string literal is a pointer
		return TInt, nil
	case *Ident:
		// A function designator used as a value decays to a pointer to that
		// function, exactly like an array: "fp = add;". Function names live in
		// a separate namespace from variables, so this must be resolved first.
		if sym, ok := c.funcAddrSym(n.Name); ok {
			c.emit("lea rax, [rip+%s]", sym)
			c.resTyp = TInt
			c.resSigned = false
			c.resW = 8 // a function address is a pointer
			return TInt, nil
		}
		vi, ok := c.vars[n.Name]
		if !ok {
			if lab, ok2 := c.staticVars[n.Name]; ok2 {
				// Static local: loaded from its .data label, exactly like a
				// true global.
				return c.loadGlobal(lab, c.globalTyp[lab])
			}
			if c.globals[n.Name] {
				// Global variable: load its value via rip-relative addressing
				// into the .data section (arrays decay to a pointer to element
				c.useLibGlobal(n.Name)
				// 0, mirroring local arrays).
				return c.loadGlobal(c.globalLab[n.Name], c.globalTyp[n.Name])
			}
			if ev, ok := enumConsts[n.Name]; ok {
				// An enumerator is a compile-time integer constant.
				c.emit("mov rax, %d", ev)
				c.resTyp = TInt
				c.resSigned = true
				c.resW = 4
				if ev > 0x7fffffff || ev < -0x80000000 {
					c.resW = 8
				}
				return TInt, nil
			}
			return TInt, fmt.Errorf("undefined variable %q", n.Name)
		}
		if vi.typ.IsArray() {
			// An array used as a value decays to a pointer to element 0.
			c.emit("lea rax, [rbp%+d]", vi.off)
			c.resTyp = TInt
			c.resSigned = false
			c.resW = 8
			return TInt, nil
		}
		if isAgg(vi.typ) {
			// A whole struct/union value cannot be loaded into rax; consumers
			// must go through genLValue (see structSrcAddr).
			return TInt, fmt.Errorf("cannot load struct/union value %q directly", n.Name)
		}
		c.loadVar(vi)
		return c.resTyp, nil
	case *Unary:
		return c.genUnary(n)
	case *Binary:
		return c.genBinary(n)
	case *Index:
		if err := c.genLValue(n); err != nil {
			return TInt, err
		}
		// An element that is itself an array ("rows[0]" of char rows[2][6])
		// used as a value decays to a pointer to ITS element 0: the address
		// genLValue left in r10 is the value -- loading bytes here would
		// yield a garbage pointer (and crash %s marshalling).
		if et := c.exprType(n); et != nil && et.IsArray() {
			c.emit("mov rax, r10")
			c.resTyp = TInt
			c.resSigned = false
			c.resW = 8
			return TInt, nil
		}
		width := c.elemWidthOf(n.Base)
		ec := c.elemClassOf(n.Base)
		signed := c.elemSignedOf(n.Base)
		c.genLoadElem("r10", width, ec, signed)
		return c.resTyp, nil
	case *MemberExpr:
		if err := c.genLValue(n); err != nil {
			return TInt, err
		}
		t := c.memberType(n.Base, n.Name)
		if t == nil {
			return TInt, fmt.Errorf("unknown member %q", n.Name)
		}
		if t.Kind == KStruct || t.Kind == KUnion {
			// A nested aggregate member (e.g. "s.inner") is not a scalar that
			// fits in rax; its address is already in r10 and callers must take
			// it from there.
			return TInt, fmt.Errorf("cannot load struct/union value %q directly", n.Name)
		}
		if t.Kind == KArr {
			// An array member used as a value decays to a pointer to element
			// 0: the address genLValue left in r10 IS the value -- loading
			// bytes here would yield garbage (and crash %s arg marshalling).
			c.emit("mov rax, r10")
			c.resTyp = TInt
			c.resSigned = false
			c.resW = 8
			return TInt, nil
		}
		if c.lvBitWidth > 0 {
			// Bit-field member: genLValue armed the field geometry (bit
			// offset/width inside the storage unit at r10); extract with
			// sign or zero extension per the member's signedness.
			c.genLoadBitfield(c.lvBitUnit, c.lvBitOff, c.lvBitWidth, c.lvBitSigned)
			return c.resTyp, nil
		}
		// Members are laid out at their C type width (MSVC x64 packs an int
		// member as 4 bytes), not the 8-byte scalar slot width -- loading the
		// slot width would read 4 bytes past a trailing int member. Double
		// members load into xmm0, not rax.
		width := c.typeWidth(t)
		if t.IsFloating() {
			c.genLoadElem("r10", width, TDouble, false)
			return c.resTyp, nil
		}
		signed := t.Kind == KInt && t.Signed
		c.genLoadElem("r10", width, TInt, signed)
		return c.resTyp, nil
	case *SizeofExpr:
		var sz int
		if n.Typ != nil {
			sz = sizeOf(n.Typ)
		} else {
			sz = c.typeWidth(c.exprType(n.E))
		}
		c.emit("mov rax, %d", sz)
		c.resTyp = TInt
		c.resSigned = true
		c.resW = 8 // sizeof is a size_t (8 bytes) on this target
		return TInt, nil
	case *CondExpr:
		if _, err := c.genExprT(n.Cond); err != nil {
			return c.resTyp, err
		}
		if err := c.ensureType(TInt); err != nil {
			return TInt, err
		}
		lElse := c.newLabel("else")
		lEnd := c.newLabel("endif")
		c.emit("cmp rax, 0")
		c.emit("je %s", lElse)
		tt, err := c.genExprT(n.Then)
		if err != nil {
			return tt, err
		}
		wThen, sThen := c.resW, c.resSigned
		c.emit("jmp %s", lEnd)
		c.line(lElse + ":\n")
		et, err := c.genExprT(n.Else)
		if err != nil {
			return et, err
		}
		wElse, sElse := c.resW, c.resSigned
		c.line(lEnd + ":\n")
		if tt == TDouble || et == TDouble {
			c.resTyp = TDouble
			c.resSigned = false
			c.resW = 8
		} else {
			// The conditional expression's type is the C common type of the
			// two arms (integer promotions applied).
			c.resTyp = TInt
			c.resW, c.resSigned = promotedArith(wThen, sThen, wElse, sElse)
		}
		return c.resTyp, nil
	case *CommaExpr:
		// Evaluate the left operand (discarding its value) and yield the right
		// operand's value, exactly like C's comma operator. Both sides run
		// through genExprT; the right side overwrites the result state.
		if _, err := c.genExprT(n.Left); err != nil {
			return c.resTyp, err
		}
		return c.genExprT(n.Right)
	case *CastExpr:
		t, err := c.genExprT(n.E)
		if err != nil {
			return t, err
		}
		// Casting an int-class value to a 4-byte-or-narrower int truncates to
		// 32 bits with the target's signedness. This is what makes
		// "(int)someLong / (int)somePointer" take the low 32 bits. It is a
		// no-op when the source is already a canonical 32-bit value, and it
		// must NOT run for double sources (xmm0 holds the value, not rax) or
		// for widening casts to long/unsigned long.
		if t == TInt && n.Typ != nil && n.Typ.Kind == KInt && n.Typ.Width <= 4 {
			c.canonInt(n.Typ.Signed)
		}
		if err := c.ensureType(n.Typ.Class()); err != nil {
			return t, err
		}
		if n.Typ.Kind == KFloat {
			// A float result is carried as a *valid* double (the single's
			// value widened back), so cvtsd2ss narrows the source to a
			// single (low 32 bits, high 32 cleared) and cvtss2sd re-widens
			// it to the equivalent double. Without the re-widen xmm0 would
			// hold the single's bit pattern in its low half with zeroed high
			// bits -- not the float's value as a double -- which breaks every
			// later consumer (store, comparison, arithmetic).
			c.emit("cvtsd2ss xmm0, xmm0")
			c.emit("cvtss2sd xmm0, xmm0")
		}
		c.resTyp = n.Typ.Class()
		c.resSigned = n.Typ.Kind == KInt && n.Typ.Signed
		c.resW = c.semWOf(n.Typ)
		return c.resTyp, nil
	case *IncDecExpr:
		return c.genIncDec(n)
	case *Call:
		return c.genCallExpr(n)
	case *IndirectCall:
		return c.genIndirectCall(n)
	case *AssignExpr:
		// Whole-struct/union assignment: neither side fits in a register, so
		// there is no scalar fast path and we never load the value into rax.
		// The RHS may be an lvalue (copied byte-for-byte) or a call returning
		// a struct (whose result buffer is consumed). This is how "s = t;"
		// and "s = make(1, 2);" are compiled; the assignment expression's own
		// value is left undefined (rarely used).
		if lt := c.exprType(n.Lhs); isAgg(lt) {
			if err := c.structSrcAddr(n.Rhs, c.exprType(n.Rhs)); err != nil {
				return lt.Class(), err
			}
			// Park the source address in a frame temporary: genLValue on the
			// left side is free to clobber r11 (element addressing uses it).
			c.emit("mov r11, r10")
			c.tmpDepth++
			srcSlot := c.tmpSlot(c.tmpDepth)
			c.emit("mov [rbp%+d], r11", srcSlot)
			if err := c.genLValue(n.Lhs); err != nil {
				c.tmpDepth--
				return lt.Class(), err
			}
			c.emit("mov r11, [rbp%+d]", srcSlot)
			c.tmpDepth--
			c.copyBytes("r10", "r11", lt.Size)
			c.releaseResStruct()
			c.resTyp = lt.Class()
			c.resSigned = false
			c.resW = 8
			return lt.Class(), nil
		}
		// Fast path: a simple scalar local/param on the left can be stored
		// directly from its home register/stack slot, with no address needed.
		if id, ok := n.Lhs.(*Ident); ok {
			if vi, ok2 := c.vars[id.Name]; ok2 {
				rt, err := c.genExprT(n.Rhs)
				if err != nil {
					return rt, err
				}
				if err := c.ensureType(vi.typ.Class()); err != nil {
					return rt, err
				}
				c.storeVar(vi)
				c.resTyp = vi.typ.Class()
				c.resSigned = vi.typ.Kind == KInt && vi.typ.Signed
				c.resW = c.semWOf(vi.typ)
				return vi.typ.Class(), nil
			}
		}
		// General case (deref / element lvalues): evaluate the right side, take
		// the left side's address, store width-aware, and leave the assigned
		// value in rax so "(a = b)" yields b.
		//
		// IMPORTANT: genLValue(n.Lhs) computes the destination address and is
		// free to clobber rax (it must, e.g. when indexing a pointer with an
		// expression). So the right-hand value is spilled to a frame temporary
		// first and reloaded after the address is known; otherwise a store like
		// "out[n] = *p" would write whatever garbage rax held at that point.
		rt, err := c.genExprT(n.Rhs)
		if err != nil {
			return rt, err
		}
		c.tmpDepth++
		rslot := c.tmpSlot(c.tmpDepth)
		if rt == TDouble {
			c.emit("movsd [rbp%+d], xmm0", rslot)
		} else {
			c.emit("mov [rbp%+d], rax", rslot)
		}
		if err := c.genLValue(n.Lhs); err != nil {
			return rt, err
		}
		width := c.lvalueWidth(n.Lhs)
		class := c.lvalueClass(n.Lhs)
		if rt == TDouble {
			c.emit("movsd xmm0, [rbp%+d]", rslot)
		} else {
			c.emit("mov rax, [rbp%+d]", rslot)
		}
		if c.lvBitWidth > 0 {
			// Bit-field store: RMW inside the storage unit; the truncated
			// field value stays in rax as the assignment's result. Bit-fields
			// are never floating point, so rt is necessarily TInt here.
			c.genStoreBitfield(c.lvBitUnit, c.lvBitOff, c.lvBitWidth, c.lvBitSigned)
		} else {
			if lt := c.exprType(n.Lhs); lt != nil && lt.Kind == KBool {
				c.normalizeBool()
			}
			c.genStoreElem("r10", width, class)
		}
		c.tmpDepth--
		return rt, nil
	case *VaArgExpr:
		return c.genVaArg(n)
	}
	return TInt, fmt.Errorf("unknown expression")
}

// genExpr emits an expression and discards its type (for statement context).
func (c *CG) genExpr(e Expr) error {
	_, err := c.genExprT(e)
	return err
}

// Gen produces the full assembly source for a program. linux selects the
// SysV ABI and the Linux goclib; otherwise Windows x64 conventions are used.
// Gen lowers the checked program to goa assembly for the given target. opt is
// the -O optimisation level; today no pass consumes it, so every level
// produces identical output (the IR seed keeps it a documented no-op until
// the first real pass lands).
func Gen(prog *Program, linux bool, opt int) (string, error) {
	if goclibErr != nil {
		return "", goclibErr
	}
	c := &CG{
		strLab:       map[*StrLit]string{},
		doubleLab:    map[float64]string{},
		vars:         map[string]varInfo{},
		funcs:        map[string]bool{},
		funcDefs:     map[string]*FuncDecl{},
		calls:        map[string]bool{},
		need:         map[string]bool{},
		globals:      map[string]bool{},
		globalLab:    map[string]string{},
		globalTyp:    map[string]*Type{},
		staticVars:   map[string]string{},
		libEmitted:   map[string]bool{},
		libGlobNames: map[string]bool{},
		libGlobUsed:  map[string]bool{},
		linux:        linux,
		opt:          opt,
	}
	for _, g := range prog.Globals {
		c.globals[g.Name] = true
		c.globalLab[g.Name] = "G_" + g.Name
		c.globalTyp[g.Name] = g.Typ
	}
	// The built-in C library joins the program: its file-scope variables
	// (rand_state, ...) share the global pool unless the user declared their
	// own of the same name, and its prototypes/definitions feed call-site
	// double promotion for every caller, user or library-internal. Registering
	// happens BEFORE the user's own functions so a user definition always
	// overwrites the library entry in funcDefs.
	if lib := clibCStore(linux); lib != nil {
		for _, g := range lib.globals {
			if c.globals[g.Name] {
				continue // user global of the same name wins
			}
			c.globals[g.Name] = true
			c.globalLab[g.Name] = "G_" + g.Name
			c.globalTyp[g.Name] = g.Typ
			c.libGlobNames[g.Name] = true
			c.libGlobals = append(c.libGlobals, g)
		}
		for _, pr := range lib.protos {
			if _, dup := c.funcDefs[pr.Name]; !dup {
				c.funcDefs[pr.Name] = pr
			}
		}
		for _, name := range lib.order {
			if _, dup := c.funcDefs[name]; !dup {
				c.funcDefs[name] = lib.funcs[name]
			}
		}
	}
	for _, f := range prog.Funcs {
		c.funcs[f.Name] = true
		c.funcDefs[f.Name] = f
	}
	// Prototypes (from #include'd headers) are registered only for call-site
	// double-promotion; they are deliberately NOT added to c.funcs, so a
	// prototype for a goclib function still triggers goclib inclusion.
	for _, f := range prog.Prototypes {
		c.funcDefs[f.Name] = f
	}

	var body strings.Builder
	for _, f := range prog.Funcs {
		if err := c.genFunc(f); err != nil {
			return "", err
		}
	}
	// Emit every needed built-in C library function through the regular code
	// generator. genFunc of a library function marks more needs (its own
	// calls: library-internal helpers and the extern OS primitives), so the
	// pass runs to a fixpoint.
	//
	// On Linux the entry stub below terminates through `call exit`. When the
	// C library provides exit, that call must reach the C function (whose
	// label would collide with an extern syscall stub of the same name), so
	// pull its body in here rather than importing the symbol.
	if c.linux {
		if lib := clibCStore(c.linux); lib != nil {
			if _, isC := lib.funcs["exit"]; isC {
				c.need["exit"] = true
			}
		}
	}
	if err := c.genClibFuncs(); err != nil {
		return "", err
	}
	// -O1 and above: structured peephole over the body stream. It sees real
	// instructions only; inline __asm is off limits (its flag effects are
	// the author's business). -O0 keeps the legacy textual pass so its
	// output stays byte-identical.
	if c.opt >= 1 {
		c.insts = peepholeIR(c.insts)
	}
	body.WriteString(printASM(c.insts))

	if _, ok := c.funcs["main"]; !ok {
		return "", fmt.Errorf("program has no main()")
	}

	// Every import the program needs: the exit routine for the entry stub,
	// whatever the C code calls directly, and whatever goclib pulled in.
	importSet := map[string]bool{}
	if c.linux {
		// The C library's exit (if compiled in) replaced the extern stub --
		// see the need["exit"] pull-in above.
		if lib := clibCStore(c.linux); lib != nil {
			if _, isC := lib.funcs["exit"]; !isC {
				importSet["exit"] = true
			}
		} else {
			importSet["exit"] = true
		}
	} else {
		importSet["ExitProcess"] = true
	}
	for name := range c.calls {
		importSet[name] = true
	}
	imports := make([]string, 0, len(importSet))
	for name := range importSet {
		if c.linux {
			if !externLinux[name] {
				return "", fmt.Errorf("unknown function %q: not in goclib (%s), and not a Linux syscall goa knows",
					name, strings.Join(goclibNames(c.linux), ", "))
			}
			// ELF targets have no DLLs: goa turns this into a syscall stub.
			imports = append(imports, fmt.Sprintf("extern %s\n", name))
			continue
		}
		dll, ok := externDLL[name]
		if !ok {
			return "", fmt.Errorf("unknown function %q: not in goclib (%s), and not in win32.def",
				name, strings.Join(goclibNames(c.linux), ", "))
		}
		imports = append(imports, fmt.Sprintf("extern %s, %s\n", name, dll))
	}
	sort.Strings(imports)

	var out strings.Builder
	out.WriteString("; generated by goc -- assembled by goa, no gcc involved\n")
	out.WriteString("section .text\n")
	out.WriteString("global _start\n")
	out.WriteString("\n")
	for _, e := range imports {
		out.WriteString(e)
	}
	out.WriteString("\n")
	// Bind every pointer slot initialised by a string literal before main runs.
	// goa has no data relocations, so a pointer value cannot live in .data; the
	// entry stub instead computes each string's address with lea and writes it
	// into the (zero-filled) pointer slot. This covers top-level globals, static
	// locals, and char* members nested anywhere inside a braced initialiser.
	var strInit strings.Builder
	for _, g := range prog.Globals {
		c.walkGlobalInit(g.Typ, g.Init, c.globalLab[g.Name], 0)
	}
	for _, se := range c.staticList {
		c.walkGlobalInit(se.d.Typ, se.d.Init, se.lab, 0)
	}
	for _, si := range c.globalStrInits {
		// lea rax,[rip+glab] -- address of the pointer slot
		// lea rdx,[rip+slab] -- address of the string constant
		// mov [rax+off],rdx -- store the string pointer into the slot
		fmt.Fprintf(&strInit, "\tlea rax, [rip+%s]\n\tlea rdx, [rip+%s]\n\tmov [rax+%d], rdx\n",
			si.glab, si.slab, si.off)
	}

	// Entry stub: align the stack, run main, and hand its return value to the
	// platform's exit routine. Linux needs no shadow space and exits through
	// the `exit` syscall stub; Windows uses ExitProcess.
	out.WriteString("_start:\n")
	out.WriteString("\tand rsp, -16\n")
	if c.linux {
		out.WriteString(strInit.String())
		out.WriteString("\tcall main\n")
		out.WriteString("\tmov rdi, rax\n")
		out.WriteString("\tcall exit\n\n")
	} else {
		out.WriteString("\tsub rsp, 48\n")
		out.WriteString(strInit.String())
		out.WriteString("\tcall main\n")
		out.WriteString("\tmov rcx, rax\n")
		out.WriteString("\tcall ExitProcess\n\n")
	}
	out.WriteString(body.String())

	// Program-level (global / static) variables live in a writable .data
	// section, referenced via rip. Only constant integer initialisers are
	// supported today (goclib's globals are all simple constants). Arrays are
	// zero-filled for their full byte size so rip-relative indexing works.
	// Built-in library globals come last, and only those the program actually
	// referenced (useLibGlobal).
	if len(prog.Globals) > 0 || len(c.staticList) > 0 || len(c.libGlobUsed) > 0 {
		out.WriteString("\nsection .data\n")
		for _, g := range prog.Globals {
			if err := c.emitGlobalVar(&out, g, c.globalLab[g.Name]); err != nil {
				return "", err
			}
		}
		for _, se := range c.staticList {
			if err := c.emitGlobalVar(&out, se.d, se.lab); err != nil {
				return "", err
			}
		}
		for _, g := range c.libGlobals {
			if !c.libGlobUsed[g.Name] {
				continue
			}
			if err := c.emitGlobalVar(&out, g, c.globalLab[g.Name]); err != nil {
				return "", err
			}
		}
	}

	if len(c.strs) > 0 {
		out.WriteString("\nsection .rdata\n")
		for i := range c.strs {
			lab := fmt.Sprintf("LC%d", i)
			out.WriteString(fmt.Sprintf("%s db \"%s\", 0\n", lab, encodeStr(c.strs[i].Bytes)))
		}
	}
	if len(c.doubles) > 0 {
		out.WriteString("\nsection .rdata\n")
		for _, v := range c.doubles {
			lab := c.doubleLab[v]
			out.WriteString(fmt.Sprintf("%s dq %s\n", lab, formatDouble(v)))
		}
	}
	asm := out.String()
	if c.opt == 0 {
		asm = peepholeASM(asm)
	}
	return asm, nil
}

// parsedLine is a body instruction split the way the peephole rules need it:
// mnemonic plus comma-separated operands, whitespace-trimmed. Only lines that
// parse cleanly take part in a pattern; everything else is left alone.
type parsedLine struct {
	op       string
	operands []string
}

// parseBodyLine splits an instruction line "\tmov rax, [rbp-8]" into its
// mnemonic and operands. ok is false for anything the peephole does not
// model: operand-less instructions ("ret", "leave"), and defensively any
// line carrying a label or comment marker (instructions never contain those,
// but the rules only run on what they can read).
func parseBodyLine(text string) (parsedLine, bool) {
	s := strings.TrimSpace(strings.TrimPrefix(text, "\t"))
	sp := strings.IndexAny(s, " \t")
	if sp <= 0 || strings.ContainsAny(s, ":;#") {
		return parsedLine{}, false
	}
	ops := strings.Split(s[sp+1:], ",")
	for i := range ops {
		ops[i] = strings.TrimSpace(ops[i])
	}
	return parsedLine{op: s[:sp], operands: ops}, true
}

func isMemOperand(s string) bool {
	return strings.HasPrefix(s, "[")
}

// isLoadForm reports whether pl is `mov <reg>, [<mem>]` and isStoreForm
// whether it is `mov [<mem>], <reg>` -- the two shapes around every
// stack-resident variable access, and the shapes the pair rules match on.
// Sized forms like `mov byte [rbp-3], al` do not match (the first operand
// does not start with '['), which is exactly the wanted conservatism.
func (pl parsedLine) isLoadForm() bool {
	return pl.op == "mov" && len(pl.operands) == 2 &&
		!isMemOperand(pl.operands[0]) && isMemOperand(pl.operands[1])
}

func (pl parsedLine) isStoreForm() bool {
	return pl.op == "mov" && len(pl.operands) == 2 &&
		isMemOperand(pl.operands[0]) && !isMemOperand(pl.operands[1])
}

// peepholeIR rewrites the instruction stream with safe, local patterns. It
// runs at -O1 and above, replacing the textual peepholeASM (which stays for
// -O0 so that level keeps emitting byte-identical legacy output). Unlike the
// textual pass it only ever touches instInstr lines: inline __asm keeps its
// bytes, whose flag effects only the author knows about.
//
// Every rule is window-limited to the instruction immediately before, and a
// label, an inline-asm line, or anything unparseable closes the window:
//
//	mov <reg>, 0     ->  xor <reg32>, <reg32>  (2 bytes, zeroes the full reg)
//	mov <reg>, [<m>]       \  second identical load dropped: nothing between
//	mov <reg>, [<m>]       /  the two can write <m> or clobber <reg>
//	mov [<m>], <reg>       \
//	mov <reg>, [<m>]       /  load dropped: <reg> already holds the value
//
// The load rules require the *same register spelling* on both sides.
// `mov [m], eax` followed by `mov rax, [m]` reads 8 bytes of which only 4
// were written, so forwarding that pair would change semantics and is not
// done. Loads do not write flags, so dropping one never moves a flag boundary.
func peepholeIR(insts []Inst) []Inst {
	out := make([]Inst, 0, len(insts))
	last := -1 // index in out of the previous instruction line, -1 if none
	for _, in := range insts {
		if in.Kind != instInstr {
			out = append(out, in)
			last = -1
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			out = append(out, in)
			last = -1
			continue
		}
		// mov <reg>, 0 -> xor <reg32>, <reg32>. gpReg32 refuses rsp/rbp and
		// the FP registers, exactly like the textual pass did.
		if pl.op == "mov" && len(pl.operands) == 2 && pl.operands[1] == "0" {
			if r := gpReg32(pl.operands[0]); r != "" {
				in = Inst{Kind: instInstr, Text: "\txor " + r + ", " + r}
				pl, _ = parseBodyLine(in.Text)
			}
		}
		if last >= 0 {
			if prev, ok := parseBodyLine(out[last].Text); ok {
				// Duplicate load: `mov rax, [m]; mov rax, [m]` -- the second
				// reads what the first already brought in.
				if prev.isLoadForm() && pl.isLoadForm() &&
					prev.operands[0] == pl.operands[0] &&
					prev.operands[1] == pl.operands[1] {
					continue
				}
				// Store into a slot followed by loading that very slot back
				// into the very register it came from: the register already
				// holds the value.
				if prev.isStoreForm() && pl.isLoadForm() &&
					prev.operands[0] == pl.operands[1] &&
					prev.operands[1] == pl.operands[0] {
					continue
				}
			}
		}
		out = append(out, in)
		last = len(out) - 1
	}
	return out
}

// peepholeASM applies a few safe, textual optimisations to the generated
// assembly before it reaches goa. It is the -O0 path: rewriting the final
// text keeps that level byte-identical to the legacy pipeline. -O1 and above
// run peepholeIR on the instruction stream instead (and leave inline __asm
// alone, unlike this pass, which happily rewrites a user's "mov eax, 0").
// The rewrites are purely local and never touch labels, offsets, or control
// flow. The main one
// replaces `mov <reg>, 0` with `xor <reg32>, <reg32>`: the latter zeroes the
// whole 64-bit register (high bits included) in 2 bytes instead of the 7-byte
// `mov rax, 0`, and goc emits an enormous number of these.
var zeroMovRe = regexp.MustCompile(`(?m)^(\s*)mov\s+([a-z0-9]+)\s*,\s*0\s*$`)

func peepholeASM(s string) string {
	return zeroMovRe.ReplaceAllStringFunc(s, func(line string) string {
		m := zeroMovRe.FindStringSubmatch(line)
		if m == nil {
			return line
		}
		if r := gpReg32(m[2]); r != "" {
			return m[1] + "xor " + r + ", " + r
		}
		return line
	})
}

// gpReg32 maps a general-purpose integer register (either spelling) to its
// 32-bit form, or "" if the operand is not a rewritable integer register
// (a memory operand, rsp/rbp, or an FP register).
func gpReg32(reg string) string {
	switch reg {
	case "rax", "eax":
		return "eax"
	case "rbx", "ebx":
		return "ebx"
	case "rcx", "ecx":
		return "ecx"
	case "rdx", "edx":
		return "edx"
	case "rsi", "esi":
		return "esi"
	case "rdi", "edi":
		return "edi"
	case "r8", "r8d":
		return "r8d"
	case "r9", "r9d":
		return "r9d"
	case "r10", "r10d":
		return "r10d"
	case "r11", "r11d":
		return "r11d"
	case "r12", "r12d":
		return "r12d"
	case "r13", "r13d":
		return "r13d"
	case "r14", "r14d":
		return "r14d"
	case "r15", "r15d":
		return "r15d"
	}
	return ""
}

// genClibFuncs emits every built-in C library function reachable from c.need
// through the regular code generator. Emitting one can mark more needs (the
// function's own calls: library-internal helpers and the assembly platform
// primitives), so the pass repeats until a round adds nothing.
func (c *CG) genClibFuncs() error {
	lib := clibCStore(c.linux)
	if lib == nil {
		return nil
	}
	for {
		var batch []string
		for name := range c.need {
			if c.libEmitted[name] {
				continue
			}
			if _, ok := lib.funcs[name]; ok {
				batch = append(batch, name)
			}
		}
		if len(batch) == 0 {
			return nil
		}
		sort.Strings(batch) // stable emission order
		for _, name := range batch {
			c.libEmitted[name] = true
			if err := c.genFunc(lib.funcs[name]); err != nil {
				return err
			}
		}
	}
}

// useLibGlobal records that the program referenced a built-in library global
// (e.g. rand_state), so Gen emits it into .data. Library globals the program
// never touches stay out of the binary.
func (c *CG) useLibGlobal(name string) {
	if c.libGlobNames[name] {
		c.libGlobUsed[name] = true
	}
}

// foldConstInit folds the constant initialiser of a global variable down to an
// integer. The parser represents "= 42" as a NumLit, "= -1" as Unary{'-'}, and
// "= GREEN" as an Ident naming an enumerator. Anything else (a double literal,
// an expression, a struct initialiser) is not foldable at emission time and
// yields ok=false, so the global falls back to zero.
func foldConstInit(e Expr) (int64, bool) {
	switch n := e.(type) {
	case nil:
		return 0, true
	case *NumLit:
		if n.Kind == TDouble {
			return 0, false
		}
		return n.Val, true
	case *Ident:
		if v, ok := enumConsts[n.Name]; ok {
			return v, true
		}
		return 0, false
	case *Unary:
		v, ok := foldConstInit(n.E)
		if !ok {
			return 0, false
		}
		switch n.Op {
		case "-":
			return -v, true
		case "+":
			return v, true
		}
	}
	return 0, false
}

// foldFloatInit folds the constant initialiser of a global float/double down
// to its value. Like foldConstInit it understands a bare literal, a negated
// literal and an enumerator name; anything else yields ok=false and the global
// falls back to 0.0.
func foldFloatInit(e Expr) (float64, bool) {
	switch n := e.(type) {
	case nil:
		return 0, true
	case *NumLit:
		if n.Kind == TDouble {
			return n.Fval, true
		}
		return float64(n.Val), true
	case *Ident:
		if v, ok := enumConsts[n.Name]; ok {
			return float64(v), true
		}
		return 0, false
	case *Unary:
		v, ok := foldFloatInit(n.E)
		if !ok {
			return 0, false
		}
		switch n.Op {
		case "-":
			return -v, true
		case "+":
			return v, true
		}
	case *Binary:
		l, ok1 := foldFloatInit(n.L)
		r, ok2 := foldFloatInit(n.R)
		if !ok1 || !ok2 {
			return 0, false
		}
		switch n.Op {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			if r == 0 {
				return 0, false
			}
			return l / r, true
		}
		return 0, false
	case *CastExpr:
		if v, ok := foldFloatInit(n.E); ok {
			return v, true
		}
		if v, ok := foldConstInit(n.E); ok {
			return float64(v), true
		}
		return 0, false
	}
	return 0, false
}

// findAddressTaken returns the set of local variable names whose address is
// taken anywhere in f (via &x). Such locals cannot live in a register,
// because there would be nowhere for the pointer to point.
func (c *CG) findAddressTaken(f *FuncDecl) map[string]bool {
	taken := map[string]bool{}
	var walkExpr func(e Expr)
	walkExpr = func(e Expr) {
		if e == nil {
			return
		}
		switch n := e.(type) {
		case *Unary:
			if n.Op == "&" {
				if id, ok := n.E.(*Ident); ok {
					taken[id.Name] = true
				}
			}
			walkExpr(n.E)
		case *Binary:
			walkExpr(n.L)
			walkExpr(n.R)
		case *Index:
			walkExpr(n.Base)
			walkExpr(n.Idx)
		case *Call:
			// va_start(ap, ...) and va_end(ap) both require ap's address
			// (va_start writes the cursor into it), and va_arg needs it too.
			if n.Name == "va_start" || n.Name == "va_end" {
				if len(n.Args) > 0 {
					if id, ok := n.Args[0].(*Ident); ok {
						taken[id.Name] = true
					}
				}
			}
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *IndirectCall:
			walkExpr(n.Fn)
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *AssignExpr:
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
		case *BraceInit:
			for _, el := range n.Elems {
				walkExpr(el.E)
			}
		case *VaArgExpr:
			if id, ok := n.Ap.(*Ident); ok {
				taken[id.Name] = true
			}
			walkExpr(n.Ap)
		}
	}
	var walkStmt func(s Stmt)
	walkStmt = func(s Stmt) {
		if s == nil {
			return
		}
		switch n := s.(type) {
		case *Block:
			for _, st := range n.Stmts {
				walkStmt(st)
			}
		case *DeclList:
			for _, d := range n.Decls {
				walkStmt(d)
			}
		case *DeclStmt:
			walkExpr(n.Init)
		case *AssignStmt:
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
		case *ExprStmt:
			walkExpr(n.E)
		case *ReturnStmt:
			walkExpr(n.E)
		case *IfStmt:
			walkExpr(n.Cond)
			walkStmt(n.Then)
			walkStmt(n.Else)
		case *WhileStmt:
			walkExpr(n.Cond)
			walkStmt(n.Body)
		case *ForStmt:
			if n.Init != nil {
				walkStmt(n.Init)
			}
			if n.Cond != nil {
				walkExpr(n.Cond)
			}
			if n.Post != nil {
				walkExpr(n.Post)
			}
			walkStmt(n.Body)
		case *DoWhileStmt:
			walkExpr(n.Cond)
			walkStmt(n.Body)
		case *SwitchStmt:
			walkExpr(n.Src)
			walkStmt(n.Body)
		case *LabelStmt:
			walkStmt(n.Stmt)
		}
	}
	walkStmt(f.Body)
	return taken
}

func (c *CG) genFunc(f *FuncDecl) error {
	c.vars = map[string]varInfo{}
	c.staticVars = map[string]string{} // fresh per function: static-local names do not leak across functions
	c.curRet = f.Ret
	c.curParam = f.ParamTypes
	c.sretSlot = 0
	// Hidden struct-return pointer (sret): a function returning a struct or
	// union receives the address of the caller's result buffer in its FIRST
	// integer argument register (RCX on Win64, RDI on SysV). Every user
	// parameter therefore shifts one register slot to the right; regShift
	// captures that offset and regCap is the user-parameter register count.
	isSret := isAgg(f.Ret)
	regShift := 0
	if isSret {
		regShift = 1
	}
	argRegs := c.argRegs()
	regCap := len(argRegs) - regShift
	for i, p := range f.Params {
		// Placeholder homes; the real ones are assigned in the frame-layout
		// phase below. Registering early keeps param names out of the local
		// declaration gather.
		c.vars[p] = varInfo{off: 16 + 8*i, typ: f.ParamTypes[i]} // [rbp+16], ...
	}

	// localDecl is one (name, type) pair for a function-local variable.
	type localDecl struct {
		name string
		typ  *Type
	}

	// Gather every local declaration (including those inside nested blocks)
	// so we can decide, up front, which ones live in callee-save registers
	// and which stay on the stack.
	var decls []localDecl
	maxSwDepth := 0
	hasAsm := false // an inline-assembly block in this function?
	var gather func(Stmt, int)
	gather = func(s Stmt, swDepth int) {
		if swDepth > maxSwDepth {
			maxSwDepth = swDepth
		}
		switch n := s.(type) {
		case *Block:
			for _, st := range n.Stmts {
				gather(st, swDepth)
			}
		case *DeclList:
			for _, d := range n.Decls {
				gather(d, swDepth)
			}
		case *DeclStmt:
			if n.Storage == "static" {
				// Static local: lives in .data under a unique label, persists
				// across calls, and is initialised once at load time. No frame
				// slot is allocated; loadVar/genLValue resolve it via
				// c.staticVars.
				lab := fmt.Sprintf("G_st%d_%s", c.staticSeq, n.Name)
				c.staticSeq++
				c.globals[lab] = true
				c.globalLab[lab] = lab
				c.globalTyp[lab] = n.Typ
				c.staticVars[n.Name] = lab
				c.staticList = append(c.staticList, staticEmit{lab: lab, d: n})
			} else if n.Storage == "extern" {
				// Extern local: a reference to a file-scope global of the same
				// name. No frame slot; loadVar/genLValue fall through to
				// c.globals to resolve it.
			} else if _, ok := c.vars[n.Name]; !ok {
				decls = append(decls, localDecl{n.Name, n.Typ})
			}
		case *IfStmt:
			gather(n.Then, swDepth)
			if n.Else != nil {
				gather(n.Else, swDepth)
			}
		case *WhileStmt:
			gather(n.Body, swDepth)
		case *DoWhileStmt:
			gather(n.Body, swDepth)
		case *ForStmt:
			if n.Init != nil {
				gather(n.Init, swDepth)
			}
			gather(n.Body, swDepth)
		case *SwitchStmt:
			// A switch needs one frame slot to hold the controlling value,
			// and it does not introduce a new scope for its labels.
			gather(n.Body, swDepth+1)
		case *LabelStmt:
			gather(n.Stmt, swDepth)
		case *AsmStmt:
			hasAsm = true
		}
	}
	for _, st := range f.Body.Stmts {
		gather(st, 0)
	}
	c.swSlots = maxSwDepth

	// A local whose address is taken (&x) cannot live in a register, doubles
	// cannot live in a GPR, and arrays obviously need memory. Struct/union
	// aggregates likewise never live in a register. Everything else that is a
	// small int is a candidate for a callee-save register home.
	addrTaken := c.findAddressTaken(f)
	regPool := []string{"rbx", "r12", "r13", "r14"}
	// A function that contains an inline-assembly block must keep every
	// local on the stack: the asm text binds C variable names to their
	// frame slots ([rbp+off]) or, worse, to a callee-save register home
	// that the hand-written asm neither knows about nor preserves. Forcing
	// the whole pool empty (regPool = nil below makes "ri < len(regPool)"
	// always false) gives the asm a stable, addressable memory home for
	// every variable it names.
	if hasAsm {
		regPool = nil
	}
	c.usedRegs = nil
	regOf := map[string]string{}
	ri := 0
	stackDecls := make([]localDecl, 0, len(decls))
	for _, d := range decls {
		// Only integer-class scalars get a callee-save home: float rides in
		// XMM registers like double (and is 4 bytes in memory), so it must
		// stay on the stack.
		intClass := d.typ != nil && !d.typ.IsArray() && !d.typ.IsFloating() &&
			d.typ.Kind != KStruct && d.typ.Kind != KUnion
		if intClass && !addrTaken[d.name] && ri < len(regPool) {
			regOf[d.name] = regPool[ri]
			c.usedRegs = append(c.usedRegs, regPool[ri])
			c.vars[d.name] = varInfo{reg: regPool[ri], typ: d.typ}
			ri++
		} else {
			stackDecls = append(stackDecls, d)
		}
	}

	// Assign stack slots to the remaining locals (and every array). The
	// callee-save save area sits just below rbp; locals grow downward from
	// there, and expression temporaries (tmpSlot) sit below the locals.
	//
	// Element width is honoured for arrays: a char[32] occupies 32 bytes
	// (byte-packed, matching how string literals and malloc'd buffers are
	// laid out), while a T[] keeps the 8-byte-per-element stride. Scalars
	// always take an 8-byte slot in this relaxed toy model.
	regArea := 8 * len(c.usedRegs)
	localBytes := 0

	// Parameter homes. Register parameters are spilled into this function's
	// OWN frame on both targets: on SysV, [rbp+16+8k] is the caller's stack-
	// argument area (there is no shadow space), so spilling there clobbers
	// stack arguments 7 and up -- observed as sum7(1..7) losing its seventh
	// argument on Linux. Windows gets the same treatment for uniformity; its
	// 32-byte shadow space stays reserved at call sites for ABI compliance.
	//
	// Struct/union parameters arrive as hidden pointers to caller-owned
	// bytes (the caller passes the address of a private copy in one GP
	// register slot). They get a full-size local slot and the prologue
	// copies the bytes in, so the body treats them like any aggregate local
	// with value semantics.
	//
	// Stack parameters are read in place from the caller's argument area:
	//   Win : [rbp+16+8*(i+regShift)]  ([rbp+16+8k] maps to caller [rsp+8k])
	//   SysV: [rbp+16+8*(i-regCap)]    (caller placed stack arg j at [rsp+8j])
	type paramCopy struct {
		name   string
		typ    *Type
		srcReg string // incoming hidden-pointer register ("" = on the stack)
		srcOff int    // incoming hidden-pointer rbp offset (stack params)
	}
	var paramCopies []paramCopy
	aggSlotBytes := func(pt *Type) int {
		w := pt.Size
		if w < 1 {
			w = 8
		}
		if w%8 != 0 {
			w += 8 - w%8
		}
		return w
	}
	for i, p := range f.Params {
		pt := f.ParamTypes[i]
		if !isAgg(pt) {
			if i < regCap {
				localBytes += 8
				c.vars[p] = varInfo{off: -(regArea + localBytes), typ: pt}
			} else if c.linux {
				c.vars[p] = varInfo{off: 16 + 8*(i-regCap), typ: pt}
			} else {
				c.vars[p] = varInfo{off: 16 + 8*(i+regShift), typ: pt}
			}
			continue
		}
		localBytes += aggSlotBytes(pt)
		c.vars[p] = varInfo{off: -(regArea + localBytes), typ: pt}
		pc := paramCopy{name: p, typ: pt}
		if i < regCap {
			pc.srcReg = argRegs[i+regShift]
		} else if c.linux {
			pc.srcOff = 16 + 8*(i-regCap)
		} else {
			pc.srcOff = 16 + 8*(i+regShift)
		}
		paramCopies = append(paramCopies, pc)
	}
	// The hidden sret pointer gets its own frame slot, parked in the
	// prologue and read back by ReturnStmt.
	if isSret {
		localBytes += 8
		c.sretSlot = -(regArea + localBytes)
	}

	for _, d := range stackDecls {
		if d.typ != nil && d.typ.IsArray() {
			ln := d.typ.Len
			if ln < 1 {
				ln = 1
			}
			w := c.typeWidth(d.typ.Elem) // element width (1 for char, 8 otherwise)
			size := w * ln
			if size%8 != 0 { // keep the next local 8-byte aligned
				size += 8 - size%8
			}
			off := -(regArea + localBytes + size)
			localBytes += size
			c.vars[d.name] = varInfo{off: off, typ: d.typ}
		} else {
			// Scalar (or struct/union aggregate) slot. The slot width MUST
			// match the width used by loadVar/storeVar/lvalueWidth, which all
			// go through slotWidth: char=1, short=2, int=8 (a goc "int" slot
			// is 8 bytes so it can hold a 64-bit pointer stored in an int
			// variable), long/pointer=8, double=8, aggregates=Size. Using
			// typeWidth here (int=4) would space int locals 4 bytes apart
			// while the 8-byte loads/stores overlap them -- a silent memory
			// corruption seen as garbled print_int output on both backends.
			w := c.slotWidth(d.typ)
			if w < 1 {
				w = 1
			}
			localBytes += w
			c.vars[d.name] = varInfo{off: -(regArea + localBytes), typ: d.typ}
		}
	}
	c.regArea = regArea
	c.localBytes = localBytes
	c.tmpDepth = 0

	// A variadic function spills every incoming argument (register args and
	// caller-stack args) into a contiguous save area so va_arg can walk a
	// single cursor. nFixed is the number of named params before "...".
	c.saveBaseOff = 0
	c.nFixed = 0
	varargSave := 0
	if f.Variadic {
		varargSave = 8 * maxArgs
		c.nFixed = len(f.Params)
	}

	// shadow space (Windows only) + callee-save save area + locals +
	// expression temporaries + variadic save area, all kept 16-byte aligned.
	//
	// On SysV there is no shadow space, so a callee's register-argument
	// spill slots ([rbp+16+8k] of the callee, i.e. [rsp+0+8k] of the
	// caller) physically land on top of the caller's frame bottom. A
	// variadic caller keeps its va_list save area right at that bottom,
	// so a callee spilling 6 register args would clobber the first
	// vararg slots (observed as printf printing 512 instead of 10 on
	// Linux). Reserve 8*len(argRegs) bytes at the frame bottom on SysV
	// so the save area sits above the callee's clobber zone.
	pad := c.shadowSpace()
	if c.linux && f.Variadic {
		pad = 8 * len(c.argRegs())
	}
	frame := pad + regArea + localBytes + 8*scratchSlots + 8*maxSwDepth + varargSave
	if frame%16 != 0 {
		frame += 16 - frame%16
	}
	// saveBaseOff points at save-area slot 0, which sits just below the
	// expression temporaries. The ABI pad added to frame above pushes it up
	// so the callee's argument-spill slots ([rsp+0..8*len(argRegs)) of the
	// caller) stay below it.
	c.saveBaseOff = -(regArea + localBytes + 8*scratchSlots + 8*maxSwDepth + varargSave)

	c.curFn = f.Name
	c.labels = map[string]string{}
	c.swDepth = 0
	c.breaks = nil
	c.line(f.Name + ":\n")
	c.emit("push rbp")
	c.emit("mov rbp, rsp")
	c.emit("sub rsp, %d", frame)
	// Save only the callee-save registers we actually use as local homes.
	for i, r := range c.usedRegs {
		c.emit("mov [rbp-%d], %s", 8*(i+1), r)
	}
	// Park the hidden struct-return pointer in its frame slot before any
	// call can clobber the first integer argument register.
	if isSret {
		c.emit("mov [rbp%+d], %s", c.sretSlot, argRegs[0])
	}

	// Spill the register arguments into this function's frame so the rest of
	// the code can read params from the stack like normal locals. Double
	// parameters arrive in XMM registers and are stored as 8 bytes (movsd).
	argXMM := c.argXMM()

	// A variadic callee must copy the caller's STACK arguments into its save
	// area BEFORE the register-argument spill below. On SysV the spill slots
	// [rbp+16+8i] physically overlap the caller's stack-argument slots
	// ([rsp+0+8i] of the caller -- no shadow space to absorb them), so
	// spilling first would clobber the very values va_arg must read later.
	if f.Variadic {
		vaStackBase := 16
		if !c.linux {
			vaStackBase = 48
		}
		for i := regCap; i < maxArgs; i++ {
			// user arg i lives at caller stack slot i-regCap; its save-area
			// slot is i+regShift (slot 0 is the hidden sret pointer, if any)
			c.emit("mov rax, [rbp+%d]", vaStackBase+8*(i-regCap))
			c.emit("mov [rbp%+d], rax", c.saveBaseOff+8*(i+regShift))
		}
	}

	fpIdx := 0 // SysV: XMM argument registers are numbered by FP-arg order
	for i := 0; i < len(f.Params) && i < regCap; i++ {
		pt := f.ParamTypes[i]
		if isAgg(pt) {
			continue // aggregate params are copied from their hidden pointer below
		}
		vi := c.vars[f.Params[i]]
		if pt.IsFloating() {
			// XMM index: Windows numbers XMM argument registers positionally
			// (shared with the GP slot counter, hidden pointer included);
			// SysV numbers them by FP-argument order only.
			xmmAt := i + regShift
			if c.linux {
				xmmAt = fpIdx
			}
			fpIdx++
			if xmmAt < len(argXMM) {
				// A float parameter arrives in the low 32 bits of its XMM
				// register; store it as a 4-byte single. The body widens it
				// back to double on every read.
				if pt.Kind == KFloat {
					c.emit("movss [rbp%+d], %s", vi.off, argXMM[xmmAt])
				} else {
					c.emit("movsd [rbp%+d], %s", vi.off, argXMM[xmmAt])
				}
				continue
			}
		}
		c.emit("mov [rbp%+d], %s", vi.off, argRegs[i+regShift])
	}
	// Copy aggregate parameters out of their caller-owned hidden pointers
	// into the local slots reserved above.
	for _, pc := range paramCopies {
		vi := c.vars[pc.name]
		if pc.srcReg != "" {
			c.emit("mov r10, %s", pc.srcReg)
		} else {
			c.emit("mov r10, [rbp%+d]", pc.srcOff)
		}
		c.emit("lea r11, [rbp%+d]", vi.off)
		c.copyBytes("r11", "r10", pc.typ.Size)
	}

	// Variadic: copy the register args into the save area. The caller-stack
	// args were copied above, before the register spill clobbered the
	// caller's stack-argument zone.
	if f.Variadic {
		for k := regShift; k < len(argRegs); k++ {
			c.emit("mov [rbp%+d], %s", c.saveBaseOff+8*k, argRegs[k])
		}
	}

	for _, st := range f.Body.Stmts {
		if err := c.genStmt(st); err != nil {
			return err
		}
	}
	// Safety epilogue in case a path has no explicit return. If the body's
	// last reachable statement is already a return, that ReturnStmt emitted a
	// full epilogue, so emitting another here would leave dead code after the
	// ret (e.g. `mov rsp, rbp; pop rbp; ret` that can never execute).
	if !c.blockEndsWithReturn(f.Body) {
		c.emitEpilogue()
	}
	return nil
}

// blockEndsWithReturn reports whether the last reachable statement of a block is
// an unconditional return, recursing through a trailing nested block. It is
// conservative: a trailing if/while/for (without a guaranteed return) yields
// false so the safety epilogue is retained.
func (c *CG) blockEndsWithReturn(b *Block) bool {
	if b == nil || len(b.Stmts) == 0 {
		return false
	}
	s := b.Stmts[len(b.Stmts)-1]
	if _, ok := s.(*ReturnStmt); ok {
		return true
	}
	if nb, ok := s.(*Block); ok {
		return c.blockEndsWithReturn(nb)
	}
	return false
}

// emitEpilogue restores the saved callee-save registers, then returns.
// (goa has no `leave`, so spell it out.)
func (c *CG) emitEpilogue() {
	for i := len(c.usedRegs) - 1; i >= 0; i-- {
		c.emit("mov %s, [rbp-%d]", c.usedRegs[i], 8*(i+1))
	}
	c.emit("mov rsp, rbp")
	c.emit("pop rbp")
	c.emit("ret")
}

func (c *CG) genStmt(s Stmt) error {
	switch n := s.(type) {
	case *Block:
		for _, st := range n.Stmts {
			if err := c.genStmt(st); err != nil {
				return err
			}
		}
	case *DeclList:
		// A multi-declarator declaration ("int a = 1, b = 2;") is one
		// statement; without this case genStmt silently emitted nothing for
		// it, leaving every initialiser unrun (all variables read as 0).
		for _, d := range n.Decls {
			if err := c.genStmt(d); err != nil {
				return err
			}
		}
	case *DeclStmt:
		// Static locals are initialised once at load time in .data, and extern
		// locals have no storage of their own; neither needs run-time
		// initialisation, so skip the frame-storing code below.
		if n.Storage == "static" || n.Storage == "extern" {
			return nil
		}
		vi := c.vars[n.Name]
		if bi, ok := n.Init.(*BraceInit); ok {
			// A braced initialiser stores each leaf directly into its frame
			// slot and zero-fills what it leaves uncovered. A scalar target
			// ("int x = {5}") just initialises from its single element.
			if isAgg(vi.typ) || vi.typ.IsArray() {
				return c.genBraceInitLocal(vi.typ, bi, vi.off)
			}
			if len(bi.Elems) == 1 {
				if _, err := c.genExprT(bi.Elems[0].E); err != nil {
					return err
				}
				if err := c.ensureType(vi.typ.Class()); err != nil {
					return err
				}
				c.storeVar(vi)
			}
			return nil
		}
		if vi.typ.IsArray() {
			// The one array initialiser that exists is a string literal for a
			// char array: copy the bytes (plus NUL) into the stack slot and
			// zero-fill the tail of a larger array. Other array initialisers
			// were rejected by the checker and never reach codegen.
			if sl, ok := n.Init.(*StrLit); ok && vi.typ.Elem.IsChar() {
				size := c.typeWidth(vi.typ)
				copied := len(sl.Bytes) + 1
				if copied > size {
					copied = size // defensive; the checker rejects oversize
				}
				if _, err := c.genExprT(n.Init); err != nil {
					return err // rax = address of the constant in .rdata
				}
				c.emit("mov r11, rax")
				c.emit("lea r10, [rbp%+d]", vi.off)
				c.copyBytes("r10", "r11", copied)
				if size > copied {
					c.emit("add r10, %d", copied)
					c.zeroBytes("r10", size-copied)
				}
			}
			return nil
		}
		if isAgg(vi.typ) {
			// Whole-aggregate declaration: copy the initialiser's bytes (or
			// zero-fill) into the local slot. The value never enters rax.
			if n.Init != nil {
				if err := c.structSrcAddr(n.Init, c.exprType(n.Init)); err != nil {
					return err
				}
				c.emit("mov r11, r10") // r11 = source address
				c.emit("lea r10, [rbp%+d]", vi.off)
				c.copyBytes("r10", "r11", vi.typ.Size)
				c.releaseResStruct()
			} else {
				c.emit("lea r10, [rbp%+d]", vi.off)
				c.zeroBytes("r10", vi.typ.Size)
			}
			return nil
		}
		if n.Init != nil {
			if _, err := c.genExprT(n.Init); err != nil {
				return err
			}
			if err := c.ensureType(vi.typ.Class()); err != nil {
				return err
			}
			c.storeVar(vi)
		} else if vi.reg != "" {
			c.emit("xor %s, %s", vi.reg, vi.reg)
		} else {
			c.emit("mov [rbp%+d], 0", vi.off)
		}
	case *AssignStmt:
		// Whole-aggregate assignment shares the AssignExpr code path (which
		// handles both lvalue and struct-returning-call right-hand sides).
		// Statement-level assignments are normally parsed as
		// ExprStmt{AssignExpr}; this branch is a safety net.
		if isAgg(c.exprType(n.Lhs)) {
			_, err := c.genExprT(&AssignExpr{Lhs: n.Lhs, Rhs: n.Rhs})
			return err
		}
		// Fast path: a simple scalar local on the left can be stored
		// directly to its home register, with no need to compute an address.
		if id, ok := n.Lhs.(*Ident); ok {
			if vi, ok2 := c.vars[id.Name]; ok2 && vi.reg != "" {
				if _, err := c.genExprT(n.Rhs); err != nil {
					return err
				}
				if err := c.ensureType(vi.typ.Class()); err != nil {
					return err
				}
				c.storeVar(vi)
				return nil
			}
		}
		rt, err := c.genExprT(n.Rhs)
		if err != nil {
			return err
		}
		// Spill the right-hand side into a frame temporary so computing the
		// lvalue address (which may itself call functions) cannot clobber it.
		c.tmpDepth++
		rslot := c.tmpSlot(c.tmpDepth)
		if rt == TDouble {
			c.emit("movsd [rbp%+d], xmm0", rslot)
		} else {
			c.emit("mov [rbp%+d], rax", rslot)
		}
		if err := c.genLValue(n.Lhs); err != nil {
			return err
		}
		ec := c.elemClassOf(n.Lhs)
		// The temporary always holds the value at its full scalar width
		// (a float expression is carried as a double); genStoreElem narrows
		// it back when the destination is a 4-byte float.
		if ec == TDouble {
			c.emit("movsd xmm0, [rbp%+d]", rslot)
		} else {
			c.emit("mov rax, [rbp%+d]", rslot)
		}
		c.genStoreElem("r10", c.lvalueWidth(n.Lhs), ec)
		c.tmpDepth--
		return nil
	case *ExprStmt:
		if _, err := c.genExprT(n.E); err != nil {
			return err
		}
		// A discarded struct-returning call leaves its result buffer live;
		// nothing will consume it here, so release it.
		c.releaseResStruct()
	case *ReturnStmt:
		if isAgg(c.curRet) {
			// Struct/union return: copy the value through the hidden result
			// pointer into the caller's buffer. rax is never used.
			if n.E == nil {
				return fmt.Errorf("missing return value in function returning %s", c.curRet.String())
			}
			if err := c.structSrcAddr(n.E, c.exprType(n.E)); err != nil {
				return err
			}
			c.emit("mov r11, r10")                  // r11 = source address
			c.emit("mov r10, [rbp%+d]", c.sretSlot) // r10 = caller's buffer
			c.copyBytes("r10", "r11", c.curRet.Size)
			c.releaseResStruct()
			c.emitEpilogue()
			return nil
		}
		if n.E != nil {
			if _, err := c.genExprT(n.E); err != nil {
				return err
			}
			if err := c.ensureType(c.curRet.Class()); err != nil {
				return err
			}
			// A float return leaves a single in the low 32 bits of xmm0; the
			// value has been computed as a double, so narrow it on the way out.
			if c.curRet != nil && c.curRet.Kind == KFloat {
				c.emit("cvtsd2ss xmm0, xmm0")
			}
			// A _Bool return must be exactly 0 or 1 (C semantics).
			if c.curRet != nil && c.curRet.Kind == KBool {
				c.normalizeBool()
			}
		} else {
			c.emit("mov rax, 0")
		}
		c.emitEpilogue()
	case *IfStmt:
		lElse := c.newLabel("else")
		lEnd := c.newLabel("endif")
		if _, err := c.genExprT(n.Cond); err != nil {
			return err
		}
		if err := c.ensureType(TInt); err != nil {
			return err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lElse)
		if err := c.genStmt(n.Then); err != nil {
			return err
		}
		if n.Else != nil {
			c.emit("jmp %s", lEnd)
			c.line(lElse + ":\n")
			if err := c.genStmt(n.Else); err != nil {
				return err
			}
			c.line(lEnd + ":\n")
		} else {
			c.line(lElse + ":\n")
		}
	case *WhileStmt:
		lTop := c.newLabel("while")
		lEnd := c.newLabel("wend")
		c.line(lTop + ":\n")
		if _, err := c.genExprT(n.Cond); err != nil {
			return err
		}
		if err := c.ensureType(TInt); err != nil {
			return err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lEnd)
		// Register break/continue targets so a break/continue inside the body
		// (or a nested loop) resolves to this loop. continue re-checks the
		// condition at lTop.
		c.loops = append(c.loops, loopLabels{breakLbl: lEnd, contLbl: lTop})
		c.breaks = append(c.breaks, lEnd)
		bodyErr := c.genStmt(n.Body)
		c.loops = c.loops[:len(c.loops)-1]
		c.breaks = c.breaks[:len(c.breaks)-1]
		if bodyErr != nil {
			return bodyErr
		}
		c.emit("jmp %s", lTop)
		c.line(lEnd + ":\n")
	case *ForStmt:
		lTop := c.newLabel("for")
		lEnd := c.newLabel("forend")
		lCont := c.newLabel("forcont")
		if n.Init != nil {
			if err := c.genStmt(n.Init); err != nil {
				return err
			}
		}
		c.line(lTop + ":\n")
		if n.Cond != nil {
			if _, err := c.genExprT(n.Cond); err != nil {
				return err
			}
			if err := c.ensureType(TInt); err != nil {
				return err
			}
			c.emit("cmp rax, 0")
			c.emit("je %s", lEnd)
		}
		// Push this loop's break/continue targets so statements inside the
		// body (which may themselves be nested loops) can resolve them.
		c.loops = append(c.loops, loopLabels{breakLbl: lEnd, contLbl: lCont})
		c.breaks = append(c.breaks, lEnd)
		bodyErr := c.genStmt(n.Body)
		c.loops = c.loops[:len(c.loops)-1]
		c.breaks = c.breaks[:len(c.breaks)-1]
		if bodyErr != nil {
			return bodyErr
		}
		c.line(lCont + ":\n")
		if n.Post != nil {
			if _, err := c.genExprT(n.Post); err != nil {
				return err
			}
		}
		c.emit("jmp %s", lTop)
		c.line(lEnd + ":\n")
	case *DoWhileStmt:
		// "do body while (cond);": the body runs first, so the condition is
		// tested at the bottom. continue jumps to that test, not to the body.
		lTop := c.newLabel("do")
		lCont := c.newLabel("docont")
		lEnd := c.newLabel("doend")
		c.line(lTop + ":\n")
		c.loops = append(c.loops, loopLabels{breakLbl: lEnd, contLbl: lCont})
		c.breaks = append(c.breaks, lEnd)
		bodyErr := c.genStmt(n.Body)
		c.loops = c.loops[:len(c.loops)-1]
		c.breaks = c.breaks[:len(c.breaks)-1]
		if bodyErr != nil {
			return bodyErr
		}
		c.line(lCont + ":\n")
		if _, err := c.genExprT(n.Cond); err != nil {
			return err
		}
		if err := c.ensureType(TInt); err != nil {
			return err
		}
		c.emit("cmp rax, 0")
		c.emit("jne %s", lTop)
		c.line(lEnd + ":\n")
	case *SwitchStmt:
		if err := c.genSwitch(n); err != nil {
			return err
		}
	case *GotoStmt:
		c.emit("jmp %s", c.labelSym(n.Label))
	case *LabelStmt:
		c.line(c.labelSym(n.Name) + ":\n")
		if n.Stmt != nil {
			return c.genStmt(n.Stmt)
		}
	case *AsmStmt:
		// Inline assembly: the raw block text goes to goa almost verbatim.
		// Each line first passes through the binder (bindAsmLine), which
		// rewrites every bare C variable name -- local, parameter, global
		// or static local -- into the memory operand that addresses it, so
		// hand-written asm can read and write C variables directly. goa
		// trims each line and skips blanks, so empty lines from the block
		// (after '{' / before '}') are harmless.
		for _, ln := range strings.Split(n.Text, "\n") {
			c.line(c.bindAsmLine(ln) + "\n")
		}
	case *BreakStmt:
		// break binds to the innermost enclosing loop *or* switch; the
		// separate stack keeps a switch from stealing a loop's continue.
		if len(c.breaks) == 0 {
			return fmt.Errorf("break outside a loop or switch")
		}
		c.emit("jmp %s", c.breaks[len(c.breaks)-1])
	case *ContinueStmt:
		if len(c.loops) == 0 {
			return fmt.Errorf("continue outside a loop")
		}
		c.emit("jmp %s", c.loops[len(c.loops)-1].contLbl)
	case *CaseStmt:
		return fmt.Errorf("line %d: case label outside a switch", n.Line)
	case *DefaultStmt:
		return fmt.Errorf("line %d: default label outside a switch", n.Line)
	}
	return nil
}

// asmReserved is every word the inline-asm binder must never rewrite into a
// C variable's memory operand: goa's register names (GP 8/16/32/64-bit, XMM,
// plus the rip pseudo-register), the NASM-style size keywords goa strips
// before parsing an operand, and the instruction/data mnemonics goa's encode
// table accepts. A C variable that collides with one of these cannot be
// named directly inside an asm block. The list mirrors src/goa/asm.go
// (register tables around line 149, encode table around line 924); keep the
// two in sync.
var asmReserved = func() map[string]bool {
	m := map[string]bool{
		// 64-bit GP registers
		"rax": true, "rcx": true, "rdx": true, "rbx": true, "rsp": true,
		"rbp": true, "rsi": true, "rdi": true,
		"r8": true, "r9": true, "r10": true, "r11": true,
		"r12": true, "r13": true, "r14": true, "r15": true,
		// 32-bit GP registers
		"eax": true, "ecx": true, "edx": true, "ebx": true, "esp": true,
		"ebp": true, "esi": true, "edi": true,
		"r8d": true, "r9d": true, "r10d": true, "r11d": true,
		"r12d": true, "r13d": true, "r14d": true, "r15d": true,
		// 16-bit GP registers
		"ax": true, "cx": true, "dx": true, "bx": true, "sp": true,
		"bp": true, "si": true, "di": true,
		"r8w": true, "r9w": true, "r10w": true, "r11w": true,
		"r12w": true, "r13w": true, "r14w": true, "r15w": true,
		// 8-bit GP registers
		"al": true, "cl": true, "dl": true, "bl": true, "spl": true,
		"bpl": true, "sil": true, "dil": true,
		"r8b": true, "r9b": true, "r10b": true, "r11b": true,
		"r12b": true, "r13b": true, "r14b": true, "r15b": true,
		// XMM registers
		"xmm0": true, "xmm1": true, "xmm2": true, "xmm3": true,
		"xmm4": true, "xmm5": true, "xmm6": true, "xmm7": true,
		"xmm8": true, "xmm9": true, "xmm10": true, "xmm11": true,
		"xmm12": true, "xmm13": true, "xmm14": true, "xmm15": true,
		// pseudo-register and operand size keywords
		"rip": true, "ptr": true, "byte": true, "word": true,
		"dword": true, "qword": true,
		// data directives
		"db": true, "dq": true, "du": true,
		// control flow
		"ret": true, "retq": true, "retn": true, "leave": true,
		"nop": true, "int3": true, "push": true, "pop": true,
		"call": true, "jmp": true, "jrcxz": true, "jecxz": true,
		// data movement / arithmetic / logic
		"lea": true, "mov": true, "add": true, "sub": true, "and": true,
		"or": true, "xor": true, "cmp": true, "test": true,
		"adc": true, "sbb": true, // carry/borrow forms for multi-word math
		"not": true, "mul": true,
		"xchg": true, "bswap": true,
		"bt": true, "bts": true, "btr": true, "btc": true, // bit test family
		"movzx": true, "movsx": true, "movsxd": true, "movslq": true,
		"movzbl": true, "movzbw": true, "movzbq": true,
		"movzwl": true, "movzwq": true,
		"movsbl": true, "movsbw": true, "movsbq": true,
		"movswl": true, "movswq": true,
		"inc": true, "dec": true, "neg": true,
		"idiv": true, "div": true, "imul": true,
		"shl": true, "sal": true, "shr": true, "sar": true,
		"rol": true, "ror": true, "rcl": true, "rcr": true, // rotations
		"cqo": true, "cqto": true, "cdq": true, "cltd": true,
		"cdqe": true, "cltq": true, "cwde": true, "cwtl": true,
		"syscall": true,
		// processor control / serialisation
		"pause": true, "hlt": true, "ud2": true,
		"cpuid": true, "rdtsc": true,
		"mfence": true, "lfence": true, "sfence": true,
		// SSE
		"movsd": true, "movss": true, "addsd": true, "subsd": true,
		"mulsd": true, "divsd": true, "sqtsd": true, "xorpd": true,
		"ucomisd": true, "cvtsi2sd": true, "cvttsd2si": true,
		"cvtss2sd": true, "cvtsd2ss": true, "movq": true,
	}
	// The j<cc> / set<cc> / cmov<cc> families are generated from ONE spelling
	// list rather than written out three times, so adding a condition code
	// cannot leave one of the families unprotected. Keep this list identical
	// to ccTable + ccAlias in src/goa/asm.go.
	ccSpellings := []string{
		// canonical codes
		"o", "no", "b", "ae", "e", "ne", "be", "a",
		"s", "ns", "p", "np", "l", "ge", "le", "g",
		// classic synonyms
		"z", "nz", "c", "nae", "nb", "nc", "na", "nbe",
		"pe", "po", "nge", "nl", "ng", "nle",
	}
	for _, prefix := range []string{"j", "set", "cmov"} {
		for _, cc := range ccSpellings {
			m[prefix+cc] = true
		}
	}
	return m
}()

func isAsmIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isAsmIdentByte(b byte) bool {
	return isAsmIdentStart(b) || (b >= '0' && b <= '9')
}

// bindAsmLine rewrites one line of inline-assembly text so that every bare C
// variable name becomes the memory operand that addresses it:
//
//	locals and parameters (c.vars)  -> [rbp+off]
//	globals and static locals       -> [rip+G_x]   (label via c.globalLab)
//
// Registers, mnemonics, numeric literals (0x10, 123), .L local labels and
// anything inside a string literal or comment are left untouched. The scan
// mirrors goa's stripComment: ';' and "//" end the line (quote-aware), and a
// backslash inside a string escapes the next character. A variable already
// sitting inside square brackets is rewritten to its bare form (G_x or
// rbp+off) so "[x]" does not double-wrap into "[ [rip+G_x] ]"; a word
// followed by ':' (a label definition) or '[' (an array-style reference
// like "x[0]") is never rewritten.
func (c *CG) bindAsmLine(ln string) string {
	t := strings.TrimLeft(ln, " \t")
	if strings.HasPrefix(t, "#") {
		return ln // goa treats a leading '#' as a comment too
	}
	var out strings.Builder
	inBracket := false
	for i := 0; i < len(ln); {
		ch := ln[i]
		if ch == '"' || ch == '\'' {
			// Copy the whole string literal verbatim (backslash escapes the
			// next character), exactly like goa's stripComment.
			j := i + 1
			for j < len(ln) {
				if ln[j] == '\\' && j+1 < len(ln) {
					j += 2
					continue
				}
				if ln[j] == ch {
					j++
					break
				}
				j++
			}
			out.WriteString(ln[i:j])
			i = j
			continue
		}
		if ch == ';' || (ch == '/' && i+1 < len(ln) && ln[i+1] == '/') {
			out.WriteString(ln[i:]) // comment runs to end of line
			break
		}
		if ch == '[' {
			inBracket = true
			out.WriteByte(ch)
			i++
			continue
		}
		if ch == ']' {
			inBracket = false
			out.WriteByte(ch)
			i++
			continue
		}
		if isAsmIdentStart(ch) || (ch >= '0' && ch <= '9') {
			j := i + 1
			for j < len(ln) && isAsmIdentByte(ln[j]) {
				j++
			}
			word := ln[i:j]
			if ch >= '0' && ch <= '9' {
				out.WriteString(word) // numeric literal (0x10, 123, ...)
				i = j
				continue
			}
			// A label definition ("foo:") or array-style reference
			// ("x[0]") is not a plain variable operand.
			if j < len(ln) && (ln[j] == ':' || ln[j] == '[') {
				out.WriteString(word)
				i = j
				continue
			}
			out.WriteString(c.asmBindWord(word, inBracket))
			i = j
			continue
		}
		out.WriteByte(ch)
		i++
	}
	return out.String()
}

// asmBindWord maps one candidate word to the memory operand that addresses
// it, or returns the word unchanged when it is not a C variable (or is a
// reserved asm word). inBracket selects the inside-of-[...] form (bare
// symbol G_x / register base rbp+off) versus the full memory operand
// ([rip+G_x] / [rbp+off]).
func (c *CG) asmBindWord(w string, inBracket bool) string {
	if asmReserved[w] {
		return w
	}
	if vi, ok := c.vars[w]; ok {
		// vi.off is always set in a function that contains an asm block
		// (hasAsm forces every local onto the stack); the register-home
		// branch is a defensive no-op.
		if vi.reg != "" {
			return w
		}
		if inBracket {
			return fmt.Sprintf("rbp%+d", vi.off)
		}
		return fmt.Sprintf("[rbp%+d]", vi.off)
	}
	if lab, ok := c.globalLab[w]; ok {
		if inBracket {
			return lab
		}
		return "[rip+" + lab + "]"
	}
	return w
}

// labelSym returns the assembly label for a C label, creating one on first
// use. Goto may jump forward to a label that has not been emitted yet, so the
// name has to be decided by whoever needs it first -- hence the cache.
func (c *CG) labelSym(name string) string {
	if s, ok := c.labels[name]; ok {
		return s
	}
	s := c.newLabel("lbl_" + name)
	c.labels[name] = s
	return s
}

// swGroup is one arm of a switch: either "case N:" or "default:", plus the
// statements that follow it up to the next label.
type swGroup struct {
	lbl       string
	val       int
	isDefault bool
	stmts     []Stmt
}

// genSwitch lowers a switch to a chain of comparisons followed by the case
// bodies laid out in source order:
//
//	mov rax, [switch value]
//	cmp rax, V0 ; je .Lcase1
//	cmp rax, V1 ; je .Lcase2
//	jmp .Ldefault            (or .Lswend when there is no default)
//	.Lcase1:  <body 0>       <- falls through into .Lcase2, as C requires
//	.Lcase2:  <body 1>
//	.Ldefault: <body>
//	.Lswend:
//
// The controlling expression is evaluated once into a dedicated frame slot
// (swSlot), so re-evaluating it per comparison -- which would duplicate side
// effects -- is never necessary and expression temporaries cannot clobber it.
func (c *CG) genSwitch(n *SwitchStmt) error {
	if n.Body == nil {
		return nil
	}
	c.swDepth++
	if c.swDepth > c.swSlots {
		c.swDepth--
		return fmt.Errorf("switch nested more than %d deep", c.swSlots)
	}
	slot := c.swSlot(c.swDepth)
	if _, err := c.genExprT(n.Src); err != nil {
		c.swDepth--
		return err
	}
	if err := c.ensureType(TInt); err != nil {
		c.swDepth--
		return err
	}
	c.emit("mov [rbp%+d], rax", slot)

	var groups []swGroup
	defIdx := -1
	for _, st := range n.Body.Stmts {
		switch cs := st.(type) {
		case *CaseStmt:
			groups = append(groups, swGroup{lbl: c.newLabel("case"), val: cs.Val})
		case *DefaultStmt:
			defIdx = len(groups)
			groups = append(groups, swGroup{lbl: c.newLabel("dflt"), isDefault: true})
		default:
			// Statements before the first label are unreachable; C accepts
			// them, so they are simply dropped rather than emitted.
			if len(groups) == 0 {
				continue
			}
			groups[len(groups)-1].stmts = append(groups[len(groups)-1].stmts, st)
		}
	}

	lEnd := c.newLabel("swend")
	c.emit("mov rax, [rbp%+d]", slot)
	for _, g := range groups {
		if g.isDefault {
			continue
		}
		c.emit("cmp rax, %d", g.val)
		c.emit("je %s", g.lbl)
	}
	if defIdx >= 0 {
		c.emit("jmp %s", groups[defIdx].lbl)
	} else {
		c.emit("jmp %s", lEnd)
	}

	// Bodies. break inside any arm targets lEnd; continue still targets the
	// enclosing loop, because a switch is not a loop.
	c.breaks = append(c.breaks, lEnd)
	var err error
	for _, g := range groups {
		c.line(g.lbl + ":\n")
		for _, st := range g.stmts {
			if err = c.genStmt(st); err != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	c.breaks = c.breaks[:len(c.breaks)-1]
	c.line(lEnd + ":\n")
	c.swDepth--
	return err
}

func (c *CG) genUnary(n *Unary) (CType, error) {
	switch n.Op {
	case "&":
		// &*p is just p (the pointer value); everything else is a real lvalue.
		if inner, ok := n.E.(*Unary); ok && inner.Op == "*" {
			if _, err := c.genExprT(inner.E); err != nil {
				return TInt, err
			}
		} else {
			if err := c.genLValue(n.E); err != nil {
				return TInt, err
			}
			c.emit("mov rax, r10")
		}
		c.resTyp = TInt
		c.resSigned = false
		c.resW = 8 // address-of yields a pointer value
		return TInt, nil
	case "*":
		// Dereference: n.E evaluates to the pointer value (in rax).
		if _, err := c.genExprT(n.E); err != nil {
			return TInt, err
		}
		// Dereferencing a pointer to FUNCTION yields the function designator,
		// whose value IS that same address -- there is nothing to load. This
		// is what makes "(*fp)(x)" branch to the same place as "fp(x)".
		if t := c.exprType(n.E); t != nil && t.IsPtr() && t.Elem != nil && t.Elem.IsFunc() {
			c.resTyp = TInt
			c.resSigned = false
			c.resW = 8
			return TInt, nil
		}
		ec := c.elemClassOf(n.E)
		width := c.elemWidthOf(n.E)
		signed := c.elemSignedOf(n.E)
		c.genLoadElem("rax", width, ec, signed)
		return c.resTyp, nil
	}
	t, err := c.genExprT(n.E)
	if err != nil {
		return TInt, err
	}
	if n.Op == "-" {
		if t == TDouble {
			// xmm0 = -xmm0  => 0.0 - xmm0
			c.emit("xorpd xmm1, xmm1")
			c.emit("subsd xmm1, xmm0")
			c.emit("movsd xmm0, xmm1")
		} else {
			c.emit("neg rax")
			// Integer promotion: a narrow operand (char/short) negates as a
			// signed int; an int keeps its own signedness; an 8-byte value
			// (long/unsigned long) keeps the full 64-bit result.
			w, s := c.resW, c.resSigned
			if w < 4 {
				w, s = 4, true
			}
			c.resW = w
			if w == 4 {
				c.canonInt(s)
			}
		}
		return t, nil
	}
	// "!" -- force the operand to int, then rax = (rax == 0)
	if err := c.ensureType(TInt); err != nil {
		return TInt, err
	}
	lTrue := c.newLabel("nott")
	lEnd := c.newLabel("note")
	c.emit("cmp rax, 0")
	c.emit("je %s", lTrue)
	c.emit("mov rax, 0")
	c.emit("jmp %s", lEnd)
	c.line(lTrue + ":\n")
	c.emit("mov rax, 1")
	c.line(lEnd + ":\n")
	c.resTyp = TInt
	c.resSigned = true
	c.resW = 4 // !x yields a (signed) int
	return TInt, nil
}

// genLValue emits code that leaves the address of the lvalue e in r10.
func (c *CG) genLValue(e Expr) error {
	// Most lvalues are not bit-fields; the MemberExpr branch below re-arms
	// these when it addresses one.
	c.lvBitWidth, c.lvBitOff, c.lvBitUnit, c.lvBitSigned = 0, 0, 0, false
	switch n := e.(type) {
	case *Ident:
		// Address of a function designator: "&add" is just another spelling of
		// "add". Both yield the function's address.
		if sym, ok := c.funcAddrSym(n.Name); ok {
			c.emit("lea r10, [rip+%s]", sym)
			return nil
		}
		vi, ok := c.vars[n.Name]
		if !ok {
			if lab, ok2 := c.staticVars[n.Name]; ok2 {
				// Address of a static local: rip-relative lea into .data.
				c.emit("lea r10, [rip+%s]", lab)
				return nil
			}
			if c.globals[n.Name] {
				// Address of a global: rip-relative lea into .data.
				c.useLibGlobal(n.Name)
				c.emit("lea r10, [rip+%s]", c.globalLab[n.Name])
				return nil
			}
			return fmt.Errorf("undefined variable %q", n.Name)
		}
		if vi.reg != "" {
			// Reached only if a register-cached local had its address taken,
			// which findAddressTaken should have prevented.
			return fmt.Errorf("cannot take address of register-allocated variable %q", n.Name)
		}
		c.emit("lea r10, [rbp%+d]", vi.off)
		return nil
	case *Unary:
		if n.Op != "*" {
			return fmt.Errorf("expression is not an lvalue")
		}
		// The address of *p is simply the pointer value p holds.
		if _, err := c.genExprT(n.E); err != nil {
			return err
		}
		c.emit("mov r10, rax")
		return nil
	case *Index:
		// Element address = base_address + index*8. Compute the index first
		// and spill it, because evaluating the base may call a function and
		// clobber the volatile registers.
		if _, err := c.genExprT(n.Idx); err != nil {
			return err
		}
		c.tmpDepth++
		islot := c.tmpSlot(c.tmpDepth)
		c.emit("mov [rbp%+d], rax", islot)
		if id, ok := n.Base.(*Ident); ok {
			vi, ok2 := c.vars[id.Name]
			if !ok2 {
				if lab, ok3 := c.staticVars[id.Name]; ok3 {
					// Static-local array: rip-relative lea of element 0.
					gt := c.globalTyp[lab]
					if gt == nil || !gt.IsArray() {
						return fmt.Errorf("cannot index non-array static local %q", id.Name)
					}
					c.emit("lea r10, [rip+%s]", lab)
				} else if c.globals[id.Name] {
					// Global array: rip-relative lea of element 0.
					gt := c.globalTyp[id.Name]
					if gt == nil || !gt.IsArray() {
						return fmt.Errorf("cannot index non-array global %q", id.Name)
					}
					c.useLibGlobal(id.Name)
					c.emit("lea r10, [rip+%s]", c.globalLab[id.Name])
				} else {
					return fmt.Errorf("undefined variable %q", id.Name)
				}
			} else if vi.typ.IsArray() {
				c.emit("lea r10, [rbp%+d]", vi.off)
			} else {
				c.loadVar(vi) // rax = pointer value
				c.emit("mov r10, rax")
			}
		} else if u, ok := n.Base.(*Unary); ok && u.Op == "*" {
			if _, err := c.genExprT(u.E); err != nil {
				return err
			}
			c.emit("mov r10, rax")
		} else if ix, ok := n.Base.(*Index); ok {
			if err := c.genLValue(ix); err != nil {
				return err
			}
			// r10 already holds the base element address
		} else if bt := c.exprType(n.Base); bt != nil && bt.IsArray() {
			// The base is an array lvalue (a struct member array, e.g.
			// "mx.name[i]"): it decays to the address of its element 0, so
			// its ADDRESS is the base -- loading its value would read the
			// array bytes as a garbage pointer and crash the store.
			if err := c.genLValue(n.Base); err != nil {
				return err
			}
		} else {
			if _, err := c.genExprT(n.Base); err != nil {
				return err
			}
			c.emit("mov r10, rax")
		}
		c.emit("mov r11, [rbp%+d]", islot)
		// byte offset = index * element_width. Char elements pack one byte
		// per slot (string literals, char arrays, char* buffers); everything
		// else keeps the 8-byte slot stride. goa supports imul-with-immediate,
		// so a single scaled multiply replaces the old triple doubling-add.
		ew := c.elemWidthOf(n.Base)
		c.emit("imul r11, %d", ew)
		c.emit("add r10, r11")
		c.tmpDepth--
		return nil
	case *MemberExpr:
		// Resolve the struct/union type behind the base so we can look the
		// member offset up. For "->" the base is a pointer to struct.
		var st *Type
		if n.Arrow {
			if _, err := c.genExprT(n.Base); err != nil {
				return err
			}
			c.emit("mov r10, rax")
			if bt := c.exprType(n.Base); bt != nil && bt.Elem != nil {
				st = bt.Elem
			}
		} else {
			if bt := c.exprType(n.Base); isAgg(bt) {
				// The base is a whole struct VALUE rather than an lvalue --
				// typically a call returning a struct ("mkpt(1,2).x"), whose
				// bytes live in a result buffer. Take its address from there
				// and leave the buffer claimed; the statement that encloses
				// this expression releases it.
				if err := c.structSrcAddr(n.Base, bt); err != nil {
					return err
				}
			} else if err := c.genLValue(n.Base); err != nil {
				return err
			}
			st = c.exprType(n.Base)
		}
		if st == nil || (st.Kind != KStruct && st.Kind != KUnion) {
			return fmt.Errorf("member access %q on non-struct type", n.Name)
		}
		off := 0
		found := false
		for _, m := range st.Members {
			if m.Name == n.Name {
				off = m.Offset
				if m.BitWidth > 0 {
					// A bit-field member: r10 points at the START of its
					// storage unit after the add below; the field lives at
					// bit offset BitOff inside it. Consumers (MemberExpr
					// loads, assignment stores, inc/dec) branch on
					// lvBitWidth and use the bit address, not a plain load.
					c.lvBitWidth = m.BitWidth
					c.lvBitOff = m.BitOff
					c.lvBitUnit = sizeOf(m.Type)
					c.lvBitSigned = m.Type.Kind == KInt && m.Type.Signed
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no member %q in %s", n.Name, st.String())
		}
		c.emit("add r10, %d", off)
		return nil
	}
	return fmt.Errorf("expression is not an lvalue")
}

// elemClassOf returns the codegen class (int vs double) of the value obtained
// by dereferencing / indexing e. It inspects the structured type behind local
// variables and the shape of nested dereferences / subscripts.
func (c *CG) elemClassOf(e Expr) CType {
	switch n := e.(type) {
	case *Ident:
		vi, ok := c.vars[n.Name]
		if !ok {
			// A static local shadows a same-named global; its type lives
			// under its .data label, not the source name.
			if lab, ok2 := c.staticVars[n.Name]; ok2 {
				if gt := c.globalTyp[lab]; gt != nil {
					if gt.Kind == KDouble {
						return TDouble
					}
					if gt.IsPtr() && gt.Elem != nil {
						return gt.Elem.Class()
					}
					if gt.IsArray() && gt.Elem != nil {
						return gt.Elem.Class()
					}
				}
				return TInt
			}
			if gt := c.globalTyp[n.Name]; gt != nil {
				if gt.Kind == KDouble {
					return TDouble
				}
				if gt.IsPtr() && gt.Elem != nil {
					return gt.Elem.Class()
				}
				if gt.IsArray() && gt.Elem != nil {
					return gt.Elem.Class()
				}
			}
			return TInt
		}
		if vi.typ.Kind == KDouble {
			return TDouble
		}
		if vi.typ.IsPtr() && vi.typ.Elem != nil {
			return vi.typ.Elem.Class()
		}
		if vi.typ.IsArray() && vi.typ.Elem != nil {
			return vi.typ.Elem.Class()
		}
		return TInt
	case *Unary:
		if n.Op == "*" {
			return c.elemClassOf(n.E)
		}
		return TInt
	case *Index:
		// Mirror elemWidthOf: derive the class from this expression's OWN
		// type. Recursing into n.Base reports the outer array's element class,
		// which for "double d[2][2]" is double[2] -- an int-class aggregate --
		// so d[i][j] was loaded with mov instead of movsd and read as 0.0.
		if t := c.exprType(n); t != nil {
			if (t.IsPtr() || t.IsArray()) && t.Elem != nil {
				return t.Elem.Class()
			}
			return t.Class()
		}
		return TInt
	case *MemberExpr:
		if mt := c.memberType(n.Base, n.Name); mt != nil {
			return mt.Class()
		}
		return TInt
	case *IncDecExpr:
		return c.elemClassOf(n.E)
	}
	return TInt
}

// typeWidth returns the byte width used to lay out / step over a value of type
// t: char=1, short=2, int/long=4/8, double/pointer=8, array=element*len,
// struct/union=its computed Size.
func (c *CG) typeWidth(t *Type) int {
	if t == nil {
		return 8
	}
	switch t.Kind {
	case KPtr, KFunc:
		return 8
	case KFloat:
		return 4
	case KDouble:
		return 8
	case KArr:
		if t.Elem != nil {
			return c.typeWidth(t.Elem) * t.Len
		}
		return 8
	case KInt:
		if t.Width < 1 {
			return 1
		}
		return t.Width
	case KBool:
		return 1
	case KStruct, KUnion:
		if t.Size != 0 {
			return t.Size
		}
		return 8
	}
	return 8
}

// exprIsPointer reports whether e has pointer type.
func (c *CG) exprIsPointer(e Expr) bool {
	t := c.exprType(e)
	if t == nil {
		return false
	}
	return t.IsPtr()
}

// memberType resolves the type of a struct/union member accessed as
// base.name. base may itself be a struct value (`.`) or a pointer to struct
// (`->`); in the latter case exprType(base) is the pointer and we dereference
// its Elem. Returns nil if base is not a struct/union or the member is absent.
func (c *CG) memberType(base Expr, name string) *Type {
	t := c.exprType(base)
	if t != nil && t.Kind == KPtr && t.Elem != nil {
		t = t.Elem
	}
	if t == nil || (t.Kind != KStruct && t.Kind != KUnion) {
		return nil
	}
	for _, m := range t.Members {
		if m.Name == name {
			return m.Type
		}
	}
	return nil
}

// exprType attempts to recover the structured type of an expression from the
// variable table / declarators the code generator already knows about.
func (c *CG) exprType(e Expr) *Type {
	switch n := e.(type) {
	case *Ident:
		if vi, ok := c.vars[n.Name]; ok {
			return vi.typ
		}
		// A static local's type is registered under its .data label, not its
		// source name (which may even collide with a file-scope global).
		if lab, ok := c.staticVars[n.Name]; ok {
			return c.globalTyp[lab]
		}
		// A function designator decays to a pointer to that function.
		if fd, ok := c.funcDefs[n.Name]; ok {
			ft := FuncType(fd.Ret, fd.ParamTypes)
			ft.Variadic = fd.Variadic
			return PtrType(ft)
		}
		return c.globalTyp[n.Name]
	case *Unary:
		if n.Op == "*" {
			if t := c.exprType(n.E); t != nil && t.IsPtr() {
				return t.Elem
			}
		}
	case *Index:
		if t := c.exprType(n.Base); t != nil {
			if t.IsPtr() || t.IsArray() {
				return t.Elem
			}
		}
	case *IncDecExpr:
		return c.exprType(n.E)
	case *MemberExpr:
		return c.memberType(n.Base, n.Name)
	case *CastExpr:
		return n.Typ
	case *Call:
		// A call's type is the callee's declared return type (nil for
		// goclib / extern calls, which return int). This is how struct-
		// returning calls are recognised at argument / assignment / return
		// positions so their result buffer can be consumed by address.
		if fd, ok := c.funcDefs[n.Name]; ok {
			return fd.Ret
		}
		// The name may designate a function-pointer VARIABLE rather than a
		// function: its result type comes from the pointer's static type.
		if _, ft, ok := c.fnPtrVar(n.Name); ok {
			return ft.Ret
		}
		return nil
	case *IndirectCall:
		if ft := funcTypeOf(c.exprType(n.Fn)); ft != nil {
			return ft.Ret
		}
		return nil
	}
	return nil
}

// elemWidthOf returns the byte width of the element referenced by a pointer or
// array expression e (1 for char, 2 for short, 4 for int, 8 otherwise). For a
// bare scalar (the value itself, not a container) it returns that scalar's
// storage width -- but the common use is indexing a pointer/array, where the
// stride must be the *element* width, never the 8-byte pointer width.
func (c *CG) elemWidthOf(e Expr) int {
	switch n := e.(type) {
	case *Ident:
		vi, ok := c.vars[n.Name]
		if !ok {
			// A static local shadows a same-named global; its type lives
			// under its .data label, not the source name.
			if lab, ok2 := c.staticVars[n.Name]; ok2 {
				gt := c.globalTyp[lab]
				if gt == nil {
					return 8
				}
				if (gt.IsPtr() || gt.IsArray()) && gt.Elem != nil {
					return c.typeWidth(gt.Elem)
				}
				return c.typeWidth(gt)
			}
			gt := c.globalTyp[n.Name]
			if gt == nil {
				return 8
			}
			if (gt.IsPtr() || gt.IsArray()) && gt.Elem != nil {
				return c.typeWidth(gt.Elem)
			}
			return c.typeWidth(gt)
		}
		if vi.typ != nil && (vi.typ.IsPtr() || vi.typ.IsArray()) && vi.typ.Elem != nil {
			return c.typeWidth(vi.typ.Elem)
		}
		return c.typeWidth(vi.typ)
	case *Unary:
		if n.Op == "*" {
			// The stride when subscripting *p: the width of what p points at.
			// If p points at an array (int (*)[N]), then (*p)[i] steps by that
			// array's ELEMENT width, not the array's own size -- so a pointer
			// to a 5-int array indexes at 4-byte strides, not 20.
			if t := c.exprType(n.E); t != nil && t.IsPtr() && t.Elem != nil {
				if t.Elem.IsArray() && t.Elem.Elem != nil {
					return c.typeWidth(t.Elem.Elem)
				}
				return c.typeWidth(t.Elem)
			}
			return c.elemWidthOf(n.E)
		}
		return 8
	case *Index:
		// "m[i]" has m's ELEMENT type, and it is that type's element width
		// which strides the next subscript: for "int m[2][3]", m[i] is int[3]
		// and m[i][j] steps 4 bytes, while m[i] itself steps 12. Recursing
		// into n.Base would reuse m's own stride for both levels and make
		// every m[i][j] address wrong.
		if t := c.exprType(n); t != nil {
			// Still subscriptable (int[3], char*, ...): the next subscript
			// strides this type's element.
			if (t.IsPtr() || t.IsArray()) && t.Elem != nil {
				return c.typeWidth(t.Elem)
			}
			// Fully subscripted: the value itself, at its layout width
			// (int is packed 4 bytes inside an array).
			return c.typeWidth(t)
		}
		return 8
	case *MemberExpr:
		if mt := c.memberType(n.Base, n.Name); mt != nil {
			if (mt.IsPtr() || mt.IsArray()) && mt.Elem != nil {
				return c.typeWidth(mt.Elem)
			}
			// A scalar member (int/long/char/short) is stepped over and
			// loaded/stored at its *layout* width (MSVC x64 packs int as 4
			// bytes), not the 8-byte scalar slot width.
			return c.typeWidth(mt)
		}
		return 8
	case *IncDecExpr:
		return c.elemWidthOf(n.E)
	case *CastExpr:
		return c.typeWidth(n.Typ)
	}
	return 8
}

// ptrElemWidth returns the byte stride of the element a pointer points at
// (used to scale pointer arithmetic). A void pointer strides one byte. An
// array used as a value has decayed to a pointer to element 0, so its stride
// is the array element width too.
func (c *CG) ptrElemWidth(t *Type) int {
	if t != nil && t.IsPtr() && t.Elem != nil {
		return c.typeWidth(t.Elem)
	}
	if t != nil && t.IsArray() && t.Elem != nil {
		return c.typeWidth(t.Elem)
	}
	return 1
}

// elemSignedOf returns whether the element referenced by a pointer/array e has
// a signed integer type (false for unsigned / double / pointer elements).
func (c *CG) elemSignedOf(e Expr) bool {
	t := c.exprType(e)
	if t == nil {
		return false
	}
	if t.IsPtr() && t.Elem != nil {
		return t.Elem.Kind == KInt && t.Elem.Signed
	}
	if t.IsArray() && t.Elem != nil {
		return t.Elem.Kind == KInt && t.Elem.Signed
	}
	if t.Kind == KInt {
		return t.Signed
	}
	return false
}

// genLoadElem emits code that loads the value at the address held in reg into
// rax (int-class, width-aware) or xmm0 (double). For a 1-byte element the byte
// is zero- or sign-extended into rax depending on signedness.
func (c *CG) genLoadElem(reg string, width int, class CType, signed bool) {
	if class == TDouble {
		if width == 4 {
			// A float element/member: 4 bytes on the wire, widened to the
			// double every expression carries.
			c.emit("movss xmm0, [%s]", reg)
			c.emit("cvtss2sd xmm0, xmm0")
		} else {
			c.emit("movsd xmm0, [%s]", reg)
		}
		c.resTyp = TDouble
		c.resSigned = false
		c.resW = 8
		return
	}
	switch width {
	case 1:
		// Load the low byte, then zero- or sign-extend in rax. We must not
		// clear rax before the load, because reg holds the address; "mov al"
		// writes only the low byte, so the address survives in the upper bits
		// until the "and" clears them.
		c.emit("mov al, [%s]", reg)
		c.extendInt(1, signed)
		c.resW = 1
	case 2:
		c.emit("mov ax, [%s]", reg) // 16-bit load, zero-extended to 32 by the CPU
		c.extendInt(2, signed)
		c.resW = 2
	case 4:
		c.emit("mov eax, [%s]", reg)
		c.extendInt(4, signed)
		c.resW = 4
	default:
		c.emit("mov rax, [%s]", reg)
		c.resW = 8
	}
	c.resTyp = TInt
	c.resSigned = signed
}

// genStoreElem emits code that stores the value currently in rax (int-class) or
// xmm0 (double) into the address held in reg, honouring the element width.
func (c *CG) genStoreElem(reg string, width int, class CType) {
	if class == TDouble {
		if width == 4 {
			// Float destination: round the double in xmm0 to a single and
			// store only its 4 bytes, so adjacent elements survive.
			c.emit("cvtsd2ss xmm0, xmm0")
			c.emit("movss [%s], xmm0", reg)
		} else {
			c.emit("movsd [%s], xmm0", reg)
		}
		return
	}
	switch width {
	case 1:
		c.emit("mov byte [%s], al", reg)
	case 2:
		c.emit("mov word [%s], rax", reg)
	case 4:
		c.emit("mov dword [%s], eax", reg)
	default:
		c.emit("mov [%s], rax", reg)
	}
}

// genLoadBitfield loads the bit-field whose storage unit starts at [r10] into
// rax, zero- or sign-extended to 64 bits. unitW is the storage-unit width in
// bytes, bitOff the field's bit offset inside it, bitW the field width. The
// extract is: load the whole unit, shift the field down, then clear the bits
// above it (a shift-out-and-back doubles as the mask, because goa has no
// movzx and no easy large immediates; the final shl/sar or shl/shr also
// performs the sign/zero extension).
func (c *CG) genLoadBitfield(unitW, bitOff, bitW int, signed bool) {
	c.genLoadElem("r10", unitW, TInt, false) // rax = zero-extended storage unit
	c.emit("shr rax, %d", bitOff)            // field now in the low bitW bits
	if signed {
		// Sign-extend: move the field to the top, then arithmetic-shift back.
		c.emit("shl rax, %d", 64-bitW)
		c.emit("sar rax, %d", 64-bitW)
	} else {
		// Zero-extend: shift the field to the top and back.
		c.emit("shl rax, %d", 64-bitW)
		c.emit("shr rax, %d", 64-bitW)
	}
	c.resTyp = TInt
	c.resSigned = signed
	c.resW = 8
}

// genStoreBitfield writes the new bit-field value (currently in rax) into the
// bit-field whose storage unit starts at [r10], preserving all other bits of
// the unit (read-modify-write: clear the field's bits, OR in the positioned
// new value, store the unit back). The new value is truncated to bitW bits,
// matching C's "the value is reduced modulo 2^bitW" rule. r11/rdx are scratch.
//
// On return rax holds the truncated field value (sign-extended when signed),
// which is the C result of the enclosing assignment/inc-dec expression -- the
// value stored, not the whole unit. The caller's resTyp/resSigned/resW state
// is preserved.
func (c *CG) genStoreBitfield(unitW, bitOff, bitW int, signed bool) {
	saveTyp, saveSgn, saveW := c.resTyp, c.resSigned, c.resW
	// rdx = new value, masked to bitW bits and shifted into position.
	c.emit("mov rdx, rax")
	c.emit("shl rdx, %d", 64-bitW)
	c.emit("shr rdx, %d", 64-bitW)
	c.emit("shl rdx, %d", bitOff)
	// rax = old storage unit (zero-extended).
	c.genLoadElem("r10", unitW, TInt, false)
	// r11 = old unit with the low bitOff+bitW bits cleared (field removed).
	c.emit("mov r11, rax")
	if bitOff+bitW < 64 {
		c.emit("shr r11, %d", bitOff+bitW)
		c.emit("shl r11, %d", bitOff+bitW)
	} else {
		// The field reaches the top of a 64-bit unit: nothing survives above it.
		c.emit("xor r11, r11")
	}
	// rax = old unit's low bits below the field (kept untouched).
	if bitOff > 0 {
		c.emit("shl rax, %d", 64-bitOff)
		c.emit("shr rax, %d", 64-bitOff)
	} else {
		c.emit("xor rax, rax")
	}
	c.emit("or rax, r11")
	c.emit("or rax, rdx")
	c.genStoreElem("r10", unitW, TInt)
	// Leave the truncated field value in rax; sign-extend a signed field so
	// the rvalue matches a fresh load of it.
	c.emit("shr rdx, %d", bitOff)
	c.emit("mov rax, rdx")
	if signed {
		c.emit("shl rax, %d", 64-bitW)
		c.emit("sar rax, %d", 64-bitW)
	}
	c.resTyp, c.resSigned, c.resW = saveTyp, saveSgn, saveW
}

// copyBytes emits code that copies n bytes from the memory at src to the memory
// at dst, register-to-register. It is used for whole-struct/union assignment,
// where the value is too large to live in a single GP register. The copy is
// unrolled in 8-byte chunks (rax scratch) followed by a byte-wise tail for any
// remainder, so no loop counter or extra registers beyond rax are needed and it
// works regardless of whether n is a multiple of 8 (struct sizes are multiples
// of their alignment, but a struct with only sub-8-aligned members can leave a
// 1/2/4-byte tail).
func (c *CG) copyBytes(dst, src string, n int) {
	if n <= 0 {
		return
	}
	off := 0
	for off+8 <= n {
		c.emit("mov rax, [%s+%d]", src, off)
		c.emit("mov [%s+%d], rax", dst, off)
		off += 8
	}
	rem := n - off
	for i := 0; i < rem; i++ {
		c.emit("mov al, byte [%s+%d]", src, off+i)
		c.emit("mov byte [%s+%d], al", dst, off+i)
	}
}

// isAgg reports whether t is a struct/union aggregate, i.e. a value that is
// never loaded into a register but always handled by address + copyBytes.
func isAgg(t *Type) bool {
	return t != nil && (t.IsStruct() || t.IsUnion())
}

// zeroBytes emits code that stores n zero bytes at the memory pointed to by
// dst (a register holding an address). Used to default-initialise struct and
// union locals that have no initialiser.
func (c *CG) zeroBytes(dst string, n int) {
	if n <= 0 {
		return
	}
	c.emit("xor eax, eax")
	off := 0
	for off+8 <= n {
		c.emit("mov [%s+%d], rax", dst, off)
		off += 8
	}
	for i := off; i < n; i++ {
		c.emit("mov byte [%s+%d], al", dst, i)
	}
}

// genBraceInitLocal initialises a local aggregate (array/struct/union) from a
// braced initialiser. The whole aggregate is zeroed first (C semantics: every
// byte not explicitly initialised is zero -- and with designated or partial
// initialisers the uncovered bytes are NOT a contiguous tail), then each leaf
// is evaluated and stored directly into its frame slot.
func (c *CG) genBraceInitLocal(t *Type, bi *BraceInit, off int) error {
	c.emit("lea r10, [rbp%+d]", off)
	c.zeroBytes("r10", c.typeWidth(t))
	return c.braceWalkLocal(t, bi, off)
}

// braceWalkLocal stores the explicit elements of bi for the aggregate t based
// at frame offset off. Every leaf lands at its layout offset; uncovered bytes
// were already zeroed by the caller.
func (c *CG) braceWalkLocal(t *Type, bi *BraceInit, off int) error {
	if t.IsArray() {
		ew := c.typeWidth(t.Elem)
		for i, el := range bi.Elems {
			if i >= t.Len {
				break
			}
			if err := c.braceElemLocal(t.Elem, el.E, off+i*ew); err != nil {
				return err
			}
		}
		return nil
	}
	if t.IsStruct() {
		allDesig := len(bi.Elems) > 0
		for _, el := range bi.Elems {
			if el.Desig == "" {
				allDesig = false
				break
			}
		}
		if allDesig {
			for _, el := range bi.Elems {
				mi := memberIndex(t, el.Desig)
				if mi < 0 {
					return fmt.Errorf("struct has no member %q", el.Desig)
				}
				m := t.Members[mi]
				if err := c.braceElemLocal(m.Type, el.E, off+m.Offset); err != nil {
					return err
				}
			}
			return nil
		}
		for i, el := range bi.Elems {
			if i >= len(t.Members) {
				break
			}
			if el.Desig != "" {
				return fmt.Errorf("cannot mix positional and designated (\".%s =\") initialisers", el.Desig)
			}
			m := t.Members[i]
			if err := c.braceElemLocal(m.Type, el.E, off+m.Offset); err != nil {
				return err
			}
		}
		return nil
	}
	if t.IsUnion() {
		if len(bi.Elems) == 0 {
			return nil
		}
		el := bi.Elems[0]
		m := t.Members[0]
		if el.Desig != "" {
			if mi := memberIndex(t, el.Desig); mi >= 0 {
				m = t.Members[mi]
			}
		}
		return c.braceElemLocal(m.Type, el.E, off+m.Offset)
	}
	// Scalar target: a single braced value.
	if len(bi.Elems) != 1 {
		return fmt.Errorf("invalid braced initialiser for scalar type %s", t)
	}
	return c.braceElemLocal(t, bi.Elems[0].E, off)
}

// braceElemLocal initialises one element of type t at frame offset off from
// the expression e -- a leaf value, a string literal for a char array, or a
// nested brace.
func (c *CG) braceElemLocal(t *Type, e Expr, off int) error {
	if nbi, ok := e.(*BraceInit); ok {
		return c.braceWalkLocal(t, nbi, off)
	}
	w := c.typeWidth(t)
	if sl, ok := e.(*StrLit); ok && t.IsArray() && t.Elem.IsChar() {
		copied := len(sl.Bytes) + 1
		if copied > w {
			copied = w
		}
		if _, err := c.genExprT(e); err != nil {
			return err
		}
		c.emit("mov r11, rax")
		c.emit("lea r10, [rbp%+d]", off)
		c.copyBytes("r10", "r11", copied)
		return nil
	}
	if _, err := c.genExprT(e); err != nil {
		return err
	}
	if err := c.ensureType(t.Class()); err != nil {
		return err
	}
	if t.Kind == KBool {
		c.normalizeBool()
	}
	c.emit("lea r10, [rbp%+d]", off)
	c.genStoreElem("r10", w, t.Class())
	return nil
}

// walkGlobalInit scans a global/static-local initialiser for every pointer
// slot initialised by a string literal and records the (label, byte offset,
// string label) triple in c.globalStrInits so the entry stub can bind it at
// startup. It mirrors the layout walk of fillBraceImage: arrays by element
// stride, structs by member offset, unions by first member. A string filling a
// char array needs no binding (its bytes live directly in .data).
func (c *CG) walkGlobalInit(t *Type, init Expr, glab string, off int) {
	if t == nil {
		return
	}
	bi, ok := init.(*BraceInit)
	if !ok {
		if sl, ok := init.(*StrLit); ok && t.Kind == KPtr {
			lab, ok := c.strLab[sl]
			if !ok {
				lab = fmt.Sprintf("LC%d", len(c.strs))
				c.strs = append(c.strs, *sl)
				c.strLab[sl] = lab
			}
			c.globalStrInits = append(c.globalStrInits,
				struct {
					glab, slab string
					off        int
				}{glab, lab, off})
		}
		return
	}
	if t.IsArray() {
		ew := c.typeWidth(t.Elem)
		for i, el := range bi.Elems {
			if i >= t.Len {
				break
			}
			c.walkGlobalInit(t.Elem, el.E, glab, off+i*ew)
		}
		return
	}
	if t.IsStruct() {
		allDesig := len(bi.Elems) > 0
		for _, el := range bi.Elems {
			if el.Desig == "" {
				allDesig = false
				break
			}
		}
		if allDesig {
			for _, el := range bi.Elems {
				if mi := memberIndex(t, el.Desig); mi >= 0 {
					m := t.Members[mi]
					c.walkGlobalInit(m.Type, el.E, glab, off+m.Offset)
				}
			}
			return
		}
		for i, el := range bi.Elems {
			if i >= len(t.Members) {
				break
			}
			m := t.Members[i]
			c.walkGlobalInit(m.Type, el.E, glab, off+m.Offset)
		}
		return
	}
	if t.IsUnion() {
		if len(bi.Elems) == 0 {
			return
		}
		el := bi.Elems[0]
		m := t.Members[0]
		if el.Desig != "" {
			if mi := memberIndex(t, el.Desig); mi >= 0 {
				m = t.Members[mi]
			}
		}
		c.walkGlobalInit(m.Type, el.E, glab, off+m.Offset)
		return
	}
	if len(bi.Elems) != 1 {
		return
	}
	c.walkGlobalInit(t, bi.Elems[0].E, glab, off)
}

// emitGlobalVar lays out one program-level variable (true global or static
// local) in .data under the given label. The emission mirrors the rules the
// .data loop used for globals: a braced initialiser is rendered as a byte
// image; a char array from a string literal keeps its bytes (NUL-padded); an
// aggregate/array with no brace is zero-filled for its full byte size; a float
// is 4 bytes of IEEE single or a double quad; everything else is an integer
// dq (zero when the initialiser does not fold).
func (c *CG) emitGlobalVar(out *strings.Builder, g *DeclStmt, lab string) error {
	if bi, ok := g.Init.(*BraceInit); ok {
		return c.emitGlobalBrace(out, g.Typ, bi, lab)
	}
	// A char array initialised by a string literal holds the bytes (plus NUL)
	// directly in .data, zero-padded to the full array size ("char g[8] =
	// \"hi\"" keeps five zero tail bytes). A global char* initialised by a
	// string literal is NOT supported: goa's dq takes no symbol operands, so
	// the pointer could not be relocated to the constant.
	if g.Typ != nil && g.Typ.IsArray() && g.Typ.Elem.IsChar() {
		if sl, ok := g.Init.(*StrLit); ok {
			size := c.typeWidth(g.Typ)
			out.WriteString(fmt.Sprintf("%s db \"%s\", 0", lab, encodeStr(sl.Bytes)))
			for i := len(sl.Bytes) + 1; i < size; i++ {
				out.WriteString(", 0")
			}
			out.WriteString("\n")
			return nil
		}
	}
	if g.Typ != nil && (g.Typ.IsArray() || isAgg(g.Typ)) {
		// typeWidth already returns the full byte size (elem width * len for
		// arrays, the computed Size for structs/unions), so that IS the size
		// to zero-fill.
		size := c.typeWidth(g.Typ)
		if size < 1 {
			size = 1
		}
		n := (size + 7) / 8
		out.WriteString(lab + " dq 0")
		for i := 1; i < n; i++ {
			out.WriteString(", 0")
		}
		out.WriteString("\n")
		return nil
	}
	if g.Typ != nil && g.Typ.IsFloating() {
		// A float global is 4 bytes of IEEE single; a double is a quad. Only a
		// literal initialiser (optionally negated) is foldable here; anything
		// else falls back to zero.
		f := 0.0
		if v, ok := foldFloatInit(g.Init); ok {
			f = v
		}
		if g.Typ.Kind == KFloat {
			bits := math.Float32bits(float32(f))
			out.WriteString(fmt.Sprintf("%s db %d, %d, %d, %d\n", lab,
				bits&0xff, (bits>>8)&0xff, (bits>>16)&0xff, (bits>>24)&0xff))
		} else {
			out.WriteString(fmt.Sprintf("%s dq %s\n", lab, formatDouble(f)))
		}
		return nil
	}
	val := int64(0)
	if v, ok := foldConstInit(g.Init); ok {
		val = v
	}
	out.WriteString(fmt.Sprintf("%s dq %d\n", lab, val))
	return nil
}

// emitGlobalBrace lays out a global aggregate from a braced initialiser as a
// byte image and emits it as db bytes. Bytes not explicitly initialised stay
// zero. A char* member initialised by a string literal also stays zero here:
// walkGlobalInit registered the (object, offset, string) triple and the entry
// stub writes the string's address at startup (goa has no data relocations).
func (c *CG) emitGlobalBrace(out *strings.Builder, t *Type, bi *BraceInit, lab string) error {
	size := c.typeWidth(t)
	if size < 1 {
		size = 1
	}
	img := make([]byte, size)
	if err := c.fillBraceImage(t, bi, img, 0); err != nil {
		return err
	}
	out.WriteString(fmt.Sprintf("%s db %d", lab, img[0]))
	for _, b := range img[1:] {
		out.WriteString(fmt.Sprintf(", %d", b))
	}
	out.WriteString("\n")
	return nil
}

// fillBraceImage fills the image of a global aggregate at byte offset off
// from a braced initialiser, mirroring the layout the local emitter uses
// (arrays by element index, structs by member Offset, unions by first member).
func (c *CG) fillBraceImage(t *Type, bi *BraceInit, img []byte, off int) error {
	if t.IsArray() {
		ew := c.typeWidth(t.Elem)
		for i, el := range bi.Elems {
			if i >= t.Len {
				break
			}
			if err := c.fillBraceElem(t.Elem, el.E, img, off+i*ew); err != nil {
				return err
			}
		}
		return nil
	}
	if t.IsStruct() {
		allDesig := len(bi.Elems) > 0
		for _, el := range bi.Elems {
			if el.Desig == "" {
				allDesig = false
				break
			}
		}
		if allDesig {
			for _, el := range bi.Elems {
				mi := memberIndex(t, el.Desig)
				if mi < 0 {
					return fmt.Errorf("struct has no member %q", el.Desig)
				}
				m := t.Members[mi]
				if err := c.fillBraceElem(m.Type, el.E, img, off+m.Offset); err != nil {
					return err
				}
			}
			return nil
		}
		for i, el := range bi.Elems {
			if i >= len(t.Members) {
				break
			}
			if el.Desig != "" {
				return fmt.Errorf("cannot mix positional and designated (\".%s =\") initialisers", el.Desig)
			}
			m := t.Members[i]
			if err := c.fillBraceElem(m.Type, el.E, img, off+m.Offset); err != nil {
				return err
			}
		}
		return nil
	}
	if t.IsUnion() {
		if len(bi.Elems) == 0 {
			return nil
		}
		el := bi.Elems[0]
		m := t.Members[0]
		if el.Desig != "" {
			if mi := memberIndex(t, el.Desig); mi >= 0 {
				m = t.Members[mi]
			}
		}
		return c.fillBraceElem(m.Type, el.E, img, off+m.Offset)
	}
	if len(bi.Elems) != 1 {
		return fmt.Errorf("invalid braced initialiser for scalar type %s", t)
	}
	return c.fillBraceElem(t, bi.Elems[0].E, img, off)
}

// fillBraceElem writes one element into the image: a nested brace recurses, a
// string fills a char array, and scalars fold to their IEEE/integer bytes.
// Anything non-foldable stays zero, matching existing global behaviour.
func (c *CG) fillBraceElem(t *Type, e Expr, img []byte, off int) error {
	if nbi, ok := e.(*BraceInit); ok {
		return c.fillBraceImage(t, nbi, img, off)
	}
	w := c.typeWidth(t)
	if off+w > len(img) {
		return fmt.Errorf("initialiser overflows global of %d bytes", len(img))
	}
	if sl, ok := e.(*StrLit); ok {
		if t.IsArray() && t.Elem.IsChar() {
			b := append(append([]byte(nil), sl.Bytes...), 0)
			if len(b) > w {
				b = b[:w]
			}
			copy(img[off:], b)
			return nil
		}
		// A pointer slot initialised by a string leaves 8 zero bytes here:
		// walkGlobalInit has already registered the (object, offset, string)
		// triple, and the entry stub writes the string's address at startup.
		// goa has no data relocations, so the value cannot be stored in .data.
	}
	switch t.Kind {
	case KFloat:
		f, _ := foldFloatInit(e)
		bits := math.Float32bits(float32(f))
		for i := 0; i < 4; i++ {
			img[off+i] = byte(bits >> (8 * i))
		}
		return nil
	case KBool:
		// A _Bool keeps exactly 0 or 1 even as a global (C semantics: any
		// non-zero initialiser becomes 1).
		v, _ := foldConstInit(e)
		if v != 0 {
			v = 1
		}
		img[off] = byte(v)
		return nil
	case KDouble:
		f, _ := foldFloatInit(e)
		bits := math.Float64bits(f)
		for i := 0; i < 8; i++ {
			img[off+i] = byte(bits >> (8 * i))
		}
		return nil
	}
	v, _ := foldConstInit(e)
	for i := 0; i < w && i < 8; i++ {
		img[off+i] = byte(v >> (8 * i))
	}
	return nil
}

// structSrcAddr emits code that leaves the address of the struct/union value
// of expression e in r10. Two kinds of sources are accepted:
//   - an lvalue (local/global struct, member, *p, arr[i]): genLValue computes
//     the address directly;
//   - a call returning a struct: genExprT leaves the value in the callee's
//     result buffer; the buffer is left LIVE and c.resStruct stays true, and
//     the caller must eventually call releaseResStruct (after copying the
//     bytes out) or claim its slots itself (when passing it on as an
//     argument, where the buffer must survive until the outer call).
func (c *CG) structSrcAddr(e Expr, t *Type) error {
	switch call := e.(type) {
	case *Call:
		if isAgg(t) {
			if _, err := c.genExprT(call); err != nil {
				return err
			}
			if !c.resStruct {
				return fmt.Errorf("call %q does not produce a struct value", call.Name)
			}
			c.emit("lea r10, [rbp%+d]", c.tmpSlot(c.resStructK))
			return nil
		}
	case *IndirectCall:
		if isAgg(t) {
			if _, err := c.genExprT(call); err != nil {
				return err
			}
			if !c.resStruct {
				return fmt.Errorf("function-pointer call does not produce a struct value")
			}
			c.emit("lea r10, [rbp%+d]", c.tmpSlot(c.resStructK))
			return nil
		}
	}
	return c.genLValue(e)
}

// releaseResStruct frees the pending struct-return result buffer (if any)
// after its bytes have been consumed by a copy.
func (c *CG) releaseResStruct() {
	if c.resStruct {
		c.tmpDepth -= c.resStructSl
		c.resStruct = false
	}
}

// lvalueWidth returns the byte width of the value stored at the lvalue e. For
// a bare scalar variable this is its slot width (char=1, short=2, int=4,
// pointer=8); for a member/pointer-deref/array-subscript it is the layout
// width of the stored object. A pointer-typed lvalue ALWAYS stores at pointer
// width (8): the value being stored is the pointer itself, not the thing it
// points at. Getting this wrong truncates `s.charPtrField = "str"` to a
// single byte (elemWidthOf returns the element *stride*, which is the right
// answer for p[i] indexing but wrong for storing the pointer).
func (c *CG) lvalueWidth(e Expr) int {
	if id, ok := e.(*Ident); ok {
		if vi, ok2 := c.vars[id.Name]; ok2 {
			if vi.typ.IsPtr() {
				return 8
			}
			if vi.typ.Kind == KInt || vi.typ.Kind == KFloat {
				// char/short stay narrow; int becomes an 8-byte slot so it can
				// hold a 64-bit pointer that was stored in an int variable.
				// float is a genuine 4-byte IEEE single.
				return c.slotWidth(vi.typ)
			}
			if vi.typ.Kind == KStruct || vi.typ.Kind == KUnion {
				return vi.typ.Size
			}
			return 8
		}
		// A static local shadows a same-named global; its type lives under
		// its .data label, not the source name.
		if lab, ok2 := c.staticVars[id.Name]; ok2 {
			if gt := c.globalTyp[lab]; gt != nil {
				if gt.IsPtr() {
					return 8
				}
				if gt.Kind == KFloat {
					return 4
				}
				if gt.Kind == KStruct || gt.Kind == KUnion {
					return gt.Size
				}
			}
			return 8
		}
		if c.globals[id.Name] {
			// A float global occupies 4 bytes in .data (emitted as four db
			// values); everything else is a quad.
			if gt := c.globalTyp[id.Name]; gt != nil && gt.Kind == KFloat {
				return 4
			}
			return 8
		}
		return 8
	}
	// Pointer-typed lvalues (s.charPtrField, char *arr[i], *pp, ...): the
	// assignment stores the 8-byte pointer value itself.
	if t := c.exprType(e); t != nil && t.IsPtr() {
		return 8
	}
	return c.elemWidthOf(e)
}

// lvalueClass returns the storage class (int vs double) of the value held at
// the lvalue e, used to pick the right store instruction for an assignment.
func (c *CG) lvalueClass(e Expr) CType {
	if id, ok := e.(*Ident); ok {
		if vi, ok2 := c.vars[id.Name]; ok2 {
			return vi.typ.Class()
		}
		// A static local shadows a same-named global; its type lives under
		// its .data label, not the source name.
		if lab, ok2 := c.staticVars[id.Name]; ok2 {
			if gt := c.globalTyp[lab]; gt != nil {
				return gt.Class()
			}
			return TInt
		}
		if c.globals[id.Name] {
			if gt := c.globalTyp[id.Name]; gt != nil {
				return gt.Class()
			}
			return TInt
		}
		return TInt
	}
	return c.elemClassOf(e)
}

// genVaArg implements va_arg(ap, T): read 8 bytes at the cursor held in ap,
// advance the cursor by 8, and leave the value in rax (int-class) or xmm0
// (double). The cursor is a va_list = char* = an address stored in an int
// slot, so reading and writing it are ordinary integer operations. Doubles are
// stored bitwise in the 8-byte slot (the caller spilled them with movq), so we
// reload the bits into xmm0 with movq xmm0, rax.
func (c *CG) genVaArg(n *VaArgExpr) (CType, error) {
	if err := c.genLValue(n.Ap); err != nil {
		return TInt, err
	}
	c.emit("mov rcx, [r10]") // rcx = current cursor
	if n.Typ.Class() == TDouble {
		c.emit("mov rax, [rcx]")
		c.emit("movq xmm0, rax")
		c.emit("add rcx, 8")
		c.emit("mov [r10], rcx")
		c.resTyp = TDouble
		c.resSigned = false
		c.resW = 8
		return TDouble, nil
	}
	c.emit("mov rax, [rcx]")
	c.emit("add rcx, 8")
	c.emit("mov [r10], rcx")
	c.resTyp = n.Typ.Class()
	c.resSigned = n.Typ.Kind == KInt && n.Typ.Signed
	c.resW = c.semWOf(n.Typ)
	return c.resTyp, nil
}

// loadDoubleConst materialises a double constant into xmm0, reusing the
// program-level constant pool so e.g. the 1.0 used by ++/-- on a double is
// emitted once, exactly like NumLit doubles.
func (c *CG) loadDoubleConst(v float64) {
	lab, ok := c.doubleLab[v]
	if !ok {
		lab = fmt.Sprintf("LD%dx", len(c.doubles))
		c.doubles = append(c.doubles, v)
		c.doubleLab[v] = lab
	}
	c.emit("movsd xmm0, [rip+%s]", lab)
}

// genIncDec emits the prefix (++x) or postfix (x++) increment/decrement of an
// lvalue. It leaves the NEW value in rax/xmm0 for prefix and the OLD value for
// postfix (the incremented value is still written back to memory either way).
//
// Pointers step by their element width (char* advances one byte, int* eight),
// everything else by one. The operand may be a scalar register, a stack slot,
// a dereferenced pointer, or an array element.
func (c *CG) genIncDec(n *IncDecExpr) (CType, error) {
	et := c.exprType(n.E)
	step := 1
	if et != nil && et.IsPtr() && et.Elem != nil {
		step = c.typeWidth(et.Elem)
	}
	signed := false
	if et != nil && et.Kind == KInt {
		signed = et.Signed
	}
	resW := 4
	if et != nil {
		resW = c.semWOf(et)
	}

	// Fast path: a register-cached scalar local (pointers are never
	// register-allocated because they can be address-taken).
	if id, ok := n.E.(*Ident); ok {
		if vi, ok2 := c.vars[id.Name]; ok2 && vi.reg != "" {
			if et != nil && et.Kind == KBool {
				// _Bool keeps exactly 0/1: normalise the register after the
				// increment/decrement. Prefix returns the new (normalised)
				// value; postfix returns the old value.
				c.emit("mov rax, %s", vi.reg) // old value
				if n.Prefix {
					if step == 1 {
						c.emit("inc %s", vi.reg)
					} else {
						c.emit("add %s, %d", vi.reg, step)
					}
					c.emit("mov rax, %s", vi.reg)
					c.normalizeBool()
					c.emit("mov %s, rax", vi.reg)
				} else {
					c.emit("mov rdx, rax") // save old value
					if step == 1 {
						c.emit("inc %s", vi.reg)
					} else {
						c.emit("add %s, %d", vi.reg, step)
					}
					c.emit("mov rax, %s", vi.reg)
					c.normalizeBool()
					c.emit("mov %s, rax", vi.reg)
					c.emit("mov rax, rdx") // restore old value as the result
				}
				c.resTyp = TInt
				c.resSigned = false
				c.resW = 1
				return TInt, nil
			}
			if n.Prefix {
				if step == 1 {
					c.emit("inc %s", vi.reg)
				} else {
					c.emit("add %s, %d", vi.reg, step)
				}
				c.emit("mov rax, %s", vi.reg)
				if resW == 4 {
					// Canonicalise both the result and the cached register: an
					// int wraps at 32 bits, and the register must stay canonical
					// for later loads of this variable.
					c.canonInt(signed)
					c.emit("mov %s, rax", vi.reg)
				}
			} else {
				c.emit("mov rax, %s", vi.reg) // old value (already canonical)
				if step == 1 {
					c.emit("inc %s", vi.reg)
				} else {
					c.emit("add %s, %d", vi.reg, step)
				}
				if resW == 4 {
					// Canonicalise the register's new value through a scratch
					// round-trip; rax keeps returning the old value.
					c.emit("mov rdx, rax") // save old value
					c.emit("mov rax, %s", vi.reg)
					c.canonInt(signed)
					c.emit("mov %s, rax", vi.reg)
					c.emit("mov rax, rdx") // restore old value as the result
				}
			}
			c.resTyp = TInt
			c.resSigned = signed
			c.resW = resW
			return TInt, nil
		}
	}

	// General lvalue path: compute the address, load the current value, modify
	// it, and store it back.
	if err := c.genLValue(n.E); err != nil {
		return TInt, err
	}
	width := c.lvalueWidth(n.E)
	double := false
	if t := c.exprType(n.E); t != nil && t.IsFloating() {
		double = true
	}
	if double {
		// width is 4 for a float lvalue: genLoadElem widens it to a double
		// and genStoreElem rounds the updated value back to a single.
		c.genLoadElem("r10", width, TDouble, false)
		c.emit("movsd xmm1, xmm0") // keep current for postfix restore
		c.loadDoubleConst(1.0)
		if n.Op == "++" {
			c.emit("addsd xmm0, xmm1")
		} else {
			c.emit("subsd xmm0, xmm1")
		}
		c.genStoreElem("r10", width, TDouble)
		if !n.Prefix {
			c.emit("movsd xmm0, xmm1")
		}
		c.resTyp = TDouble
		c.resSigned = false
		c.resW = 8
		return TDouble, nil
	}
	if c.lvBitWidth > 0 {
		// Bit-field: load with extract, modify, RMW back. genStoreBitfield
		// leaves the truncated new field value in rax (the prefix result);
		// for postfix the old value was spilled below and is restored.
		c.genLoadBitfield(c.lvBitUnit, c.lvBitOff, c.lvBitWidth, c.lvBitSigned)
		os := 0
		saved := false
		if !n.Prefix {
			c.tmpDepth++
			os = c.tmpSlot(c.tmpDepth)
			c.emit("mov [rbp%+d], rax", os)
			saved = true
		}
		if n.Op == "++" {
			c.emit("inc rax")
		} else {
			c.emit("dec rax")
		}
		c.genStoreBitfield(c.lvBitUnit, c.lvBitOff, c.lvBitWidth, c.lvBitSigned)
		if saved {
			c.emit("mov rax, [rbp%+d]", os)
			c.tmpDepth--
		}
		c.resTyp = TInt
		c.resSigned = c.lvBitSigned
		c.resW = resW
		if resW == 4 {
			c.canonInt(c.lvBitSigned)
		}
		return TInt, nil
	}
	c.genLoadElem("r10", width, TInt, signed)
	os := 0
	saved := false
	if !n.Prefix {
		c.tmpDepth++
		os = c.tmpSlot(c.tmpDepth)
		c.emit("mov [rbp%+d], rax", os)
		saved = true
	}
	if n.Op == "++" {
		if step == 1 {
			c.emit("inc rax")
		} else {
			c.emit("add rax, %d", step)
		}
	} else {
		if step == 1 {
			c.emit("dec rax")
		} else {
			c.emit("sub rax, %d", step)
		}
	}
	if et != nil && et.Kind == KBool {
		c.normalizeBool()
	}
	c.genStoreElem("r10", width, TInt)
	if saved {
		c.emit("mov rax, [rbp%+d]", os)
		c.tmpDepth--
	}
	c.resTyp = TInt
	c.resSigned = signed
	c.resW = resW
	if resW == 4 {
		// Keep the result a canonical 32-bit int (prefix results wrap at 32
		// bits; the postfix old value was already canonical, so this is a
		// no-op there).
		c.canonInt(signed)
	}
	return TInt, nil
}

// setcc emits "rax = (left OP right)" using a conditional branch, because goa
// does not implement the setcc/movzx pair that gcc's assembler provides.
func (c *CG) emitCompare(jmpIfTrue string) {
	lTrue := c.newLabel("cmp")
	lEnd := c.newLabel("cmpe")
	c.emit("%s %s", jmpIfTrue, lTrue)
	c.emit("mov rax, 0")
	c.emit("jmp %s", lEnd)
	c.line(lTrue + ":\n")
	c.emit("mov rax, 1")
	c.line(lEnd + ":\n")
}

// normalizeBool canonicalises the 64-bit value in rax to exactly 0 or 1 using
// a conditional branch (goa has no setcc). It is applied whenever a _Bool is
// stored: C semantics say any non-zero value becomes 1. It is idempotent on
// already-normalised values (1 -> 1, 0 -> 0).
func (c *CG) normalizeBool() {
	lFalse := c.newLabel("bnf")
	lEnd := c.newLabel("bne")
	c.emit("cmp rax, 0")
	c.emit("je %s", lFalse)
	c.emit("mov rax, 1")
	c.emit("jmp %s", lEnd)
	c.line(lFalse + ":\n")
	c.emit("mov rax, 0")
	c.line(lEnd + ":\n")
}

func (c *CG) genBinary(n *Binary) (CType, error) {
	switch n.Op {
	case "&&":
		lFalse := c.newLabel("andf")
		lEnd := c.newLabel("andd")
		if _, err := c.genExprT(n.L); err != nil {
			return TInt, err
		}
		if err := c.ensureType(TInt); err != nil {
			return TInt, err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lFalse)
		if _, err := c.genExprT(n.R); err != nil {
			return TInt, err
		}
		if err := c.ensureType(TInt); err != nil {
			return TInt, err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lFalse)
		c.emit("mov rax, 1")
		c.emit("jmp %s", lEnd)
		c.line(lFalse + ":\n")
		c.emit("mov rax, 0")
		c.line(lEnd + ":\n")
		c.resTyp = TInt
		c.resSigned = true
		c.resW = 4
		return TInt, nil
	case "||":
		lTrue := c.newLabel("ort")
		lEnd := c.newLabel("ore")
		if _, err := c.genExprT(n.L); err != nil {
			return TInt, err
		}
		if err := c.ensureType(TInt); err != nil {
			return TInt, err
		}
		c.emit("cmp rax, 0")
		c.emit("jne %s", lTrue)
		if _, err := c.genExprT(n.R); err != nil {
			return TInt, err
		}
		if err := c.ensureType(TInt); err != nil {
			return TInt, err
		}
		c.emit("cmp rax, 0")
		c.emit("jne %s", lTrue)
		c.emit("mov rax, 0")
		c.emit("jmp %s", lEnd)
		c.line(lTrue + ":\n")
		c.emit("mov rax, 1")
		c.line(lEnd + ":\n")
		c.resTyp = TInt
		c.resSigned = true
		c.resW = 4
		return TInt, nil
	}

	// Numeric / comparison operators. The left operand is evaluated first and
	// spilled into a frame temporary (push/pop would break the 16-byte RSP
	// alignment at call sites). When either side is a double, both operands
	// are promoted to double and the SSE2 scalar instructions take over.
	c.tmpDepth++
	k := c.tmpDepth
	off := c.tmpSlot(k)
	lt, err := c.genExprT(n.L)
	if err != nil {
		return TInt, err
	}
	// Signedness of the LEFT operand drives signed vs unsigned division,
	// remainder, and right-shift (the dividend and the value being shifted
	// are always the left operand). The right operand is only a count.
	leftSigned := c.resSigned
	leftW := c.resW
	if lt == TDouble {
		c.emit("movsd [rbp%+d], xmm0", off)
	} else {
		c.emit("mov [rbp%+d], rax", off)
	}
	rt, err := c.genExprT(n.R)
	if err != nil {
		return TInt, err
	}
	c.tmpDepth--
	// For symmetric comparisons the two operands normally share signedness,
	// so the right operand's flag is an acceptable proxy there.
	rightSigned := c.resSigned
	rightW := c.resW

	// The right operand is now in rax (int) or xmm0 (double).
	isDbl := lt == TDouble || rt == TDouble

	// loadLeftDbl puts the spilled left operand into xmm0 as a double.
	loadLeftDbl := func() {
		if lt == TDouble {
			c.emit("movsd xmm0, [rbp%+d]", off)
		} else {
			c.emit("mov rax, [rbp%+d]", off)
			c.emit("cvtsi2sd xmm0, rax")
		}
	}

	switch n.Op {
	case "+", "-", "*", "/":
		if isDbl {
			if err := c.ensureType(TDouble); err != nil { // right -> xmm0
				return TInt, err
			}
			c.emit("movsd xmm1, xmm0") // right -> xmm1
			loadLeftDbl()              // xmm0 = left (as double)
			switch n.Op {
			case "+":
				c.emit("addsd xmm0, xmm1")
			case "-":
				c.emit("subsd xmm0, xmm1")
			case "*":
				c.emit("mulsd xmm0, xmm1")
			case "/":
				c.emit("divsd xmm0, xmm1")
			}
			c.resTyp = TDouble
			return TDouble, nil
		}
		// Usual arithmetic conversions decide the promoted width/sign of the
		// operation. Only a 4-byte result is canonicalised afterwards; an
		// 8-byte result (long/unsigned long) keeps its full 64-bit value.
		w, resSign := promotedArith(leftW, leftSigned, rightW, rightSigned)
		c.emit("mov r10, [rbp%+d]", off) // left -> r10, right -> rax
		// ---- pointer arithmetic ----
		// A pointer value is carried as an 8-byte integer in rax/r10. When an
		// operand has pointer type we stride the integer side by the pointed-to
		// element size, exactly as array subscripting does.
		ltType := c.exprType(n.L)
		rtType := c.exprType(n.R)
		// An array used as an operand is a value context, so it has already
		// decayed to a pointer to element 0 (see loadVar); treat KArr exactly
		// like KPtr for pointer arithmetic. "a + 2" must step by the element
		// width, not by 2 bytes.
		lPtr := ltType != nil && (ltType.IsPtr() || ltType.IsArray())
		rPtr := rtType != nil && (rtType.IsPtr() || rtType.IsArray())
		if lPtr || rPtr {
			if n.Op == "*" || n.Op == "/" {
				return TInt, fmt.Errorf("operator %q is not defined for pointers", n.Op)
			}
			if lPtr && rPtr {
				if n.Op != "-" {
					return TInt, fmt.Errorf("only subtraction is defined for two pointers")
				}
				ew := c.ptrElemWidth(ltType)
				c.emit("sub r10, rax") // byte difference (left - right)
				c.emit("mov rax, r10")
				if ew != 1 {
					c.emit("cqo")
					c.emit("mov r11, %d", ew)
					c.emit("idiv r11")
				}
				c.resTyp = TInt
				c.resSigned = true
				c.resW = 8
				return TInt, nil
			}
			// Exactly one pointer operand: bring the pointer into r10 and the
			// integer into rax, then scale the integer by the element size.
			ew := 1
			if lPtr {
				ew = c.ptrElemWidth(ltType)
			} else {
				ew = c.ptrElemWidth(rtType)
				// left (int) is in r10, right (pointer) is in rax: swap.
				c.emit("mov r11, rax") // r11 = pointer
				c.emit("mov rax, r10") // rax = integer
				c.emit("mov r10, r11") // r10 = pointer
			}
			if ew != 1 {
				c.emit("imul rax, %d", ew)
			}
			if n.Op == "+" {
				c.emit("add rax, r10")
			} else {
				c.emit("sub r10, rax")
				c.emit("mov rax, r10")
			}
			c.resTyp = TInt
			c.resSigned = false
			c.resW = 8
			return TInt, nil
		}
		switch n.Op {
		case "+":
			c.emit("add rax, r10")
		case "-":
			c.emit("sub r10, rax")
			c.emit("mov rax, r10")
		case "*":
			c.emit("imul rax, r10")
		case "/":
			c.emit("mov r11, rax") // divisor
			c.emit("mov rax, r10") // dividend
			if resSign {
				if w == 4 {
					c.canonInt(true)
				}
				c.emit("cqo")
				c.emit("idiv r11")
			} else {
				if w == 4 {
					c.canonInt(false)
				}
				c.emit("xor rdx, rdx")
				c.emit("div r11")
			}
		}
		c.resSigned = resSign
		c.resW = w
		if w == 4 {
			c.canonInt(resSign)
		}
		c.resTyp = TInt
		return TInt, nil
	case "%":
		if isDbl {
			return TInt, fmt.Errorf("%% requires integer operands")
		}
		w, resSign := promotedArith(leftW, leftSigned, rightW, rightSigned)
		c.emit("mov r10, [rbp%+d]", off)
		c.emit("mov r11, rax") // divisor
		c.emit("mov rax, r10") // dividend
		if resSign {
			if w == 4 {
				c.canonInt(true)
			}
			c.emit("cqo")
			c.emit("idiv r11")
		} else {
			if w == 4 {
				c.canonInt(false)
			}
			c.emit("xor rdx, rdx")
			c.emit("div r11")
		}
		c.emit("mov rax, rdx")
		c.resSigned = resSign
		c.resW = w
		if w == 4 {
			c.canonInt(resSign)
		}
		c.resTyp = TInt
		return TInt, nil
	case "<<", ">>":
		if isDbl {
			return TInt, fmt.Errorf("%q requires integer operands", n.Op)
		}
		// C: each shift operand is individually integer-promoted and the
		// result type is the promoted LEFT operand's type; the right operand
		// is only a count. So width/sign come from the left side alone --
		// promotedArith must NOT be used here (int << someLong is still int).
		lw, ls := leftW, leftSigned
		if lw < 4 {
			lw, ls = 4, true
		}
		// Shift count must live in cl (low 8 bits of rcx); the left operand
		// rides in a frame temporary.
		c.emit("mov rcx, rax")           // count
		c.emit("mov rax, [rbp%+d]", off) // left
		if lw == 4 {
			c.canonInt(ls)
		}
		switch n.Op {
		case "<<":
			c.emit("shl rax, cl")
		case ">>":
			if ls {
				c.emit("sar rax, cl") // arithmetic (sign-extending) shift
			} else {
				c.emit("shr rax, cl") // logical shift
			}
		}
		c.resSigned = ls
		c.resW = lw
		if lw == 4 {
			c.canonInt(ls)
		}
		c.resTyp = TInt
		return TInt, nil
	case "&", "|", "^":
		if isDbl {
			return TInt, fmt.Errorf("%q requires integer operands", n.Op)
		}
		w, resSign := promotedArith(leftW, leftSigned, rightW, rightSigned)
		c.emit("mov r11, [rbp%+d]", off) // left
		switch n.Op {
		case "&":
			c.emit("and rax, r11")
		case "|":
			c.emit("or rax, r11")
		case "^":
			c.emit("xor rax, r11")
		}
		c.resSigned = resSign
		c.resW = w
		if w == 4 {
			c.canonInt(resSign)
		}
		c.resTyp = TInt
		return TInt, nil
	case "<", ">", "<=", ">=", "==", "!=":
		var jmp string
		if isDbl {
			if err := c.ensureType(TDouble); err != nil {
				return TInt, err
			}
			c.emit("movsd xmm1, xmm0") // right -> xmm1
			loadLeftDbl()              // xmm0 = left
			c.emit("ucomisd xmm0, xmm1")
			// ucomisd sets CF for `<` and ZF for `==`; the unsigned jumps
			// read those flags directly, which is the canonical way to
			// compare ordered doubles.
			switch n.Op {
			case "<":
				jmp = "jb"
			case ">":
				jmp = "ja"
			case "<=":
				jmp = "jbe"
			case ">=":
				jmp = "jae"
			case "==":
				jmp = "je"
			case "!=":
				jmp = "jne"
			}
		} else {
			_, resSign := promotedArith(leftW, leftSigned, rightW, rightSigned)
			c.emit("mov r10, [rbp%+d]", off)
			c.emit("cmp r10, rax")
			// Signedness selects the condition codes: signed uses jl/jg/...,
			// unsigned uses jb/ja/... so that negative values compare right.
			// The promoted common type decides which -- a negative int
			// compared against an unsigned long must read as unsigned.
			if resSign {
				switch n.Op {
				case "<":
					jmp = "jl"
				case ">":
					jmp = "jg"
				case "<=":
					jmp = "jle"
				case ">=":
					jmp = "jge"
				case "==":
					jmp = "je"
				case "!=":
					jmp = "jne"
				}
			} else {
				switch n.Op {
				case "<":
					jmp = "jb"
				case ">":
					jmp = "ja"
				case "<=":
					jmp = "jbe"
				case ">=":
					jmp = "jae"
				case "==":
					jmp = "je"
				case "!=":
					jmp = "jne"
				}
			}
		}
		c.emitCompare(jmp)
		c.resTyp = TInt
		c.resSigned = true
		c.resW = 4 // a comparison yields a (signed) int
		return TInt, nil
	}
	return TInt, fmt.Errorf("unsupported operator %q", n.Op)
}

// argSlot remembers a call argument's frame temporary slot and its type.
type argSlot struct {
	slot int
	typ  CType
	// f32 marks an argument bound to a float parameter: the ABI carries it in
	// the low 32 bits of an XMM register (or stack slot), so the double every
	// expression is computed in must be narrowed at the call site.
	f32 bool
}

// variadicFn reports whether a goclib function takes printf-style varargs.
// For those, goc passes every argument in an 8-byte "general-purpose slot"
// (doubles are moved bitwise into rax first), so the slots line up
// positionally with the format string: the %d/%f/%s consumers all walk
// the same 8-byte va_list cursor.
func variadicFn(name string) bool {
	return name == "printf" || name == "sprintf"
}

// funcAddrSym resolves a function designator to the symbol whose address it
// decays to ("fp = add;"). Only functions whose code lands in this translation
// unit qualify: user definitions and goclib helpers. Taking the address counts
// as a use, so a goclib helper referenced this way is pulled in exactly as if
// it had been called.
func (c *CG) funcAddrSym(name string) (string, bool) {
	if c.funcs[name] {
		return name, true
	}
	if lib := clibCStore(c.linux); lib != nil {
		if _, ok := lib.funcs[name]; ok {
			c.need[name] = true
			return name, true
		}
	}
	return "", false
}

// genCallExpr emits a call and returns the callee's result type.
//
// Calling conventions:
//   - Typed calls (user functions): each argument goes to the register of
//     its position and type -- doubles to xmm0-3 (Win) / xmm0-7 (SysV),
//     everything else to rcx/rdx/r8/r9 (Win) / rdi/rsi/... (SysV).
//   - Varargs (printf/sprintf): every argument rides in an 8-byte GP slot,
//     double bit patterns included, so the va_list cursor needs no XMM
//     handling.
//
// Each argument is evaluated and spilled to a frame temporary slot, and only
// loaded into the argument registers right before the call. This is required
// because an argument may itself be a function call whose own argument setup
// would otherwise clobber the values of earlier arguments.
func (c *CG) genCallExpr(n *Call) (CType, error) {
	// va_start / va_end are compiler builtins, not real functions. va_start
	// seeds the va_list cursor with the address of the first variadic slot;
	// va_end is a no-op in goc's flat-cursor model.
	if n.Name == "va_start" || n.Name == "va_end" {
		if n.Name == "va_start" {
			if len(n.Args) < 1 {
				return TInt, fmt.Errorf("va_start requires at least the va_list argument")
			}
			if err := c.genLValue(n.Args[0]); err != nil {
				return TInt, err
			}
			// The cursor starts at the first variadic slot. Slot 0 of the
			// save area is the hidden sret pointer when this function
			// returns a struct, so named params (and the cursor) shift by
			// one there too.
			rs := 0
			if isAgg(c.curRet) {
				rs = 1
			}
			c.emit("lea rax, [rbp%+d]", c.saveBaseOff+8*(c.nFixed+rs))
			c.emit("mov [r10], rax")
		}
		c.resTyp = TInt
		return TInt, nil
	}
	// A call whose name designates a VARIABLE holding a function pointer is an
	// indirect call: C spells it exactly like a direct call ("fp(x)"), but the
	// address has to be loaded from the variable at run time.
	if !c.funcs[n.Name] {
		if e, ft, ok := c.fnPtrVar(n.Name); ok {
			return c.genCall("", e, ft, n.Args)
		}
	}
	return c.genCall(n.Name, nil, nil, n.Args)
}

// fnPtrVar resolves a name that designates a local variable, parameter or
// global holding a function pointer. It returns the callee expression (the
// variable read itself) together with the static function type behind it.
func (c *CG) fnPtrVar(name string) (Expr, *Type, bool) {
	if vi, ok := c.vars[name]; ok {
		if ft := funcTypeOf(vi.typ); ft != nil {
			return &Ident{Name: name}, ft, true
		}
	}
	if gt, ok := c.globalTyp[name]; ok {
		if ft := funcTypeOf(gt); ft != nil {
			return &Ident{Name: name}, ft, true
		}
	}
	return nil, nil, false
}

// genIndirectCall emits a call through a computed function address: (*fp)(x),
// tab[i](x), s.cb(x). The callee expression is evaluated once, before the
// arguments, so that evaluating an argument cannot clobber it.
func (c *CG) genIndirectCall(n *IndirectCall) (CType, error) {
	return c.genCall("", n.Fn, funcTypeOf(c.exprType(n.Fn)), n.Args)
}

// genCall emits a call and returns the callee's result type. name selects a
// directly called symbol; when fnExpr is non-nil the call is indirect and the
// address comes from evaluating that expression (ft is the static function type
// behind the pointer, nil when it could not be recovered -- such a call then
// behaves like an untyped extern returning int). Both paths marshall arguments
// identically; only the branch differs.
func (c *CG) genCall(name string, fnExpr Expr, ft *Type, args []Expr) (CType, error) {
	indirect := fnExpr != nil
	diag := name
	if indirect {
		diag = "function pointer"
	}
	argRegs := c.argRegs()
	argXMM := c.argXMM()
	nargs := len(args)
	if nargs > maxArgs {
		return TInt, fmt.Errorf("%s: too many arguments (max %d)", diag, maxArgs)
	}
	// Struct/union return: the caller allocates a temporary result buffer,
	// passes its address as the hidden first argument (argRegs[0]) and every
	// user argument shifts one register slot to the right.
	retT := (*Type)(nil)
	varargs := false
	var paramTypes []*Type
	if indirect {
		if ft != nil {
			retT = ft.Ret
			varargs = ft.Variadic
			paramTypes = ft.Params
		}
	} else {
		if f, ok := c.funcDefs[name]; ok {
			retT = f.Ret
			paramTypes = f.ParamTypes
			// A user-defined variadic function ("int log(const char*, ...)")
			// needs the same calling convention as printf: every argument
			// travels in a general-purpose slot, because the callee's
			// prologue only spills the integer argument registers into its
			// va_list save area. Without this a double argument would be
			// passed in xmm0 and va_arg would read garbage (0.0).
			varargs = f.Variadic
		}
		if !varargs {
			varargs = variadicFn(name)
		}
	}
	sretSz := 0
	if isAgg(retT) {
		sretSz = retT.Size
	}
	regShift := 0
	if sretSz > 0 {
		regShift = 1
	}
	// Anything past the register arguments goes on the stack. On Windows the
	// stack arguments live at [rsp+32] and up, *above* the 32-byte shadow
	// space the callee is allowed to spill its register params into, so the
	// caller must reserve that shadow space too. On Linux there is no shadow
	// space (args at [rsp]). The total is rounded up to 16 so RSP stays
	// aligned at every call site. With a hidden struct-return pointer the
	// user register capacity is one slot smaller (argRegs[0] carries the
	// pointer, which never spills to the stack).
	stackArgs := nargs - (len(argRegs) - regShift)
	if stackArgs < 0 {
		stackArgs = 0
	}
	extra := 0
	if stackArgs > 0 {
		extra = 8 * stackArgs
	}
	if !c.linux {
		// Windows requires a 32-byte shadow space below the stack arguments
		// for every call, regardless of how many args spill.
		extra += 32
	}
	if extra > 0 && extra%16 != 0 {
		extra += 8
	}

	// Resolve the callee. Direct calls only: an indirect target is a run-time
	// value, not a symbol we can book here.
	target := name
	if !indirect {
		if !c.funcs[name] && name != "main" {
			if lib := clibCStore(c.linux); lib != nil {
				if _, ok := lib.funcs[name]; ok {
					c.need[name] = true // built-in C library function
				} else {
					c.calls[name] = true
				}
			} else {
				c.calls[name] = true
			}
		}
	}
	// Reserve the struct-return result buffer BELOW the argument spill
	// slots: it must stay live while the arguments are evaluated (an
	// argument may itself nest calls) and is consumed by the caller
	// afterwards (decl / assignment / return / argument position).
	resK, resSl := 0, 0
	if sretSz > 0 {
		resSl = (sretSz + 7) / 8
		resK = c.tmpDepth + 1
		c.tmpDepth += resSl
	}
	slots := make([]argSlot, nargs)
	consumed := 0
	// Indirect call: evaluate the target address now and park it in its own
	// frame slot, above the result buffer and below the argument slots.
	// Argument evaluation may itself contain calls, so nothing here may be
	// left in a volatile register.
	tgtSlot := 0
	if indirect {
		if _, err := c.genExprT(fnExpr); err != nil {
			return TInt, err
		}
		c.tmpDepth++
		tgtSlot = c.tmpDepth
		c.emit("mov [rbp%+d], rax", c.tmpSlot(tgtSlot))
		consumed++
	}
	for i := 0; i < nargs; i++ {
		// Struct/union arguments are passed by hidden pointer: evaluate the
		// ADDRESS of the value into r10 and spill that address into one GP
		// slot (the callee copies the bytes into its own local slot).
		if at := c.exprType(args[i]); isAgg(at) {
			if err := c.structSrcAddr(args[i], at); err != nil {
				return TInt, err
			}
			if c.resStruct {
				// The address points into a nested call's result buffer;
				// claim its slots so later argument evaluation cannot
				// overwrite the bytes we are about to pass by address. The
				// claim is released together with the argument slots after
				// the call returns.
				c.tmpDepth += c.resStructSl
				consumed += c.resStructSl
				c.resStruct = false
			}
			c.tmpDepth++
			c.emit("mov [rbp%+d], r10", c.tmpSlot(c.tmpDepth))
			slots[i] = argSlot{slot: c.tmpDepth, typ: TInt}
			consumed++
			continue
		}
		t, err := c.genExprT(args[i])
		if err != nil {
			return TInt, err
		}
		// C's usual conversion at call sites: an int argument passed to a
		// declared floating parameter is widened to double before it is
		// spilled, so the callee (which reads FP params from an XMM register)
		// sees the right value. A float parameter is narrowed back to a
		// single when the argument is marshalled into its XMM/stack slot.
		f32 := false
		if !varargs && i < len(paramTypes) && paramTypes[i] != nil {
			pt := paramTypes[i]
			if pt.IsFloating() {
				f32 = pt.Kind == KFloat
				if t == TInt {
					c.emit("cvtsi2sd xmm0, rax")
					t = TDouble
					c.resTyp = TDouble
				}
			}
		}
		c.tmpDepth++
		if t == TDouble && !varargs {
			c.emit("movsd [rbp%+d], xmm0", c.tmpSlot(c.tmpDepth))
		} else {
			if t == TDouble { // varargs: double bits ride in a GP slot
				c.emit("movq rax, xmm0")
			}
			c.emit("mov [rbp%+d], rax", c.tmpSlot(c.tmpDepth))
		}
		slots[i] = argSlot{slot: c.tmpDepth, typ: t, f32: f32}
		consumed++
	}
	if extra > 0 {
		c.emit("sub rsp, %d", extra)
	}
	// regIdx is an argument's ABI register-slot index: shifted by one for
	// struct-returning calls, whose slot 0 carries the hidden result
	// pointer. XMM indices: Windows numbers XMM argument registers
	// positionally (shared with the GP slot counter), SysV by FP-arg order.
	xmmIdx := 0
	for i := 0; i < nargs; i++ {
		s := slots[i]
		regIdx := i + regShift
		xmmAt := regIdx
		if c.linux {
			xmmAt = xmmIdx
		}
		if varargs {
			if regIdx < len(argRegs) {
				c.emit("mov %s, [rbp%+d]", argRegs[regIdx], c.tmpSlot(s.slot))
				continue
			}
			c.emit("mov rax, [rbp%+d]", c.tmpSlot(s.slot))
			c.emit("mov [rsp+%d], rax", c.stackArgOff(regIdx-len(argRegs)))
			continue
		}
		if s.typ == TDouble {
			if regIdx < len(argRegs) && xmmAt < len(argXMM) {
				if s.f32 {
					// float parameter: the ABI carries a single in the low
					// 32 bits, so narrow the double here. Load the double
					// straight into the destination XMM register and narrow
					// in place -- do NOT route through xmm0 as a scratch,
					// because xmm0 may itself be one of the argument
					// registers the previous iteration already populated.
					c.emit("movsd %s, [rbp%+d]", argXMM[xmmAt], c.tmpSlot(s.slot))
					c.emit("cvtsd2ss %s, %s", argXMM[xmmAt], argXMM[xmmAt])
				} else {
					c.emit("movsd %s, [rbp%+d]", argXMM[xmmAt], c.tmpSlot(s.slot))
				}
				xmmIdx++
				continue
			}
			c.emit("movsd xmm0, [rbp%+d]", c.tmpSlot(s.slot))
			if s.f32 {
				// A stack-passed float sits in the low half of an 8-byte slot
				// (the callee reads it in place with a 4-byte load).
				c.emit("cvtsd2ss xmm0, xmm0")
				c.emit("movss [rsp+%d], xmm0", c.stackArgOff(regIdx-len(argRegs)))
				continue
			}
			c.emit("movq rax, xmm0")
			c.emit("mov [rsp+%d], rax", c.stackArgOff(regIdx-len(argRegs)))
			continue
		}
		if regIdx < len(argRegs) {
			c.emit("mov %s, [rbp%+d]", argRegs[regIdx], c.tmpSlot(s.slot))
			continue
		}
		c.emit("mov rax, [rbp%+d]", c.tmpSlot(s.slot))
		c.emit("mov [rsp+%d], rax", c.stackArgOff(regIdx-len(argRegs)))
	}
	// The hidden result pointer rides in the first integer argument
	// register; its buffer was reserved below the argument slots.
	if sretSz > 0 {
		c.emit("lea %s, [rbp%+d]", argRegs[0], c.tmpSlot(resK))
	}
	if indirect {
		// Reload the target address last: marshalling the arguments above is
		// free to clobber rax, but nothing between here and the call needs it.
		c.emit("mov rax, [rbp%+d]", c.tmpSlot(tgtSlot))
		c.emit("call rax")
	} else {
		c.emit("call %s", target)
	}
	// Windows API imports return 32-bit values (BOOL/DWORD/int) in EAX; the
	// upper 32 bits of RAX are not guaranteed to be zero, unlike goclib
	// functions which leave a clean 64-bit RAX. goc's register model treats
	// every int-class result as a full 64-bit word, so widen the result to
	// match the declared return type: sign-extend for signed int, zero-extend
	// for unsigned (DWORD/UINT). 8-byte returns (HANDLE, LONG, pointers) are
	// left untouched.
	if !c.linux && !indirect {
		if f, ok := c.funcDefs[name]; ok && externDLL[name] != "" &&
			f.Ret != nil && f.Ret.Kind == KInt && f.Ret.Width < 8 {
			sh := 64 - 8*f.Ret.Width
			c.emit("shl rax, %d", sh)
			if f.Ret.Signed {
				c.emit("sar rax, %d", sh)
			} else {
				c.emit("shr rax, %d", sh)
			}
		}
	}
	if extra > 0 {
		c.emit("add rsp, %d", extra)
	}
	// Free the argument spill slots (including any claimed nested-call
	// result buffers). A struct-returning call's OWN result buffer stays
	// live below them until its consumer releases it.
	c.tmpDepth -= consumed

	// A float return arrives in the low 32 bits of xmm0. Widen it back to the
	// double every expression is carried in, so the caller needs no special
	// case for where the value came from.
	if retT != nil && retT.Kind == KFloat {
		c.emit("cvtss2sd xmm0, xmm0")
	}
	// Result type: user functions declare it; goclib and extern calls return int.
	ret := TInt
	if retT != nil {
		ret = retT.Class()
	}
	c.resTyp = ret
	if sretSz > 0 {
		// The struct value lives in the caller's result buffer, NOT in rax;
		// consumers (decl / assignment / return / argument) must go through
		// structSrcAddr and releaseResStruct.
		c.resStruct = true
		c.resStructSz = sretSz
		c.resStructK = resK
		c.resStructSl = resSl
		c.resSigned = false
		c.resW = 8
		return ret, nil
	}
	c.resStruct = false
	if retT != nil {
		switch {
		case retT.Kind == KInt:
			c.resSigned = retT.Signed
			c.resW = retT.Width
		case retT.Kind == KPtr || retT.Kind == KFunc:
			c.resSigned = false
			c.resW = 8
		default:
			c.resSigned = true
			c.resW = 4
		}
	} else {
		// goclib and extern calls are declared as returning int.
		c.resSigned = true
		c.resW = 4
	}
	return ret, nil
}

// encodeStr renders decoded string bytes as a double-quoted literal with
// escapes, for goa's db directive.
func encodeStr(b []byte) string {
	var sb strings.Builder
	for _, ch := range b {
		switch ch {
		case '"':
			sb.WriteString("\\\"")
		case '\\':
			sb.WriteString("\\\\")
		case '\n':
			sb.WriteString("\\n")
		case '\t':
			sb.WriteString("\\t")
		case '\r':
			sb.WriteString("\\r")
		default:
			if ch >= 32 && ch < 127 {
				sb.WriteByte(ch)
			} else {
				fmt.Fprintf(&sb, "\\%03o", ch)
			}
		}
	}
	return sb.String()
}

// formatDouble renders a float64 as a Go-syntax literal that goa's `dq`
// directive can parse back into IEEE-754 bits ("1.5", "0x1.2p3", ...).
//
// 'g' formatting would print integral doubles as bare integers ("10"), and goa
// parses those via ParseInt -> the integer bit pattern (0xa) instead of the
// double (0x4024000000000000). Force a trailing ".0" so goa's ParseFloat path
// is taken.
func formatDouble(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
