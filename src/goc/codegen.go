package compiler

import (
	"fmt"
	"goc/common"
	"goc/common/link"
	"goc/frontend"
	"path/filepath"
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
	typ *frontend.Type
	reg string
	// addr is set when the variable's address is taken somewhere in the
	// function (&x). Such a variable can be written through an int* with a
	// 4-byte store, which leaves the upper half of its 8-byte slot holding
	// whatever was there before -- so loading it must re-extend from the
	// 4-byte value instead of reading all 8 bytes (see loadVar).
	addr bool
	// ptr is set when the ptrCapable analysis (see src/ptrcap.go) determined
	// that this int-typed variable may hold a 64-bit pointer value. Narrow
	// consumption sites then skip the movsxd (T1.6 C4): the high 32 bits are
	// meaningful pointer bits, not a materialized int's zero padding.
	ptr bool
	// vlaSize is the frame offset of the hidden slot holding a C99
	// variable-length array's byte size, and 0 for every other variable.
	//
	// A VLA has no size until the declaration that created it has run, and the
	// object itself is stored as a POINTER to a run-time stack allocation --
	// so varInfo.typ is the decayed pointer type and every subscript, decay and
	// pointer-arithmetic site works on it unchanged. What cannot work that way
	// is sizeof: it is a run-time value here, and this slot is where the
	// declaration parks it for sizeof to read back.
	vlaSize int
}

type CG struct {
	insts     []Inst // the function-body instruction stream (see Inst)
	opt       int    // optimisation level from -O; 0 keeps the legacy output
	strs      []frontend.StrLit
	strLab    map[*frontend.StrLit]string
	doubles   []float64
	doubleLab map[float64]string
	label     int
	// frontend.Block-scoped locals and parameters. Every declaration gets a unique
	// uid; its home (off/reg/typ) lives in varEnts[uid]. Name resolution
	// walks scopes (innermost last), so a declaration correctly shadows an
	// outer one with the same name, and sibling blocks may reuse a name
	// without their homes colliding.
	varEnts map[int]varInfo  // uid -> home
	scopes  []map[string]int // name -> uid; innermost scope is last
	varUID  int              // next uid to assign
	// addrTaken holds the locals whose address is taken (&x) in the current
	// function; declareVar copies the flag into each varInfo.addr.
	addrTaken map[string]bool
	// ptrCapable holds the current function's ptrCapable analysis result
	// (variable name -> may hold a 64-bit pointer); declareVar copies the flag
	// into each varInfo.ptr. Built once per function by analyzePtrCapable
	// before any declareVar runs (see src/ptrcap.go).
	ptrCapable map[string]bool
	// t21Called is the set of function names some function calls. T2.1 R2
	// needs it before generating any body: a small call-free function that
	// nobody calls is never inlined, so its parameters can safely live in
	// callee-save registers, while the same function WITH a caller would
	// lose its inlinability. Filled once from prog.Funcs.
	t21Called map[string]bool
	// resPtr records whether the value currently in rax was loaded from a
	// ptrCapable variable: such a value's high 32 bits are pointer bits, and
	// the narrow sites (N5b/N11/N12/N17/N22) must not movsxd it. Every
	// expression evaluation resets it except a bare load of a marked variable.
	resPtr  bool
	declUID map[*frontend.DeclStmt]int // declaration node -> uid (filled during gather)
	// clOff maps each compound literal in the current function to its
	// persistent frame slot. The unnamed object must stay addressable for as
	// long as the statement/expression containing it runs (a call argument,
	// an &-operand), so unlike transient tmpSlots it is never reused, and it
	// is re-initialised on every evaluation (fresh object per evaluation --
	// observable inside loops).
	clOff      map[*frontend.CompoundLit]int
	localBytes int // bytes consumed by stack-resident locals (incl. array padding)
	regArea    int // bytes reserved just below rbp for saved callee-save regs
	// saveRegs is the callee-save set the current function actually pushes and
	// restores, in push order. It is a subset of calleeSaveAll: a register the
	// body never writes needs no save at all (the ABI only demands that a
	// callee-save register be preserved if it is modified). See
	// calleeSavePool. Empty is legal -- then the prologue pushes nothing.
	saveRegs  []string
	globals   map[string]bool           // names of program-level (global/static) variables
	globalLab map[string]string         // name -> .data label for a global variable
	globalTyp map[string]*frontend.Type // name -> declared type of a global variable
	// Static locals: a "static int x;" inside a function gets a unique .data
	// label (collision-free even when two functions name their static "x") and
	// persists across calls. c.staticVars maps the source name to that label
	// for the function currently being generated; c.staticList accumulates every
	// static local across all functions so emitAssembly can lay them out in
	// .data. staticSeq gives each a unique label.
	//
	// c.staticScopes gives static locals real block scoping. A flat
	// name->label map cannot express it: two blocks each declaring
	// `static const char *srcs[]` would both read whichever was declared last,
	// because codegen resolves names as it walks the statements. The stack runs
	// in lockstep with c.scopes, so lookupStatic walks it innermost-first and a
	// binding disappears from view exactly when its block does.
	staticVars   map[string]string // label per source name, for emission
	staticScopes []map[string]string
	// staticLabelOf maps a static local's declaration line to the label the
	// frame-layout pre-pass minted for it, so genStmt can bind the name to that
	// exact object when it reaches the declaration in source order.
	staticLabelOf map[int]string
	staticList    []staticEmit
	staticSeq     int
	// staticPrefix distinguishes one unit's function-local statics from another's.
	// The bare label is G_st<N>_<name>, and N restarts at0 in every unit, so two
	// units that both declare `static int v` inside a function of the same name
	// mint the same label -- and a program linked from both would then have two
	// definitions of one symbol. A unit that will become an object file carries
	// a prefix that no other unit can produce; a whole program needs none,
	// because there is only one unit to be unique among.
	staticPrefix string
	// internalSyms names the file-scope symbols declared `static`: functions and
	// variables both. They go into the object as IMAGE_SYM_CLASS_STATIC so a
	// program linked from two units that each declare `static int scale(...)`
	// keeps both copies instead of reporting the second as a duplicate.
	//
	// It is only consulted when relocatable is set. A whole program has one unit
	// per name by definition, and a one-shot build merges everything into one
	// object anyway, so marking them there would change existing output for no
	// reason.
	internalSyms map[string]bool
	// Thread-local storage: _Thread_local variables live in a dedicated .tls
	// section (one instance per thread). c.tlsVars maps a source name to its
	// layout; c.tlsList collects every TLS declaration (global or static local)
	// for emission; tlsBytes tracks the running .tls offset for alignment.
	tlsVars       map[string]*tlsVarInfo
	tlsList       []*frontend.DeclStmt
	tlsBytes      int
	usedRegs      []string        // callee-save registers actually used as local homes
	tmpDepth      int             // live expression-temporary slots
	grownMaxTmp   int             // deepest maxTmp the frame has already been grown to cover
	funcs         map[string]bool // user-defined functions (by name)
	funcDefs      map[string]*frontend.FuncDecl
	calls         map[string]bool // functions called that are not defined here
	need          map[string]bool // goclib functions this program actually uses
	mainTakesArgs bool            // main declares parameters: the Windows stub must build argc/argv
	// relocatable relaxes the two assumptions a single-pass build can make.
	//
	// A whole-program build has every fact in front of it: main is here, every
	// called function is either in the C library or a known DLL, so a name it
	// cannot account for is a mistake worth reporting. An object file has no
	// such luxury -- it is one translation unit out of several, and the functions
	// it calls may well be defined in a sibling unit that has not been compiled
	// yet. So with this set, a missing main and an unaccounted-for call are both
	// not errors: the first is the linker's business, and the second becomes an
	// undefined symbol the linker resolves.
	relocatable bool
	// entryFn is the program entry function. entryIsGUI records that it is
	// one of the MSVC GUI entries (WinMain / wWinMain) rather than main, and
	// entryWide that it is the Unicode one -- which also decides whether the
	// command line is fetched as LPWSTR or LPSTR. A GUI program has no main
	// at all, so the stub calls the named function and builds its arguments.
	entryFn    string
	entryIsGUI bool
	entryWide  bool
	exitSym    string // entry-stub terminator: "exit" (full C exit) or "__goclib_exit" (bare)
	// Built-in C library (clibCStore): needed C functions are emitted through
	// genFunc (which marks more needs, so Gen iterates to a fixpoint), and the
	// library's file-scope variables join the .data pool -- but only those the
	// program actually references (libGlobUsed).
	libEmitted map[string]bool
	// libSyms is the set of library symbol names this unit's image defines, for
	// the linker to recognise a second inlined copy as the same library. A
	// library function's name is its C ABI name, so unlike the compiler's own
	// labels it cannot be marked by a prefix -- the set is what carries the
	// information.
	libSyms map[string]bool
	// skipFuncs names functions the caller is producing as LLVM IR. They are
	// neither generated nor pulled in from the C library, but everything they
	// call still is -- so they seed c.need and genClibFuncs ignores them. That
	// split is what lets the two generators share one program without either
	// one duplicating or dropping a symbol.
	skipFuncs    map[string]bool
	libGlobNames map[string]bool
	libGlobUsed  map[string]bool
	libGlobals   []*frontend.DeclStmt
	linux        bool              // true -> SysV ABI + ELF output
	curRet       *frontend.Type    // return type of the function being generated
	curParam     []*frontend.Type  // parameter types of the current function
	resTyp       frontend.CType    // type of the value left by the last genExprT
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
	resBig       bool              // the last genExprT produced a _BitInt value: r10 holds its address
	resBigT      *frontend.Type    // its exact _BitInt type
	resBigK      int               // tmpSlot index of its result buffer (0: it is a plain lvalue)
	resBigSl     int               // tmp slots claimed by that buffer (0 for a lvalue)
	bigLits      [][]uint64        // wb/uwb literal pool (.rdata word arrays)
	bigLab       map[string]string // literal key -> .rdata label
	maxTmp       int               // deepest tmpSlot index reached by this function
	frameIdx     int               // index (in c.insts) of the prologue's frame allocation
	frameLen     int               // instruction count of that block (probe loop = several)
	chkSeq       int               // stack-probe label counter
	curFrame     int               // current frame bytes, grown by growFrameForTemps
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
	d   *frontend.DeclStmt
}

// tlsVarInfo records the layout of one thread-local variable in the .tls
// section. off is the byte offset from the .tls section start; lab is the
// assembly label naming the variable (also its rip-resolvable address on Linux).
// typ is the declared type (used for emission and access-code generation).
type tlsVarInfo struct {
	off int
	lab string
	typ *frontend.Type
}

// argRegs returns the integer argument registers for the target ABI.
func (c *CG) argRegs() []string {
	if c.linux {
		return []string{"rdi", "rsi", "rdx", "rcx", "r8", "r9"} // SysV AMD64
	}
	return []string{"rcx", "rdx", "r8", "r9"} // Windows x64
}

// callArgRegs returns the integer argument registers to use at a *call site*
// for the callee named `name`.
//
// A Linux ELF target has no libc: goa turns every extern in externLinux into a
// `mov rax,N; mov r10,rcx; call __goc_syscall; ret` stub, so the callee is the
// raw syscall entry, not an ordinary function. Linux takes its fourth argument
// from r10 rather than rcx because the `syscall` instruction destroys rcx.
//
// That move is inside the stub, and used not to be -- this function returned
// rdi,rsi,rdx,r10,r8,r9 for a name it knew was a syscall. True, and also why
// the convention belongs to the stub: a caller that does not know the callee is
// one -- the LLVM back end, which calls these stubs through the ordinary SysV
// register file -- leaves the fourth argument in rcx, the kernel reads r10, and
// every call with four or more arguments fails with EFAULT. Found on select(2),
// setsockopt(2) and recvfrom(2), whose only common feature was their arity.
//
// With the move in the stub a syscall is called like any other function, so
// there is one answer for every call. gocrun's Win32 translator still reads
// a1..a6 = rdi, rsi, rdx, r10, r8, r9, and the move puts the fourth argument
// there for it as well.
func (c *CG) callArgRegs(name string, indirect bool) []string {
	return c.argRegs()
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

// dllOf maps an imported Win32 symbol to the DLL that exports it. It is filled
// from every prototype that names its import library inline, e.g.
//
//	extern BOOL CloseHandle(HANDLE h), kernel32;
//
// Each goclib header carries its own DLL name -- winbase.h -> kernel32,
// winuser.h -> user32, wingdi.h -> gdi32, and the directory/stat APIs in
// dirent.h/stat.h -> kernel32 -- so a source file is self-describing and there
// is no central ownership table to keep in sync. dllOf is populated while the C
// library and the user program are parsed: once at start-up for the built-in
// library (buildClibC), and again for every user translation unit. It is the
// single source of truth for the "extern Name, dll" imports Gen emits for the
// PE target.
//
// The C library itself (printf, strlen, malloc, ...) is implemented in portable
// C (goclib/*.c) on top of kernel32 (Windows) or syscalls (Linux), and goc
// compiles it into every program at start-up. There is no msvcrt anywhere in
// the pipeline.
var dllOf = map[string]string{}

// dllFor resolves the import library of an extern symbol. The DLL is named
// directly on the prototype that declares the symbol; if no prototype names
// one, the symbol is not a known Windows import and Gen reports a hard error
// rather than a silent guess.
func dllFor(name string) (string, bool) {
	d, ok := common.DLLNames[name]
	return d, ok
}

// externLinux lists the syscall names an ELF-target program may reach for.
// goa turns each into a `mov rax,N; syscall; ret` stub, so the C code never
// links against a library.
var externLinux = map[string]bool{
	"read": true, "write": true, "open": true, "close": true,
	"lseek": true, "mmap": true, "munmap": true, "brk": true, "ioctl": true,
	"writev": true, "nanosleep": true, "getpid": true, "kill": true,
	"exit": true, "exit_group": true, "gettimeofday": true, "__goc_clock_gettime": true,
	"unlink": true, "__goclib_rename": true,
	"__goclib_stat": true, "__goclib_mkdir": true, "__goclib_rmdir": true,
	"__goclib_getdents64": true,
	"__goclib_getcwd":     true, "__goclib_chmod": true, "__goclib_access": true,
	"__goclib_fstat": true,
	"__goclib_vfork": true, "__goclib_execve": true, "__goclib_wait4": true,
	"__goclib_clone": true, "__goclib_futex": true, "__goclib_gettid": true,
	"__goclib_sched_yield": true, "__goclib_exit_thread": true,
	// Sockets. select matters here more than most: it is the only entry on this
	// list with five arguments, so its fifth lands in r8, not rcx -- exactly the
	// trap the comment above is about.
	"__goclib_socket": true, "__goclib_connect": true, "__goclib_accept": true,
	"__goclib_sendto": true, "__goclib_recvfrom": true, "__goclib_shutdown": true,
	"__goclib_bind": true, "__goclib_listen": true, "__goclib_getsockname": true,
	"__goclib_getpeername": true, "__goclib_setsockopt": true,
	"__goclib_getsockopt": true, "__goclib_select": true,
	"__goclib_fcntl": true,
}

// ---------------------------------------------------------------------------
// goclib: the C library
// ---------------------------------------------------------------------------
//
// The library is plain C (goclib/gocommon.c plus the headers it includes) that
// goc compiles at start-up exactly like a user program, once per target.
// Functions land in the output only when a program actually calls them (plus
// their transitive callees), so a hello-world does not pay for malloc.
//
// It is read from disk rather than embedded, so goc and the library can be
// separate projects; see libfs.go for the search order and for what that costs
// (goc.exe is no longer self-contained).
// The C library lives in package clib, shared with the LLVM back end (gocl).
// What is left here is the vocabulary this package uses: the compiled program
// per target, the function names a program may call, and the two entry points
// that forward to common.
var (
	// clibCWin and clibCLinux are the compiled library per target. They are
	// read through storeFor rather than aliased at init time, because the
	// library is compiled lazily: an alias captured before the first compile
	// would be nil forever.
	clibCErr  error
	goclibErr error
)

// storeFor returns the compiled library for a target, or nil when that target
// did not build.
func storeFor(linux bool) *common.Program {
	if linux {
		return common.Linux
	}
	return common.Win
}

// SetLibrary redirects the C library. It must be called before the first
// compile, and the load is deferred rather than performed here because of Go's
// initialisation order -- a package's init() runs before its importer's, so an
// entry point injecting a library from its own init() would otherwise arrive
// too late and silently do nothing. See common.SetLibrary.
func SetLibrary(src common.Source) { common.SetLibrary(src) }

// ensureLib compiles the C library on first use. Every entry point calls it
// before anything else: the preprocessor resolves `#include <stdio.h>` out of
// the library, so reaching that with no library is a nil dereference inside a
// header lookup rather than a diagnostic.
func ensureLib() {
	common.Ensure()
	goclibErr = common.Err
	clibCErr = common.Err
}

// ---------------------------------------------------------------------------
// goclib in C: the library is compiled by goc itself
// ---------------------------------------------------------------------------
//
// gocommon.c (plus the headers it includes) is a plain C translation unit that
// goc compiles at start-up exactly like a user program. Every function the
// library defines is emitted through the regular code generator (genFunc) when
// a program needs it, so the C source is the single implementation of the
// built-in library.
//
// Function implementations may live either in a declaration header or in a .c
// file: the umbrella gocommon.h is processed as its own translation unit first
// (collecting any definitions placed in the headers it includes), then every
// goclib/*.c. A later definition of the same name wins, so a .c definition
// overrides a header one and duplicates never reach the linker.

// Diagnostic runtime (compile-time optional via `goc -rtdiag`). When enabled,
// a second copy of the C library is compiled with GOC_RTDIAG defined,
// so goclib's malloc/free/calloc/realloc wrappers and the allocation tracker
// in rt.c activate. The two targets are compiled independently so a failure
// on one does not block the other.
var (
	rtdiagMode     bool
	rtdiagErr      error
	clibCWinDiag   *common.Program
	clibCLinuxDiag *common.Program
)

// SetRtdiag turns the compile-time diagnostic runtime on. It eagerly compiles
// both target libraries with GOC_RTDIAG defined so any error surfaces before
// Gen runs (Gen checks rtdiagErr up front). Safe to call once; repeated calls
// are ignored.
func SetRtdiag(on bool) {
	if !on || rtdiagMode {
		return
	}
	rtdiagMode = true
	var e error
	if clibCWinDiag, e = common.Build(false, "#define GOC_RTDIAG 1\n"); e != nil {
		rtdiagErr = e
		return
	}
	if clibCLinuxDiag, e = common.Build(true, "#define GOC_RTDIAG 1\n"); e != nil {
		rtdiagErr = e
	}
}

// calleeSaveAll is the complete set of callee-save GPRs the register allocator
// may use as local-register homes (and as scratch). Their order fixes the
// allocation order AND the push order, and hence each register's save slot:
// the k-th pushed register lands at [rbp-8*(k+1)].
//
// A function does NOT push all four. It pushes exactly the subset the body can
// write, computed by calleeSavePool: the ABI only requires preserving a
// callee-save register that is actually modified, so pushing one the body never
// touches is pure dead work (a fixed 3-push/3-restore tax on every call, which
// is what made recursive fib cost 5.4x gcc).
var calleeSaveAll = []string{"rbx", "r12", "r13", "r14"}

// calleeSavePool returns the callee-save registers a function must push and
// restore, in push order. used is the set of registers handed out as variable
// homes; any calleeSaveAll member not in it is never written by the body and
// is therefore dropped.
//
// An inline-assembly function keeps the whole pool: the hand-written asm text
// may use any of them as scratch, and codegen cannot see those writes.
func calleeSavePool(used []string, hasAsm bool) []string {
	if hasAsm {
		return calleeSaveAll
	}
	sub := make([]string, 0, len(calleeSaveAll))
	for _, r := range calleeSaveAll {
		for _, u := range used {
			if u == r {
				sub = append(sub, r)
				break
			}
		}
	}
	return sub
}

// alignFrame rounds a raw frame size up so that every call site in the current
// function sees a 16-byte-aligned rsp.
//
// At the function entry R (rsp just after the return address was pushed) is
// 8 mod 16, because the caller had rsp 16-aligned before its `call`. The
// prolog then pushes rbp plus c.saveRegs and finally subtracts the frame, so
// a call in the body runs with rsp = R - 8*(1+len(saveRegs)) - frame, and both
// ABIs require that to be 0 mod 16:
//
//	8 - 8*(1+n) - frame == 0 (mod 16)  =>  8*n + frame == 0 (mod 16)
//
// Rounding the frame to a plain multiple of 16 -- what the code did back when
// the prolog always pushed all four callee-saves -- satisfies that only when
// 8*n is itself a multiple of 16, i.e. when n is EVEN. Dead callee-save
// elimination makes n vary (0..4), so the parity has to be folded in here.
// Getting it wrong misaligns rsp at every call by 8 bytes, which faults the
// moment an imported routine performs an aligned SSE store: with the naive
// fix, even `int main(void){return 0;}` segfaulted inside ExitProcess.
func (c *CG) alignFrame(n int) int {
	if want := (8*len(c.saveRegs) + n) % 16; want != 0 {
		n += 16 - want
	}
	return n
}

// clibCStore picks the compiled C library for a target. With the diagnostic
// runtime enabled it returns the GOC_RTDIAG-instrumented copy instead.
func clibCStore(linux bool) *common.Program {
	if rtdiagMode {
		if linux {
			return clibCLinuxDiag
		}
		return clibCWinDiag
	}
	return storeFor(linux)
}

// goclibNames lists the public (non-internal) built-in library functions, for
// error messages.
func goclibNames(linux bool) []string {
	var out []string
	if lib := common.Store(linux); lib != nil {
		for _, n := range lib.Order {
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

// collectCompoundLits walks the function body -- statements and every nested
// expression -- and returns each compound literal once, in evaluation order.
// Mirrors check.go's frontend.WalkStmts plus a full expression recursion.
func (c *CG) collectCompoundLits(f *frontend.FuncDecl) []*frontend.CompoundLit {
	var lits []*frontend.CompoundLit
	var walkExpr func(frontend.Expr)
	var walkStmt func(frontend.Stmt)
	walkExpr = func(e frontend.Expr) {
		switch n := e.(type) {
		case nil:
			return
		case *frontend.Unary:
			walkExpr(n.E)
		case *frontend.Binary:
			walkExpr(n.L)
			walkExpr(n.R)
		case *frontend.Call:
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *frontend.IndirectCall:
			// A UFCS-resolved call carries its whole rewritten form in
			// n.UFCS; that is what codegen emits, so that is what we walk.
			if n.UFCS != nil {
				walkExpr(n.UFCS)
			} else {
				walkExpr(n.Fn)
				for _, a := range n.Args {
					walkExpr(a)
				}
			}
		case *frontend.Index:
			walkExpr(n.Base)
			walkExpr(n.Idx)
		case *frontend.CondExpr:
			walkExpr(n.Cond)
			walkExpr(n.Then)
			walkExpr(n.Else)
		case *frontend.CommaExpr:
			walkExpr(n.Left)
			walkExpr(n.Right)
		case *frontend.CastExpr:
			walkExpr(n.E)
		case *frontend.IncDecExpr:
			walkExpr(n.E)
		case *frontend.MemberExpr:
			walkExpr(n.Base)
		case *frontend.SizeofExpr:
			if n.E != nil {
				walkExpr(n.E)
			}
		case *frontend.AssignExpr:
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
		case *frontend.VaArgExpr:
			walkExpr(n.Ap)
		case *frontend.GenericExpr:
			// Only the branch the checker selected reaches codegen.
			if n.Chosen != nil {
				walkExpr(n.Chosen)
			}
		case *frontend.BraceInit:
			for _, el := range n.Elems {
				walkExpr(el.E)
			}
		case *frontend.CompoundLit:
			lits = append(lits, n)
			if n.Init != nil {
				walkExpr(n.Init)
			}
		}
	}
	walkStmt = func(s frontend.Stmt) {
		switch n := s.(type) {
		case nil:
			return
		case *frontend.Block:
			for _, st := range n.Stmts {
				walkStmt(st)
			}
		case *frontend.DeclList:
			for _, d := range n.Decls {
				walkStmt(d)
			}
		case *frontend.DeclStmt:
			walkExpr(n.Init)
		case *frontend.AssignStmt:
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
		case *frontend.ExprStmt:
			walkExpr(n.E)
		case *frontend.ReturnStmt:
			walkExpr(n.E)
		case *frontend.IfStmt:
			walkExpr(n.Cond)
			walkStmt(n.Then)
			walkStmt(n.Else)
		case *frontend.WhileStmt:
			walkExpr(n.Cond)
			walkStmt(n.Body)
		case *frontend.ForStmt:
			walkStmt(n.Init)
			walkExpr(n.Cond)
			walkExpr(n.Post)
			walkStmt(n.Body)
		case *frontend.DoWhileStmt:
			walkStmt(n.Body)
			walkExpr(n.Cond)
		case *frontend.SwitchStmt:
			walkExpr(n.Src)
			walkStmt(n.Body)
		case *frontend.LabelStmt:
			walkStmt(n.Stmt)
		}
	}
	walkStmt(f.Body)
	return lits
}

// tmpSlot returns the rbp offset of the k-th (1-indexed) expression
// temporary. These live in the function frame, above the locals, and are
// used to spill the left operand of a binary expression without touching
// RSP (so 16-byte stack alignment at calls is preserved).
func (c *CG) tmpSlot(k int) int {
	// Remember the deepest temporary this function reaches: a wide _BitInt
	// (or a large struct) can claim thousands of slots, far more than the
	// fixed scratchSlots reservation, and the prologue's `sub rsp` is grown
	// to match before the function ends (see growFrameForTemps).
	if k > c.maxTmp {
		c.maxTmp = k
	}
	return -(c.regArea + c.localBytes + 8*k)
}

// tmpSlotBlock returns the rbp offset of the BASE of an n-slot temporary
// whose slot indices are k..k+n-1 (1-indexed, k being the shallowest).
//
// Multi-slot values (structs, unions, _BitInt) are written UPWARD from their
// base, so the base must be the DEEPEST slot of the block: using the
// shallowest slot makes the value spill into the slots above it -- which
// belong to the last local or an earlier temporary. That overlap silently
// corrupted "struct P b = mk(7)" (b.y came back as b.x) and crashed every
// _BitInt operation whose result buffer sat against a wide local.
func (c *CG) tmpSlotBlock(k, n int) int {
	if n < 1 {
		n = 1
	}
	return c.tmpSlot(k + n - 1)
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
func (c *CG) slotWidth(t *frontend.Type) int {
	if t == nil {
		return 8
	}
	switch t.Kind {
	case frontend.KInt:
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
	case frontend.KBool:
		return 1 // bool stored as 1 byte
	case frontend.KFloat:
		// A float scalar really is 4 bytes in memory: it is stored as an IEEE
		// single and widened to double on every read. (A double stays 8.)
		return 4
	case frontend.KPtr, frontend.KFunc, frontend.KDouble:
		return 8
	case frontend.KLongDouble:
		// binary128: a 16-byte value, two full slots (see tf128.go).
		return 16
	case frontend.KStruct, frontend.KUnion:
		if t.Size != 0 {
			return t.Size
		}
	case frontend.KBitInt:
		// A _BitInt(N) value occupies ceil(N/64) 8-byte words in its slot.
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
	case 1, 2: // char/bool or short: a single sign/zero-extending move
		c.extendRegNarrow("rax", width, signed)
	case 4: // int
		if signed {
			c.emitWrap("shl rax, 32")
			c.emitWrap("sar rax, 32")
		} else {
			// T1.6 (C5-b): mark the zero-extending half too. Only a marked pair
			// is visible to elimRedundantExt, so without this the unsigned form
			// could never be dropped or collapsed at all.
			c.emitWrap("shl rax, 32")
			c.emitWrap("shr rax, 32")
		}
		// case 8 for long/pointer is no-op, fall through
	}
}

// narrowSubReg maps a 64-bit GP register to its low-byte (8-bit) and low-word
// (16-bit) sub-register spellings, for emitting a single sign/zero-extending
// move in extendRegNarrow.
var narrowSubReg = map[string][2]string{
	"rax": {"al", "ax"}, "rbx": {"bl", "bx"}, "rcx": {"cl", "cx"},
	"rdx": {"dl", "dx"}, "rsi": {"sil", "si"}, "rdi": {"dil", "di"},
	"r8": {"r8b", "r8w"}, "r9": {"r9b", "r9w"}, "r10": {"r10b", "r10w"},
	"r11": {"r11b", "r11w"}, "r12": {"r12b", "r12w"}, "r13": {"r13b", "r13w"},
	"r14": {"r14b", "r14w"}, "r15": {"r15b", "r15w"},
}

// extendRegNarrow sign/zero-extends the low width bytes of reg to the full
// 64-bit register using a single movsx/movzx, replacing the old
// `shl reg, N*8 ; sar/shr reg, N*8` shift pair (or `and reg, mask`). goc loads a
// char/short into the register's low byte/word, so the whole extension is one
// instruction (3 bytes vs ~8, and one fewer uop). It is emitted directly by
// codegen -- not by a peephole pass -- so it helps every -O level, including
// -O0, and carries none of the flag-discipline risk a late peephole would.
func (c *CG) extendRegNarrow(reg string, width int, signed bool) {
	sub, ok := narrowSubReg[reg]
	if !ok {
		// Unknown register: fall back to the shift idiom so behaviour is
		// preserved (callers only pass GP index/accumulator registers).
		switch width {
		case 1:
			if signed {
				c.emit("shl " + reg + ", 56")
				c.emit("sar " + reg + ", 56")
			} else {
				c.emit("and " + reg + ", 0xff")
			}
		case 2:
			if signed {
				c.emit("shl " + reg + ", 48")
				c.emit("sar " + reg + ", 48")
			} else {
				c.emit("and " + reg + ", 0xffff")
			}
		}
		return
	}
	if width == 1 {
		if signed {
			c.emit("movsx %s, %s", reg, sub[0])
		} else {
			c.emit("movzx %s, %s", reg, sub[0])
		}
	} else { // width 2
		if signed {
			c.emit("movsx %s, %s", reg, sub[1])
		} else {
			c.emit("movzx %s, %s", reg, sub[1])
		}
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
		c.emitWrap("shl rax, 32")
		c.emitWrap("sar rax, 32")
	} else {
		// T1.6 (C5-b): mark the zero-extending half too. Only a marked pair is
		// visible to elimRedundantExt, so without this the unsigned form could
		// never be dropped or collapsed at all.
		c.emitWrap("shl rax, 32")
		c.emitWrap("shr rax, 32")
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
func (c *CG) semWOf(t *frontend.Type) int {
	if t == nil {
		return 8
	}
	if t.Kind == frontend.KInt {
		if t.Width < 1 {
			return 1
		}
		return t.Width
	}
	if t.Kind == frontend.KBool {
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
	// IntWrap marks the two instructions of a compiler-emitted int
	// canonicalisation pair (shl r,32; sar r,32 from canonInt/extendInt).
	// A peephole may only treat a shift pair as an int wrap when both halves
	// carry the flag -- a user's own shifts never do.
	IntWrap bool
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

// emitWrap is emit for one half of an int canonicalisation pair: the IntWrap
// marker lets elimRedundantExt recognise (and possibly drop) the pair.
func (c *CG) emitWrap(format string, a ...any) {
	c.insts = append(c.insts, Inst{Kind: instInstr, Text: "\t" + fmt.Sprintf(format, a...), IntWrap: true})
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
func (c *CG) loadGlobal(lab string, gt *frontend.Type) (frontend.CType, error) {
	if gt != nil && gt.IsArray() {
		c.emit("lea rax, [rip+%s]", lab)
		c.resTyp = frontend.TInt
		c.resSigned = false
		c.resW = 8
		return frontend.TInt, nil
	}
	if isAgg(gt) {
		// A whole struct/union value cannot be loaded into rax; consumers
		// must go through genLValue (see structSrcAddr).
		return frontend.TInt, fmt.Errorf("cannot load struct/union value at %q directly", lab)
	}
	if gt != nil && gt.IsFloating() {
		// A float global is stored as a 4-byte single and widened on the way
		// in, exactly like a float local.
		if gt.Kind == frontend.KFloat {
			c.emit("movss xmm0, [rip+%s]", lab)
			c.emit("cvtss2sd xmm0, xmm0")
		} else {
			c.emit("movsd xmm0, [rip+%s]", lab)
		}
		c.resTyp = frontend.TDouble
		c.resSigned = false
		c.resW = 8
		return frontend.TDouble, nil
	}
	// Narrow globals live in 8-byte slots (every global gets a resq), but
	// their address is always available to the whole program, so a store
	// through a char*/short*/int* (goclib's %n, %hhn, %h stores are exactly
	// this) leaves the C-width value with stale high bytes -- the same
	// staleness loadVar re-extends for address-taken locals. Load exactly the
	// semantic width and extend, mirroring the local-variable load path.
	w := c.semWOf(gt)
	signed := gt != nil && gt.Kind == frontend.KInt && gt.Signed
	switch w {
	case 1:
		c.emit("xor rax, rax")
		c.emit("mov al, [rip+%s]", lab)
		c.extendInt(1, signed)
	case 2:
		c.emit("mov eax, [rip+%s]", lab) // low 16 bits hold the value
		c.extendInt(2, signed)
	case 4:
		if signed {
			c.emit("movsxd rax, dword [rip+%s]", lab)
		} else {
			c.emit("mov eax, [rip+%s]", lab) // zero-extends to 64
		}
	default:
		c.emit("mov rax, [rip+%s]", lab)
	}
	c.resTyp = frontend.TInt
	c.resSigned = signed
	c.resW = w
	return frontend.TInt, nil
}

// genTLSAddr loads the linear address of a thread-local variable named name into
// r10 (mirroring genLValue for a normal variable). On Linux the .tls section is
// the main thread's instance, so a rip-relative lea suffices; on Windows the
// module's TLS block is reached through gs:0x58 (the TEB's
// ThreadLocalStoragePointer), indexed by our module's TLS index (0 here), plus
// the variable's offset inside the block.
func (c *CG) genTLSAddr(name string) error {
	t := c.tlsVars[name]
	if t == nil {
		return fmt.Errorf("internal: TLS variable %q has no layout", name)
	}
	if c.linux {
		c.emit("lea r10, [rip+%s]", t.lab)
		return nil
	}
	// Windows: the TEB's ThreadLocalStoragePointer (gs:0x58) is an array of
	// per-module TLS block pointers; the module's index lives in the loader-
	// filled goc_tls_index slot. Load it and index into the array to reach this
	// thread's copy of the .tls template.
	c.emit("mov ecx, [rip+G_goc_tls_index]")
	c.emit("xor eax, eax")
	c.emit("mov rax, gs:[rax+0x58]")
	c.emit("mov rax, [rax+rcx*8]")
	c.emit("lea r10, [rax+%d]", t.off)
	return nil
}

// genTLSLoadValue loads the value of a thread-local variable (scalar, array
// pointer, or floating point) into rax / xmm0, the same calling convention
// loadGlobal uses for a normal global. Arrays/aggregates decay to a pointer to
// element 0.
func (c *CG) genTLSLoadValue(name string) error {
	t := c.tlsVars[name]
	if t == nil {
		return fmt.Errorf("internal: TLS variable %q has no layout", name)
	}
	if err := c.genTLSAddr(name); err != nil {
		return err
	}
	if t.typ != nil && (t.typ.IsArray() || isAgg(t.typ)) {
		c.emit("mov rax, r10")
		c.resTyp = frontend.TInt
		c.resSigned = false
		c.resW = 8
		return nil
	}
	if t.typ != nil && t.typ.IsFloating() {
		if t.typ.Kind == frontend.KFloat {
			c.emit("movss xmm0, [r10]")
			c.emit("cvtss2sd xmm0, xmm0")
		} else {
			c.emit("movsd xmm0, [r10]")
		}
		c.resTyp = frontend.TDouble
		c.resSigned = false
		c.resW = 8
		return nil
	}
	c.emit("mov rax, [r10]")
	c.resTyp = frontend.TInt
	c.resSigned = t.typ != nil && t.typ.Kind == frontend.KInt && t.typ.Signed
	c.resW = c.semWOf(t.typ)
	return nil
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
		// operation sites (genBinary/genUnary/genIncDec) and in frontend.CastExpr.
		if vi.typ != nil && vi.typ.Kind == frontend.KInt && vi.typ.Width < 4 {
			c.extendInt(vi.typ.Width, vi.typ.Signed)
		}
		c.resTyp = frontend.TInt
		c.resSigned = vi.typ != nil && vi.typ.Kind == frontend.KInt && vi.typ.Signed
		c.resW = c.semWOf(vi.typ)
		c.resPtr = vi.ptr // T1.6 (C4): reg-cached loads return before the tail
		return
	}
	if vi.typ != nil && vi.typ.IsFloating() {
		// A float scalar is stored as a 4-byte IEEE single; widen it to the
		// double every expression carries. Doubles load unchanged.
		if vi.typ.Kind == frontend.KFloat {
			c.emit("movss xmm0, [rbp%+d]", vi.off)
			c.emit("cvtss2sd xmm0, xmm0")
		} else {
			c.emit("movsd xmm0, [rbp%+d]", vi.off)
		}
		c.resTyp = frontend.TDouble
		c.resSigned = false
		c.resW = 8
		return
	}
	w := c.slotWidth(vi.typ)
	signed := vi.typ != nil && vi.typ.Kind == frontend.KInt && vi.typ.Signed
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
		//
		// The exception is a variable whose address was taken: it can also be
		// written through an int*, and that store is 4 bytes wide
		// (lvalueWidth reports the C width for a deref), so the top half of
		// the slot is stale. Re-extending from the 4-byte value is what makes
		// "int *p = &x; *p = -23;" read back as -23 instead of 4294967273.
		if vi.addr && c.semWOf(vi.typ) == 4 {
			if signed {
				c.emit("movsxd rax, dword [rbp%+d]", vi.off)
			} else {
				c.emit("mov eax, [rbp%+d]", vi.off) // zero-extends to 64
			}
		} else {
			c.emit("mov rax, [rbp%+d]", vi.off)
		}
	}
	c.resTyp = frontend.TInt
	c.resSigned = signed
	c.resW = c.semWOf(vi.typ)
	// T1.6 (C4): a load of a ptrCapable variable leaves a 64-bit pointer
	// candidate in rax; the narrow sites must not movsxd it.
	c.resPtr = vi.ptr
}

// storeVar emits code that stores the value currently in rax (int) or xmm0
// (double) into variable vi's slot or home register.
func (c *CG) storeVar(vi varInfo) {
	// T1.6 (N17): a materialized signed int (resW==4, low 32 valid, high 32=0)
	// stored into a genuinely 64-bit variable (long / unsigned long / pointer)
	// must be sign-extended first, or a negative int reads back as a huge
	// positive when the slot is later consumed as a 64-bit signed value (e.g.
	// `long l = -7` printing as 4294967289, and bi_conv's `fill = -1` filling
	// 64-bit limbs with 0x00000000FFFFFFFF). Int (width-4) variables keep
	// their 8-byte slots in the materialized high-0 form (design invariant),
	// and narrow homes (char/short) get hardware truncation, so neither needs
	// this extension here.
	narrow := vi.typ != nil && !vi.typ.IsFloating() && vi.typ.Kind != frontend.KBool &&
		c.slotWidth(vi.typ) == 8 &&
		(vi.typ.Kind != frontend.KInt || vi.typ.Width == 8) &&
		c.resW == 4 && c.resSigned && !c.resPtr
	if vi.reg != "" {
		// Register-cached int local: keep it in the callee-save (which
		// survives function calls, so no spill is needed).
		if vi.typ != nil && vi.typ.Kind == frontend.KBool {
			c.normalizeBool()
		}
		if narrow {
			c.emit("movsxd rax, eax")
		}
		c.emit("mov %s, rax", vi.reg)
		return
	}
	if vi.typ != nil && vi.typ.IsFloating() {
		// Narrow the double in xmm0 down to a 4-byte IEEE single for float
		// storage; doubles store unchanged.
		if vi.typ.Kind == frontend.KFloat {
			c.emit("cvtsd2ss xmm0, xmm0")
			c.emit("movss [rbp%+d], xmm0", vi.off)
		} else {
			c.emit("movsd [rbp%+d], xmm0", vi.off)
		}
		return
	}
	if vi.typ != nil && vi.typ.Kind == frontend.KBool {
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
		if narrow {
			c.emit("movsxd rax, eax")
		}
		c.emit("mov [rbp%+d], rax", vi.off)
	}
}

// ensureType converts the value currently in rax/xmm0 (scalar) or at the
// resBig address (long double / _BitInt) to the requested type (if they
// differ) and updates c.resTyp.
func (c *CG) ensureType(want frontend.CType) error {
	// A long double value (shared resBig carrier, resBigT the type):
	// conversion out of binary128 goes through the runtime.
	if c.resBig && isLD(c.resBigT) {
		switch want {
		case frontend.TF128:
			c.resTyp = frontend.TF128
			return nil
		case frontend.TInt:
			// Best-effort signed conversion: callers that need a specific
			// integer target (its signedness matters) go through the cast
			// path, which knows the type.
			c.callBigLib("goc_tf_to_ll", []bigArg{{reg: "r10"}})
			c.releaseResBig()
			c.resTyp = frontend.TInt
			c.resW = 8
			c.resSigned = true
			return nil
		case frontend.TDouble:
			c.callBigLib("goc_tf_to_double", []bigArg{{reg: "r10"}})
			c.emit("movq xmm0, rax")
			c.releaseResBig()
			c.resTyp = frontend.TDouble
			c.resW = 8
			c.resSigned = true
			return nil
		}
		return fmt.Errorf("long double values only convert to int/double (got %v)", want)
	}
	// A scalar converting TO long double: materialise it as binary128 in a
	// fresh temporary under the shared carrier.
	if want == frontend.TF128 && !c.resBig {
		return c.tfScalarToLD()
	}
	// A _BitInt value: conversion to int truncates to the low 64-bit word
	// (the two's-complement low word IS the int64 pattern regardless of
	// signedness). Conditions must use genTruth instead (full-width test).
	if c.resBig {
		if want == frontend.TInt {
			c.emit("mov rax, [r10]")
			c.tmpDepth -= c.resBigSl
			c.resSigned = c.resBigT.Signed
			c.resW = 8
			c.resBig = false
			c.resTyp = frontend.TInt
			return nil
		}
		return fmt.Errorf("_BitInt values only convert to integer types (got %v)", want)
	}
	if c.resTyp == want {
		return nil
	}
	switch {
	case c.resTyp == frontend.TDouble && want == frontend.TInt:
		c.emit("cvttsd2si rax, xmm0")
		c.resTyp = frontend.TInt
	case c.resTyp == frontend.TInt && want == frontend.TDouble:
		// T1.6 (N22): a signed materialized int (low 32 valid, high 32 = 0)
		// must be sign-extended before cvtsi2sd, or -5 converts to
		// 4294967291.0. An unsigned int is already zero-extended (correct); a
		// long (resW==8) is a full 64-bit value; a ptrCapable int (C4) holds
		// pointer bits and must pass through unextended.
		if c.resW == 4 && c.resSigned && !c.resPtr {
			c.emit("movsxd rax, eax")
		}
		c.emit("cvtsi2sd xmm0, rax")
		c.resTyp = frontend.TDouble
	default:
		return fmt.Errorf("type mismatch: cannot convert %v to %v", c.resTyp, want)
	}
	return nil
}

// ---------------------------------------------------------------------------
// C23 _BitInt(N) ("bigint") support.
//
// A _BitInt(N) value is a little-endian array of ceil(N/64) 64-bit words in
// memory (8-byte aligned). It rides the struct by-address value model: a
// value-typed expression leaves the value's ADDRESS in r10 and sets resBig /
// resBigT / resBigK / resBigSl. Arithmetic is emitted as calls to the pure-C
// goclib/bitint.c helpers, with every operand materialised into a frame
// temporary of the operation's result width first (bigOperand below).
// ---------------------------------------------------------------------------

// bigWordsOf is the link package's _BitInt width rule: a global _BitInt is
// emitted as a word array of exactly this many words, so the code that reads
// one and the .data image holding it have to agree.
func bigWordsOf(t *frontend.Type) int {
	return link.BigWordsOf(t)
}

func ptWords(t *frontend.Type) int { return bigWordsOf(t) }

// bigArg is one argument of a goclib bitint helper call: exactly one of
// reg / imm / addrOff / valOff is set.
type bigArg struct {
	reg     string
	imm     int64
	addrOff int // lea reg, [rbp+off]
	valOff  int // mov reg, [rbp+off]
}

// callBigLib emits a direct call to a goclib bitint helper. The result (when
// the helper has one) arrives in rax. Claims shadow space on Windows and
// restores RSP alignment afterwards, matching genCall's discipline.
// emitCall grows the frame to cover any live big-aggregate temporaries, then
// emits a direct or indirect call. Big temporaries live below RSP until the
// prologue's `sub rsp` is widened to their full depth (see growFrameForTemps),
// so a call emitted before that widening would trample them -- a _BitInt
// expression like `*r = *r * base` keeps its left operand, base and the result
// buffer in frame temps across the bi_mul call, and a too-small frame makes the
// call clobber them (garbage result or an infinite loop inside the helper).
// target == "" means an indirect call through rax. Once the frame already
// covers maxTmp the grow is a cheap no-op, so calling this at every call site
// costs nothing for ordinary functions.
func (c *CG) emitCall(target string) {
	c.growFrameForTemps()
	if target == "" {
		c.emit("call rax")
	} else {
		c.emit("call %s", target)
	}
}

func (c *CG) callBigLib(name string, args []bigArg) {
	aregs := c.argRegs()
	extra := 0
	if !c.linux && len(args) > len(aregs) {
		// The 32-byte Windows shadow is reserved once in the prologue; only
		// calls that spill arguments past the register window need a per-call
		// reservation, which still requires the shadow region above the spills.
		extra = 32 + (len(args)-len(aregs))*8
	}
	if extra > 0 {
		c.emit("sub rsp, %d", extra)
	}
	for i, a := range args {
		if i < len(aregs) {
			ar := aregs[i]
			switch {
			case a.reg != "":
				if a.reg != ar {
					c.emit("mov %s, %s", ar, a.reg)
				}
			case a.addrOff != 0:
				c.emit("lea %s, [rbp%+d]", ar, a.addrOff)
			case a.valOff != 0:
				c.emit("mov %s, [rbp%+d]", ar, a.valOff)
			default:
				c.emit("mov %s, %d", ar, a.imm)
			}
		} else {
			// Extra argument past the register window: push it on the
			// caller stack (immediates only today, routed through rax).
			// growFrameForTemps only rewrites the prologue allocation, so
			// the [rsp+..] slot stays put across the call.
			c.emit("mov rax, %d", a.imm)
			c.emit("mov [rsp%+d], rax", c.stackArgOff(i-len(aregs)))
		}
	}
	c.need[name] = true
	c.emitCall(name)
	if extra > 0 {
		c.emit("add rsp, %d", extra)
	}
}

// claimBig reserves w consecutive tmp slots for a bitint temporary and
// returns the slot index and its rbp offset.
func (c *CG) claimBig(w int) (int, int) {
	k := c.tmpDepth + 1
	c.tmpDepth += w
	return k, c.tmpSlotBlock(k, w)
}

// markBig records that the value just produced is a _BitInt living at the
// claimed buffer (k, sl); r10 is left holding its address.
func (c *CG) markBig(t *frontend.Type, k, sl int) {
	c.resBig = true
	c.resBigT = t
	c.resBigK = k
	c.resBigSl = sl
	c.emit("lea r10, [rbp%+d]", c.tmpSlotBlock(k, sl))
	c.resTyp = frontend.TInt
	c.resW = 8
	c.resSigned = t.Signed
}

// releaseResBig frees the pending big-value buffer (or falls back to the
// struct-return result buffer for struct values).
func (c *CG) releaseResBig() {
	if c.resBig {
		c.tmpDepth -= c.resBigSl
		c.resBig = false
		return
	}
	c.releaseResStruct()
}

// bigOperand evaluates e and materialises its value -- converted to want's
// width and signedness -- into a freshly claimed temporary. Returns the slot
// index, slot count and rbp offset. Claims stay live until the caller's bulk
// unwind (operands must survive the helper call).
func (c *CG) bigOperand(e frontend.Expr, want *frontend.Type) (int, int, int, error) {
	et := c.exprType(e)
	w := bigWordsOf(want)
	k, off := c.claimBig(w)
	if frontend.IsBig(et) {
		if _, err := c.genExprT(e); err != nil {
			return 0, 0, 0, err
		}
		if !c.resBig {
			return 0, 0, 0, fmt.Errorf("internal: _BitInt operand did not produce a value address")
		}
		c.emit("lea r11, [rbp%+d]", off)
		if et.Bits == want.Bits {
			c.copyBytes("r11", "r10", w*8)
		} else {
			name := "__goclib_bi_widen_u"
			if et.Signed {
				name = "__goclib_bi_widen_s"
			}
			c.callBigLib(name, []bigArg{
				{addrOff: off}, {reg: "r10"}, {imm: int64(w)}, {imm: int64(bigWordsOf(et))},
			})
		}
		return k, w, off, nil
	}
	if et != nil && et.IsFloating() {
		return 0, 0, 0, fmt.Errorf("floating operands with _BitInt are not supported")
	}
	if _, err := c.genExprT(e); err != nil {
		return 0, 0, 0, err
	}
	if err := c.ensureType(frontend.TInt); err != nil {
		return 0, 0, 0, err
	}
	sg := int64(0)
	if want.Signed {
		sg = 1
	}
	// N17: a materialized signed int operand consumed as a 64-bit signed
	// value by bi_from_i64 must be sign-extended first. `(S8)a == -56`
	// would otherwise compare against the zero-extended +4294967240.
	if c.resW == 4 && c.resSigned && want.Signed {
		c.emit("movsxd rax, eax")
	}
	c.callBigLib("__goclib_bi_from_i64", []bigArg{
		{addrOff: off}, {reg: "rax"}, {imm: int64(w)}, {imm: sg},
	})
	return k, w, off, nil
}

// genBigValue emits a value-typed _BitInt expression: the value's address is
// left in r10 and the resBig flags describe it.
func (c *CG) genBigValue(e frontend.Expr, t *frontend.Type) (frontend.CType, error) {
	switch n := e.(type) {
	case *frontend.Binary:
		return c.genBigBinary(n)
	case *frontend.Unary:
		if n.Op == "-" || n.Op == "~" || n.Op == "!" {
			return c.genBigUnary(n, t)
		}
	case *frontend.NumLit:
		if n.BigWords != nil {
			return c.genBigLiteral(n, t)
		}
	case *frontend.CastExpr:
		return c.genBigCast(n, t)
	case *frontend.IncDecExpr:
		return c.genBigIncDec(n)
	case *frontend.Call:
		return c.genBigFromCall(func() (frontend.CType, error) { return c.genCall(n.Name, nil, nil, n.Args) }, t)
	case *frontend.IndirectCall:
		return c.genBigFromCall(func() (frontend.CType, error) { return c.genIndirectCall(n) }, t)
	}
	// Default: lvalue-shaped (frontend.Ident / frontend.Member / frontend.Index / *p / frontend.CompoundLit).
	if err := c.genLValue(e); err != nil {
		return frontend.TInt, err
	}
	c.resBig = true
	c.resBigT = t
	c.resBigK = 0
	c.resBigSl = 0
	c.resTyp = frontend.TInt
	c.resW = 8
	c.resSigned = t.Signed
	return frontend.TInt, nil
}

// genBigFromCall wraps a call whose result type is a _BitInt: the sret
// machinery has left the value in a result buffer; transfer it to the resBig
// carrier.
func (c *CG) genBigFromCall(gen func() (frontend.CType, error), t *frontend.Type) (frontend.CType, error) {
	ct, err := gen()
	if err != nil {
		return ct, err
	}
	if !c.resStruct {
		return frontend.TInt, fmt.Errorf("internal: call did not produce a _BitInt result buffer")
	}
	c.resBig = true
	c.resBigT = t
	c.resBigK = c.resStructK
	c.resBigSl = c.resStructSl
	c.resStruct = false
	c.emit("lea r10, [rbp%+d]", c.tmpSlotBlock(c.resBigK, c.resBigSl))
	c.resTyp = frontend.TInt
	c.resW = 8
	c.resSigned = t.Signed
	return frontend.TInt, nil
}

// bigLitKey identifies a literal by its word content and width.
func bigLitKey(words []uint64, bits int) string {
	return fmt.Sprintf("%d:%v", bits, words)
}

// genBigLiteral emits a wb/uwb literal: the words go into a .rdata constant
// and the value's address is the expression's value. Literals are read-only
// rvalues, so no per-use copy is needed; width conversion happens in
// bigOperand when the operation's result type differs.
func (c *CG) genBigLiteral(n *frontend.NumLit, t *frontend.Type) (frontend.CType, error) {
	w := bigWordsOf(t)
	words := make([]uint64, w)
	for i := 0; i < w && i < len(n.BigWords); i++ {
		words[i] = n.BigWords[i]
	}
	// A signed literal is non-negative by construction; zero-pad. (Values
	// needing the sign bit cannot appear: the declared width holds the value.)
	key := bigLitKey(words, t.Bits)
	lab, ok := c.bigLab[key]
	if !ok {
		lab = fmt.Sprintf("%sLBIG%d", c.staticPrefix, len(c.bigLits))
		c.bigLits = append(c.bigLits, words)
		c.bigLab[key] = lab
	}
	c.emit("lea r10, [rip+%s]", lab)
	c.resBig = true
	c.resBigT = t
	c.resBigK = 0
	c.resBigSl = 0
	c.resTyp = frontend.TInt
	c.resW = 8
	c.resSigned = t.Signed
	return frontend.TInt, nil
}

// genBigBinary emits an arithmetic / bitwise / shift / comparison operation
// with at least one _BitInt operand.
func (c *CG) genBigBinary(n *frontend.Binary) (frontend.CType, error) {
	lt0, rt0 := c.exprType(n.L), c.exprType(n.R)
	entryDepth := c.tmpDepth

	switch n.Op {
	case "==", "!=", "<", "<=", ">", ">=":
		// A bare numeric literal is not in any type table (exprType reports
		// nil for it), so treat a missing operand type as int instead of
		// dereferencing a nil *frontend.Type -- bigWordsOf(nil) panicked on "x > 200".
		lt := lt0
		if lt == nil {
			lt = frontend.IntType()
		}
		rt := rt0
		if rt == nil {
			rt = frontend.IntType()
		}
		w := bigWordsOf(lt)
		if rw := bigWordsOf(rt); rw > w {
			w = rw
		}
		useSigned := int64(0)
		if lt.Signed && rt.Signed {
			useSigned = 1
		}
		ct := &frontend.Type{Kind: frontend.KBitInt, Bits: w * 64, Size: w * 8, Signed: useSigned == 1}
		_, _, loff, err := c.bigOperand(n.L, ct)
		if err != nil {
			return frontend.TInt, err
		}
		_, _, roff, err := c.bigOperand(n.R, ct)
		if err != nil {
			return frontend.TInt, err
		}
		c.callBigLib("__goclib_bi_cmp", []bigArg{
			{addrOff: loff}, {addrOff: roff}, {imm: int64(w)}, {imm: useSigned},
		})
		c.tmpDepth = entryDepth
		switch n.Op {
		case "==":
			c.emit("cmp rax, 0")
			c.emit("sete al")
		case "!=":
			c.emit("cmp rax, 0")
			c.emit("setne al")
		case "<":
			c.emit("cmp rax, 0")
			c.emit("setl al")
		case "<=":
			c.emit("cmp rax, 0")
			c.emit("setle al")
		case ">":
			c.emit("cmp rax, 0")
			c.emit("setg al")
		case ">=":
			c.emit("cmp rax, 0")
			c.emit("setge al")
		}
		c.emit("movzx rax, al")
		// The comparison yields a plain int; clear the big-value flag so
		// truth-context callers do not re-test the (stale) r10 address.
		c.resBig = false
		c.resBigT = nil
		c.resTyp = frontend.TInt
		c.resSigned = true
		c.resW = 4
		return frontend.TInt, nil

	case "<<", ">>":
		if !frontend.IsBig(lt0) {
			return frontend.TInt, fmt.Errorf("left operand of %q must be a _BitInt, got %s", n.Op, lt0)
		}
		if frontend.IsBig(rt0) {
			return frontend.TInt, fmt.Errorf("_BitInt shift counts are not supported; use an integer count")
		}
		resT := lt0
		w := bigWordsOf(resT)
		rk, resOff := c.claimBig(w)
		if _, lsl, loff, err := c.bigOperand(n.L, resT); err != nil {
			return frontend.TInt, err
		} else {
			_ = lsl
			// count: evaluate after the left operand is materialised
			if _, err := c.genExprT(n.R); err != nil {
				return frontend.TInt, err
			}
			if err := c.ensureType(frontend.TInt); err != nil {
				return frontend.TInt, err
			}
			name := "__goclib_bi_shl"
			if n.Op == ">>" {
				name = "__goclib_bi_shr_u"
				if resT.Signed {
					name = "__goclib_bi_shr_s"
				}
			}
			c.callBigLib(name, []bigArg{
				{addrOff: resOff}, {addrOff: loff}, {reg: "rax"}, {imm: int64(w)},
			})
		}
		c.tmpDepth = rk + w - 1
		c.markBig(resT, rk, w)
		return frontend.TInt, nil
	}

	// + - * / % & | ^
	resT := frontend.BigArithResult(n.Op, lt0, rt0)
	if !frontend.IsBig(resT) {
		return frontend.TInt, fmt.Errorf("internal: no result type for %q on %s and %s", n.Op, lt0, rt0)
	}
	w := bigWordsOf(resT)
	rk, resOff := c.claimBig(w)
	if _, _, loff, err := c.bigOperand(n.L, resT); err != nil {
		return frontend.TInt, err
	} else if _, _, roff, err := c.bigOperand(n.R, resT); err != nil {
		return frontend.TInt, err
	} else {
		var name string
		switch n.Op {
		case "+":
			name = "__goclib_bi_add"
		case "-":
			name = "__goclib_bi_sub"
		case "*":
			name = "__goclib_bi_mul"
		case "&":
			name = "__goclib_bi_and"
		case "|":
			name = "__goclib_bi_or"
		case "^":
			name = "__goclib_bi_xor"
		case "/":
			name = "__goclib_bi_div_u"
			if resT.Signed {
				name = "__goclib_bi_div_s"
			}
		case "%":
			name = "__goclib_bi_mod_u"
			if resT.Signed {
				name = "__goclib_bi_mod_s"
			}
		}
		c.callBigLib(name, []bigArg{
			{addrOff: resOff}, {addrOff: loff}, {addrOff: roff}, {imm: int64(w)},
		})
		// C23 arithmetic on a narrow _BitInt wraps to the operand's own
		// width; the goclib helpers compute in 64-bit words, so truncate a
		// <64-bit result back into range (e.g. U8 200+100 -> 44).
		if resT.Bits < 64 {
			sg := int64(0)
			if resT.Signed {
				sg = 1
			}
			c.callBigLib("__goclib_bi_conv", []bigArg{
				{addrOff: resOff}, {addrOff: resOff}, {imm: int64(resT.Bits)}, {imm: sg}, {imm: 64}, {imm: sg},
			})
		}
	}
	c.tmpDepth = rk + w - 1
	c.markBig(resT, rk, w)
	return frontend.TInt, nil
}

// genBigUnary emits unary '-' (negate), '~' (complement) and '!' (zero test)
// on a _BitInt operand.
func (c *CG) genBigUnary(n *frontend.Unary, t *frontend.Type) (frontend.CType, error) {
	entryDepth := c.tmpDepth
	w := bigWordsOf(t)
	if n.Op == "!" {
		if _, err := c.genExprT(n.E); err != nil {
			return frontend.TInt, err
		}
		if !c.resBig {
			return frontend.TInt, fmt.Errorf("internal: '!' operand did not produce a _BitInt value")
		}
		c.callBigLib("__goclib_bi_is_zero", []bigArg{{reg: "r10"}, {imm: int64(w)}})
		c.emit("xor rax, 1")
		c.tmpDepth = entryDepth
		c.resBig = false
		c.resTyp = frontend.TInt
		c.resW = 4
		c.resSigned = true
		return frontend.TInt, nil
	}
	rk, resOff := c.claimBig(w)
	if _, _, loff, err := c.bigOperand(n.E, t); err != nil {
		return frontend.TInt, err
	} else {
		name := "__goclib_bi_neg"
		if n.Op == "~" {
			name = "__goclib_bi_not"
		}
		c.callBigLib(name, []bigArg{
			{addrOff: resOff}, {addrOff: loff}, {imm: int64(w)},
		})
	}
	c.tmpDepth = rk + w - 1
	c.markBig(t, rk, w)
	return frontend.TInt, nil
}

// genBigCast emits a cast whose target type is a _BitInt: the operand is
// converted (int -> bitint, or bitint -> bitint by truncation/extension).
func (c *CG) genBigCast(n *frontend.CastExpr, t *frontend.Type) (frontend.CType, error) {
	w := bigWordsOf(t)
	rk, resOff := c.claimBig(w)
	if _, _, loff, err := c.bigOperand(n.E, t); err != nil {
		return frontend.TInt, err
	} else {
		// same conversion rule as the operand path: copy / widen into the result
		et := c.exprType(n.E)
		if frontend.IsBig(et) && et.Bits == t.Bits {
			c.emit("lea r11, [rbp%+d]", resOff)
			c.emit("lea r10, [rbp%+d]", loff)
			c.copyBytes("r11", "r10", w*8)
		} else if frontend.IsBig(et) {
			ssg := int64(0)
			if et.Signed {
				ssg = 1
			}
			dsg := int64(0)
			if t.Signed {
				dsg = 1
			}
			c.callBigLib("__goclib_bi_conv", []bigArg{
				{addrOff: resOff}, {addrOff: loff}, {imm: int64(t.Bits)}, {imm: dsg}, {imm: int64(et.Bits)}, {imm: ssg},
			})
		} else {
			// scalar operand: bigOperand materialised it at 64-bit width in
			// loff; convert with wrap-around to the target width.
			sg := int64(0)
			if t.Signed {
				sg = 1
			}
			c.callBigLib("__goclib_bi_conv", []bigArg{
				{addrOff: resOff}, {addrOff: loff}, {imm: int64(t.Bits)}, {imm: sg}, {imm: 64}, {imm: sg},
			})
		}
	}
	c.tmpDepth = rk + w - 1
	c.markBig(t, rk, w)
	return frontend.TInt, nil
}

// genBigIncDec emits ++/-- on a _BitInt lvalue. Postfix yields the OLD value
// from a claimed buffer; prefix yields the operand itself as an lvalue.
func (c *CG) genBigIncDec(n *frontend.IncDecExpr) (frontend.CType, error) {
	t := c.exprType(n.E)
	w := bigWordsOf(t)
	entryDepth := c.tmpDepth
	// the "+1"/"-1" addend, materialised at the operation's width
	k1, oneOff := c.claimBig(w)
	_ = k1
	{
		sg := int64(0)
		if t.Signed {
			sg = 1
		}
		c.callBigLib("__goclib_bi_from_i64", []bigArg{
			{addrOff: oneOff}, {imm: 1}, {imm: int64(w)}, {imm: sg},
		})
	}
	resK, resSl := 0, 0
	if !n.Prefix {
		var resOff int
		resK, resOff = c.claimBig(w)
		_ = resOff
		resSl = w
	}
	if err := c.genLValue(n.E); err != nil {
		return frontend.TInt, err
	}
	if !n.Prefix {
		// old value -> result buffer (r10 = operand address)
		c.emit("lea r11, [rbp%+d]", c.tmpSlotBlock(resK, resSl))
		c.copyBytes("r11", "r10", w*8)
	}
	name := "__goclib_bi_add"
	if n.Op == "--" {
		name = "__goclib_bi_sub"
	}
	c.callBigLib(name, []bigArg{
		{reg: "r10"}, {reg: "r10"}, {addrOff: oneOff}, {imm: int64(w)},
	})
	if !n.Prefix {
		c.tmpDepth = resK + resSl - 1
		c.markBig(t, resK, resSl)
	} else {
		c.tmpDepth = entryDepth
		c.resBig = true
		c.resBigT = t
		c.resBigK = 0
		c.resBigSl = 0
		c.resTyp = frontend.TInt
		c.resW = 8
		c.resSigned = t.Signed
	}
	return frontend.TInt, nil
}

// genTruth evaluates a condition: a _BitInt operand tests non-zero over its
// FULL width (a low-word truncation would read 2^64 multiples as false); a
// long double operand is true iff it is not a zero (a NaN is not a zero).
func (c *CG) genTruth(e frontend.Expr) error {
	if _, err := c.genExprT(e); err != nil {
		return err
	}
	if c.resBig && isLD(c.resBigT) {
		// Compare against +0.0 through the runtime; the answer is nonzero
		// exactly when the value is a nonzero (or NaN) long double.
		_, _, zoff := c.tfTemp()
		c.emit("lea r11, [rbp%+d]", zoff)
		c.zeroBytes("r11", 16)
		c.callBigLib("goc_tf_cmp", []bigArg{{reg: "r10"}, {addrOff: zoff}})
		c.emit("movsxd rax, eax")
		c.emit("cmp rax, 0")
		c.emit("setne al")
		c.emit("movzx rax, al")
		if c.resBigSl > 0 {
			c.tmpDepth -= c.resBigSl + tfWords
		}
		c.resBig = false
		c.resBigT = nil
		c.resTyp = frontend.TInt
		c.resW = 4
		c.resSigned = true
		return nil
	}
	if c.resBig {
		w := bigWordsOf(c.resBigT)
		sl := c.resBigSl
		c.callBigLib("__goclib_bi_is_zero", []bigArg{{reg: "r10"}, {imm: int64(w)}})
		c.emit("xor rax, 1")
		c.tmpDepth -= sl
		c.resBig = false
		c.resTyp = frontend.TInt
		c.resW = 4
		c.resSigned = true
		return nil
	}
	return c.ensureType(frontend.TInt)
}

// genExpr is the entry point for emitting an expression. The integer/double
// type of the result is returned so callers can route it to the right
// register.
// genExprT evaluates e into rax (int) or xmm0 (double) and records the
// result's type class in c.resTyp plus width/signedness in c.resW/c.resSigned.
// c.resPtr (T1.6 C4) tells the narrow sites whether rax may hold a 64-bit
// pointer loaded from a ptrCapable variable: only a bare variable reference
// leaves that flag set (loadVar sets it; the frontend.Ident case clears it for every
// non-local path), every other expression yields an ordinary value and the
// wrapper resets the flag. Internal recursive calls route through this
// wrapper too, so a flag captured after an operand evaluation is the operand's
// own, not a leftover.
func (c *CG) genExprT(e frontend.Expr) (frontend.CType, error) {
	t, err := c.genExprT1(e)
	if _, ok := e.(*frontend.Ident); !ok {
		c.resPtr = false
	}
	return t, err
}

func (c *CG) genExprT1(e frontend.Expr) (frontend.CType, error) {
	// C23 _BitInt values ride a by-address model (like structs): a value-typed
	// expression leaves the address of the value in r10 and sets resBig;
	// consumers (binary ops, conditions, assignments, calls) read the flags.
	if et := c.exprType(e); frontend.IsBig(et) {
		c.resBig = false
		return c.genBigValue(e, et)
	}
	// A comparison whose operands are _BitInt yields an int, so the frontend.IsBig
	// dispatch above never fires and the scalar path would compare the
	// operands' ADDRESSES. Route it to the big path explicitly.
	if n, ok := e.(*frontend.Binary); ok {
		switch n.Op {
		case "==", "!=", "<", "<=", ">", ">=":
			if frontend.IsBig(c.exprType(n.L)) || frontend.IsBig(c.exprType(n.R)) {
				c.resBig = false
				return c.genBigBinary(n)
			}
			if c.isLDExpr(n.L) || c.isLDExpr(n.R) {
				c.resBig = false
				return c.genTFBinary(n)
			}
		}
	}
	// Long double: a value-typed expression leaves the value's address in
	// r10 and the shared resBig carrier describes it (resBigT is the long
	// double type); every operation is a call into goclib's binary128
	// runtime (tf128.go). Assignment expressions are excluded -- the
	// AssignExpr case below handles them with the aggregate machinery.
	if _, isAssign := e.(*frontend.AssignExpr); !isAssign {
		// A cast FROM long double to a narrower type runs the tf path even
		// though the result is a scalar: which runtime entry point runs
		// depends on the target's signedness, and the generic cast path's
		// ensureType only knows the class.
		if ce, ok := e.(*frontend.CastExpr); ok && !isLD(ce.Typ) && c.isLDExpr(ce.E) {
			return c.genTFCast(ce)
		}
		if c.isLDExpr(e) {
			c.resBig = false
			t := c.exprType(e)
			if !isLD(t) {
				t = frontend.LongDoubleType()
			}
			return c.genTFValue(e, t)
		}
	}
	c.resBig = false
	switch n := e.(type) {
	case *frontend.GenericExpr:
		// C11/C23 generic selection: the controlling expression is never
		// evaluated, so it is not emitted at all. The checker already picked
		// (and type-checked) the matching association; only Chosen reaches
		// codegen.
		if n.Chosen == nil {
			return frontend.TInt, fmt.Errorf("line %d: _Generic selection was not resolved by the type checker", n.Line)
		}
		return c.genExprT(n.Chosen)
	case *frontend.NumLit:
		if n.Kind == frontend.TDouble {
			lab, ok := c.doubleLab[n.Fval]
			if !ok {
				lab = fmt.Sprintf("%sLD%dx", c.staticPrefix, len(c.doubles))
				c.doubles = append(c.doubles, n.Fval)
				c.doubleLab[n.Fval] = lab
			}
			c.emit("movsd xmm0, [rip+%s]", lab)
			c.resTyp = frontend.TDouble
			return frontend.TDouble, nil
		}
		c.emit("mov rax, %d", n.Val)
		c.resTyp = frontend.TInt
		// Width and signedness come from the literal itself, not just from
		// its value: `1` is an int (32 bits) while `1LL` is 64 bits wide,
		// and the difference is observable -- `1 << 52` is zero because the
		// shift is done in 32 bits, while `1LL << 52` is 2^52. A value that
		// does not fit in an int is long whether it has a suffix or not.
		c.resW = 4
		if n.Long || n.Val > 0x7fffffff || n.Val < -0x80000000 {
			c.resW = 8
		}
		c.resSigned = !n.Unsig
		return frontend.TInt, nil
	case *frontend.StrLit:
		lab, ok := c.strLab[n]
		if !ok {
			lab = fmt.Sprintf("%sLC%d", c.staticPrefix, len(c.strs))
			c.strs = append(c.strs, *n)
			c.strLab[n] = lab
		}
		c.emit("lea rax, [rip+%s]", lab)
		c.resTyp = frontend.TInt
		c.resSigned = false
		c.resW = 8 // a string literal is a pointer
		return frontend.TInt, nil
	case *frontend.Ident:
		// Resolution order mirrors the checker and C block scoping: a
		// function-local name (parameter or local variable) shadows any
		// file-scope name of the same spelling, including a thread-local
		// global. Resolving local scope FIRST prevents a library function's
		// local (e.g. goclib's `char t[20]` in its integer formatter) from
		// being mistaken for a user-declared thread-local global of the same
		// name -- which would otherwise emit a TLS load whose runtime value
		// corrupts the surrounding code (printf + initialised TLS).
		if vi, ok := c.lookupVar(n.Name); ok {
			if vi.typ.IsArray() {
				// An array used as a value decays to a pointer to element 0.
				c.emit("lea rax, [rbp%+d]", vi.off)
				c.resTyp = frontend.TInt
				c.resSigned = false
				c.resW = 8
				return frontend.TInt, nil
			}
			if isAgg(vi.typ) {
				// A whole struct/union value cannot be loaded into rax; consumers
				// must go through genLValue (see structSrcAddr).
				return frontend.TInt, fmt.Errorf("cannot load struct/union value %q directly", n.Name)
			}
			c.loadVar(vi)
			return c.resTyp, nil
		}
		// File-scope names. A static local shadows a same-named TLS global
		// within its function, but a static *thread-local* local still needs
		// the segment reach (it is also registered in tlsVars).
		// T1.6 (C4): a non-local load never yields a ptrCapable value, so the
		// flag is cleared here -- genExprT's wrapper only preserves resPtr for
		// bare frontend.Ident evaluations (i.e. loadVar), and this branch must not leak
		// an earlier load's flag into the caller.
		c.resPtr = false
		if lab, ok2 := c.lookupStatic(n.Name); ok2 {
			if _, isTLS := c.tlsVars[n.Name]; !isTLS {
				return c.loadGlobal(lab, c.globalTyp[lab])
			}
		}
		if _, ok := c.tlsVars[n.Name]; ok {
			// Thread-local variable: load via the fs/gs segment reach.
			if err := c.genTLSLoadValue(n.Name); err != nil {
				return frontend.TInt, err
			}
			return c.resTyp, nil
		}
		if lab, ok2 := c.lookupStatic(n.Name); ok2 {
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
		if sym, ok2 := c.funcAddrSym(n.Name); ok2 {
			c.emit("lea rax, [rip+%s]", sym)
			c.resTyp = frontend.TInt
			c.resSigned = false
			c.resW = 8 // a function address is a pointer
			return frontend.TInt, nil
		}
		if ev, ok2 := frontend.EnumConsts[n.Name]; ok2 {
			// An enumerator is a compile-time integer constant.
			c.emit("mov rax, %d", ev)
			c.resTyp = frontend.TInt
			c.resSigned = true
			c.resW = 4
			if ev > 0x7fffffff || ev < -0x80000000 {
				c.resW = 8
			}
			return frontend.TInt, nil
		}
		return frontend.TInt, fmt.Errorf("undefined variable %q", n.Name)
	case *frontend.CompoundLit:
		// Compound literal as a value. Aggregate objects never enter rax:
		// consumers take their address through genLValue/structSrcAddr (the
		// initialisation runs there). Arrays decay; scalars load.
		t := n.Typ
		if isAgg(t) {
			return frontend.TInt, fmt.Errorf("line %d: cannot load struct/union compound literal directly", n.Line)
		}
		if err := c.genLValue(n); err != nil {
			return frontend.TInt, err
		}
		if t.IsArray() {
			// Decay: an array literal's value is its address.
			c.emit("mov rax, r10")
			c.resTyp = frontend.TInt
			c.resSigned = false
			c.resW = 8
			return frontend.TInt, nil
		}
		c.loadVar(varInfo{off: c.clOff[n], typ: t})
		return c.resTyp, nil
	case *frontend.Unary:
		return c.genUnary(n)
	case *frontend.Binary:
		return c.genBinary(n)
	case *frontend.Index:
		if err := c.genLValue(n); err != nil {
			return frontend.TInt, err
		}
		// An element that is itself an array ("rows[0]" of char rows[2][6])
		// used as a value decays to a pointer to ITS element 0: the address
		// genLValue left in r10 is the value -- loading bytes here would
		// yield a garbage pointer (and crash %s marshalling).
		if et := c.exprType(n); et != nil && et.IsArray() {
			c.emit("mov rax, r10")
			c.resTyp = frontend.TInt
			c.resSigned = false
			c.resW = 8
			return frontend.TInt, nil
		}
		width := c.elemWidthOf(n.Base)
		ec := c.elemClassOf(n.Base)
		signed := c.elemSignedOf(n.Base)
		c.genLoadElem("r10", width, ec, signed)
		return c.resTyp, nil
	case *frontend.MemberExpr:
		if err := c.genLValue(n); err != nil {
			return frontend.TInt, err
		}
		t := c.memberType(n.Base, n.Name)
		if t == nil {
			return frontend.TInt, fmt.Errorf("line %d: unknown member %q in type %v", n.Line, n.Name, c.exprType(n.Base))
		}
		if t.Kind == frontend.KStruct || t.Kind == frontend.KUnion {
			// A nested aggregate member (e.g. "s.inner") is not a scalar that
			// fits in rax; its address is already in r10 and callers must take
			// it from there.
			return frontend.TInt, fmt.Errorf("cannot load struct/union value %q directly", n.Name)
		}
		if t.Kind == frontend.KArr {
			// An array member used as a value decays to a pointer to element
			// 0: the address genLValue left in r10 IS the value -- loading
			// bytes here would yield garbage (and crash %s arg marshalling).
			c.emit("mov rax, r10")
			c.resTyp = frontend.TInt
			c.resSigned = false
			c.resW = 8
			return frontend.TInt, nil
		}
		if c.lvBitWidth > 0 {
			// Bit-field member: genLValue armed the field geometry (bit
			// offset/width inside the storage unit at r10); extract with
			// sign or zero extension per the member's signedness.
			c.genLoadBitfield(c.lvBitUnit, c.lvBitOff, c.lvBitWidth, c.lvBitSigned)
			c.releaseCallResultBuffer()
			return c.resTyp, nil
		}
		// Members are laid out at their C type width (MSVC x64 packs an int
		// member as 4 bytes), not the 8-byte scalar slot width -- loading the
		// slot width would read 4 bytes past a trailing int member. Double
		// members load into xmm0, not rax.
		width := c.typeWidth(t)
		if t.IsFloating() {
			c.genLoadElem("r10", width, frontend.TDouble, false)
			c.releaseCallResultBuffer()
			return c.resTyp, nil
		}
		signed := t.Kind == frontend.KInt && t.Signed
		c.genLoadElem("r10", width, frontend.TInt, signed)
		c.releaseCallResultBuffer()
		return c.resTyp, nil
	case *frontend.SizeofExpr:
		// sizeof on a variable-length array is NOT a constant: the size exists
		// only once the declaration has run. Read it back from the hidden slot
		// the allocation parked it in -- and, for a VLA *type* operand
		// (sizeof(int[n])), evaluate the length now, which is the one case
		// where C does evaluate sizeof's operand.
		if off, ok := c.vlaSizeSlot(n.E); ok {
			c.emit("mov rax, [rbp%+d]", off)
			c.resTyp = frontend.TInt
			c.resSigned = true
			c.resW = 8
			return frontend.TInt, nil
		}
		if n.Typ != nil && n.Typ.IsVLA() {
			if _, err := c.genExprT(n.Typ.VLALen); err != nil {
				return frontend.TInt, err
			}
			if err := c.ensureType(frontend.TInt); err != nil {
				return frontend.TInt, err
			}
			if c.resW == 4 && c.resSigned {
				c.emit("movsxd rax, eax")
			}
			if w := c.typeWidth(n.Typ.Elem); w != 1 {
				c.emit("mov r10, %d", w)
				c.emit("imul rax, r10")
			}
			c.resTyp = frontend.TInt
			c.resSigned = true
			c.resW = 8
			return frontend.TInt, nil
		}
		var sz int
		if n.Typ != nil {
			sz = frontend.Sizeof(n.Typ)
		} else if sl, ok := n.E.(*frontend.StrLit); ok {
			// A string literal is a char[N] array, so "sizeof("abc")" is 4
			// (three characters plus the terminator) -- not 8. It only decays
			// to a pointer when it is used as a value, and sizeof does not use
			// it as a value. Real code leans on this: cJSON's strdup is
			// "strlen(s) + sizeof("")", which read 8 instead of 1 here and
			// copied (and later printed) a byte of neighbouring memory. A wide
			// L"abc" literal is a wchar_t[N] array (2 bytes per element).
			if sl.Wide {
				sz = (len(sl.Bytes)/2 + 1) * 2
			} else {
				sz = len(sl.Bytes) + 1
			}
		} else {
			sz = c.typeWidth(c.exprType(n.E))
		}
		c.emit("mov rax, %d", sz)
		c.resTyp = frontend.TInt
		c.resSigned = true
		c.resW = 8 // sizeof is a size_t (8 bytes) on this target
		return frontend.TInt, nil
	case *frontend.CondExpr:
		if err := c.genTruth(n.Cond); err != nil {
			return frontend.TInt, err
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
		if tt == frontend.TDouble || et == frontend.TDouble {
			c.resTyp = frontend.TDouble
			c.resSigned = false
			c.resW = 8
		} else {
			// The conditional expression's type is the C common type of the
			// two arms (integer promotions applied).
			c.resTyp = frontend.TInt
			c.resW, c.resSigned = promotedArith(wThen, sThen, wElse, sElse)
		}
		return c.resTyp, nil
	case *frontend.CommaExpr:
		// Evaluate the left operand (discarding its value) and yield the right
		// operand's value, exactly like C's comma operator. Both sides run
		// through genExprT; the right side overwrites the result state.
		if _, err := c.genExprT(n.Left); err != nil {
			return c.resTyp, err
		}
		return c.genExprT(n.Right)
	case *frontend.CastExpr:
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
		// Casting an integer value to a narrow int type (char/short) must keep
		// only the target's low bits and re-extend with the target's
		// signedness. canonInt (below) only handles the 4-byte case -- it keeps
		// bits 0..31 and clears 32..63, so a width-1/2 cast leaves the upper
		// bits of the source in place. That makes `(unsigned char)0xE2` (loaded
		// as the sign-extended 0xFFFFFFE2) wrongly stay 0xFFFFFFE2 instead of
		// 0xE2, which breaks UTF-8 byte emission and unsigned char arithmetic;
		// and `(char)0x80` would wrongly stay 0x80 instead of sign-extending to
		// 0xFFFFFF80. A width-1/2 signed cast therefore sign-extends from the
		// low byte/half-word, not the low dword.
		if n.Typ != nil && n.Typ.Kind == frontend.KInt && (n.Typ.Width == 1 || n.Typ.Width == 2) &&
			t == frontend.TInt {
			c.extendRegNarrow("rax", n.Typ.Width, n.Typ.Signed)
		} else if t == frontend.TInt && n.Typ != nil && n.Typ.Kind == frontend.KInt && n.Typ.Width == 4 {
			c.canonInt(n.Typ.Signed)
		}
		// T1.6 (C3/N17): a widening cast of a signed materialized int to an
		// 8-byte int target must sign-extend. Pre-C3 the value was canonical
		// (call sites sign-extended int params), which masked this site; now a
		// materialized -7 is 0x00000000FFFFFFF9 and "((long)n)*100" would
		// multiply as a huge positive without the movsxd. Unsigned targets use
		// the same sign-extension: int -> unsigned long converts modulo 2^64,
		// which is exactly the movsxd pattern. resW==8 sources and unsigned
		// int sources (already zero-extended, the correct widening) need none.
		if t == frontend.TInt && n.Typ != nil && n.Typ.Kind == frontend.KInt && n.Typ.Width == 8 &&
			c.resW == 4 && c.resSigned && !c.resPtr {
			c.emit("movsxd rax, eax")
		}
		if err := c.ensureType(n.Typ.Class()); err != nil {
			return t, err
		}
		if n.Typ.Kind == frontend.KFloat {
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
		c.resSigned = n.Typ.Kind == frontend.KInt && n.Typ.Signed
		c.resW = c.semWOf(n.Typ)
		return c.resTyp, nil
	case *frontend.IncDecExpr:
		return c.genIncDec(n)
	case *frontend.Call:
		return c.genCallExpr(n)
	case *frontend.IndirectCall:
		return c.genIndirectCall(n)
	case *frontend.AssignExpr:
		// Compound assignment "E1 op= E2" (C11 6.5.16.2): the parser no
		// longer desugars this into "E1 = E1 op E2" (that re-evaluated E1,
		// so a[i++] += 10 incremented i twice); codegen now evaluates the
		// lvalue exactly once, combines, and stores back.
		if n.Op != "" {
			if frontend.IsBig(c.exprType(n.Lhs)) || frontend.IsBig(c.exprType(n.Rhs)) {
				return c.genBigCompoundAssign(n)
			}
			if isLD(c.exprType(n.Lhs)) || c.isLDExpr(n.Rhs) {
				return c.genTFCompoundAssign(n)
			}
			return c.genCompoundAssign(n)
		}
		// Whole-struct/union assignment: neither side fits in a register, so
		// there is no scalar fast path and we never load the value into rax.
		// The RHS may be an lvalue (copied byte-for-byte) or a call returning
		// a struct (whose result buffer is consumed). This is how "s = t;"
		// and "s = make(1, 2);" are compiled; the assignment expression's own
		// value is left undefined (rarely used).
		if lt := c.exprType(n.Lhs); isAgg(lt) {
			// Scalar RHS assigned to a _BitInt lvalue: convert via from_i64.
			if frontend.IsBig(lt) && !frontend.IsBig(c.exprType(n.Rhs)) {
				if _, err := c.genExprT(n.Rhs); err != nil {
					return lt.Class(), err
				}
				if err := c.ensureType(frontend.TInt); err != nil {
					return lt.Class(), err
				}
				// Capture the RHS's own width/sign now: genLValue below may
				// clobber c.resW while computing the destination address.
				rw, rs := c.resW, c.resSigned
				// Park the RHS value in a frame temporary: genLValue on the
				// left side is free to clobber r11 (element addressing uses
				// it as its scratch), and the parked value is the argument.
				c.emit("mov r11, rax")
				c.tmpDepth++
				valSlot := c.tmpSlot(c.tmpDepth)
				c.emit("mov [rbp%+d], r11", valSlot)
				if err := c.genLValue(n.Lhs); err != nil {
					c.tmpDepth--
					return lt.Class(), err
				}
				c.emit("mov r11, [rbp%+d]", valSlot)
				c.tmpDepth--
				// N17: a materialized signed int RHS is consumed as a 64-bit
				// signed value by bi_from_i64_trunc; sign-extend before the
				// call (a signed _BitInt lvalue assigned -12345 would
				// otherwise hold +4294954951).
				if rw == 4 && rs && lt.Signed {
					c.emit("movsxd r11, r11d")
				}
				sg := int64(0)
				if lt.Signed {
					sg = 1
				}
				c.callBigLib("__goclib_bi_from_i64_trunc", []bigArg{
					{reg: "r10"}, {reg: "r11"}, {imm: int64(lt.Bits)}, {imm: sg},
				})
				c.resTyp = frontend.TInt
				c.resSigned = lt.Signed
				c.resW = 8
				return lt.Class(), nil
			}
			// Scalar RHS assigned to a long double lvalue: materialise the
			// value converted to binary128, then copy the 16 bytes in.
			if isLD(lt) && !frontend.IsBig(c.exprType(n.Rhs)) && !c.isLDExpr(n.Rhs) {
				_, sl, _, err := c.tfOperand(n.Rhs)
				if err != nil {
					return lt.Class(), err
				}
				// Park the temporary's address: genLValue on the left side
				// is free to clobber r10/r11.
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
				c.copyBytes("r10", "r11", 16)
				c.tmpDepth -= sl // release the operand temporary
				c.resTyp = frontend.TInt
				c.resSigned = true
				c.resW = 8
				return lt.Class(), nil
			}
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
			// _BitInt assignment with mismatched widths converts by widening
			// (or truncating) instead of a raw byte copy.
			if rt2 := c.exprType(n.Rhs); frontend.IsBig(lt) && frontend.IsBig(rt2) && rt2.Bits != lt.Bits {
				ssg := int64(0)
				if rt2.Signed {
					ssg = 1
				}
				dsg := int64(0)
				if lt.Signed {
					dsg = 1
				}
				c.callBigLib("__goclib_bi_conv", []bigArg{
					{reg: "r10"}, {reg: "r11"}, {imm: int64(lt.Bits)}, {imm: dsg}, {imm: int64(rt2.Bits)}, {imm: ssg},
				})
			} else {
				c.copyBytes("r10", "r11", lt.Size)
			}
			c.releaseResBig()
			c.resTyp = lt.Class()
			c.resSigned = false
			c.resW = 8
			return lt.Class(), nil
		}
		// Fast path: a simple scalar local/param on the left can be stored
		// directly from its home register/stack slot, with no address needed.
		if id, ok := n.Lhs.(*frontend.Ident); ok {
			if vi, ok2 := c.lookupVar(id.Name); ok2 {
				rt, err := c.genExprT(n.Rhs)
				if err != nil {
					return rt, err
				}
				if err := c.ensureType(vi.typ.Class()); err != nil {
					return rt, err
				}
				c.storeVar(vi)
				c.resTyp = vi.typ.Class()
				c.resSigned = vi.typ.Kind == frontend.KInt && vi.typ.Signed
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
		// Capture the value's own width/signedness NOW: the genLValue below may
		// clobber c.resW/c.resSigned while computing the destination address.
		rw, rs := c.resW, c.resSigned
		c.tmpDepth++
		rslot := c.tmpSlot(c.tmpDepth)
		if rt == frontend.TDouble {
			c.emit("movsd [rbp%+d], xmm0", rslot)
		} else {
			c.emit("mov [rbp%+d], rax", rslot)
		}
		if err := c.genLValue(n.Lhs); err != nil {
			return rt, err
		}
		width := c.lvalueWidth(n.Lhs)
		class := c.lvalueClass(n.Lhs)
		if rt == frontend.TDouble {
			c.emit("movsd xmm0, [rbp%+d]", rslot)
		} else {
			c.emit("mov rax, [rbp%+d]", rslot)
		}
		if c.lvBitWidth > 0 {
			// Bit-field store: RMW inside the storage unit; the truncated
			// field value stays in rax as the assignment's result. Bit-fields
			// are never floating point, so rt is necessarily frontend.TInt here.
			c.genStoreBitfield(c.lvBitUnit, c.lvBitOff, c.lvBitWidth, c.lvBitSigned)
		} else {
			if lt := c.exprType(n.Lhs); lt != nil && lt.Kind == frontend.KBool {
				c.normalizeBool()
			}
			// T1.6 (N17): the movsxd for a materialized signed int stored into
			// an 8-byte slot lives inside genStoreElem (single source of truth;
			// this call site must NOT re-emit it). rw/rs were captured right
			// after the value was evaluated, before genLValue clobbered them.
			c.genStoreElem("r10", width, class, rw, rs)
		}
		c.tmpDepth--
		return rt, nil
	case *frontend.TmpLoad:
		// frontend.TmpLoad is an internal node produced only by genCompoundAssign to
		// re-read a left operand whose address/value was already evaluated
		// (the lvalue is evaluated exactly once, its old value parked in a
		// frame temporary, and the arithmetic re-loads it here as a plain
		// memory read with no side effects). It is never produced by the
		// parser, so it cannot appear in user code. Slot is the already-
		// computed frame OFFSET (the value tmpSlot returned when the operand
		// was parked), not a slot index: re-deriving it through tmpSlot would
		// read a wildly wrong address.
		if n.Typ != nil && n.Typ.IsFloating() {
			c.emit("movsd xmm0, [rbp%+d]", n.Slot)
			c.resTyp = frontend.TDouble
			c.resSigned = false
			c.resW = 8
			return frontend.TDouble, nil
		}
		c.emit("mov rax, [rbp%+d]", n.Slot)
		c.resTyp = frontend.TInt
		c.resSigned = n.Typ != nil && n.Typ.Kind == frontend.KInt && n.Typ.Signed
		c.resW = 8
		if n.Typ != nil {
			c.resW = c.semWOf(n.Typ)
		}
		return frontend.TInt, nil
	case *frontend.VaArgExpr:
		return c.genVaArg(n)
	}
	return frontend.TInt, fmt.Errorf("unknown expression")
}

// genCompoundAssign compiles "E1 op= E2" for scalar lvalues (C11 6.5.16.2).
// The lvalue is evaluated exactly once: its address (or register/stack home)
// is computed and the old value parked in a frame temporary, then E2 is
// evaluated and combined via the shared genBinary arithmetic -- which re-reads
// the parked old value through a frontend.TmpLoad, a pure memory read with no side
// effects -- and the result is stored back to the same address. The parser
// used to desugar "E1 op= E2" into "E1 = E1 op E2", which duplicated the
// lvalue: "a[i++] += 10" incremented i twice.
//
// genAtomicCompound emits an atomic read-modify-write for "lhs op= rhs" where
// lhs is an _Atomic integer of width aw (1, 2, 4 or 8) and addrSlot holds its
// address. The result -- the NEW value, as C defines it -- is left in rax.
//
// The caller parked the address and owns tmpDepth; the CAS loop below takes
// three slots of its own and gives them back.
func (c *CG) genAtomicCompound(n *frontend.AssignExpr, lt *frontend.Type, aw int, signed bool, addrSlot int) (frontend.CType, error) {
	wn := map[int]string{1: "byte", 2: "word", 4: "dword", 8: "qword"}[aw]
	acc := map[int]string{1: "al", 2: "ax", 4: "eax", 8: "rax"}[aw]
	// Evaluate the right operand once: it is the delta for += / -= and the
	// operand for every other operator.
	if _, err := c.genExprT(n.Rhs); err != nil {
		return frontend.TInt, err
	}
	if err := c.ensureType(frontend.TInt); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	valSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov [rbp%+d], rax", valSlot)

	// AssignExpr.Op holds the bare binary operator (the parser's assignOp
	// maps "x += y" onto Op "+"), so these two are the add/sub forms.
	if n.Op == "+" || n.Op == "-" {
		// LOCK XADD is a fetch-and-add: memory becomes old+delta and the
		// register comes away with old, so the new value is old+delta.
		if n.Op == "-" {
			c.emit("neg %s", acc)
		}
		c.emit("mov [rbp%+d], rax", valSlot)
		c.emit("mov r10, [rbp%+d]", addrSlot)
		c.emit("lock xadd %s [r10], %s", wn, acc)
		c.widenFrom(aw, signed)
		if aw == 8 {
			c.emit("add rax, [rbp%+d]", valSlot)
		} else {
			c.emit("add eax, [rbp%+d]", valSlot)
		}
		c.truncTo(aw, signed)
		c.tmpDepth--
		c.resTyp = frontend.TInt
		c.resSigned = signed
		c.resW = c.semWOf(lt)
		if c.resW == 4 {
			c.canonInt(signed)
		}
		return frontend.TInt, nil
	}

	// Every other operator needs a compare-and-swap retry loop: CMPXCHG
	// stores the desired value only while memory still holds the value it was
	// computed from, so a racing update is detected (ZF cleared) and retried
	// instead of being silently lost.
	c.tmpDepth++
	oldSlot := c.tmpSlot(c.tmpDepth)
	newSlot := c.tmpSlot(c.tmpDepth + 1)
	top := c.newLabel("atomic_cas")
	c.line(top + ":\n")
	c.emit("mov r10, [rbp%+d]", addrSlot)
	c.emit("mov %s, %s [r10]", acc, wn)
	c.widenFrom(aw, signed)
	c.emit("mov [rbp%+d], rax", oldSlot)
	bin := &frontend.Binary{Op: n.Op,
		L: &frontend.TmpLoad{Slot: oldSlot, Typ: lt},
		R: &frontend.TmpLoad{Slot: valSlot, Typ: lt}}
	if _, err := c.genBinary(bin); err != nil {
		c.tmpDepth -= 2
		return frontend.TInt, err
	}
	if err := c.ensureType(frontend.TInt); err != nil {
		c.tmpDepth -= 2
		return frontend.TInt, err
	}
	c.truncTo(aw, signed)
	c.emit("mov [rbp%+d], rax", newSlot)
	des := map[int]string{1: "r11b", 2: "r11w", 4: "r11d", 8: "r11"}[aw]
	c.emit("mov r10, [rbp%+d]", addrSlot)
	c.emit("mov %s, [rbp%+d]", acc, oldSlot)
	c.emit("mov %s, [rbp%+d]", des, newSlot)
	c.emit("lock cmpxchg %s [r10], %s", wn, des)
	c.emit("jne %s", top)
	c.emit("mov rax, [rbp%+d]", newSlot)
	c.tmpDepth -= 2
	c.resTyp = frontend.TInt
	c.resSigned = signed
	c.resW = c.semWOf(lt)
	if c.resW == 4 {
		c.canonInt(signed)
	}
	return frontend.TInt, nil
}

// widenFrom extends the value an aw-byte XADD left in its narrow accumulator
// back out to a full rax, the way genLoadElem would have loaded it.
func (c *CG) widenFrom(aw int, signed bool) {
	switch aw {
	case 1:
		c.emit("mov%sx rax, al", map[bool]string{true: "s", false: "z"}[signed])
	case 2:
		c.emit("mov%sx rax, ax", map[bool]string{true: "s", false: "z"}[signed])
	}
}

// truncTo narrows rax to the low aw bytes, so a value computed in 64 bits
// matches what an object of that width actually holds.
func (c *CG) truncTo(aw int, signed bool) {
	switch aw {
	case 1:
		c.emit("mov%sx rax, al", map[bool]string{true: "s", false: "z"}[signed])
	case 2:
		c.emit("mov%sx rax, ax", map[bool]string{true: "s", false: "z"}[signed])
	case 4:
		if signed {
			c.emit("movsxd rax, eax")
		} else {
			// A 32-bit operation already zeroes the upper half of rax.
			c.emit("mov eax, eax")
		}
	}
}

// genAtomicBuiltin lowers one of the <stdatomic.h> builtins (see
// frontend.LookupAtomicBuiltin). The fetch family and atomic_exchange return
// the value the object held BEFORE the update -- the one thing a C expression
// cannot produce, because the read and the write have to be indivisible.
// genMarkerBuiltin lowers the marker builtins (see src/frontend/marker.go).
// None of them does any runtime work, but __builtin_expect and its relatives
// still have to *evaluate* their first argument: it is the value they report,
// and skipping it would drop a call with side effects.
func (c *CG) genMarkerBuiltin(n *frontend.Call, ms frontend.MarkerSig) (frontend.CType, error) {
	if n.Name == "__builtin_trap" {
		// The one marker with a runtime effect: the path is meant to be fatal.
		c.emit("ud2")
		return frontend.TInt, nil
	}
	if len(n.Args) == 0 {
		// __builtin_unreachable / __builtin_assume: nothing to emit. The dead
		// path simply falls through, and whatever the caller assigned before
		// the call stays in its register or slot.
		c.resTyp = frontend.TInt
		c.resW = 4
		return frontend.TInt, nil
	}
	// The branch-hint forms: evaluate and report the first argument, then
	// discard the rest (the expected probability and the hint are constants).
	ct, err := c.genExprT(n.Args[0])
	if err != nil {
		return frontend.TInt, err
	}
	return ct, nil
}

// genOverflowBuiltin lowers GCC's overflow-checked arithmetic (see
// src/frontend/overflow.go): the sum/difference/product goes to *res and the
// call yields 1 when the exact result did not fit. x86 hands back both answers
// from the one instruction -- the wrapped result in the register and the
// condition that says it wrapped -- so this is a handful of instructions rather
// than a library call that would have to redo the arithmetic in a wider type.
func (c *CG) genOverflowBuiltin(n *frontend.Call, ob frontend.OverflowBuiltin) (frontend.CType, error) {
	if len(n.Args) != 3 {
		return frontend.TInt, fmt.Errorf("%s: expected 3 arguments, got %d", n.Name, len(n.Args))
	}
	// The width is part of the name (sadd is 32-bit, saddll is 64-bit); the
	// generic __builtin_add_overflow spelling has to read it off the result
	// pointer instead.
	w := ob.Width
	signed := ob.Signed
	if w == 0 {
		pt := c.exprType(n.Args[2])
		if pt == nil || !pt.IsPtr() || pt.Elem == nil {
			return frontend.TInt, fmt.Errorf("%s: third argument must be a pointer to the result", n.Name)
		}
		w = c.typeWidth(pt.Elem)
		signed = pt.Elem.Kind == frontend.KInt && pt.Elem.Signed
	}
	if w != 1 && w != 2 && w != 4 && w != 8 {
		return frontend.TInt, fmt.Errorf("%s: no overflow check for a result of width %d", n.Name, w)
	}
	wn := map[int]string{1: "byte", 2: "word", 4: "dword", 8: "qword"}[w]
	acc := map[int]string{1: "al", 2: "ax", 4: "eax", 8: "rax"}[w]
	entry := c.tmpDepth
	defer func() { c.tmpDepth = entry }()

	// Every operand is evaluated before the arithmetic starts: the check reads
	// the flags the operation itself sets, and a call hiding in an argument
	// would clobber them.
	slot := make([]int, 3)
	for i, a := range n.Args {
		if _, err := c.genExprT(a); err != nil {
			return frontend.TInt, err
		}
		c.tmpDepth++
		slot[i] = c.tmpSlot(c.tmpDepth)
		c.emit("mov [rbp%+d], rax", slot[i])
	}
	c.emit("mov r10, [rbp%+d]", slot[2])

	switch ob.Op {
	case "+", "-":
		mnem := "add"
		if ob.Op == "-" {
			mnem = "sub"
		}
		c.emit("mov %s, [rbp%+d]", acc, slot[0])
		c.emit("%s %s, %s [rbp%+d]", mnem, acc, wn, slot[1])
		// OF is signed overflow. CF is the carry out of the field, which is
		// unsigned overflow going up and a borrow coming down -- the same flag
		// covers both directions of the unsigned check.
		if signed {
			c.emit("seto r11b")
		} else {
			c.emit("setc r11b")
		}
		c.emit("mov %s [r10], %s", wn, acc)
	case "*":
		if w >= 4 {
			// One-operand IMUL/MUL widen into rdx:rax and set OF/CF when the
			// upper half is not the sign extension of the lower (signed) or is
			// simply not zero (unsigned) -- which is exactly "did not fit".
			c.emit("mov %s, [rbp%+d]", acc, slot[0])
			if signed {
				c.emit("imul %s [rbp%+d]", wn, slot[1])
				c.emit("seto r11b")
			} else {
				c.emit("mul %s [rbp%+d]", wn, slot[1])
				c.emit("setc r11b")
			}
			c.emit("mov %s [r10], %s", wn, acc)
			break
		}
		// A byte or word product has no widening multiply worth relying on, so
		// it is computed in 32 bits and then tested against the field: sign-
		// extending the low bits back has to reproduce the product (signed),
		// or the bits above the field have to be clear (unsigned).
		c.emit("mov eax, [rbp%+d]", slot[0])
		if signed {
			c.emit("imul dword [rbp%+d]", slot[1])
		} else {
			c.emit("mul dword [rbp%+d]", slot[1])
		}
		// The wrapped product is stored first: the test below needs rax.
		c.emit("mov %s [r10], %s", wn, acc)
		c.emit("mov r11d, eax")
		c.emit("mov eax, r11d")
		if signed {
			c.emit("shl eax, %d", 32-8*w)
			c.emit("sar eax, %d", 32-8*w)
			c.emit("cmp eax, r11d")
		} else {
			// SHR sets ZF on the shifted result, so the zero test is free.
			c.emit("shr eax, %d", 8*w)
		}
		c.emit("setne r11b")
	default:
		return frontend.TInt, fmt.Errorf("%s: unknown overflow operation %q", n.Name, ob.Op)
	}
	c.emit("movzx eax, r11b")
	c.resTyp = frontend.TInt
	c.resSigned = false
	c.resW = 4
	return frontend.TInt, nil
}

func (c *CG) genAtomicBuiltin(n *frontend.Call, ab frontend.AtomicBuiltin) (frontend.CType, error) {
	// The GCC __atomic_* spellings carry trailing arguments the model has no
	// use for (a memory_order, plus a weak flag for the compare-exchange), so
	// they are dropped here and what remains is exactly the __goc_ form's
	// argument list. Trimming a copy keeps the AST the checker already
	// validated untouched for every other pass.
	if ab.GCCOrderArgs > 0 {
		trimmed := *n
		trimmed.Args = n.Args[:len(n.Args)-ab.GCCOrderArgs]
		n = &trimmed
	}
	if ab.CAS {
		return c.genAtomicCAS(n)
	}
	if ab.LoadOnly {
		return c.genAtomicLoad(n)
	}
	return c.genAtomicFetch(n, ab.Op)
}

// genAtomicLoad implements __atomic_load_n(p, mo). An aligned scalar read is
// indivisible on x86-64, so this is the ordinary load of *p -- the same
// reasoning stdatomic.h's atomic_load gives -- and the memory_order argument is
// dropped because there is no reordering barrier to emit. The width comes from
// the object, so a char-sized atomic loads a byte.
func (c *CG) genAtomicLoad(n *frontend.Call) (frontend.CType, error) {
	et := c.atomicPointee(n.Args[0])
	if et == nil {
		return frontend.TInt, fmt.Errorf("%s: first argument must be a pointer to an _Atomic integer", n.Name)
	}
	aw := c.atomicWidth(et)
	if aw == 0 {
		return frontend.TInt, fmt.Errorf("%s: no atomic instruction for an object of width %d", n.Name, c.typeWidth(et))
	}
	wn := map[int]string{1: "byte", 2: "word", 4: "dword", 8: "qword"}[aw]
	acc := map[int]string{1: "al", 2: "ax", 4: "eax", 8: "rax"}[aw]
	signed := et.Kind == frontend.KInt && et.Signed
	entry := c.tmpDepth
	defer func() { c.tmpDepth = entry }()

	if _, err := c.genExprT(n.Args[0]); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	addrSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov [rbp%+d], rax", addrSlot)
	c.emit("mov r10, [rbp%+d]", addrSlot)
	c.emit("mov %s, %s [r10]", acc, wn)
	c.widenFrom(aw, signed)
	return frontend.TInt, nil
}

// atomicPointee returns the integer (or _Bool) type the first argument of an
// atomic builtin points at, or nil when the argument is not such a pointer.
// The width of the locked access comes from here and never from the operand:
// atomic_fetch_add on an _Atomic char must not touch the seven bytes after it.
func (c *CG) atomicPointee(e frontend.Expr) *frontend.Type {
	pt := c.exprType(e)
	if pt == nil || !pt.IsPtr() || pt.Elem == nil {
		return nil
	}
	et := pt.Elem
	// Pointers join the integers and _Bool: the locked access is one
	// instruction wide for an aligned pointer, and a lock-free allocator
	// (Nim's own, for one) keeps its free lists in atomic pointer slots.
	if et.Kind != frontend.KInt && et.Kind != frontend.KBool && et.Kind != frontend.KPtr {
		return nil
	}
	return et
}

// atomicWidth reports the locked-access width for an atomic object type, or 0
// when the width has no single-instruction atomic form.
func (c *CG) atomicWidth(et *frontend.Type) int {
	aw := c.typeWidth(et)
	if aw == 1 || aw == 2 || aw == 4 || aw == 8 {
		return aw
	}
	return 0
}

func (c *CG) genAtomicFetch(n *frontend.Call, op string) (frontend.CType, error) {
	if len(n.Args) != 2 {
		return frontend.TInt, fmt.Errorf("%s: expected 2 arguments, got %d", n.Name, len(n.Args))
	}
	et := c.atomicPointee(n.Args[0])
	if et == nil {
		return frontend.TInt, fmt.Errorf("%s: first argument must be a pointer to an _Atomic integer", n.Name)
	}
	aw := c.atomicWidth(et)
	if aw == 0 {
		return frontend.TInt, fmt.Errorf("%s: no atomic instruction for an object of width %d", n.Name, c.typeWidth(et))
	}
	wn := map[int]string{1: "byte", 2: "word", 4: "dword", 8: "qword"}[aw]
	acc := map[int]string{1: "al", 2: "ax", 4: "eax", 8: "rax"}[aw]
	des := map[int]string{1: "r11b", 2: "r11w", 4: "r11d", 8: "r11"}[aw]
	signed := et.Kind == frontend.KInt && et.Signed
	entry := c.tmpDepth
	defer func() { c.tmpDepth = entry }()

	// The address is evaluated once, before the operand, so evaluating the
	// operand cannot disturb it.
	if _, err := c.genExprT(n.Args[0]); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	addrSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov [rbp%+d], rax", addrSlot)
	if _, err := c.genExprT(n.Args[1]); err != nil {
		return frontend.TInt, err
	}
	if err := c.ensureType(frontend.TInt); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	valSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov [rbp%+d], rax", valSlot)

	switch op {
	case "+", "-":
		// LOCK XADD is a fetch-and-add: memory becomes old+operand and the
		// register comes away with old, which is exactly what fetch_add and
		// fetch_sub return.
		c.emit("mov r10, [rbp%+d]", addrSlot)
		c.emit("mov %s, [rbp%+d]", acc, valSlot)
		if op == "-" {
			c.emit("neg %s", acc)
		}
		c.emit("lock xadd %s [r10], %s", wn, acc)
		c.widenFrom(aw, signed)
	case "":
		// atomic_exchange: XCHG against a memory operand carries the bus lock
		// implicitly, and leaves the previous contents in the register. goa
		// spells it with the register first.
		c.emit("mov r10, [rbp%+d]", addrSlot)
		c.emit("mov %s, [rbp%+d]", acc, valSlot)
		c.emit("xchg %s, %s [r10]", acc, wn)
		c.widenFrom(aw, signed)
	default:
		// &, | and ^ have no single atomic instruction. CMPXCHG stores the
		// combined value only while memory still holds the value it was
		// computed from, so a concurrent update is detected (ZF cleared) and
		// retried instead of being silently lost.
		c.tmpDepth++
		oldSlot := c.tmpSlot(c.tmpDepth)
		newSlot := c.tmpSlot(c.tmpDepth + 1)
		top := c.newLabel("atomic_fetch")
		c.line(top + ":\n")
		c.emit("mov r10, [rbp%+d]", addrSlot)
		c.emit("mov %s, %s [r10]", acc, wn)
		c.widenFrom(aw, signed)
		c.emit("mov [rbp%+d], rax", oldSlot)
		bin := &frontend.Binary{Op: op,
			L: &frontend.TmpLoad{Slot: oldSlot, Typ: et},
			R: &frontend.TmpLoad{Slot: valSlot, Typ: et}}
		if _, err := c.genBinary(bin); err != nil {
			return frontend.TInt, err
		}
		if err := c.ensureType(frontend.TInt); err != nil {
			return frontend.TInt, err
		}
		c.truncTo(aw, signed)
		c.emit("mov [rbp%+d], rax", newSlot)
		c.emit("mov r10, [rbp%+d]", addrSlot)
		c.emit("mov %s, [rbp%+d]", acc, oldSlot)
		c.emit("mov %s, [rbp%+d]", des, newSlot)
		c.emit("lock cmpxchg %s [r10], %s", wn, des)
		c.emit("jne %s", top)
		c.emit("mov rax, [rbp%+d]", oldSlot)
	}
	c.resTyp = frontend.TInt
	c.resSigned = signed
	c.resW = c.semWOf(et)
	if c.resW == 4 {
		c.canonInt(signed)
	}
	return frontend.TInt, nil
}

// genAtomicCAS lowers atomic_compare_exchange_strong/weak, which goc spells
// the same way because it emits no spurious failure: (object, expected,
// desired) -> true when *object equalled *expected and now holds desired,
// false otherwise -- in which case *expected receives the value observed.
func (c *CG) genAtomicCAS(n *frontend.Call) (frontend.CType, error) {
	if len(n.Args) != 3 {
		return frontend.TInt, fmt.Errorf("%s: expected 3 arguments, got %d", n.Name, len(n.Args))
	}
	et := c.atomicPointee(n.Args[0])
	if et == nil {
		return frontend.TInt, fmt.Errorf("%s: first argument must be a pointer to an _Atomic integer", n.Name)
	}
	aw := c.atomicWidth(et)
	if aw == 0 {
		return frontend.TInt, fmt.Errorf("%s: no atomic instruction for an object of width %d", n.Name, c.typeWidth(et))
	}
	wn := map[int]string{1: "byte", 2: "word", 4: "dword", 8: "qword"}[aw]
	acc := map[int]string{1: "al", 2: "ax", 4: "eax", 8: "rax"}[aw]
	des := map[int]string{1: "r11b", 2: "r11w", 4: "r11d", 8: "r11"}[aw]
	entry := c.tmpDepth
	defer func() { c.tmpDepth = entry }()

	if _, err := c.genExprT(n.Args[0]); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	addrSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov [rbp%+d], rax", addrSlot)
	if _, err := c.genExprT(n.Args[1]); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	expSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov [rbp%+d], rax", expSlot)
	if _, err := c.genExprT(n.Args[2]); err != nil {
		return frontend.TInt, err
	}
	if err := c.ensureType(frontend.TInt); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	valSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov [rbp%+d], rax", valSlot)

	// CMPXCHG wants the expected value in the accumulator and the desired
	// value in a general register; on failure it reloads the accumulator with
	// the value it observed, which is what *expected has to receive.
	c.emit("mov r11, [rbp%+d]", expSlot)
	c.emit("mov %s, %s [r11]", acc, wn)
	c.emit("mov r10, [rbp%+d]", addrSlot)
	c.emit("mov %s, [rbp%+d]", des, valSlot)
	c.emit("lock cmpxchg %s [r10], %s", wn, des)
	ok := c.newLabel("cas_ok")
	c.emit("je %s", ok)
	c.emit("mov r11, [rbp%+d]", expSlot)
	c.emit("mov %s [r11], %s", wn, acc)
	c.line(ok + ":\n")
	// No flag-setting instruction separates the branch from here, so ZF still
	// reports the outcome of the exchange.
	c.emit("sete al")
	c.emit("movzx rax, al")
	c.resTyp = frontend.TInt
	c.resSigned = false
	c.resW = 4
	return frontend.TInt, nil
}

// An _Atomic lvalue is the exception: see genAtomicCompound.
func (c *CG) genCompoundAssign(n *frontend.AssignExpr) (frontend.CType, error) {
	// An _Atomic operand takes the general path even when it is a plain
	// Ident: the fast path below is a load / operate / store sequence, which
	// is exactly the non-atomic shape an atomic update must not take.
	lt := c.exprType(n.Lhs)
	atomic := lt != nil && lt.Atomic
	if id, ok := n.Lhs.(*frontend.Ident); ok && !atomic {
		if vi, ok2 := c.lookupVar(id.Name); ok2 {
			entry := c.tmpDepth
			c.loadVar(vi) // old value in rax (int) / xmm0 (double)
			c.tmpDepth++
			slot := c.tmpSlot(c.tmpDepth)
			if vi.typ != nil && vi.typ.IsFloating() {
				c.emit("movsd [rbp%+d], xmm0", slot)
			} else {
				c.emit("mov [rbp%+d], rax", slot)
			}
			bin := &frontend.Binary{Op: n.Op, L: &frontend.TmpLoad{Slot: slot, Typ: lt}, R: n.Rhs}
			rt, err := c.genBinary(bin)
			c.tmpDepth = entry
			if err != nil {
				return rt, err
			}
			if err := c.ensureType(vi.typ.Class()); err != nil {
				return rt, err
			}
			c.storeVar(vi)
			c.resTyp = vi.typ.Class()
			c.resSigned = vi.typ.Kind == frontend.KInt && vi.typ.Signed
			c.resW = c.semWOf(vi.typ)
			return vi.typ.Class(), nil
		}
	}
	// General case (deref / element / member / bit-field lvalues): evaluate
	// the address once, park it and the old value, evaluate E2 through the
	// shared binary machinery, then store back to the parked address.
	entry := c.tmpDepth
	if err := c.genLValue(n.Lhs); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	addrSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov r11, r10")
	c.emit("mov [rbp%+d], r11", addrSlot)
	width := c.lvalueWidth(n.Lhs)
	class := c.lvalueClass(n.Lhs)
	// C11 _Atomic compound assignment. The generic shape below (load,
	// operate, store) loses any update another thread performs in between, so
	// an atomic lvalue gets one of two locked sequences instead:
	//   - "+=" / "-=" are a single LOCK XADD (fetch-and-add), which also hands
	//     back the previous value -- the result is that plus the delta;
	//   - every other operator has no single atomic instruction, so it runs a
	//     LOCK CMPXCHG retry loop.
	if atomic && c.lvBitWidth == 0 && class == frontend.TInt {
		if aw := c.typeWidth(lt); aw == 1 || aw == 2 || aw == 4 || aw == 8 {
			signed := lt != nil && lt.Kind == frontend.KInt && lt.Signed
			res, err := c.genAtomicCompound(n, lt, aw, signed, addrSlot)
			c.tmpDepth = entry
			return res, err
		}
	}
	if c.lvBitWidth > 0 {
		c.genLoadBitfield(c.lvBitUnit, c.lvBitOff, c.lvBitWidth, c.lvBitSigned)
	} else {
		signed := lt != nil && lt.Kind == frontend.KInt && lt.Signed
		c.genLoadElem("r10", width, class, signed)
	}
	c.tmpDepth++
	valSlot := c.tmpSlot(c.tmpDepth)
	if class == frontend.TDouble {
		c.emit("movsd [rbp%+d], xmm0", valSlot)
	} else {
		c.emit("mov [rbp%+d], rax", valSlot)
	}
	bin := &frontend.Binary{Op: n.Op, L: &frontend.TmpLoad{Slot: valSlot, Typ: lt}, R: n.Rhs}
	rt, err := c.genBinary(bin)
	if err != nil {
		c.tmpDepth = entry
		return rt, err
	}
	c.tmpDepth = entry
	// genBinary canonicalised the combined value; convert it to the lvalue
	// class and store back to the parked address.
	if err := c.ensureType(class); err != nil {
		return rt, err
	}
	c.emit("mov r10, [rbp%+d]", addrSlot)
	if c.lvBitWidth > 0 {
		c.genStoreBitfield(c.lvBitUnit, c.lvBitOff, c.lvBitWidth, c.lvBitSigned)
	} else {
		if lt != nil && lt.Kind == frontend.KBool {
			c.normalizeBool()
		}
		// T1.6 (N17): the movsxd for a materialized signed int stored into an
		// 8-byte slot lives inside genStoreElem (single source of truth; this
		// call site must NOT re-emit it).
		c.genStoreElem("r10", width, class, c.resW, c.resSigned)
	}
	return rt, nil
}

// genBigCompoundAssign compiles "E1 op= E2" when the left operand (or the
// result of the operation) is a _BitInt. The lvalue is evaluated exactly
// once: the address is parked, the old value is snapshotted (and widened to
// the result width when the right operand is wider), the right operand is
// materialised, the __goclib_bi_* helper runs, and the result is stored back
// to the same address.
func (c *CG) genBigCompoundAssign(n *frontend.AssignExpr) (frontend.CType, error) {
	lt := c.exprType(n.Lhs)
	rt := c.exprType(n.Rhs)
	resT := lt
	if n.Op != "<<" && n.Op != ">>" {
		resT = frontend.BigArithResult(n.Op, lt, rt)
	}
	if !frontend.IsBig(resT) {
		return frontend.TInt, fmt.Errorf("internal: no result type for %q on %s and %s", n.Op, lt, rt)
	}
	w := bigWordsOf(resT)
	// 1. Evaluate the lvalue address exactly once.
	if err := c.genLValue(n.Lhs); err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth++
	addrSlot := c.tmpSlot(c.tmpDepth)
	c.emit("mov r11, r10")
	c.emit("mov [rbp%+d], r11", addrSlot)
	// 2. Snapshot the old value (sw source words) and widen it to w words
	//    when the operation promotes the lvalue (e.g. _BitInt(64) +=
	//    _BitInt(128) or int += _BitInt).
	sw := bigWordsOf(lt)
	_, loff := c.claimBig(w)
	cp := sw
	if cp > w {
		cp = w
	}
	for i := 0; i < cp; i++ {
		c.emit("mov rax, [r10%+d]", i*8)
		c.emit("mov [rbp%+d], rax", loff+i*8)
	}
	if w > sw {
		c.emit("mov r10, [rbp%+d]", addrSlot)
		wname := "__goclib_bi_widen_u"
		if lt != nil && lt.Signed {
			wname = "__goclib_bi_widen_s"
		}
		c.callBigLib(wname, []bigArg{
			{addrOff: loff}, {reg: "r10"}, {imm: int64(w)}, {imm: int64(sw)},
		})
	}
	// 3. The right operand: an integer count for shifts, otherwise a big
	//    operand materialised to the result type (evaluated once).
	if n.Op == "<<" || n.Op == ">>" {
		if _, err := c.genExprT(n.Rhs); err != nil {
			return frontend.TInt, err
		}
		if err := c.ensureType(frontend.TInt); err != nil {
			return frontend.TInt, err
		}
		rk, resOff := c.claimBig(w)
		name := "__goclib_bi_shl"
		if n.Op == ">>" {
			name = "__goclib_bi_shr_u"
			if resT.Signed {
				name = "__goclib_bi_shr_s"
			}
		}
		c.callBigLib(name, []bigArg{
			{addrOff: resOff}, {addrOff: loff}, {reg: "rax"}, {imm: int64(w)},
		})
		// Store the result back to the lvalue (w words, the lvalue width).
		c.emit("mov r10, [rbp%+d]", addrSlot)
		c.emit("lea r11, [rbp%+d]", resOff)
		for i := 0; i < w; i++ {
			c.emit("mov rax, [r11%+d]", i*8)
			c.emit("mov [r10%+d], rax", i*8)
		}
		c.tmpDepth = rk + w - 1
		c.markBig(resT, rk, w)
		return frontend.TInt, nil
	}
	_, _, roff, err := c.bigOperand(n.Rhs, resT)
	if err != nil {
		return frontend.TInt, err
	}
	// 4. Compute old op rhs into a result block.
	rk, resOff := c.claimBig(w)
	var name string
	switch n.Op {
	case "+":
		name = "__goclib_bi_add"
	case "-":
		name = "__goclib_bi_sub"
	case "*":
		name = "__goclib_bi_mul"
	case "&":
		name = "__goclib_bi_and"
	case "|":
		name = "__goclib_bi_or"
	case "^":
		name = "__goclib_bi_xor"
	case "/":
		name = "__goclib_bi_div_u"
		if resT.Signed {
			name = "__goclib_bi_div_s"
		}
	case "%":
		name = "__goclib_bi_mod_u"
		if resT.Signed {
			name = "__goclib_bi_mod_s"
		}
	default:
		return frontend.TInt, fmt.Errorf("internal: no big helper for %q", n.Op)
	}
	c.callBigLib(name, []bigArg{
		{addrOff: resOff}, {addrOff: loff}, {addrOff: roff}, {imm: int64(w)},
	})
	// C23 compound assignment converts the result back to the lvalue's
	// type, so a narrow (<64-bit) lvalue wraps (e.g. U8 x; x += 300 -> 44).
	if lt != nil && frontend.IsBig(lt) && lt.Bits < 64 {
		sg := int64(0)
		if lt.Signed {
			sg = 1
		}
		c.callBigLib("__goclib_bi_conv", []bigArg{
			{addrOff: resOff}, {addrOff: resOff}, {imm: int64(lt.Bits)}, {imm: sg}, {imm: 64}, {imm: sg},
		})
	}
	// 5. Store back: the result is converted to the lvalue's type, so only
	//    the lvalue's (sw) low words are written when the result is wider.
	sw2 := w
	if sw2 > bigWordsOf(lt) {
		sw2 = bigWordsOf(lt)
	}
	c.emit("mov r10, [rbp%+d]", addrSlot)
	c.emit("lea r11, [rbp%+d]", resOff)
	for i := 0; i < sw2; i++ {
		c.emit("mov rax, [r11%+d]", i*8)
		c.emit("mov [r10%+d], rax", i*8)
	}
	// 6. The assignment expression's value is resT at resOff, flagged like
	//    genBigBinary leaves it.
	c.tmpDepth = rk + w - 1
	c.markBig(resT, rk, w)
	return frontend.TInt, nil
}

// genExpr emits an expression and discards its type (for statement context).
func (c *CG) genExpr(e frontend.Expr) error {
	_, err := c.genExprT(e)
	return err
}

// Gen produces the full assembly source for a program. linux selects the
// SysV ABI and the Linux goclib; otherwise Windows x64 conventions are used.
// Gen lowers the checked program to goa assembly for the given target. opt is
// the -O optimisation level; today no pass consumes it, so every level
// produces identical output (the IR seed keeps it a documented no-op until
// the first real pass lands).
// The winGUI flag (-mwindows) applies to PE targets only: it emits goa's
// `subsystem windows` directive so the Subsystem field in the PE header is 2
// (windows GUI) instead of 3 (console) and Windows allocates no console
// window. The entry point itself is unchanged -- WinMain's arguments are a
// CRT convention, not something the OS entry point provides; a GUI program
// gets hInstance from GetModuleHandleA(NULL) like any CRT would.
func Gen(prog *frontend.Program, linux bool, opt int, winGUI bool) (string, error) {
	return genWith(prog, linux, opt, winGUI, nil)
}

// newCGFor builds the code generator's state for a program.
//
// Both back ends start from this: the assembly one because it has always, and
// the IR one because it needs the same function table, the same global types
// and the same analysis the assembly path consults. Sharing it is what keeps the
// two from reaching different conclusions about the same program.
func newCGFor(prog *frontend.Program, linux bool, opt int) *CG {
	return &CG{
		strLab:       map[*frontend.StrLit]string{},
		doubleLab:    map[float64]string{},
		varEnts:      map[int]varInfo{},
		scopes:       nil,
		declUID:      map[*frontend.DeclStmt]int{},
		clOff:        map[*frontend.CompoundLit]int{},
		funcs:        map[string]bool{},
		funcDefs:     map[string]*frontend.FuncDecl{},
		calls:        map[string]bool{},
		need:         map[string]bool{},
		globals:      map[string]bool{},
		globalLab:    map[string]string{},
		globalTyp:    map[string]*frontend.Type{},
		staticVars:   map[string]string{},
		tlsVars:      map[string]*tlsVarInfo{},
		tlsList:      nil,
		libEmitted:   map[string]bool{},
		libGlobNames: map[string]bool{},
		libGlobUsed:  map[string]bool{},
		linux:        linux,
		opt:          opt,
	}
}

// genWith is Gen with a hook for the LLVM backend.
//
// skipFuncs names the functions the caller is generating some other way -- as
// LLVM IR rather than as assembly. They are left out of the output here, and
// their absence is also what decides the C runtime subset to emit: a library
// function is generated exactly when the IR side is NOT producing it, so the
// two halves together always cover every symbol the program calls, with no
// duplicates and nothing missing.
//
// What comes out is still a complete program: the entry stub, the globals, and
// every library function the IR side did not claim.
func genWith(prog *frontend.Program, linux bool, opt int, winGUI bool, skipFuncs map[string]bool) (string, error) {
	// long double still arrives here as plain double (see the note in the
	// parser's "double" specifier case). Once #48 gives this back end fp128
	// codegen, a long double that reaches here un-lowered is a bug, not
	// something to fall back on.
	return genOpts(prog, genConfig{linux: linux, opt: opt, winGUI: winGUI, skipFuncs: skipFuncs})
}

// genConfig is what distinguishes one build from another. It exists as a struct
// rather than as more positional parameters because the list had already grown
// past what a call site could be read against, and because the next distinction
// to be added -- an object file rather than a whole program -- changes what the
// generator is allowed to assume rather than what it should produce.
type genConfig struct {
	linux     bool
	opt       int
	winGUI    bool
	skipFuncs map[string]bool

	// relocatable produces one translation unit's worth of code for a later link
	// rather than a whole program. See CG.relocatable for what that changes.
	relocatable bool

	// unit names the source being compiled, and only matters with relocatable:
	// it becomes the prefix that keeps one unit's function-local statics from
	// colliding with another's. Empty for a whole program, which has one unit
	// and so has nothing to be unique among.
	unit string
}

func genOpts(prog *frontend.Program, cfg genConfig) (string, error) {
	asm, _, err := genOptsCG(prog, cfg)
	return asm, err
}

// LibSyms reports which C library symbols this unit's image defines.
//
// The library's exported functions keep their C ABI names, so nothing in the
// assembly marks them as the library's; the assembler has to be told, or a
// second unit carrying its own copy of printf looks like a duplicate
// definition when two units are linked.
func (c *CG) LibSyms() map[string]bool {
	if len(c.libSyms) == 0 {
		return nil
	}
	return c.libSyms
}

// InternalSyms names the file-scope symbols this unit declared `static`. Like
// LibSyms it is a fact only the front end holds -- `static` is not visible in
// the assembly text, since both a static and an extern function are just a label
// goa is asked to define. The assembler records it so the object can file those
// symbols as IMAGE_SYM_CLASS_STATIC, which is what lets two units each declare
// their own `static int scale(...)` without the second looking like a duplicate
// definition.
//
// Returns nil unless this build is a relocatable one: a whole program is one
// unit per name by construction, and marking symbols there would change output
// that is already correct.
func (c *CG) InternalSyms() map[string]bool {
	if !c.relocatable || len(c.internalSyms) == 0 {
		return nil
	}
	return c.internalSyms
}

// markInternal records one file-scope symbol as internal-linkage.
//
// The map is created on demand rather than in the constructor because the
// multi-file whole-program path never reads it back and would otherwise pay for
// an allocation per build to hold a set nothing consumes.
func (c *CG) markInternal(name string) {
	if !c.relocatable {
		return
	}
	if c.internalSyms == nil {
		c.internalSyms = map[string]bool{}
	}
	c.internalSyms[name] = true
}

// genOptsCG is genOpts, additionally handing back the code generator so a caller
// that assembles an object file can read what the generator decided -- which C
// library symbols this unit carries a copy of. A library function's name is
// fixed by the C ABI, so that set cannot be recovered from the assembly; and the
// assembler needs it to tell a second inlined copy of printf from a user who
// defined printf twice.
func genOptsCG(prog *frontend.Program, cfg genConfig) (string, *CG, error) {
	linux, opt, winGUI, skipFuncs := cfg.linux, cfg.opt, cfg.winGUI, cfg.skipFuncs
	ensureLib()
	if goclibErr != nil {
		return "", nil, goclibErr
	}
	if rtdiagMode && rtdiagErr != nil {
		return "", nil, fmt.Errorf("rtdiag library build failed: %w", rtdiagErr)
	}
	c := newCGFor(prog, linux, opt)
	c.skipFuncs = skipFuncs
	c.relocatable = cfg.relocatable
	if cfg.relocatable && cfg.unit != "" {
		// The prefix has to be something a label can start with and that two
		// different sources cannot produce between them, so the file name is
		// reduced to its stem and made unmistakable as a prefix.
		stem := strings.TrimSuffix(filepath.Base(cfg.unit), filepath.Ext(cfg.unit))
		c.staticPrefix = "U_" + sanitizeLabel(stem) + "_"
	}
	for _, g := range prog.Globals {
		if g.IsTLS {
			// Thread-local global: lay it out in the .tls section, not .data.
			off := c.tlsPlace(g.Typ)
			c.tlsVars[g.Name] = &tlsVarInfo{off: off, lab: "TL_" + g.Name, typ: g.Typ}
			c.tlsList = append(c.tlsList, g)
			c.globalTyp[g.Name] = g.Typ
			continue
		}
		if skipFuncs["G_"+g.Name] {
			// Owned by the LLVM object; goa must not re-define it.
			continue
		}
		c.globals[g.Name] = true
		c.globalLab[g.Name] = "G_" + g.Name
		c.globalTyp[g.Name] = g.Typ
		// The label, not the source name: a variable's symbol in the object is
		// "G_"+name (see globalLab), while a function's is its bare name. Marking
		// the source name would miss every variable and rename no function.
		if g.Storage == "static" {
			c.markInternal("G_" + g.Name)
		}
	}
	// The built-in C library joins the program: its file-scope variables
	// (rand_state, ...) share the global pool unless the user declared their
	// own of the same name, and its prototypes/definitions feed call-site
	// double promotion for every caller, user or library-internal. Registering
	// happens BEFORE the user's own functions so a user definition always
	// overwrites the library entry in funcDefs.
	if lib := common.Store(linux); lib != nil {
		for _, g := range lib.Globals {
			if c.globals[g.Name] {
				continue // user global of the same name wins
			}
			if skipFuncs["G_"+g.Name] {
				// Owned by the LLVM object; goa must not re-define it.
				continue
			}
			c.globals[g.Name] = true
			c.globalLab[g.Name] = "G_" + g.Name
			c.globalTyp[g.Name] = g.Typ
			c.libGlobNames[g.Name] = true
			c.libGlobals = append(c.libGlobals, g)
		}
		for _, pr := range lib.Protos {
			if _, dup := c.funcDefs[pr.Name]; !dup {
				c.funcDefs[pr.Name] = pr
			}
			if pr.DLL != "" {
				common.DLLNames[pr.Name] = pr.DLL
			}
		}
		for _, name := range lib.Order {
			if _, dup := c.funcDefs[name]; !dup {
				c.funcDefs[name] = lib.Funcs[name]
			}
		}
	}
	for _, f := range prog.Funcs {
		c.funcs[f.Name] = true
		c.funcDefs[f.Name] = f
		if f.Storage == "static" {
			c.markInternal(f.Name)
		}
		if f.Name == "main" && len(f.Params) > 0 {
			// main(int argc, char **argv) -- the Windows entry stub must
			// parse the command line before calling it. main(void) skips
			// that entirely (see the need["__goclib_get_args"] pull-in).
			c.mainTakesArgs = true
		}
	}
	// Pick the program entry. main is the C standard name; a Windows GUI
	// program has none and instead defines wWinMain (Unicode) or WinMain
	// (ANSI) -- both are just as much a language-level entry point, so the
	// compiler accepts them rather than making the caller write a main()
	// shim. wWinMain wins if present, matching the /SUBSYSTEM:WINDOWS
	// convention of preferring the wide API. Neither is valid on Linux,
	// where the entry really is main(argc, argv, envp).
	if c.funcs["main"] {
		c.entryFn = "main"
	} else if !c.linux && c.funcs["wWinMain"] {
		c.entryFn, c.entryIsGUI, c.entryWide = "wWinMain", true, true
	} else if !c.linux && c.funcs["WinMain"] {
		c.entryFn, c.entryIsGUI = "WinMain", true
	}
	// Prototypes (from #include'd headers) are registered only for call-site
	// double-promotion; they are deliberately NOT added to c.funcs, so a
	// prototype for a goclib function still triggers goclib inclusion.
	for _, f := range prog.Prototypes {
		c.funcDefs[f.Name] = f
	}
	for _, f := range prog.Prototypes {
		if f.DLL != "" {
			common.DLLNames[f.Name] = f.DLL
		}
	}

	// T2.1 (R2): build the call graph over the user's functions before any
	// body is generated, so genFunc can tell "small leaf that nobody calls"
	// (safe to register-home parameters) from "small leaf with a caller"
	// (must stay inlinable). Only prog.Funcs is scanned: a goclib helper is
	// emitted on demand and never competes with a user function for the
	// inliner's attention in a way that depends on our choice here.
	c.t21Called = map[string]bool{}
	for _, f := range prog.Funcs {
		for name := range t21BodyScan(f).callees {
			c.t21Called[name] = true
		}
	}

	var body strings.Builder
	for _, f := range prog.Funcs {
		if skipFuncs[f.Name] {
			continue
		}
		if err := c.genFunc(f); err != nil {
			return "", nil, err
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
	// Windows: a PE entry point receives no argc/argv. Parsing the command
	// line is pure overhead for main(void) programs, so the stub only calls
	// this goclib helper when main actually declares parameters (there is no
	// other way to reach argc/argv in C).
	if !c.linux && c.mainTakesArgs {
		c.need["__goclib_get_args"] = true
	}
	// A GUI (wWinMain/WinMain) entry receives the command line with argv[0]
	// already stripped, like the MSVC CRT -- so the stub calls a goclib
	// helper that skips the program name instead of GetCommandLine* directly.
	if !c.linux && c.entryIsGUI {
		if c.entryWide {
			c.need["__goclib_lp_cmdline_w"] = true
		} else {
			c.need["__goclib_lp_cmdline_a"] = true
		}
	}
	// Entry-stub terminator. Start from the bare __goclib_exit (just
	// ExitProcess / exit_group) and upgrade to the full C exit -- atexit
	// handlers plus the std*-stream flush the C standard requires -- only
	// when the program actually touched stdout or stderr (every FILE-layer
	// output goes through the __goclib_stdout/__goclib_stderr accessors) or
	// registered an atexit handler. A program that never prints -- or prints
	// only through the print builtin, whose __goclib_write is an unbuffered
	// direct OS write -- then carries no flush machinery at all. Without a
	// C library the stub keeps calling extern exit (legacy behaviour).
	c.exitSym = "exit"
	// Only a unit that carries the entry stub has anything to exit through.
	// Pulling __goclib_exit into a unit without an entry put ExitProcess and
	// the stdio flush chain into every helper module -- and with two or more
	// of those in a link, every one of them defined a private copy of the same
	// goclib functions, which is a duplicate-symbol error with no useful
	// message.
	if lib := common.Store(c.linux); lib != nil && c.entryFn != "" {
		if _, ok := lib.Funcs["__goclib_exit"]; ok {
			c.need["__goclib_exit"] = true
			c.exitSym = "__goclib_exit"
		}
	}
	if err := c.genClibFuncs(); err != nil {
		return "", nil, err
	}
	if c.exitSym == "__goclib_exit" && c.needsFullExit() {
		// Upgrade: pull the full C exit (its flush chain is partly there
		// already) and generate whatever it needs in a second fixpoint.
		c.need["exit"] = true
		c.exitSym = "exit"
		if err := c.genClibFuncs(); err != nil {
			return "", nil, err
		}
	}
	// -O1 and above: structured passes over the whole-program body stream.
	// Inlining first (its argument spills then feed constants to the
	// tracker), constant forwarding second (it materialises the immediates
	// the peephole then turns into xor). All passes see real instructions
	// only; inline __asm is off limits (its flag effects are the author's
	// business). -O0 keeps the legacy textual pass so its output stays
	// byte-identical.
	//
	// opt 2 (-Os/-Oz) is the size-first level: inlining is the only pass
	// here that grows the program (measured +17KB across the examples), so
	// it stays off; every other pass only deletes instructions and runs
	// exactly as at -O1. Levels 3 (-O2) and 4 (-O3/-Ofast) are the T1.1 hook
	// points: they run this same set today and future passes gate on
	// c.opt >= 3 / c.opt >= 4.
	if c.opt >= 1 {
		if c.opt != 2 {
			c.insts = inlineCalls(c.insts)
		}
		c.insts = constProp(c.insts)
		if !elimRedundantExtSkip {
			c.insts = elimRedundantExt(c.insts)
		}
		if !slotCacheSkip {
			c.insts = slotCache(c.insts)
		}
		if !copyElimSkip {
			c.insts = copyElim(c.insts)
		}
		c.insts = peepholeIR(c.insts)
		c.insts = fuseCmpBranch(c.insts)
		if !algebraicIdentSkip {
			c.insts = algebraicIdent(c.insts)
		}
		if !sibFoldSkip {
			c.insts = sibFold(c.insts)
		}
		c.insts = deadStores(c.insts)
		c.insts = livenessDSE(c.insts)
		// Copy propagation needs the dead copies gone first: a stranded
		// `mov rax, r14` redefines the very register a later `mov r14, rax`
		// wants to propagate, and refusing it there loses the rewrite. So:
		// clear dead copies, propagate, then clear what propagation stranded.
		c.insts = deadMoveElim(c.insts)
		c.insts = copyProp(c.insts)
		c.insts = deadMoveElim(c.insts)
		c.insts = foldLea(c.insts)
		c.insts = foldCmpMem(c.insts)
	}
	body.WriteString(printASM(c.insts))

	if c.entryFn == "" {
		if c.linux && (c.funcs["wWinMain"] || c.funcs["WinMain"]) {
			return "", nil, fmt.Errorf("wWinMain/WinMain is a Windows entry point; an ELF program must define main()")
		}
		// An object file is one unit of several: main may be in a sibling. The
		// linker is what decides whether the program as a whole has one, and it
		// reports the failure by name if not -- which is a better diagnostic
		// than this, because it happens after every unit has been seen.
		if !c.relocatable {
			return "", nil, fmt.Errorf("program has no main()")
		}
	}

	// Every import the program needs: the exit routine for the entry stub,
	// whatever the C code calls directly, and whatever goclib pulled in.
	importSet := map[string]bool{}
	if c.linux {
		// The C library's exit (if compiled in) replaced the extern stub --
		// see the need["exit"] pull-in above.
		if lib := common.Store(c.linux); lib != nil {
			if _, isC := lib.Funcs["exit"]; !isC {
				importSet["exit"] = true
			}
		} else {
			importSet["exit"] = true
		}
	} else {
		// The entry stub calls the C library's exit when available (pulled in
		// via need["exit"] above); the ExitProcess import is then only needed
		// by __goclib_exit, which the need closure tracks. Keep the import for
		// the no-library fallback path.
		if lib := common.Store(c.linux); lib == nil {
			importSet["ExitProcess"] = true
		}
	}
	// A WinMain entry needs the module handle and the raw command line; the
	// stub calls them directly rather than through gocommon. The W variant of
	// GetCommandLine matches wWinMain's LPWSTR parameter.
	if c.entryIsGUI {
		// hInstance comes straight from GetModuleHandleA; the command line is
		// handed to wWinMain/WinMain argv[0]-stripped via __goclib_lp_cmdline_*
		// (pulled in through the need graph above), which itself imports
		// GetCommandLine* -- so it is not listed here.
		importSet["GetModuleHandleA"] = true
	}
	for name := range c.calls {
		importSet[name] = true
	}
	imports := make([]string, 0, len(importSet))
	for name := range importSet {
		if c.linux {
			if !externLinux[name] {
				if c.relocatable {
					// Not a syscall, but the object may still be linking
					// against another unit that defines it. Emit an extern with
					// no library: goa turns that into an ordinary external, and
					// the object records the name as undefined for the linker to
					// resolve.
					imports = append(imports, fmt.Sprintf("extern %s\n", name))
					continue
				}
				return "", nil, fmt.Errorf("unknown function %q: not in goclib (%s), and not a Linux syscall goa knows",
					name, strings.Join(goclibNames(c.linux), ", "))
			}
			// ELF targets have no DLLs: goa turns this into a syscall stub.
			imports = append(imports, fmt.Sprintf("extern %s\n", name))
			continue
		}
		dll, ok := dllFor(name)
		if !ok {
			if c.relocatable {
				// Same reasoning as the Linux branch above: an unknown callee in
				// an object file is a callee in another unit, not a mistake.
				// The name is left undefined rather than bound to a guessed DLL,
				// because a wrong guess produces an import that loads and then
				// fails at run time with no explanation.
				imports = append(imports, fmt.Sprintf("extern %s\n", name))
				continue
			}
			return "", nil, fmt.Errorf("unknown function %q: not in goclib (%s), and no DLL named on its prototype (declare it as 'extern ret %s(args), dllname;')",
				name, strings.Join(goclibNames(c.linux), ", "), name)
		}
		imports = append(imports, fmt.Sprintf("extern %s, %s\n", name, dll))
	}
	// imports is sorted by the linker, which is what writes them out.

	// The entry stub, the data sections and the literal pools are not C: they
	// are the same for any back end that targets this assembler, so they live
	// in goc/common/link and are driven entirely by the data collected above.
	// What the code generator alone knows -- the string pool (a static
	// initialiser can introduce a literal no function body mentions), the
	// function-address table, the .tls layout its own access code already
	// refers to -- is handed over rather than recomputed, so the image cannot
	// drift away from the code that addresses it.
	d := &link.Data{
		Program:        prog,
		Body:           body.String(),
		Linux:          linux,
		WinGUI:         winGUI,
		Opt:            opt,
		Imports:        imports,
		Globals:        c.globalLab,
		StaticVars:     c.staticVars,
		FuncAddr:       c.funcAddrSym,
		StrLabs:        c.strLab,
		LibGlobals:     c.libGlobals,
		LibGlobalsUsed: c.libGlobUsed,
		Entry: link.Entry{
			Func:      c.entryFn,
			Exit:      c.exitSym,
			IsGUI:     c.entryIsGUI,
			Wide:      c.entryWide,
			TakesArgs: c.mainTakesArgs,
		},
		UnitPrefix: c.staticPrefix,
	}
	for i, s := range c.strs {
		d.Strings = append(d.Strings, link.StringConst{
			Label: fmt.Sprintf("%sLC%d", c.staticPrefix, i),
			Text:  string(s.Bytes),
			Wide:  s.Wide,
		})
	}
	for _, words := range c.bigLits {
		d.BigWords = append(d.BigWords, link.WordArray{Words: words})
	}
	for _, v := range c.doubles {
		d.Doubles = append(d.Doubles, link.DoubleConst{Label: c.doubleLab[v], Value: v})
	}
	for _, se := range c.staticList {
		d.Statics = append(d.Statics, link.StaticLocal{Label: se.lab, Decl: se.d})
	}
	for _, decl := range c.tlsList {
		t := c.tlsVars[decl.Name]
		d.TLSVars = append(d.TLSVars, link.TLSVar{
			Name:   decl.Name,
			Decl:   decl,
			Offset: t.off,
			Label:  t.lab,
		})
	}
	asm, err := link.Emit(d)
	if err != nil {
		return "", nil, err
	}
	return asm, c, nil
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

// regToFull64 maps every integer GP register (64-bit, 32-bit, 16-bit and
// 8-bit forms) emitted by goc to its 64-bit parent register. It is empty for
// non-register operands.
var regToFull64 = func() map[string]string {
	m := map[string]string{}
	for r := range gp64Regs {
		m[r] = r
	}
	// classic 8: explicit 32/16/8-bit sub-registers
	subs := map[string]string{
		"eax": "rax", "ax": "rax", "al": "rax", "ah": "rax",
		"ebx": "rbx", "bx": "rbx", "bl": "rbx", "bh": "rbx",
		"ecx": "rcx", "cx": "rcx", "cl": "rcx", "ch": "rcx",
		"edx": "rdx", "dx": "rdx", "dl": "rdx", "dh": "rdx",
		"esi": "rsi", "si": "rsi", "sil": "rsi",
		"edi": "rdi", "di": "rdi", "dil": "rdi",
		"ebp": "rbp", "bp": "rbp", "bpl": "rbp",
		"esp": "rsp", "sp": "rsp", "spl": "rsp",
	}
	for k, v := range subs {
		m[k] = v
	}
	for i := 8; i < 16; i++ {
		n := fmt.Sprintf("r%d", i)
		m[n+"d"] = n
		m[n+"w"] = n
		m[n+"b"] = n
	}
	return m
}()

// fullRegOf returns the 64-bit parent of a (possibly sub-) register operand, and
// whether o is an integer GP register at all.
func fullRegOf(o string) (string, bool) {
	r, ok := regToFull64[o]
	return r, ok
}

// subRegOf reports whether o is a STRICT sub-register of the 64-bit GP register
// reg (eax/ax/al/ah are sub-registers of rax). It is false when o == reg itself,
// so callers that already handle the full register specially stay correct.
func subRegOf(reg, o string) bool {
	if !gp64Regs[reg] || o == reg {
		return false
	}
	fr, ok := regToFull64[o]
	return ok && fr == reg
}

// memSizePrefixes are the optional size mnemonics that may precede a memory
// operand ("dword [rax]", "byte [rbp-3]", ...).
var memSizePrefixes = map[string]bool{
	"byte": true, "word": true, "dword": true, "qword": true,
	"tbyte": true, "xmmword": true, "ymmword": true,
}

// isMemOperandSized is isMemOperand plus sized memory operands such as
// "dword [r10]" -- goc emits these for narrow stores/loads, and they must be
// recognised as memory so value tracking and use-classification stay correct.
func isMemOperandSized(s string) bool {
	if isMemOperand(s) {
		return true
	}
	if i := strings.Index(s, "["); i > 0 {
		if memSizePrefixes[strings.TrimSpace(s[:i])] {
			return true
		}
	}
	return false
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

// ccInverse maps an x86 conditional-jump mnemonic to the one that fires on
// exactly the opposite condition.
var ccInverse = map[string]string{
	"je": "jne", "jne": "je",
	"jl": "jge", "jge": "jl",
	"jle": "jg", "jg": "jle",
	"ja": "jbe", "jbe": "ja",
	"jb": "jae", "jae": "jb",
	"js": "jns", "jns": "js",
	"jo": "jno", "jno": "jo",
	"jp": "jnp", "jnp": "jp",
}

// fuseCmpBranch collapses the boolean materialisation emitCompare splices in
// front of every conditional branch.
//
// emitCompare has to spell "rax = (A OP B)" out with a branch because goa has
// no setcc, so an `if`/`for`/`while` test comes out as seven instructions:
//
//	cmp A, B          cmp A, B
//	jl  Lt            <-- materialise
//	mov rax, 0            |
//	jmp Le                |
//	Lt: mov rax, 1        |
//	Le: cmp rax, 0    <-- then immediately test the 0/1 and branch
//	je  Target
//
// When -- as here -- the 0/1 is consumed by nothing but the branch, the whole
// thing is one instruction: the branch want to reach Target when the
// comparison is FALSE (`je`), which is exactly `jge` off the original flags.
// Measured on tmp/bench/fib_iter.c this takes the loop body from 19
// instructions per iteration to 13, and goc from ~3x gcc -O2 to ~2x.
//
// The flags live untouched across the materialisation (neither `mov` nor
// `jmp` touches them), so re-testing them after the join is always valid.
func fuseCmpBranch(insts []Inst) []Inst {
	// Count jump targets first: the two labels of a materialisation may only
	// be dropped when nothing else in the function lands on them.
	ref := map[string]int{}
	for _, in := range insts {
		if in.Kind != instInstr {
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok || len(pl.operands) != 1 {
			continue
		}
		if pl.op == "jmp" || ccInverse[pl.op] != "" {
			ref[pl.operands[0]]++
		}
	}
	out := make([]Inst, 0, len(insts))
	for i := 0; i < len(insts); {
		if n, fused := matchCmpMaterialisation(insts, i, ref); n > 0 {
			out = append(out, insts[i]) // keep the comparison verbatim
			out = append(out, Inst{Kind: instInstr, Text: "\t" + fused})
			i += n
			continue
		}
		out = append(out, insts[i])
		i++
	}
	return out
}

// matchCmpMaterialisation recognises the nine-line shape documented on
// fuseCmpBranch starting at insts[i]. It returns the number of lines consumed
// and the single conditional jump that replaces all of them, or (0, "") when
// insts[i] does not start that shape.
func matchCmpMaterialisation(insts []Inst, i int, ref map[string]int) (int, string) {
	if i+9 > len(insts) {
		return 0, ""
	}
	instr := func(k int) (parsedLine, bool) {
		if insts[i+k].Kind != instInstr {
			return parsedLine{}, false
		}
		return parseBodyLine(insts[i+k].Text)
	}
	labelName := func(k int) (string, bool) {
		if insts[i+k].Kind != instLabel {
			return "", false
		}
		s := strings.TrimSpace(insts[i+k].Text)
		if !strings.HasSuffix(s, ":") {
			return "", false
		}
		return strings.TrimSuffix(s, ":"), true
	}

	cmp, ok := instr(0)
	if !ok || cmp.op != "cmp" || len(cmp.operands) != 2 {
		return 0, ""
	}
	// j1 must be an ordinary condition. `jp`/`jnp` belong to the float
	// comparison shape (emitCompareDbl), which carries an extra unordered
	// test that has no single-instruction equivalent here.
	j1, ok := instr(1)
	if !ok || len(j1.operands) != 1 || ccInverse[j1.op] == "" ||
		j1.op == "jp" || j1.op == "jnp" {
		return 0, ""
	}
	lTrue := j1.operands[0]
	if ref[lTrue] != 1 {
		return 0, ""
	}
	zero, ok := instr(2)
	if !ok || !writesRaxConst(zero, "0") {
		return 0, ""
	}
	jm, ok := instr(3)
	if !ok || jm.op != "jmp" || len(jm.operands) != 1 {
		return 0, ""
	}
	lEnd := jm.operands[0]
	if ref[lEnd] != 1 || lEnd == lTrue {
		return 0, ""
	}
	if name, ok := labelName(4); !ok || name != lTrue {
		return 0, ""
	}
	one, ok := instr(5)
	if !ok || one.op != "mov" || len(one.operands) != 2 ||
		one.operands[0] != "rax" || one.operands[1] != "1" {
		return 0, ""
	}
	if name, ok := labelName(6); !ok || name != lEnd {
		return 0, ""
	}
	// The re-test: `cmp rax, 0` as emitted, or `test rax, rax` once
	// algebraicIdent has normalised it.
	ret, ok := instr(7)
	if !ok || len(ret.operands) != 2 || !testsRaxZero(ret) {
		return 0, ""
	}
	br, ok := instr(8)
	if !ok || len(br.operands) != 1 || (br.op != "je" && br.op != "jne") {
		return 0, ""
	}
	if br.operands[0] == lTrue || br.operands[0] == lEnd {
		return 0, ""
	}
	// `je` reaches the target when rax==0, i.e. when the comparison is FALSE:
	// that is the inverse condition. `jne` reaches it when it is TRUE.
	if br.op == "je" {
		return 9, ccInverse[j1.op] + " " + br.operands[0]
	}
	return 9, j1.op + " " + br.operands[0]
}

// writesRaxConst reports whether pl stores the integer constant v into rax,
// in either the emitted spelling (`mov rax, 0`) or the one peepholeIR
// rewrites it to (`xor eax, eax`).
func writesRaxConst(pl parsedLine, v string) bool {
	if pl.op == "mov" && len(pl.operands) == 2 {
		return pl.operands[0] == "rax" && pl.operands[1] == v
	}
	if pl.op == "xor" && len(pl.operands) == 2 && pl.operands[0] == pl.operands[1] {
		return regToFull64[pl.operands[0]] == "rax" && v == "0"
	}
	return false
}

// testsRaxZero reports whether pl is the "is the boolean false?" re-test.
func testsRaxZero(pl parsedLine) bool {
	if pl.op == "cmp" {
		return pl.operands[0] == "rax" && pl.operands[1] == "0"
	}
	if pl.op == "test" {
		return pl.operands[0] == "rax" && pl.operands[1] == "rax"
	}
	return false
}

// gp64Regs is the set of full 64-bit register spellings; gpRegs adds the
// 32-bit names, because a partial-register destination still defines the
// register (mov eax, [m] zero-extends into all of rax) and must therefore
// invalidate any known value, even though it never forwards one.
var gp64Regs = map[string]bool{
	"rax": true, "rbx": true, "rcx": true, "rdx": true,
	"rsi": true, "rdi": true, "rbp": true, "rsp": true,
	"r8": true, "r9": true, "r10": true, "r11": true,
	"r12": true, "r13": true, "r14": true, "r15": true,
}

var gpRegs = func() map[string]bool {
	m := map[string]bool{}
	for r := range gp64Regs {
		m[r] = true
	}
	for _, r := range []string{"eax", "ebx", "ecx", "edx", "esi", "edi"} {
		m[r] = true
	}
	for i := 8; i < 16; i++ {
		m[fmt.Sprintf("r%dd", i)] = true
	}
	return m
}()

// isSlotOperand reports whether s is a direct, unsized variable access as goc
// emits it for every stack slot and global: "[rbp-N]" / "[rbp+N]" /
// "[rip+G_x]". These are exactly the operands whose addresses are distinct
// from each other, so a store to one slot never perturbs another tracked one.
// Anything else -- "[r10]", "[rax+8]", "byte [rbp-3]" -- could alias any of
// them and is treated as clobbering all knowledge.
func isSlotOperand(s string) bool {
	return strings.HasPrefix(s, "[rbp") || strings.HasPrefix(s, "[rip+")
}

// ---------- -O1: small-function inlining over the whole-program IR ----------

// inlineCand is an extracted, inlining-ready body of a leaf function: the
// prologue is stripped, every `mov rsp, rbp; pop rbp; ret` epilogue is
// replaced by a placeholder, and the remaining instructions passed the
// safety screen (no calls, no inline asm, no callee-save writes, no stack
// arguments).
type inlineCand struct {
	body    []Inst          // prologue/epilogue-stripped template
	labels  map[string]bool // internal label names, for per-site renaming
	maxSlot int             // deepest [rbp-N] the template touches
	need    int             // frame bytes the remapped body occupies (16-aligned)
}

// inlineRetPlaceholder marks where a stripped epilogue used to be; each
// instantiation rewrites it into a jump to its call-site continuation label.
const inlineRetPlaceholder = "@@goc_inline_ret@@"

// internalLabelRe matches the compiler-generated local labels. Every one of
// them starts with a dot (.L1:, .Lcmp3:, .Lcase7:, .Lswend6:); top-level
// symbols (_start:, add:, main:) never do, so the dot is the whole story.
var internalLabelRe = regexp.MustCompile(`^\.`)

var rbpSlotRe = regexp.MustCompile(`\[rbp-(\d+)\]`)

// calleeSaveWrites lists every spelling of a register whose clobbering would

// calleeSaveWrites lists every spelling of a register whose clobbering would
// corrupt the caller's state across an inlined body: the callee-save integer
// registers plus rsp/rbp. goc homes locals in rbx/r12-r14, so a template
// writing any of these could silently overwrite a caller local.
var calleeSaveWrites = func() map[string]bool {
	m := map[string]bool{"rbp": true, "rsp": true}
	fams := []struct {
		r64, r32, r16, r8, r8h string
	}{
		{"rbx", "ebx", "bx", "bl", "bh"},
		{"r12", "r12d", "r12w", "r12b", ""},
		{"r13", "r13d", "r13w", "r13b", ""},
		{"r14", "r14d", "r14w", "r14b", ""},
		{"r15", "r15d", "r15w", "r15b", ""},
	}
	for _, f := range fams {
		for _, r := range []string{f.r64, f.r32, f.r16, f.r8, f.r8h} {
			if r != "" {
				m[r] = true
			}
		}
	}
	return m
}()

// isCalleeSaveRestore reports whether text is one of the epilogue's
// callee-save restores, `mov rX, [rbp-8..32]`, emitted right before
// mov rsp,rbp. Restores are skipped when building an inline template: the
// caller's own prologue pushed (and its epilogue will pop) these registers,
// so reloading them from the caller's frame would only clobber the caller's
// saved copies with the callee's stale values.
func isCalleeSaveRestore(text string) bool {
	pl, ok := parseBodyLine(text)
	if !ok || pl.op != "mov" || len(pl.operands) != 2 {
		return false
	}
	if !calleeSaveRegs[pl.operands[0]] {
		return false
	}
	return strings.HasPrefix(pl.operands[1], "[rbp-") && strings.HasSuffix(pl.operands[1], "]")
}

// parseEq reports whether text parses as exactly the given mnemonic and
// operands.
func parseEq(text, op string, ops ...string) bool {
	pl, ok := parseBodyLine(text)
	if !ok || pl.op != op || len(pl.operands) != len(ops) {
		return false
	}
	for i := range ops {
		if pl.operands[i] != ops[i] {
			return false
		}
	}
	return true
}

// extractInlineCand prepares the block [lo, hi) (lo points at the function
// label) for inlining, or returns nil when any safety rule rejects it.
func extractInlineCand(insts []Inst, lo, hi int) *inlineCand {
	body := insts[lo+1 : hi]
	// Canonical prologue: push rbp / mov rbp, rsp / [push <callee-save>]* /
	// sub rsp, N. The entry stub (_start:) and anything else irregular is
	// excluded, which also guarantees a caller-side rbp frame exists when
	// this body is a caller. The prologue pushes rbx/r12/r13/r14
	// unconditionally between mov rbp,rsp and the frame alloc, so the shape
	// check must skip them before requiring the `sub rsp` -- otherwise every
	// candidate would fail and -O1 would inline nothing.
	if len(body) < 3 {
		return nil
	}
	if !parseEq(body[0].Text, "push", "rbp") || !parseEq(body[1].Text, "mov", "rbp", "rsp") {
		return nil
	}
	i := 2
	for i < len(body) {
		fr, ok := parseBodyLine(body[i].Text)
		if !ok || fr.op != "push" || len(fr.operands) != 1 || !calleeSaveRegs[fr.operands[0]] {
			break
		}
		i++
	}
	if i >= len(body) {
		return nil
	}
	if fr, ok := parseBodyLine(body[i].Text); !ok || fr.op != "sub" ||
		len(fr.operands) != 2 || fr.operands[0] != "rsp" {
		return nil
	}
	// Every ret must close a mov rsp,rbp / pop rbp / ret triple: those are
	// the only epilogues goc emits (goa has no `leave`), and anything else
	// means a shape we do not model. `ret` carries no operands, so
	// parseBodyLine rejects it -- compare the trimmed text instead.
	isRet := func(in Inst) bool { return strings.TrimSpace(in.Text) == "ret" }
	isMvr := func(in Inst) bool { return parseEq(in.Text, "mov", "rsp", "rbp") }
	isPop := func(in Inst) bool { return parseEq(in.Text, "pop", "rbp") }
	for j := 2; j < len(body); j++ {
		if isRet(body[j]) && !(isMvr(body[j-2]) && isPop(body[j-1])) {
			return nil
		}
	}
	// Build the template: skip the prologue and the callee-save restores (the
	// `mov rX, [rbp-8..32]` bookkeeping -- the caller saved those registers
	// itself), and replace each epilogue triple with the placeholder.
	tmpl := make([]Inst, 0, len(body))
	for j := i + 1; j < len(body); {
		if isCalleeSaveRestore(body[j].Text) {
			j++
			continue
		}
		if j+2 < len(body) && isMvr(body[j]) && isPop(body[j+1]) && isRet(body[j+2]) {
			tmpl = append(tmpl, Inst{Kind: instInstr, Text: inlineRetPlaceholder})
			j += 3
			continue
		}
		tmpl = append(tmpl, body[j])
		j++
	}
	// Safety screen over the template.
	cand := &inlineCand{body: tmpl, labels: map[string]bool{}}
	for _, in := range tmpl {
		switch in.Kind {
		case instRaw:
			return nil
		case instLabel:
			cand.labels[strings.TrimSuffix(in.Text, ":")] = true
		case instInstr:
			if in.Text == "\t"+inlineRetPlaceholder || in.Text == inlineRetPlaceholder {
				continue
			}
			pl, ok := parseBodyLine(in.Text)
			if !ok {
				return nil // unparseable: play safe
			}
			switch {
			case pl.op == "call":
				return nil // leaf functions only
			case pl.op == "push" || pl.op == "pop":
				return nil // stack effects we do not track
			}
			for _, o := range pl.operands {
				// [rsp...] and any positive [rbp+...] offset belong to stack
				// arguments / shadow space / variadic saves -- not modelled.
				// ([rip+G_x] global references are fine and stay untouched.)
				if strings.Contains(o, "[rsp") || strings.Contains(o, "[rbp+") {
					return nil
				}
			}
			if len(pl.operands) > 0 && calleeSaveWrites[pl.operands[0]] {
				return nil // would corrupt the caller's register-homed locals
			}
		}
	}
	// Frame need: the template's deepest remapped slot, 16-aligned so the
	// caller's sub rsp stays a multiple of 16.
	for _, in := range tmpl {
		if in.Kind != instInstr {
			continue
		}
		for _, sm := range rbpSlotRe.FindAllStringSubmatch(in.Text, -1) {
			if n, err := strconv.Atoi(sm[1]); err == nil && n > cand.maxSlot {
				cand.maxSlot = n
			}
		}
	}
	cand.need = (cand.maxSlot + 15) / 16 * 16
	return cand
}

// expandInline instantiates the template for one call site: slots shift by
// base-8, internal labels gain the site suffix, the placeholder becomes a
// jump to cont, and cont's label lands right after the body.
func expandInline(tmpl *inlineCand, base int, suffix, cont string) []Inst {
	out := make([]Inst, 0, len(tmpl.body)+1)
	for _, in := range tmpl.body {
		switch in.Kind {
		case instLabel:
			out = append(out, Inst{Kind: instLabel,
				Text: strings.TrimSuffix(in.Text, ":") + suffix + ":"})
		case instInstr:
			if in.Text == "\t"+inlineRetPlaceholder || in.Text == inlineRetPlaceholder {
				out = append(out, Inst{Kind: instInstr, Text: "\tjmp " + cont})
				continue
			}
			if pl, ok := parseBodyLine(in.Text); ok &&
				strings.HasPrefix(pl.op, "j") && len(pl.operands) == 1 &&
				tmpl.labels[pl.operands[0]] {
				out = append(out, Inst{Kind: instInstr,
					Text: "\t" + pl.op + " " + pl.operands[0] + suffix})
				continue
			}
			txt := rbpSlotRe.ReplaceAllStringFunc(in.Text, func(m string) string {
				n, _ := strconv.Atoi(rbpSlotRe.FindStringSubmatch(m)[1])
				return fmt.Sprintf("[rbp-%d]", n-8+base)
			})
			out = append(out, Inst{Kind: instInstr, Text: txt, IntWrap: in.IntWrap})
		default:
			out = append(out, in)
		}
	}
	out = append(out, Inst{Kind: instLabel, Text: cont + ":"})
	return out
}

// maxRBPSlot returns the deepest [rbp-N] offset used by the instructions.
func maxRBPSlot(insts []Inst) int {
	m := 0
	for _, in := range insts {
		if in.Kind != instInstr {
			continue
		}
		for _, sm := range rbpSlotRe.FindAllStringSubmatch(in.Text, -1) {
			if n, err := strconv.Atoi(sm[1]); err == nil && n > m {
				m = n
			}
		}
	}
	return m
}

// inlineCalls expands calls to small leaf functions across the whole-program
// instruction stream at -O1 and above. A block qualifies as a call site's
// target when its template was extracted (leaf, regular prologue, no shape
// we do not model); a block qualifies as a caller when it has the canonical
// prologue (so a real rbp frame exists for the remapped slots).
//
// Per caller: call sites are collected in order, each gets a disjoint slot
// area below every slot the caller itself uses (base_k = callerMax + 8 +
// sum of previous needs), the prologue's `sub rsp, N` grows by the total,
// and the sites are then expanded back-to-front so indices stay valid. The
// Windows shadow-space sandwich (`sub rsp, 32` ... `call` ... `add rsp, 32`)
// is dropped along with the call: an inlined body makes no calls, so it
// needs no shadow space. SysV call sites have no such sandwich and are
// unaffected.
func inlineCalls(insts []Inst) []Inst {
	type block struct {
		start, end int // [start] = top-level label line; end exclusive
		regular    bool
	}
	var blocks []block
	for i, in := range insts {
		if in.Kind != instLabel {
			continue
		}
		name := strings.TrimSuffix(in.Text, ":")
		if name == in.Text || internalLabelRe.MatchString(name) {
			continue // internal label, stays inside its function block
		}
		if len(blocks) > 0 {
			blocks[len(blocks)-1].end = i
		}
		blocks = append(blocks, block{start: i})
	}
	if len(blocks) > 0 {
		blocks[len(blocks)-1].end = len(insts)
	}
	for bi := range blocks {
		b := &blocks[bi]
		// Canonical prologue: push rbp / mov rbp, rsp / [push <callee-save>]*
		// / sub rsp, N. Blocks without it (the _start entry stub) are neither
		// candidates nor callers -- expanding into them would reference an
		// uninitialised rbp. The callee-save pushes are skipped; only the
		// frame alloc is required.
		b.regular = b.start+2 < b.end &&
			parseEq(insts[b.start+1].Text, "push", "rbp") &&
			parseEq(insts[b.start+2].Text, "mov", "rbp", "rsp") &&
			func() bool {
				for j := b.start + 3; j < b.end; j++ {
					fr, ok := parseBodyLine(insts[j].Text)
					if !ok {
						return false
					}
					if fr.op == "sub" && len(fr.operands) == 2 && fr.operands[0] == "rsp" {
						return true
					}
					if fr.op == "push" && len(fr.operands) == 1 && calleeSaveRegs[fr.operands[0]] {
						continue
					}
					return false
				}
				return false
			}()
	}
	// Templates first (from the untouched stream), then expansion.
	cands := map[string]*inlineCand{}
	for _, b := range blocks {
		if !b.regular {
			continue
		}
		if cd := extractInlineCand(insts, b.start, b.end); cd != nil {
			cands[strings.TrimSuffix(insts[b.start].Text, ":")] = cd
		}
	}
	if len(cands) == 0 {
		return insts
	}
	siteSeq := 0
	var out []Inst
	for _, b := range blocks {
		seg := insts[b.start:b.end]
		if !b.regular {
			out = append(out, seg...)
			continue
		}
		// frontend.Call sites in order, with disjoint slot bases.
		type site struct {
			idx  int // index within seg of the call line
			base int
			cd   *inlineCand
		}
		callerMax := maxRBPSlot(seg)
		base := callerMax + 8
		var sites []site
		for i, in := range seg {
			if in.Kind != instInstr {
				continue
			}
			pl, ok := parseBodyLine(in.Text)
			if !ok || pl.op != "call" || len(pl.operands) != 1 {
				continue
			}
			if cd := cands[pl.operands[0]]; cd != nil {
				sites = append(sites, site{idx: i, base: base, cd: cd})
				base += cd.need
			}
		}
		if len(sites) == 0 {
			out = append(out, seg...)
			continue
		}
		// Grow the prologue frame by the total inlined need.
		total := base - (callerMax + 8)
		if fr, ok := parseBodyLine(seg[3].Text); ok && fr.op == "sub" && len(fr.operands) == 2 {
			if n, err := strconv.Atoi(fr.operands[1]); err == nil {
				seg[3] = Inst{Kind: instInstr, Text: fmt.Sprintf("\tsub rsp, %d", n+total)}
			}
		}
		// Expand back-to-front; earlier site indices stay valid.
		for si := len(sites) - 1; si >= 0; si-- {
			s := sites[si]
			cont := fmt.Sprintf(".L__inl%d", siteSeq)
			suffix := fmt.Sprintf("_inl%d", siteSeq)
			siteSeq++
			loDel, hiDel := s.idx, s.idx+1
			if s.idx-1 > 0 && parseEq(seg[s.idx-1].Text, "sub", "rsp", "32") &&
				s.idx+1 < len(seg) && parseEq(seg[s.idx+1].Text, "add", "rsp", "32") {
				loDel, hiDel = s.idx-1, s.idx+2 // drop the shadow-space sandwich
			}
			rep := expandInline(s.cd, s.base, suffix, cont)
			newSeg := make([]Inst, 0, len(seg)-(hiDel-loDel)+len(rep))
			newSeg = append(newSeg, seg[:loDel]...)
			newSeg = append(newSeg, rep...)
			newSeg = append(newSeg, seg[hiDel:]...)
			seg = newSeg
		}
		out = append(out, seg...)
	}
	return out
}

// deadStores drops a store to a slot that a later full-width store to the
// same slot covers before anything reads the value back. This is the DCE
// half that pairs with constProp's load elimination: inlining an argument
// spills the incoming register into the callee frame, constProp forwards the
// value and deletes the load -- and without this pass the now-unread spill
// stayed behind (its bytes are pure bloat, one per inlined call site).
//
// The scan is linear with a conservative window, mirroring constProp's
// rules: a pending store survives until the slot is read exactly (lea
// counts -- taking the address lets the callee write it), is covered by a
// full-width re-store (unsized `mov [slot], reg`), or the scan hits a
// boundary -- labels, calls, jumps, inline asm, or any other memory operand
// (an indirect or sized store may alias the slot; a byte store only rewrites
// part of it, so neither counts as a covering store).
func deadStores(insts []Inst) []Inst {
	dead := map[int]bool{}
	pending := map[string]int{} // slot operand -> index of the pending store
	clearAll := func() { pending = map[string]int{} }
	for i, in := range insts {
		if in.Kind != instInstr {
			clearAll() // label or inline asm: other paths may read anything
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			clearAll()
			continue
		}
		if pl.op == "call" || strings.HasPrefix(pl.op, "j") {
			clearAll()
			continue
		}
		covered := pl.op == "mov" && len(pl.operands) == 2 &&
			isMemOperand(pl.operands[0]) && isSlotOperand(pl.operands[0])
		if covered {
			slot := pl.operands[0]
			if j, isPend := pending[slot]; isPend {
				dead[j] = true // nobody read it: the earlier store is covered
			}
			pending[slot] = i
		}
		for _, o := range pl.operands {
			if !strings.Contains(o, "[") {
				continue
			}
			if covered && o == pl.operands[0] {
				continue // the store's own destination is a write, not a read
			}
			if _, isPend := pending[o]; isPend {
				delete(pending, o) // the value is read: its store must stay
				continue
			}
			clearAll() // indirect/sized/foreign memory: may alias any slot
			break
		}
	}
	out := make([]Inst, 0, len(insts))
	for i, in := range insts {
		if !dead[i] {
			out = append(out, in)
		}
	}
	return out
}

// constProp forward-propagates known values through registers and variable
// slots: `mov rax, 7; mov [x], rax` records that [x] holds 7, and a later
// `mov rcx, [x]` -- an argument load, a re-read, anything -- is rewritten to
// `mov rcx, 7`. Two-operand integer ALU ops over known values are evaluated
// (the instruction still emits -- only the tracker learns the result), so a
// fully-constant expression collapses into `mov rax, <imm>` chains. It runs
// at -O1, after inlining (inlined argument spills then feed it constants)
// and before peepholeIR (whose xor rewrite then eats the zero constants this
// pass materialises).
//
// The analysis is deliberately narrow, because guessing x86 semantics is how
// this project ended up retiring a hand-written interpreter: knowledge
// survives ONLY across the few instruction shapes the compiler itself emits
// around variable access, and is dropped wholesale at everything else.
//
//   - `mov r64, imm` defines the destination's value; register-to-register
//     copies propagate it; a 32-bit destination or an indirect source drops
//     the wide value (partial write / unknown source).
//   - `mov [slot], r64` with a known source records the slot; from an
//     unknown or 32-bit source it invalidates that slot.
//   - `mov r64, [slot]` forwards the constant when one is known; the load
//     disappears entirely if the destination register already holds it.
//   - `lea` writes an address (a value we do not track) and touches no
//     memory.
//   - calls and jumps invalidate everything: calls may write any slot
//     through a pointer, and control transfers make the following code
//     reachable from paths the linear scan has not seen.
//   - one-operand instructions and anything carrying a memory operand
//     invalidate everything (idiv defines rdx:rax implicitly, neg writes its
//     operand, `add [x],1` writes a slot, xchg touches memory unmodelled).
//   - cmp/test write flags only and invalidate nothing.
//   - a pure two-operand register/immediate ALU instruction keeps slot
//     knowledge (it writes no memory); its destination value is folded when
//     both operands are known, else dropped.
//   - every label or inline-asm line invalidates everything. Labels because
//     knowledge must hold on all incoming paths.
//
// Loads and stores of 8 bytes are the only widths involved: knowledge is
// recorded exclusively from a 64-bit register store and forwarded only into
// 64-bit register loads, so a `mov dword [x], eax` between them invalidates
// rather than misleads. Flags are never read by any rewritten shape, so the
// rewrites cannot move a condition-code boundary.
// reg64Name maps any integer register spelling to its 64-bit name; unknown
// spellings pass through unchanged. A 32-bit destination partially redefines
// the 64-bit register, so knowledge of the wide value must drop.
func reg64Name(r string) string {
	if gp64Regs[r] {
		return r
	}
	switch r {
	case "eax":
		return "rax"
	case "ebx":
		return "rbx"
	case "ecx":
		return "rcx"
	case "edx":
		return "rdx"
	case "esi":
		return "rsi"
	case "edi":
		return "rdi"
	}
	if len(r) == 4 && r[0] == 'r' && r[3] == 'd' { // r8d..r15d
		return r[:3]
	}
	return r
}

// reg32Name returns the 32-bit spelling of a 64-bit GP register
// (rax -> eax, r10 -> r10d), or r unchanged when it is not one. It is the
// inverse of reg64Name and is what the shl/sar -> movsxd collapse needs.
func reg32Name(r string) string {
	switch r {
	case "rax":
		return "eax"
	case "rbx":
		return "ebx"
	case "rcx":
		return "ecx"
	case "rdx":
		return "edx"
	case "rsi":
		return "esi"
	case "rdi":
		return "edi"
	case "rbp":
		return "ebp"
	case "rsp":
		return "esp"
	}
	if gp64Regs[r] && len(r) >= 2 && r[0] == 'r' {
		if n, err := strconv.Atoi(r[1:]); err == nil && n >= 8 && n <= 15 {
			return r + "d"
		}
	}
	return r
}

// foldALU evaluates a two-operand integer instruction over known values with
// 64-bit wrap-around semantics, matching x86. Shift counts outside [0,63]
// refuse to fold: x86 masks the count (shl rax,64 == shl rax,0) while Go's
// shift would saturate. Division never folds (single operand, plus a
// divide-by-zero fault the constant folder must not swallow).
//
// Folding only *records* the result in the value tracker; the instruction
// itself still emits, so its flag effects stay exactly where they were.
// T1.6 (C5): width is the destination's width in bytes -- 8 for the 64-bit
// forms and 4 for the 32-bit forms that now carry `int`. A 32-bit op wraps at
// 2^32 and the result is ZERO-EXTENDED into the parent register (writing a
// 32-bit register clears the upper half), which is exactly the int carry
// model's invariant, so the folded value is the parent's new known value.
// Shift counts are masked to 5 bits for a 32-bit op (x86: `shl eax,32` is
// `shl eax,0`), and `sar` must go through int32 so a negative low half shifts
// in sign bits instead of zeroes.
func foldALU(op string, a, b int64, width int) (int64, bool) {
	w4 := width == 4
	switch op {
	case "add":
		return truncWidth(a+b, w4), true
	case "sub":
		return truncWidth(a-b, w4), true
	case "and":
		return truncWidth(a&b, w4), true
	case "or":
		return truncWidth(a|b, w4), true
	case "xor":
		return truncWidth(a^b, w4), true
	case "imul":
		// Only the low half of the product is defined; the low 32/64 bits of
		// a product depend only on the low 32/64 bits of the operands.
		return truncWidth(a*b, w4), true
	case "shl":
		if b < 0 || b > 63 || (w4 && b > 31) {
			return 0, false
		}
		return truncWidth(a<<uint(b), w4), true
	case "sar":
		if b < 0 || b > 63 || (w4 && b > 31) {
			return 0, false
		}
		if w4 {
			return int64(uint32(int32(a) >> uint(b))), true
		}
		return a >> uint(b), true
	case "shr":
		if b < 0 || b > 63 || (w4 && b > 31) {
			return 0, false
		}
		return truncWidth(int64(uint64(a)>>uint(b)), w4), true
	}
	return 0, false
}

// truncWidth reduces v to the destination width and zero-extends it back, so
// the stored value is what the parent 64-bit register actually holds after a
// 32-bit op.
func truncWidth(v int64, w4 bool) int64 {
	if w4 {
		return int64(uint32(v))
	}
	return v
}

// isReg32 reports whether o is a 32-bit GP register spelling (eax, r10d).
// Only 32-bit sub-registers are modelled: they are the ones T1.6 made the
// `int` carrier. 16/8-bit writes stay unmodelled (conservatively killed).
func isReg32(o string) bool {
	switch o {
	case "eax", "ebx", "ecx", "edx", "esi", "edi", "ebp", "esp",
		"r8d", "r9d", "r10d", "r11d", "r12d", "r13d", "r14d", "r15d":
		return true
	}
	return false
}

func constProp(insts []Inst) []Inst {
	type state struct {
		regs map[string]int64 // 64-bit register spelling -> known value
		imms map[string]int64 // slot operand -> known value
	}
	s := state{regs: map[string]int64{}, imms: map[string]int64{}}
	reset := func() { s.regs = map[string]int64{}; s.imms = map[string]int64{} }
	// isBoundary reports whether an instruction is a control transfer or an
	// implicit-register clobber that must end a dead-constant scan and wipe all
	// tracked knowledge: calls and jumps are obvious; on the ELF target goc
	// emits raw `syscall`/`sysenter` whose number lives in rax (set by a
	// preceding `mov rax, N`) and which clobbers every caller-save register.
	isBoundary := func(op string) bool {
		return op == "call" || strings.HasPrefix(op, "j") ||
			op == "syscall" || op == "sysenter" || op == "sysret" ||
			op == "cpuid" || op == "int"
	}
	// killReg drops knowledge of the 64-bit register a destination overlaps.
	killReg := func(dst string) { delete(s.regs, reg64Name(dst)) }
	knownOperand := func(o string) (int64, bool) {
		if v, err := strconv.ParseInt(o, 10, 64); err == nil {
			return v, true
		}
		if gp64Regs[o] {
			v, ok := s.regs[o]
			return v, ok
		}
		// T1.6 (C5): a 32-bit sub-register reads the low half of its parent's
		// tracked value. The parent holds the zero-extended result of the last
		// 32-bit write, so masking to 32 bits gives the operand's real value.
		if isReg32(o) {
			if fr, ok := fullRegOf(o); ok {
				if v, ok := s.regs[fr]; ok {
					return int64(uint32(v)), true
				}
			}
		}
		return 0, false
	}
	// immFits reports whether v can be encoded as an x86 imm32 (sign-extended),
	// which the two-operand ALU/cmp/test immediate forms require. `mov` takes a
	// full 64-bit immediate, so it never needs this check.
	immFits := func(v int64) bool { return v >= -(1<<31) && v < (1<<31) }

	// useKind classifies how an instruction mentions reg as an operand.
	type useKind int
	const (
		useNone   useKind = iota
		useInline         // a safe-to-inline source reference (constant can replace reg)
		useOther          // a reference we must keep (memory base, dst, push, call, ...)
	)
	classifyUse := func(pl parsedLine, reg string) useKind {
		// Sub-register references (eax/ax/al/ah for rax, r8d/r8w/r8b for r8,
		// ...) read only part of the tracked 64-bit value, so we can neither
		// replace them with the full constant nor conclude that feeding the
		// full register is dead. Treat every strict sub-register mention as a
		// kept use: the definition survives. (o == reg is NOT a sub-register,
		// so the full-register inlinable cases below still work.)
		for _, o := range pl.operands {
			if subRegOf(reg, o) {
				return useOther
			}
		}
		switch pl.op {
		case "mov":
			if len(pl.operands) == 2 {
				// A store `mov [mem], reg` reads reg but writing an immediate
				// into memory is the one inlining we do NOT perform (the
				// constant width, base-register edge cases, and the fact that
				// we would lose slot knowledge make it unsafe). Treat it as a
				// kept use so the feeding constant definition is preserved.
				if isMemOperandSized(pl.operands[0]) {
					return useOther
				}
				if pl.operands[1] == reg && pl.operands[0] != reg {
					return useInline // src is a pure source use
				}
				if pl.operands[0] == reg && pl.operands[1] == reg {
					return useOther // self-copy reads reg
				}
			}
			// operands[0] == reg is a pure overwrite (no read); otherwise reg
			// is absent. Neither is an inlinable use.
			return useNone
		case "cmp", "test":
			// x86 cmp/test take an immediate only on the RIGHT operand; the
			// left operand is the destination-style operand and must stay a
			// register (or memory). Only a right-operand register reference is
			// safely inlinable.
			if len(pl.operands) == 2 && pl.operands[0] != pl.operands[1] {
				if pl.operands[1] == reg {
					return useInline
				}
				if pl.operands[0] == reg {
					return useOther
				}
			}
			return useNone
		case "add", "sub", "and", "or", "xor":
			if len(pl.operands) == 2 && pl.operands[0] != pl.operands[1] {
				if pl.operands[1] == reg {
					return useInline // src is a pure source use
				}
				if pl.operands[0] == reg {
					return useOther // dst is read then written
				}
			}
			return useNone
		}
		// Any other instruction: if reg is mentioned (memory base, implicit
		// operand, ...) it is a use we cannot inline.
		for _, o := range pl.operands {
			if o == reg {
				return useOther
			}
		}
		return useNone
	}
	writesReg := func(pl parsedLine, reg string) bool {
		if len(pl.operands) > 0 && pl.operands[0] == reg {
			return true
		}
		switch pl.op {
		case "mul", "imul", "div", "idiv", "cqo", "cdq":
			if reg == "rax" || reg == "rdx" {
				return true
			}
		}
		return false
	}
	// isDeadConstDef reports whether the `mov reg, imm` at index i is dead:
	// every reference to reg before it is next redefined (or the stream resets)
	// is an inlinable one, so the constant can be substituted at each use and
	// the definition dropped outright (T1.2: constant folding that truly
	// deletes instructions, not just records values).
	isDeadConstDef := func(i int, reg string) bool {
		for j := i + 1; j < len(insts); j++ {
			in := insts[j]
			if in.Kind != instInstr {
				return false // label / inline asm: value may be live past it
			}
			pl, ok := parseBodyLine(in.Text)
			if !ok {
				return false
			}
			if isBoundary(pl.op) {
				return false
			}
			redef := writesReg(pl, reg)
			if !redef {
				switch classifyUse(pl, reg) {
				case useOther:
					return false
				case useInline:
					// A non-mov inlinable consumer needs an imm32; if the
					// constant is too wide the substitution cannot happen, so
					// the definition must survive.
					if pl.op != "mov" {
						if v, ok := s.regs[reg]; !ok || !immFits(v) {
							return false
						}
					}
				case useNone:
				}
			} else {
				// reg is overwritten. A pure overwrite (`mov reg, X`) ends the
				// scan and makes the constant definition dead. But a
				// read-and-write instruction consumes the old value first
				// (`add reg, Y` reads reg before writing it; the mul/div family
				// reads rax/rdx implicitly), so the definition is still live.
				if classifyUse(pl, reg) == useOther {
					return false
				}
				switch pl.op {
				case "mul", "imul", "div", "idiv", "cqo", "cdq":
					if reg == "rax" || reg == "rdx" {
						return false
					}
				}
				return true
			}
		}
		return true
	}
	// inlineableSrc returns (operand index, reg, value) when pl consumes a
	// known-constant register in a position that accepts an immediate.
	inlineableSrc := func(pl parsedLine) (int, string, int64, bool) {
		switch pl.op {
		case "mov", "add", "sub", "and", "or", "xor":
			// Only inline into a register-destination instruction. A store
			// (`mov [mem], reg` / `add [mem], reg`) writes memory with the
			// constant, which we deliberately leave to its source register so
			// the width and aliasing stay correct.
			if len(pl.operands) == 2 && pl.operands[1] != pl.operands[0] &&
				!isMemOperandSized(pl.operands[0]) && gp64Regs[pl.operands[1]] {
				if v, ok := s.regs[pl.operands[1]]; ok {
					return 1, pl.operands[1], v, true
				}
			}
			// T1.6 (C5): a 32-bit source register reads the low half of its
			// parent's tracked value, so it can be inlined the same way. The
			// guard is immFits, not "mov takes any immediate": the low half is
			// zero-extended and can be up to 2^32-1, which no 32-bit immediate
			// form can carry. Inlining leaves the instruction a 32-bit op, so
			// the destination's zero-extension semantics are unchanged.
			if len(pl.operands) == 2 && pl.operands[1] != pl.operands[0] &&
				!isMemOperandSized(pl.operands[0]) && isReg32(pl.operands[1]) {
				if fr, ok := fullRegOf(pl.operands[1]); ok {
					if v, ok := s.regs[fr]; ok {
						v32 := int64(uint32(v))
						if immFits(v32) {
							return 1, pl.operands[1], v32, true
						}
					}
				}
			}
		case "cmp", "test":
			// Only the right operand of cmp/test accepts an immediate, so only
			// inline a known constant there (the left operand must stay a
			// register/memory).
			if len(pl.operands) == 2 && pl.operands[0] != pl.operands[1] {
				if gp64Regs[pl.operands[1]] {
					if v, ok := s.regs[pl.operands[1]]; ok {
						return 1, pl.operands[1], v, true
					}
				}
				// T1.6 (C5): same for a 32-bit right operand; the compare is
				// already a 32-bit op, so the immFits guard is the only extra
				// condition.
				if isReg32(pl.operands[1]) {
					if fr, ok := fullRegOf(pl.operands[1]); ok {
						if v, ok := s.regs[fr]; ok {
							v32 := int64(uint32(v))
							if immFits(v32) {
								return 1, pl.operands[1], v32, true
							}
						}
					}
				}
			}
		}
		return 0, "", 0, false
	}
	buildInlined := func(pl parsedLine, idx int, v int64) Inst {
		ops := append([]string(nil), pl.operands...)
		ops[idx] = strconv.FormatInt(v, 10)
		return Inst{Kind: instInstr, Text: "\t" + pl.op + " " + strings.Join(ops, ", ")}
	}

	out := make([]Inst, 0, len(insts))
	for i, in := range insts {
		if in.Kind != instInstr {
			out = append(out, in)
			reset()
			continue
		}
		pl, ok := parseBodyLine(in.Text)
		if !ok {
			out = append(out, in)
			reset()
			continue
		}
		// T1.2: a constant definition `mov reg, imm` whose every later use is
		// inlinable is dead -- record the constant so the uses get it directly,
		// then drop the definition entirely.
		if pl.op == "mov" && len(pl.operands) == 2 && gp64Regs[pl.operands[0]] && !isMemOperand(pl.operands[1]) {
			if iv, err := strconv.ParseInt(pl.operands[1], 10, 64); err == nil {
				if isDeadConstDef(i, pl.operands[0]) {
					s.regs[pl.operands[0]] = iv
					continue
				}
			}
		}
		// A write to a STRICT sub-register (`xor eax, eax`, `add eax, 1`,
		// `mov eax, 5`) changes part of the 64-bit register, so the tracked
		// full-register value is no longer valid. goc's Intel-syntax IR puts
		// the destination first, so checking operands[0] covers the writing
		// form; a store like `mov [mem], eax` has a memory operand[0] and only
		// READS eax, and is correctly left alone.
		//
		// T1.6 (C5): a 32-bit destination is exempt -- the handlers below model
		// it exactly (a 32-bit write clears the parent's upper half, so the
		// parent gets a known zero-extended value). Deleting it here would
		// make every 32-bit op a dead end again.
		if len(pl.operands) > 0 && !gp64Regs[pl.operands[0]] && !isReg32(pl.operands[0]) {
			if fr, isReg := fullRegOf(pl.operands[0]); isReg {
				delete(s.regs, fr)
			}
		}
		// T1.2: inline a known-constant source operand into a safe consumer,
		// turning e.g. `mov rbx, rax` (rax=5) into `mov rbx, 5` and
		// `add rbx, rax` into `add rbx, 5` directly at the use site.
		if idx, _, v, iok := inlineableSrc(pl); iok {
			if pl.op == "mov" || immFits(v) {
				in = buildInlined(pl, idx, v)
				pl, _ = parseBodyLine(in.Text)
			}
		}
		switch {
		case pl.op == "mov" && len(pl.operands) == 2 && gpRegs[pl.operands[0]]:
			dst, src := pl.operands[0], pl.operands[1]
			switch {
			case gp64Regs[dst] && !isMemOperand(src):
				if v, err := strconv.ParseInt(src, 10, 64); err == nil {
					s.regs[dst] = v
				} else if gp64Regs[src] {
					// register-to-register copy: the value follows the source
					if sv, ok := s.regs[src]; ok {
						s.regs[dst] = sv
					} else {
						delete(s.regs, dst)
					}
				} else {
					delete(s.regs, dst)
				}
			case gp64Regs[dst] && isSlotOperand(src):
				// a 64-bit register load; forward a known slot value
				if v, known := s.imms[src]; known {
					if dv, held := s.regs[dst]; held && dv == v {
						continue // the register already holds it: drop the load
					}
					in = Inst{Kind: instInstr, Text: "\tmov " + dst + ", " + strconv.FormatInt(v, 10)}
					s.regs[dst] = v
				} else {
					delete(s.regs, dst)
				}
			default:
				// T1.6 (C5): a 32-bit destination is no longer a dead end.
				// Writing a 32-bit register clears its parent's upper half, so
				// a known source yields a KNOWN parent value: the zero-extended
				// low 32 bits. This is the int carry model's invariant, and it
				// is what lets constant propagation survive the 32-bit opcode
				// forms that now carry every `int`.
				if fr, ok := fullRegOf(dst); ok && isReg32(dst) {
					switch {
					case !isMemOperand(src):
						if v, err := strconv.ParseInt(src, 10, 64); err == nil {
							s.regs[fr] = int64(uint32(v))
						} else if sv, ok := knownOperand(src); ok {
							s.regs[fr] = int64(uint32(sv))
						} else {
							delete(s.regs, fr)
						}
					case isSlotOperand(src):
						if v, known := s.imms[src]; known {
							s.regs[fr] = int64(uint32(v))
						} else {
							delete(s.regs, fr)
						}
					default:
						delete(s.regs, fr)
					}
				} else {
					// A 16/8-bit write or an indirect address: a partial write
					// or an unknown source -- the wide value cannot be trusted.
					killReg(dst)
				}
			}
			out = append(out, in)
		case pl.op == "mov" && len(pl.operands) == 2 && isMemOperand(pl.operands[0]):
			dst, src := pl.operands[0], pl.operands[1]
			if isSlotOperand(dst) {
				// A store of a known value (a tracked register, or a constant
				// once T1.2 has inlined one into the source) makes the slot
				// known. Anything else invalidates it.
				if v, err := strconv.ParseInt(src, 10, 64); err == nil {
					s.imms[dst] = v
				} else if gp64Regs[src] {
					if v, ok := s.regs[src]; ok {
						s.imms[dst] = v
					} else {
						delete(s.imms, dst)
					}
				} else {
					delete(s.imms, dst)
				}
			} else {
				// indirect or sized store: may alias any slot. Registers are
				// untouched (the store does not write one).
				s.imms = map[string]int64{}
			}
			out = append(out, in)
		case pl.op == "movsxd" && len(pl.operands) == 2:
			// T1.6 (C5): movsxd sign-extends the low 32 bits -- the inverse of
			// the zero-extension that a 32-bit write performs on the parent. A
			// known source therefore yields a known FULL 64-bit destination,
			// which is what keeps a materialized int propagating across the
			// narrow sites (N5b/N11/N12/N17/N22) instead of dying there.
			if gp64Regs[pl.operands[0]] {
				if v, ok := knownOperand(pl.operands[1]); ok {
					s.regs[pl.operands[0]] = int64(int32(v))
				} else {
					delete(s.regs, pl.operands[0])
				}
			} else {
				reset()
			}
			out = append(out, in)
		case pl.op == "lea" && len(pl.operands) == 2:
			killReg(pl.operands[0]) // addresses are values we do not track
			out = append(out, in)
		case isBoundary(pl.op):
			// calls may store through pointers to any slot; a jump makes the
			// following instructions reachable from elsewhere, and the label
			// rule below is what keeps merge points safe -- be consistent
			// and treat every control transfer as a boundary.
			reset()
			out = append(out, in)
		default:
			// ALU ops, cmp, SSE, sized stores, anything else parsed.
			switch {
			case (pl.op == "cmp" || pl.op == "test") && len(pl.operands) == 2:
				// writes flags only: reads nothing we track, writes nothing
				// we track -- keep every bit of knowledge
			case len(pl.operands) == 1 || containsMemoryOperand(pl.operands):
				// One-operand instructions write their operand and often
				// implicit registers (idiv defines rdx:rax, neg the operand
				// itself, push/pop move the stack); any memory operand may
				// be written (add [x],1) or touched unmodelled (xchg). Drop
				// everything.
				reset()
			case gp64Regs[pl.operands[0]]:
				// A pure register/immediate two-operand instruction writes no
				// memory, so slot knowledge survives. Fold when both operands
				// are known; otherwise the destination value dies.
				dst := pl.operands[0]
				folded := false
				if av, aok := s.regs[dst]; aok && len(pl.operands) == 2 {
					if bv, bok := knownOperand(pl.operands[1]); bok {
						if r, ok := foldALU(pl.op, av, bv, 8); ok {
							s.regs[dst] = r
							folded = true
						}
					}
				}
				if !folded {
					delete(s.regs, dst)
				}
			case isReg32(pl.operands[0]):
				// T1.6 (C5): a 32-bit destination is the normal form for `int`
				// now, so killing it here made every int computation a dead end
				// for constant propagation. Fold at 32-bit width instead and
				// record the zero-extended result in the PARENT register --
				// writing a 32-bit register clears the parent's upper half, so
				// the parent's value is known too, not just its low half.
				dst := pl.operands[0]
				folded := false
				if av, aok := knownOperand(dst); aok && len(pl.operands) == 2 {
					if bv, bok := knownOperand(pl.operands[1]); bok {
						if r, ok := foldALU(pl.op, av, bv, 4); ok {
							if fr, isReg := fullRegOf(dst); isReg {
								s.regs[fr] = r
								folded = true
							}
						}
					}
				}
				if !folded {
					killReg(dst)
				}
			case gpRegs[pl.operands[0]]:
				// A 16/8-bit destination: a partial write whose extension
				// semantics are not modelled -- drop the wide value.
				killReg(pl.operands[0])
			default:
				reset() // exotic shape: play safe
			}
			out = append(out, in)
		}
	}
	return out
}

// containsMemoryOperand reports whether any operand addresses memory in any
// form: "[rbp-8]", "[r10]", "byte [rbp-3]" -- anything with a bracket in it.
func containsMemoryOperand(ops []string) bool {
	for _, o := range ops {
		if strings.Contains(o, "[") {
			return true
		}
	}
	return false
}

// genClibFuncs emits every built-in C library function reachable from c.need
// through the regular code generator. Emitting one can mark more needs (the
// function's own calls: library-internal helpers and the assembly platform
// primitives), so the pass repeats until a round adds nothing.
func (c *CG) genClibFuncs() error {
	lib := common.Store(c.linux)
	if lib == nil {
		return nil
	}
	for {
		var batch []string
		for name := range c.need {
			if c.libEmitted[name] || c.skipFuncs[name] {
				continue
			}
			if _, ok := lib.Funcs[name]; ok {
				batch = append(batch, name)
			}
		}
		if len(batch) == 0 {
			return nil
		}
		sort.Strings(batch) // stable emission order
		for _, name := range batch {
			c.libEmitted[name] = true
			// Remember that this unit carries a copy of the library's symbol.
			// A unit that inlines printf has no way to rename it -- the name is
			// the C ABI -- so the linker is told which definitions came from the
			// library, and treats a second copy as the same library rather than
			// as the user defining the same name twice.
			if c.libSyms == nil {
				c.libSyms = map[string]bool{}
			}
			c.libSyms[name] = true
			if err := c.genFunc(lib.Funcs[name]); err != nil {
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

// foldConstInit, boolVal and foldFloatInit are the link package's constant
// folders. A static initialiser is folded in exactly one place now -- when the
// .data image is written -- so a shift the folder refuses here is refused for
// the emitted bytes too, and the two can never disagree about what "= 1 << 3"
// means.
func foldConstInit(e frontend.Expr) (int64, bool) {
	return link.FoldConstInit(e)
}

func foldFloatInit(e frontend.Expr) (float64, bool) {
	return link.FoldFloatInit(e)
}

// findAddressTaken returns the set of local variable names whose address is
// taken anywhere in f (via &x). Such locals cannot live in a register,
// because there would be nowhere for the pointer to point.
func (c *CG) findAddressTaken(f *frontend.FuncDecl) map[string]bool {
	taken := map[string]bool{}
	var walkExpr func(e frontend.Expr)
	walkExpr = func(e frontend.Expr) {
		if e == nil {
			return
		}
		switch n := e.(type) {
		case *frontend.Unary:
			if n.Op == "&" {
				if id, ok := n.E.(*frontend.Ident); ok {
					taken[id.Name] = true
				}
			}
			walkExpr(n.E)
		case *frontend.Binary:
			walkExpr(n.L)
			walkExpr(n.R)
		case *frontend.Index:
			walkExpr(n.Base)
			walkExpr(n.Idx)
		case *frontend.CastExpr:
			// A cast does not hide the address-of inside it: "(char **)&x"
			// is the common spelling when a function takes an out-parameter,
			// and skipping this branch used to leave x in a register that
			// had no address to take.
			walkExpr(n.E)
		case *frontend.CondExpr:
			// Either arm may carry the "&x", so both have to be visited.
			walkExpr(n.Cond)
			walkExpr(n.Then)
			walkExpr(n.Else)
		case *frontend.CommaExpr:
			walkExpr(n.Left)
			walkExpr(n.Right)
		case *frontend.IncDecExpr:
			walkExpr(n.E)
		case *frontend.MemberExpr:
			walkExpr(n.Base)
		case *frontend.Call:
			// va_start(ap, ...), va_end(ap) and va_copy(dst, src) all take
			// their va_list arguments by address (va_start writes the cursor
			// into it, va_arg needs it, va_copy reads one and writes the
			// other), and va_arg needs it too.
			if n.Name == "va_start" || n.Name == "va_end" || n.Name == "va_copy" {
				for _, a := range n.Args {
					if id, ok := a.(*frontend.Ident); ok {
						taken[id.Name] = true
					}
				}
			}
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *frontend.IndirectCall:
			walkExpr(n.Fn)
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *frontend.AssignExpr:
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
		case *frontend.BraceInit:
			for _, el := range n.Elems {
				walkExpr(el.E)
			}
		case *frontend.VaArgExpr:
			if id, ok := n.Ap.(*frontend.Ident); ok {
				taken[id.Name] = true
			}
			walkExpr(n.Ap)
		}
	}
	var walkStmt func(s frontend.Stmt)
	walkStmt = func(s frontend.Stmt) {
		if s == nil {
			return
		}
		switch n := s.(type) {
		case *frontend.Block:
			for _, st := range n.Stmts {
				walkStmt(st)
			}
		case *frontend.DeclList:
			for _, d := range n.Decls {
				walkStmt(d)
			}
		case *frontend.DeclStmt:
			walkExpr(n.Init)
		case *frontend.AssignStmt:
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
		case *frontend.ExprStmt:
			walkExpr(n.E)
		case *frontend.ReturnStmt:
			walkExpr(n.E)
		case *frontend.IfStmt:
			walkExpr(n.Cond)
			walkStmt(n.Then)
			walkStmt(n.Else)
		case *frontend.WhileStmt:
			walkExpr(n.Cond)
			walkStmt(n.Body)
		case *frontend.ForStmt:
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
		case *frontend.DoWhileStmt:
			walkExpr(n.Cond)
			walkStmt(n.Body)
		case *frontend.SwitchStmt:
			walkExpr(n.Src)
			walkStmt(n.Body)
		case *frontend.LabelStmt:
			walkStmt(n.Stmt)
		}
	}
	walkStmt(f.Body)
	return taken
}

// --- block-scoped variable resolution -------------------------------------
// pushScope opens a fresh (empty) lexical scope for declarations.
func (c *CG) pushScope() {
	c.scopes = append(c.scopes, map[string]int{})
	c.staticScopes = append(c.staticScopes, map[string]string{})
}

// bindStatic records a static local's label in the innermost block. The
// function's outermost scope is pushed before any statement is generated, but
// a declaration reached from a path that has not pushed yet still needs a
// place to live, so an empty stack is tolerated rather than indexed blindly.
func (c *CG) bindStatic(name, lab string) {
	if len(c.staticScopes) == 0 {
		c.staticScopes = append(c.staticScopes, map[string]string{})
	}
	c.staticScopes[len(c.staticScopes)-1][name] = lab
}

// popScope discards the innermost lexical scope.
func (c *CG) popScope() {
	if len(c.scopes) > 0 {
		c.scopes = c.scopes[:len(c.scopes)-1]
	}
	if len(c.staticScopes) > 0 {
		c.staticScopes = c.staticScopes[:len(c.staticScopes)-1]
	}
}

// lookupStatic resolves a static local by name through the block scopes,
// innermost first, mirroring lookupVar. Returns "" when the name is not a
// visible static local. Falling back to the flat c.staticVars would defeat
// block scoping: a name bound in a block that has already been left must not
// resolve, and two blocks may bind the same name to different labels.
func (c *CG) lookupStatic(name string) (string, bool) {
	for i := len(c.staticScopes) - 1; i >= 0; i-- {
		if lab, ok := c.staticScopes[i][name]; ok {
			return lab, true
		}
	}
	return "", false
}

// declareVar registers name -> a fresh uid holding info in the current
// (innermost) scope and returns the uid. Used for parameters (declared into
// the function's outermost scope) and, implicitly, for locals when their
// frontend.DeclStmt is emitted (see genStmt's *frontend.DeclStmt case).
func (c *CG) declareVar(name string, info varInfo) int {
	uid := c.varUID
	c.varUID++
	info.addr = c.addrTaken[name]
	info.ptr = c.ptrCapable[name]
	c.varEnts[uid] = info
	c.scopes[len(c.scopes)-1][name] = uid
	return uid
}

// lookupVar resolves name through the scope stack (innermost first). It covers
// locals and parameters only; static locals and globals are handled by the
// callers' own fallback logic.
func (c *CG) lookupVar(name string) (varInfo, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if uid, ok := c.scopes[i][name]; ok {
			return c.varEnts[uid], true
		}
	}
	return varInfo{}, false
}

func (c *CG) genFunc(f *frontend.FuncDecl) error {
	c.varEnts = map[int]varInfo{}
	c.scopes = nil
	c.varUID = 0
	c.declUID = map[*frontend.DeclStmt]int{}
	// T1.6 (C4): mark the int-typed locals/parameters that may hold a 64-bit
	// pointer before any declareVar consults the flag (loadVar/slot layouts
	// depend on varInfo.ptr).
	c.ptrCapable = c.analyzePtrCapable(f)
	c.staticVars = map[string]string{} // fresh per function: static-local names do not leak across functions
	c.staticLabelOf = map[int]string{} // likewise per function
	c.staticScopes = nil               // must precede pushScope, which seeds the stack
	c.pushScope()                      // function / parameter scope (scope 0)
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
	paramUIDs := make([]int, len(f.Params))
	for i, p := range f.Params {
		// Register the parameter in the function's outermost scope now; its
		// final home (register-spill slot or stack slot) is assigned in the
		// frame-layout phase below.
		paramUIDs[i] = c.declareVar(p, varInfo{off: 16 + 8*i, typ: f.ParamTypes[i]})
	}

	// localDecl is one (name, type, uid) triple for a function-local variable.
	// line is the declaration's source line, kept so the checks that can only
	// run once the frame is laid out (a variable-length array's shape, say) can
	// still report where the offending declaration is.
	type localDecl struct {
		name string
		typ  *frontend.Type
		uid  int
		line int
	}

	// Gather every local declaration (including those inside nested blocks)
	// so we can decide, up front, which ones live in callee-save registers
	// and which stay on the stack.
	var decls []localDecl
	maxSwDepth := 0
	hasAsm := false // an inline-assembly block in this function?
	var gather func(frontend.Stmt, int)
	gather = func(s frontend.Stmt, swDepth int) {
		if swDepth > maxSwDepth {
			maxSwDepth = swDepth
		}
		switch n := s.(type) {
		case *frontend.Block:
			for _, st := range n.Stmts {
				gather(st, swDepth)
			}
		case *frontend.DeclList:
			for _, d := range n.Decls {
				gather(d, swDepth)
			}
		case *frontend.DeclStmt:
			if n.IsTLS {
				// Thread-local variable: one instance per thread, laid out in
				// the .tls section. The offset is assigned up-front so the
				// access code generated later can reach it. Address/value
				// resolution goes through c.tlsVars (checked before staticVars).
				off := c.tlsPlace(n.Typ)
				lab := fmt.Sprintf("%sTL_st%d_%s", c.staticPrefix, c.staticSeq, n.Name)
				c.staticSeq++
				c.tlsVars[n.Name] = &tlsVarInfo{off: off, lab: lab, typ: n.Typ}
				c.tlsList = append(c.tlsList, n)
			} else if n.Storage == "static" {
				// Static local: lives in .data under a unique label, persists
				// across calls, and is initialised once at load time. No frame
				// slot is allocated.
				//
				// This pass runs BEFORE any statement is generated, and it walks
				// the whole function body, so the name cannot be bound here: two
				// sibling blocks that both declare `static int v` would end up
				// with a single binding and the earlier block would read the
				// later one's object. What this pass does is mint a unique LABEL
				// and queue the emitter entry; the name -> label binding happens
				// in genStmt's DeclStmt case, where the scope stack is current
				// and the binding therefore lands in the block that declared it.
				lab := fmt.Sprintf("%sG_st%d_%s", c.staticPrefix, c.staticSeq, n.Name)
				c.staticSeq++
				c.globals[lab] = true
				c.globalLab[lab] = lab
				c.globalTyp[lab] = n.Typ
				// recorded by DeclStmt's line number so genStmt can recover the
				// label this pass minted for the very same declaration
				c.staticLabelOf[n.Line] = lab
				c.staticList = append(c.staticList, staticEmit{lab: lab, d: n})
			} else if n.Storage == "extern" {
				// Extern local: a reference to a file-scope global of the same
				// name. No frame slot; loadVar/genLValue fall through to
				// c.globals to resolve it.
			} else {
				// Each declaration -- even two with the same name in sibling
				// blocks -- gets its own uid so their frame homes never
				// collide. Name->uid scoping is rebuilt at emission time.
				uid := c.varUID
				c.varUID++
				c.declUID[n] = uid
				decls = append(decls, localDecl{name: n.Name, typ: n.Typ, uid: uid, line: n.Line})
			}
		case *frontend.IfStmt:
			gather(n.Then, swDepth)
			if n.Else != nil {
				gather(n.Else, swDepth)
			}
		case *frontend.WhileStmt:
			gather(n.Body, swDepth)
		case *frontend.DoWhileStmt:
			gather(n.Body, swDepth)
		case *frontend.ForStmt:
			if n.Init != nil {
				gather(n.Init, swDepth)
			}
			gather(n.Body, swDepth)
		case *frontend.SwitchStmt:
			// A switch needs one frame slot to hold the controlling value,
			// and it does not introduce a new scope for its labels.
			gather(n.Body, swDepth+1)
		case *frontend.LabelStmt:
			gather(n.Stmt, swDepth)
		case *frontend.AsmStmt:
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
	c.addrTaken = addrTaken
	// regPool is the set of callee-save GPRs available for local-register
	// homes in THIS function. It starts as the full calleeSaveAll pool; an
	// inline-assembly function forces it empty so every local lives on the
	// stack (the asm text only knows [rbp+off] slots). Regardless of how many
	// homes are handed out, the prologue always pushes the WHOLE
	// calleeSaveAll pool (see calleeSaveAll's doc comment) so the callee-save
	// ABI contract is honoured for every caller.
	regPool := calleeSaveAll
	// A function that contains an inline-assembly block must keep every
	// local on the stack: the asm text binds C variable names to their
	// frame slots ([rbp+off]) or, worse, to a callee-save register home
	// that the hand-written asm neither knows about nor preserves. Forcing
	// the whole pool empty (regPool = nil below makes "ri < len(regPool)"
	// always false) gives the asm a stable, addressable memory home for
	// every variable it names. The callee-save registers are still pushed
	// (and popped) so the asm block, if it uses them as scratch, cannot
	// damage a caller's cached locals.
	if hasAsm {
		regPool = nil
	}
	// T2.1 (R3): a call-free function may also park a local in a CALLER-save
	// register. Nothing in the body can clobber r8/r9 (there is no call to
	// pass them to), and the fixed prologue/epilogue push and restore exactly
	// calleeSaveAll, so a home there costs no push, no restore and no frame
	// slot at all. r10/r11 are deliberately NOT offered: genBinary and
	// genCall use them as scratch (45 and 19 emit sites), so a local living
	// there would be shredded by any arithmetic expression. rbx/r12-r14 are
	// the only registers goc never touches as scratch, which is why the pool
	// was capped at four and why spilling was so common.
	bst := t21BodyScan(f)
	if regPool != nil && !bst.hasCall && !bst.hasAgg {
		regPool = append(append([]string{}, calleeSaveAll...), "r8", "r9")
	}
	c.usedRegs = nil
	regOf := map[string]string{}
	ri := 0
	stackDecls := make([]localDecl, 0, len(decls))
	for _, d := range decls {
		// Only integer-class scalars get a register home: float rides in
		// XMM registers like double (and is 4 bytes in memory), so it must
		// stay on the stack.
		intClass := regCapable(d.typ)
		if intClass && !addrTaken[d.name] && ri < len(regPool) {
			regOf[d.name] = regPool[ri]
			c.usedRegs = append(c.usedRegs, regPool[ri])
			// T1.6 (C4): this direct varEnts construction bypasses declareVar,
			// so the ptrCapable flag must be copied here explicitly.
			c.varEnts[d.uid] = varInfo{reg: regPool[ri], typ: d.typ, ptr: c.ptrCapable[d.name]}
			ri++
		} else {
			stackDecls = append(stackDecls, d)
		}
	}
	// T2.1 (R2): parameters are variables too, and the probe attributes a
	// large share of the removable frame traffic to parameter RELOADS
	// (bench2's bsort re-reads its array argument 7 times inside one
	// call-free loop). A parameter in a register needs no frame slot at all:
	// the prologue PUSH already preserves it across every call, so each
	// reload disappears outright rather than being traded for a write-back.
	//
	// They are allocated AFTER the locals, and that ordering is the whole
	// point -- an earlier version gave parameters first place and it was a
	// measured LOSS (+218 instructions on the -Os corpus). The pool is only
	// four callee-save registers, so a parameter taken off the top is a
	// local pushed back onto the stack, and a spilled local is far more
	// expensive than a spilled parameter: fmt_int_part's loop counter went
	// from `dec r13d` to load/dec/store/reload, four extra memory round trips
	// per iteration, which swamped the reloads saved on the parameters. With
	// the locals served first, a parameter only gets a register when the
	// function never needed one for a local, so R2 is now free of downside.
	//
	// Excluded, each for a concrete reason:
	//   - variadic functions: va_arg reads the incoming arguments by slot
	//     index in the save area, so they must stay addressable;
	//   - aggregates: they arrive through a hidden pointer and are copied
	//     into a slot this function owns;
	//   - address-taken parameters: something can write the slot behind the
	//     compiler's back, and a register copy could not be observed;
	//   - floating parameters: they live in XMM argument registers, and the
	//     pool is GPR-only;
	//   - i >= regCap (stack-passed): their incoming home is already a
	//     caller-owned slot, and R2 keeps the scope to register parameters;
	//   - small call-free functions somebody calls: register-homing them
	//     puts a callee-save write in the body and the inliner rightly
	//     refuses such a template (see t21InlineLikely).
	// hasAsm empties regPool above, so an asm function keeps every parameter
	// on the stack automatically.
	paramReg := map[string]bool{}
	if !f.Variadic && !c.t21InlineLikely(f, bst) {
		for i, p := range f.Params {
			if i >= regCap || !regCapable(f.ParamTypes[i]) || addrTaken[p] || ri >= len(regPool) {
				continue
			}
			regOf[p] = regPool[ri]
			paramReg[p] = true
			c.usedRegs = append(c.usedRegs, regPool[ri])
			c.varEnts[paramUIDs[i]] = varInfo{reg: regPool[ri],
				typ: f.ParamTypes[i], ptr: c.ptrCapable[p]}
			ri++
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
	// The callee-save save region lives just below rbp, laid down by the
	// prologue as a chain of PUSHes: `push rbx; push r12; push r13; push r14`
	// places the four registers at [rbp-8], [rbp-16], [rbp-24], [rbp-32] --
	// each an 8-byte slot, so the save region physically spans [rbp-8..rbp-39]
	// (the last slot is [rbp-32..rbp-39]). Locals must therefore start at
	// rbp-40 -- 8 bytes below the save region -- or a sub-8-byte local
	// (char/short/_Bool) would overlap r14's slot and be silently clobbered.
	// regArea therefore carries an extra +8 gap on top of the
	// 8*len(calleeSaveAll) push bytes. (The leading `push rbp` stores the
	// caller's rbp AT rbp, i.e. offset 0, not within this region.) Every local
	// and parameter-down offset is expressed relative to it.
	//
	// It tracks c.saveRegs (the registers this function REALLY pushes), not
	// len(calleeSaveAll): the save region only spans [rbp-8..rbp-8*len(saveRegs)],
	// so a function using one home starts its locals at rbp-16 instead of
	// rbp-40. The +8 gap is kept unconditionally -- it is what stops a
	// sub-8-byte local (char/short/_Bool) from overlapping the last save slot.
	c.saveRegs = calleeSavePool(c.usedRegs, hasAsm)
	regArea := 8*len(c.saveRegs) + 8
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
		typ    *frontend.Type
		srcReg string // incoming hidden-pointer register ("" = on the stack)
		srcOff int    // incoming hidden-pointer rbp offset (stack params)
	}
	var paramCopies []paramCopy
	aggSlotBytes := func(pt *frontend.Type) int {
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
		if paramReg[p] {
			// T2.1 (R2): homed in a callee-save register; it has no slot.
			continue
		}
		if !isAgg(pt) {
			if i < regCap {
				localBytes += 8
				// T1.6 (C4): these layout writes overwrite the declareVar
				// entries, so the ptrCapable flag must be carried over.
				c.varEnts[paramUIDs[i]] = varInfo{off: -(regArea + localBytes), typ: pt, ptr: c.ptrCapable[p]}
			} else if c.linux {
				c.varEnts[paramUIDs[i]] = varInfo{off: 16 + 8*(i-regCap), typ: pt, ptr: c.ptrCapable[p]}
			} else {
				c.varEnts[paramUIDs[i]] = varInfo{off: 16 + 8*(i+regShift), typ: pt, ptr: c.ptrCapable[p]}
			}
			continue
		}
		localBytes += aggSlotBytes(pt)
		c.varEnts[paramUIDs[i]] = varInfo{off: -(regArea + localBytes), typ: pt, ptr: c.ptrCapable[p]}
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
	// prologue and read back by frontend.ReturnStmt.
	if isSret {
		localBytes += 8
		c.sretSlot = -(regArea + localBytes)
	}

	for _, d := range stackDecls {
		// Honour a _Alignas(N) request: round the slot start up to the
		// requested alignment (no-op when none was specified, Align==0).
		if a := d.typ.Align; a > 1 {
			localBytes = (localBytes + a - 1) / a * a
		}
		if d.typ != nil && d.typ.HasVLA() {
			// A C99 variable-length array. Its extent does not exist yet, so
			// the elements cannot be laid out inside the frame: what lives
			// here is a POINTER to a run-time allocation, plus a hidden slot
			// holding the total byte size (sizeof reads it back, and the
			// enclosing block's exit uses it to give the space back).
			//
			// Handing the rest of the compiler the decayed pointer type is what
			// keeps this cheap: subscripting, decay in a value context and
			// pointer arithmetic all stride by the element width, which is
			// still a constant, so every one of those sites works unchanged.
			if !d.typ.IsVLA() || d.typ.Elem.HasVLA() {
				// "int a[3][n]" / "int a[n][m]": the element type is itself
				// variable-length, so the stride of a[i] is a run-time value.
				// Refuse instead of emitting a constant stride -- a wrong one
				// is indistinguishable from memory corruption.
				return fmt.Errorf("line %d: only a single variable-length dimension is supported (%s)", d.line, d.typ)
			}
			localBytes += 8 // the pointer to the run-time allocation
			ptrOff := -(regArea + localBytes)
			localBytes += 8 // hidden slot: total byte size, for sizeof and the block exit
			sizeOff := -(regArea + localBytes)
			c.varEnts[d.uid] = varInfo{off: ptrOff, typ: frontend.PtrType(d.typ.Elem),
				addr: c.addrTaken[d.name], ptr: true, vlaSize: sizeOff}
			continue
		}
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
			c.varEnts[d.uid] = varInfo{off: off, typ: d.typ, addr: c.addrTaken[d.name]}
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
			// T1.6 (C4): carry the ptrCapable flag (see the parameter loop).
			c.varEnts[d.uid] = varInfo{off: -(regArea + localBytes), typ: d.typ,
				addr: c.addrTaken[d.name], ptr: c.ptrCapable[d.name]}
		}
	}
	// Compound literals: each occurrence in the body gets its own persistent
	// frame slot, allocated before the frame size is frozen.
	for _, cl := range c.collectCompoundLits(f) {
		t := cl.Typ
		var size int
		switch {
		case t != nil && t.IsArray():
			ln := t.Len
			if ln < 1 {
				ln = 1
			}
			size = c.typeWidth(t.Elem) * ln
		case t != nil && isAgg(t):
			size = t.Size
		default:
			size = c.slotWidth(t)
		}
		if size < 1 {
			size = 1
		}
		if size%8 != 0 { // keep the next slot 8-byte aligned
			size += 8 - size%8
		}
		localBytes += size
		c.clOff[cl] = -(regArea + localBytes)
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
	// The mid-frame `pad` was historically Windows' 32-byte shadow reserve,
	// but it sat above the locals where a call's [rsp]..[rsp+32] shadow never
	// reached it -- so every call still claimed its own shadow with a
	// `sub rsp, 32` / `add rsp, 32` sandwich. We now reserve that shadow ONCE
	// at the very bottom of the frame (winShadow) and drop the per-call
	// sandwich for the common <=4-register-argument case. The mid-frame pad is
	// left as-is (dead on Windows, variadic-placement on SysV) so no local or
	// scratch offset shifts and the alignment invariant is untouched.
	pad := c.shadowSpace()
	if c.linux && f.Variadic {
		pad = 8 * len(c.argRegs())
	}
	winShadow := 0
	if !c.linux {
		// 32-byte caller-owned scratch every Windows call needs at
		// [rsp]..[rsp+32]; reserved once, so calls need no adjustment.
		winShadow = 32
	}
	// NOTE: regArea is intentionally NOT added here. The callee-save save
	// region is now allocated by the prolog's pushes (below rbp), not by the
	// `sub rsp` frame allocation, so `frame` covers only pad + locals + scratch
	// + switch depth + variadic save area. The +8 mirrors the +8 gap baked into
	// regArea above: the locals now start 8 bytes lower (rbp-40 instead of
	// rbp-32), so the frame must grow by 8 to keep them inside it.
	frame := 8 + pad + localBytes + 8*scratchSlots + 8*maxSwDepth + varargSave + winShadow
	// Must fold in the push count, not just round to 16 -- see alignFrame.
	frame = c.alignFrame(frame)
	// saveBaseOff points at save-area slot 0, which sits just below the
	// expression temporaries. The ABI pad added to frame above pushes it up
	// so the callee's argument-spill slots ([rsp+0..8*len(argRegs)) of the
	// caller) stay below it.
	c.saveBaseOff = -(regArea + localBytes + 8*scratchSlots + 8*maxSwDepth + varargSave)
	if winShadow != 0 {
		// The variadic save area sits just above the bottom shadow; shift it
		// down by the shadow size so it still lands at the frame bottom.
		c.saveBaseOff -= winShadow
	}

	c.curFn = f.Name
	c.labels = map[string]string{}
	c.swDepth = 0
	c.breaks = nil
	c.line(f.Name + ":\n")
	c.emit("push rbp")
	c.emit("mov rbp, rsp")
	// Save only the callee-save registers this function can actually write
	// (c.saveRegs, see calleeSavePool), not the whole pool. The k-th push lands
	// at [rbp-8*(k+1)], which is exactly where emitEpilogue reloads it.
	//
	// The old rule pushed all four unconditionally, on the theory that a
	// register with no local home still had to be preserved "to honour the ABI
	// contract". That is wrong: the contract is conditional -- a callee-save
	// register must be preserved only if the callee MODIFIES it. A register the
	// body never writes is already intact on return, so pushing it is dead
	// work. (The fear behind the old rule was that printf "legitimately uses
	// r13/r14 for its own locals" and would trash a caller's c/d; but printf
	// using them obliges printf to save and restore them, which is precisely
	// what makes the caller safe.)
	//
	// Nothing downstream depends on the count: goa's unwind metadata records
	// whatever pushes it actually sees, and both prologue shape checks in the
	// inliner skip a variable-length run of callee-save pushes.
	for _, r := range c.saveRegs {
		c.emit("push %s", r)
	}
	c.emitFrameAlloc(frame)
	c.maxTmp = 0
	c.grownMaxTmp = 0
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
		vi, _ := c.lookupVar(f.Params[i])
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
				if pt.Kind == frontend.KFloat {
					c.emit("movss [rbp%+d], %s", vi.off, argXMM[xmmAt])
				} else {
					c.emit("movsd [rbp%+d], %s", vi.off, argXMM[xmmAt])
				}
				continue
			}
		}
		if vi.reg != "" {
			// T2.1 (R2): the parameter already lives in its callee-save home;
			// the prologue PUSH preserved it, so this is a pure register move
			// and the frame slot is never allocated.
			c.emit("mov %s, %s", vi.reg, argRegs[i+regShift])
			continue
		}
		c.emit("mov [rbp%+d], %s", vi.off, argRegs[i+regShift])
	}
	// Copy aggregate parameters out of their caller-owned hidden pointers
	// into the local slots reserved above.
	for _, pc := range paramCopies {
		vi, _ := c.lookupVar(pc.name)
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

	// The function body is a block scope (child of the parameter scope): its
	// top-level declarations live here, and nested blocks push their own.
	c.pushScope()
	for _, st := range f.Body.Stmts {
		if err := c.genStmt(st); err != nil {
			c.popScope()
			return err
		}
	}
	// Safety epilogue in case a path has no explicit return. If the body's
	// last reachable statement is already a return, that frontend.ReturnStmt emitted a
	// full epilogue, so emitting another here would leave dead code after the
	// ret (e.g. `mov rsp, rbp; pop rbp; ret` that can never execute).
	if !c.blockEndsWithReturn(f.Body) {
		c.growFrameForTemps()
		c.emitEpilogue()
	}
	c.popScope() // function-body block scope
	return nil
}

// blockEndsWithReturn reports whether the last reachable statement of a block is
// an unconditional return, recursing through a trailing nested block. It is
// conservative: a trailing if/while/for (without a guaranteed return) yields
// false so the safety epilogue is retained.
func (c *CG) blockEndsWithReturn(b *frontend.Block) bool {
	if b == nil || len(b.Stmts) == 0 {
		return false
	}
	s := b.Stmts[len(b.Stmts)-1]
	if _, ok := s.(*frontend.ReturnStmt); ok {
		return true
	}
	if nb, ok := s.(*frontend.Block); ok {
		return c.blockEndsWithReturn(nb)
	}
	return false
}

// emitEpilogue restores the saved callee-save registers, then returns.
// (goa has no `leave`, so spell it out.)
// growFrameForTemps widens the prologue's `sub rsp, N` when the body claimed
// more expression-temporary slots than the fixed scratchSlots reservation.
// A _BitInt(N) temporary is ceil(N/64) slots wide, so a 512 KiB value needs
// 65536 of them -- without this the temps would live below rsp and every
// call would trample them.
func (c *CG) emitFrameAlloc(n int) {
	start := len(c.insts)
	// Windows commits stack one guard page at a time, so a frame of many
	// pages cannot simply be skipped over with `sub rsp, N`: touching a
	// page below the guard page is an access violation, not a request to
	// grow (that is what MSVC's __chkstk is for). Walk down a page at a
	// time, probing each, then correct the overshoot. Linux expands the
	// stack for any access below rsp within the rlimit, so it needs none.
	if !c.linux && n > 4096 {
		c.emit("mov r11, %d", n)
		lab := fmt.Sprintf(".Lchkstk%d", c.chkSeq)
		c.chkSeq++
		c.line(lab + ":")
		c.emit("sub rsp, 4096")
		c.emit("sub r11, 4096")
		c.emit("mov rax, [rsp]") // touch (mov does not disturb the flags)
		c.emit("jg %s", lab)
		c.emit("sub rsp, r11") // r11 <= 0: give back the overshoot
	} else {
		c.emit("sub rsp, %d", n)
	}
	c.frameIdx = start
	c.frameLen = len(c.insts) - start
	c.curFrame = n
}

// vlaSizeSlot returns the hidden frame slot holding the byte size of a
// variable-length array, when e names one. Anything else -- including an
// ordinary array, whose size is a constant -- reports false.
func (c *CG) vlaSizeSlot(e frontend.Expr) (int, bool) {
	id, ok := e.(*frontend.Ident)
	if !ok {
		return 0, false
	}
	vi, ok := c.lookupVar(id.Name)
	if !ok || vi.vlaSize == 0 {
		return 0, false
	}
	return vi.vlaSize, true
}

// genVLAAlloc emits the run-time allocation behind a C99 variable-length array
// declaration "T a[n];". The length is part of the type, but it can only be
// evaluated here, where the declaration actually executes: the frame was laid
// out long before anything knew the value of n.
//
// The frame holds a pointer to the elements (see the HasVLA branch of the
// layout pass). This takes the bytes off the stack, points that pointer at
// them, and parks the byte count in the hidden size slot so that sizeof(a) --
// a run-time value for a VLA -- can read it back.
//
// The space is not given back here. The epilogue restores rsp from rbp, which
// covers every return path; the enclosing block's exit additionally releases it
// (see releaseVLAs) so a loop body that declares a VLA does not grow the frame
// once per iteration.
func (c *CG) genVLAAlloc(n *frontend.DeclStmt, vi varInfo) error {
	// C forbids an initialiser on a variable-length array (6.7.9p3): there is
	// no way to know how many elements the braces would have to fill. Reject
	// it rather than silently dropping the initialiser -- an uninitialised VLA
	// that was supposed to be zeroed is a bug nobody can see.
	if n.Init != nil {
		return fmt.Errorf("line %d: a variable-length array cannot have an initialiser", n.Line)
	}
	if _, err := c.genExprT(n.Typ.VLALen); err != nil {
		return err
	}
	if err := c.ensureType(frontend.TInt); err != nil {
		return err
	}
	// The length is an int expression, but the byte count has to be 64-bit
	// before it is multiplied or a long array would wrap in 32 bits.
	if c.resW == 4 && c.resSigned {
		c.emit("movsxd rax, eax")
	}
	w := c.typeWidth(n.Typ.Elem)
	if w != 1 {
		c.emit("mov r10, %d", w)
		c.emit("imul rax, r10")
	}
	c.emit("mov [rbp%+d], rax", vi.vlaSize)
	// Round up to 16 so the stack stays aligned for later calls: rsp is
	// 16-aligned on entry and every allocation here keeps it that way.
	// shr/shl rather than `and rax, -16` -- a negative immediate is not
	// something the assembler is asked for anywhere else in this pipeline.
	c.emit("add rax, 15")
	c.emit("shr rax, 4")
	c.emit("shl rax, 4")
	if c.linux {
		c.emit("sub rsp, rax")
	} else {
		// Windows commits stack one guard page at a time, so a large VLA has
		// to be walked down a page at a time rather than skipped over -- the
		// run-time twin of the probing loop in emitFrameAlloc.
		c.emit("mov r11, rax")
		lab := fmt.Sprintf(".Lvlachk%d", c.chkSeq)
		c.chkSeq++
		c.line(lab + ":")
		c.emit("sub rsp, 4096")
		c.emit("sub r11, 4096")
		c.emit("mov rcx, [rsp]") // touch the page; mov leaves the flags alone
		c.emit("jg %s", lab)
		c.emit("sub rsp, r11") // r11 <= 0: hand the overshoot back
	}
	c.emit("mov [rbp%+d], rsp", vi.off)
	return nil
}

// blockVLASizes collects the hidden size slots of every variable-length array
// declared directly in b, in declaration order.
func (c *CG) blockVLASizes(b *frontend.Block) []int {
	var out []int
	collect := func(d *frontend.DeclStmt) {
		if d == nil || d.Typ == nil || !d.Typ.HasVLA() {
			return
		}
		if uid, ok := c.declUID[d]; ok {
			if vi, ok2 := c.varEnts[uid]; ok2 && vi.vlaSize != 0 {
				out = append(out, vi.vlaSize)
			}
		}
	}
	for _, st := range b.Stmts {
		switch n := st.(type) {
		case *frontend.DeclStmt:
			collect(n)
		case *frontend.DeclList:
			for _, d := range n.Decls {
				collect(d)
			}
		}
	}
	return out
}

// releaseVLAs gives the stack space of the block's variable-length arrays back
// at the end of the block that declared them. Without this a VLA declared in a
// loop body would consume a fresh slice of stack on every iteration and the
// frame would grow without bound.
func (c *CG) releaseVLAs(sizes []int) {
	for _, off := range sizes {
		c.emit("mov rax, [rbp%+d]", off)
		c.emit("add rax, 15")
		c.emit("shr rax, 4")
		c.emit("shl rax, 4")
		c.emit("add rsp, rax")
	}
}

func (c *CG) growFrameForTemps() {
	if c.maxTmp <= scratchSlots || c.frameIdx < 0 || c.frameIdx+1 > len(c.insts) {
		return
	}
	// Grow by the DELTA since the last growth, not the full 8*(maxTmp-32)
	// again: this helper runs once per return/epilogue site (a function with
	// hundreds of returns would otherwise multiply its frame by that count --
	// pi100k's main reached a 402 MB frame and died with stack overflow).
	from := c.grownMaxTmp
	if from < scratchSlots {
		from = scratchSlots
	}
	if c.maxTmp <= from {
		return
	}
	extra := 8 * (c.maxTmp - from)
	c.grownMaxTmp = c.maxTmp
	need := c.curFrame + extra
	// Same push-count-aware alignment as the initial frame -- see alignFrame.
	need = c.alignFrame(need)
	if need <= c.curFrame {
		return
	}
	// Re-emit the whole allocation block in place: the frame may cross the
	// stack-probe threshold only now that the body's temporaries are known.
	rest := append([]Inst{}, c.insts[c.frameIdx+c.frameLen:]...)
	c.insts = c.insts[:c.frameIdx]
	c.emitFrameAlloc(need)
	c.insts = append(c.insts, rest...)
}

func (c *CG) emitEpilogue() {
	// Restore the callee-save registers saved by the prolog's PUSHes.
	//
	// The prolog does `push rbp; mov rbp,rsp; push <saveRegs>; sub rsp, frame`,
	// so the k-th pushed register sits at [rbp-8*(k+1)].
	//
	// It is essential to restore them with rbp-RELATIVE loads, NOT with `pop`:
	// the prolog's `sub rsp, frame` is not undone until the later `mov rsp,
	// rbp`, so at this point rsp is still way below rbp. A `pop` would read
	// the uninitialised local area, not the saved registers -- silently zeroing
	// (or scrambling) every caller-cached local that lived in a callee-save
	// register across the call. Because the restore must be position-
	// independent w.r.t. rsp, we load from [rbp-8*(k+1)] exactly where the
	// pushes stored them.
	//
	// Only c.saveRegs is restored, and k is the index within THAT slice (not
	// within calleeSaveAll): the save slots are laid down by however many
	// pushes the prolog actually emitted, so a function that saved just rbx
	// finds it at [rbp-8]. Restoring a register the prolog never pushed would
	// load garbage from the local area and trash a caller's cached value.
	for k, r := range c.saveRegs {
		c.emit("mov %s, [rbp-%d]", r, 8*(k+1))
	}
	c.emit("mov rsp, rbp")
	c.emit("pop rbp")
	c.emit("ret")
}

func (c *CG) genStmt(s frontend.Stmt) error {
	switch n := s.(type) {
	case *frontend.Block:
		// A compound statement opens a new lexical scope. Its declarations
		// register themselves into this scope when their frontend.DeclStmt is emitted.
		c.pushScope()
		// Variable-length arrays declared directly in this block own stack
		// space that has to go back when the block ends -- otherwise a VLA in
		// a loop body eats a fresh slice of stack on every iteration.
		vlaSizes := c.blockVLASizes(n)
		for _, st := range n.Stmts {
			if err := c.genStmt(st); err != nil {
				c.popScope()
				return err
			}
		}
		c.releaseVLAs(vlaSizes)
		c.popScope()
	case *frontend.DeclList:
		// A multi-declarator declaration ("int a = 1, b = 2;") is one
		// statement; without this case genStmt silently emitted nothing for
		// it, leaving every initialiser unrun (all variables read as 0).
		for _, d := range n.Decls {
			if err := c.genStmt(d); err != nil {
				return err
			}
		}
	case *frontend.DeclStmt:
		// Static locals are initialised once at load time in .data, and extern
		// locals have no storage of their own; neither needs run-time
		// initialisation, so skip the frame-storing code below.
		if n.Storage == "static" {
			// Bind the name HERE rather than in the frame-layout pre-pass: this
			// statement is reached while its own block's scope is current, so
			// the binding lands in the right place and two blocks declaring the
			// same name get separate bindings. The label itself was already
			// minted (and the .data entry queued) by the pre-pass.
			if lab, ok := c.staticLabelOf[n.Line]; ok {
				c.bindStatic(n.Name, lab)
			}
			return nil
		}
		if n.Storage == "extern" {
			return nil
		}
		// Register this declaration into the current (innermost) lexical
		// scope so later references resolve to its own uid -- this is what
		// lets a declaration shadow an outer one with the same name, and lets
		// sibling blocks reuse a name without their homes colliding.
		if uid, ok := c.declUID[n]; ok {
			c.scopes[len(c.scopes)-1][n.Name] = uid
		}
		vi, _ := c.lookupVar(n.Name)
		if vi.vlaSize != 0 {
			// A variable-length array: its elements can only be allocated
			// here, where the declaration executes and its length is known.
			return c.genVLAAlloc(n, vi)
		}
		if bi, ok := n.Init.(*frontend.BraceInit); ok {
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
			if sl, ok := n.Init.(*frontend.StrLit); ok && ((sl.Wide && vi.typ.Elem.Width == 2) || (!sl.Wide && vi.typ.Elem.IsChar())) {
				size := c.typeWidth(vi.typ)
				copied := len(sl.Bytes)
				if sl.Wide {
					copied += 2 // 2-byte NUL terminator for wchar_t[]
				} else {
					copied += 1
				}
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
				it := c.exprType(n.Init)
				if frontend.IsBig(vi.typ) && !frontend.IsBig(it) {
					// scalar initialiser for a _BitInt local
					if _, err := c.genExprT(n.Init); err != nil {
						return err
					}
					if err := c.ensureType(frontend.TInt); err != nil {
						return err
					}
					// N17: a materialized signed int initialiser consumed as a
					// 64-bit signed value by bi_from_i64_trunc must be
					// sign-extended first (`S64 x = -12345` would otherwise
					// land as +4294954951). Unsigned destinations are already
					// zero-extended and need nothing.
					if c.resW == 4 && c.resSigned && vi.typ.Signed {
						c.emit("movsxd r11, eax")
					} else {
						c.emit("mov r11, rax")
					}
					c.emit("lea r10, [rbp%+d]", vi.off)
					sg := int64(0)
					if vi.typ.Signed {
						sg = 1
					}
					c.callBigLib("__goclib_bi_from_i64_trunc", []bigArg{
						{reg: "r10"}, {reg: "r11"}, {imm: int64(vi.typ.Bits)}, {imm: sg},
					})
					return nil
				}
				// Scalar initialiser for a long double local: materialise
				// the value converted to binary128, copy the 16 bytes in.
				if isLD(vi.typ) && !frontend.IsBig(it) && !c.isLDExpr(n.Init) {
					_, sl, _, err := c.tfOperand(n.Init)
					if err != nil {
						return err
					}
					c.emit("mov r11, r10")
					c.emit("lea r10, [rbp%+d]", vi.off)
					c.copyBytes("r10", "r11", 16)
					c.tmpDepth -= sl
					return nil
				}
				if err := c.structSrcAddr(n.Init, it); err != nil {
					return err
				}
				c.emit("mov r11, r10") // r11 = source address
				c.emit("lea r10, [rbp%+d]", vi.off)
				if frontend.IsBig(vi.typ) && frontend.IsBig(it) && it.Bits != vi.typ.Bits {
					ssg := int64(0)
					if it.Signed {
						ssg = 1
					}
					dsg := int64(0)
					if vi.typ.Signed {
						dsg = 1
					}
					c.callBigLib("__goclib_bi_conv", []bigArg{
						{reg: "r10"}, {reg: "r11"}, {imm: int64(vi.typ.Bits)}, {imm: dsg}, {imm: int64(it.Bits)}, {imm: ssg},
					})
				} else {
					c.copyBytes("r10", "r11", vi.typ.Size)
				}
				c.releaseResBig()
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
			// Zero a stack scalar that has no initialiser. The store MUST use
			// the variable's slot width: a 1-byte _Bool/char must emit
			// `mov byte [...]`, otherwise goa lowers the bare `mov [mem], 0`
			// to an 8-byte store that also zeroes the seven adjacent higher
			// stack slots and silently corrupts neighbouring narrow locals
			// (the #81 _Bool-local codegen bug). genStoreElem uses the same
			// width switch.
			switch c.slotWidth(vi.typ) {
			case 1:
				c.emit("mov byte [rbp%+d], 0", vi.off)
			case 2:
				c.emit("mov word [rbp%+d], 0", vi.off)
			case 4:
				c.emit("mov dword [rbp%+d], 0", vi.off)
			default:
				c.emit("mov [rbp%+d], 0", vi.off)
			}
		}
	case *frontend.AssignStmt:
		// Whole-aggregate assignment shares the frontend.AssignExpr code path (which
		// handles both lvalue and struct-returning-call right-hand sides).
		// Statement-level assignments are normally parsed as
		// frontend.ExprStmt{frontend.AssignExpr}; this branch is a safety net.
		if isAgg(c.exprType(n.Lhs)) {
			_, err := c.genExprT(&frontend.AssignExpr{Lhs: n.Lhs, Rhs: n.Rhs})
			return err
		}
		// Fast path: a simple scalar local on the left can be stored
		// directly to its home register, with no need to compute an address.
		if id, ok := n.Lhs.(*frontend.Ident); ok {
			if vi, ok2 := c.lookupVar(id.Name); ok2 && vi.reg != "" {
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
		// Capture the value's own width/signedness before genLValue clobbers
		// the expression state (atexit crash class: a pointer truncated by a
		// stale int index's resW).
		rw, rs := c.resW, c.resSigned
		// Spill the right-hand side into a frame temporary so computing the
		// lvalue address (which may itself call functions) cannot clobber it.
		c.tmpDepth++
		rslot := c.tmpSlot(c.tmpDepth)
		if rt == frontend.TDouble {
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
		if ec == frontend.TDouble {
			c.emit("movsd xmm0, [rbp%+d]", rslot)
		} else {
			c.emit("mov rax, [rbp%+d]", rslot)
		}
		c.genStoreElem("r10", c.lvalueWidth(n.Lhs), ec, rw, rs)
		c.tmpDepth--
		return nil
	case *frontend.ExprStmt:
		if _, err := c.genExprT(n.E); err != nil {
			return err
		}
		// A discarded value leaves its result buffer live; nothing will
		// consume it here, so release it.
		c.releaseResBig()
	case *frontend.ReturnStmt:
		if isAgg(c.curRet) {
			// Struct/union/_BitInt return: copy the value through the hidden
			// result pointer into the caller's buffer. rax is never used.
			if n.E == nil {
				return fmt.Errorf("missing return value in function returning %s", c.curRet.String())
			}
			et := c.exprType(n.E)
			if frontend.IsBig(c.curRet) && !frontend.IsBig(et) {
				// scalar return value for a _BitInt function
				if _, err := c.genExprT(n.E); err != nil {
					return err
				}
				if err := c.ensureType(frontend.TInt); err != nil {
					return err
				}
				// N17: same materialized-int-to-signed-big widening as the
				// local-initialiser path above.
				if c.resW == 4 && c.resSigned && c.curRet.Signed {
					c.emit("movsxd r11, eax")
				} else {
					c.emit("mov r11, rax")
				}
				c.emit("mov r10, [rbp%+d]", c.sretSlot)
				sg := int64(0)
				if c.curRet.Signed {
					sg = 1
				}
				c.callBigLib("__goclib_bi_from_i64_trunc", []bigArg{
					{reg: "r10"}, {reg: "r11"}, {imm: int64(c.curRet.Bits)}, {imm: sg},
				})
				c.growFrameForTemps()
				c.emitEpilogue()
				return nil
			}
			// Scalar return value for a long double function: materialise
			// the value converted to binary128, copy the 16 bytes through
			// the hidden result pointer.
			if isLD(c.curRet) && !frontend.IsBig(et) && !c.isLDExpr(n.E) {
				_, sl, _, err := c.tfOperand(n.E)
				if err != nil {
					return err
				}
				c.emit("mov r11, r10")
				c.emit("mov r10, [rbp%+d]", c.sretSlot)
				c.copyBytes("r10", "r11", 16)
				c.tmpDepth -= sl
				c.growFrameForTemps()
				c.emitEpilogue()
				return nil
			}
			if err := c.structSrcAddr(n.E, et); err != nil {
				return err
			}
			c.emit("mov r11, r10")                  // r11 = source address
			c.emit("mov r10, [rbp%+d]", c.sretSlot) // r10 = caller's buffer
			if frontend.IsBig(c.curRet) && frontend.IsBig(et) && et.Bits != c.curRet.Bits {
				ssg := int64(0)
				if et.Signed {
					ssg = 1
				}
				dsg := int64(0)
				if c.curRet.Signed {
					dsg = 1
				}
				c.callBigLib("__goclib_bi_conv", []bigArg{
					{reg: "r10"}, {reg: "r11"}, {imm: int64(c.curRet.Bits)}, {imm: dsg}, {imm: int64(et.Bits)}, {imm: ssg},
				})
			} else {
				c.copyBytes("r10", "r11", c.curRet.Size)
			}
			c.releaseResBig()
			c.growFrameForTemps()
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
			if c.curRet != nil && c.curRet.Kind == frontend.KFloat {
				c.emit("cvtsd2ss xmm0, xmm0")
			}
			// A _Bool return must be exactly 0 or 1 (C semantics).
			if c.curRet != nil && c.curRet.Kind == frontend.KBool {
				c.normalizeBool()
			}
			// N17: a materialized signed int (resW==4, signed, high 32 = 0)
			// returned from a signed 8-byte function must be sign-extended.
			// `return -1;` in a long-returning goclib helper compiles the neg
			// as `neg eax` (0x00000000FFFFFFFF under C1); the caller's 64-bit
			// signed consumption (`cmp rax,0; setl` in bi_cmp users) would see
			// a huge positive and flip the comparison.
			if c.curRet != nil && c.curRet.Kind == frontend.KInt && c.curRet.Signed && c.curRet.Width == 8 && c.resW == 4 && c.resSigned {
				c.emit("movsxd rax, eax")
			}
		} else {
			c.emit("mov rax, 0")
		}
		c.growFrameForTemps()
		c.emitEpilogue()
	case *frontend.IfStmt:
		lElse := c.newLabel("else")
		lEnd := c.newLabel("endif")
		if err := c.genTruth(n.Cond); err != nil {
			return err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lElse)
		// Each branch is its own scope, so a bare declaration in "if (c) int x;"
		// (or one inside a compound body) does not leak into the other branch.
		c.pushScope()
		if err := c.genStmt(n.Then); err != nil {
			c.popScope()
			return err
		}
		c.popScope()
		if n.Else != nil {
			c.emit("jmp %s", lEnd)
			c.line(lElse + ":\n")
			c.pushScope()
			if err := c.genStmt(n.Else); err != nil {
				c.popScope()
				return err
			}
			c.popScope()
			c.line(lEnd + ":\n")
		} else {
			c.line(lElse + ":\n")
		}
	case *frontend.WhileStmt:
		lTop := c.newLabel("while")
		lEnd := c.newLabel("wend")
		c.line(lTop + ":\n")
		if err := c.genTruth(n.Cond); err != nil {
			return err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lEnd)
		// Register break/continue targets so a break/continue inside the body
		// (or a nested loop) resolves to this loop. continue re-checks the
		// condition at lTop.
		c.loops = append(c.loops, loopLabels{breakLbl: lEnd, contLbl: lTop})
		c.breaks = append(c.breaks, lEnd)
		c.pushScope() // loop body scope
		bodyErr := c.genStmt(n.Body)
		c.popScope()
		c.loops = c.loops[:len(c.loops)-1]
		c.breaks = c.breaks[:len(c.breaks)-1]
		if bodyErr != nil {
			return bodyErr
		}
		c.emit("jmp %s", lTop)
		c.line(lEnd + ":\n")
	case *frontend.ForStmt:
		lTop := c.newLabel("for")
		lEnd := c.newLabel("forend")
		lCont := c.newLabel("forcont")
		// The for-statement scope covers the initialiser, the condition, the
		// body and the post expression -- a declaration such as
		// "for (int i = 0; ...)" is visible across all of them. A compound
		// body opens its own nested scope on top of this one.
		c.pushScope()
		if n.Init != nil {
			if err := c.genStmt(n.Init); err != nil {
				c.popScope()
				return err
			}
		}
		c.line(lTop + ":\n")
		if n.Cond != nil {
			if err := c.genTruth(n.Cond); err != nil {
				c.popScope()
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
			c.popScope()
			return bodyErr
		}
		c.line(lCont + ":\n")
		if n.Post != nil {
			if _, err := c.genExprT(n.Post); err != nil {
				c.popScope()
				return err
			}
		}
		c.emit("jmp %s", lTop)
		c.line(lEnd + ":\n")
		c.popScope()
	case *frontend.DoWhileStmt:
		// "do body while (cond);": the body runs first, so the condition is
		// tested at the bottom. continue jumps to that test, not to the body.
		lTop := c.newLabel("do")
		lCont := c.newLabel("docont")
		lEnd := c.newLabel("doend")
		c.line(lTop + ":\n")
		c.loops = append(c.loops, loopLabels{breakLbl: lEnd, contLbl: lCont})
		c.breaks = append(c.breaks, lEnd)
		c.pushScope() // loop body scope
		bodyErr := c.genStmt(n.Body)
		c.popScope()
		c.loops = c.loops[:len(c.loops)-1]
		c.breaks = c.breaks[:len(c.breaks)-1]
		if bodyErr != nil {
			return bodyErr
		}
		c.line(lCont + ":\n")
		if err := c.genTruth(n.Cond); err != nil {
			return err
		}
		c.emit("cmp rax, 0")
		c.emit("jne %s", lTop)
		c.line(lEnd + ":\n")
	case *frontend.SwitchStmt:
		if err := c.genSwitch(n); err != nil {
			return err
		}
	case *frontend.GotoStmt:
		c.emit("jmp %s", c.labelSym(n.Label))
	case *frontend.LabelStmt:
		c.line(c.labelSym(n.Name) + ":\n")
		if n.Stmt != nil {
			return c.genStmt(n.Stmt)
		}
	case *frontend.AsmStmt:
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
	case *frontend.BreakStmt:
		// break binds to the innermost enclosing loop *or* switch; the
		// separate stack keeps a switch from stealing a loop's continue.
		if len(c.breaks) == 0 {
			return fmt.Errorf("break outside a loop or switch")
		}
		c.emit("jmp %s", c.breaks[len(c.breaks)-1])
	case *frontend.ContinueStmt:
		if len(c.loops) == 0 {
			return fmt.Errorf("continue outside a loop")
		}
		c.emit("jmp %s", c.loops[len(c.loops)-1].contLbl)
	case *frontend.CaseStmt:
		return fmt.Errorf("line %d: case label outside a switch", n.Line)
	case *frontend.DefaultStmt:
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
//	locals and parameters (resolved through c.lookupVar)  -> [rbp+off]
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
	if vi, ok := c.lookupVar(w); ok {
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

// sanitizeLabel reduces a file name to something usable inside an assembler
// label: letters, digits and underscore. A path may hold characters a label
// cannot (a dot in `a.b.c`, a dash), and on Windows a backslash; keeping them
// would produce assembly goa cannot parse, which is a far worse failure than
// two units colliding on an odd name.
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "unit"
	}
	return out
}

// swGroup is one arm of a switch: either "case N:" or "default:", plus the
// statements that follow it up to the next label.
type swGroup struct {
	lbl       string
	val       int
	isDefault bool
	stmts     []frontend.Stmt
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
func (c *CG) genSwitch(n *frontend.SwitchStmt) error {
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
	if err := c.ensureType(frontend.TInt); err != nil {
		c.swDepth--
		return err
	}
	// N21: an int switch value is materialized (high 32 = 0); the case
	// comparisons must be 32-bit so a negative case constant (imm sign-extended
	// by the CPU in a 32-bit cmp) matches. A long/pointer value keeps 64-bit.
	swW := c.resW
	c.emit("mov [rbp%+d], rax", slot)
	// A switch body is a block scope; a declaration inside a case belongs to
	// it and does not leak out of the switch.
	c.pushScope()

	var groups []swGroup
	defIdx := -1
	for _, st := range n.Body.Stmts {
		switch cs := st.(type) {
		case *frontend.CaseStmt:
			groups = append(groups, swGroup{lbl: c.newLabel("case"), val: cs.Val})
		case *frontend.DefaultStmt:
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
	if swW == 4 {
		c.emit("mov eax, [rbp%+d]", slot)
	} else {
		c.emit("mov rax, [rbp%+d]", slot)
	}
	for _, g := range groups {
		if g.isDefault {
			continue
		}
		if swW == 4 {
			// N21: 32-bit compare against the materialized int switch value.
			c.emit("cmp eax, %d", g.val)
		} else {
			c.emit("cmp rax, %d", g.val)
		}
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
	c.popScope()
	c.swDepth--
	return err
}

func (c *CG) genUnary(n *frontend.Unary) (frontend.CType, error) {
	switch n.Op {
	case "&":
		// &*p is just p (the pointer value); everything else is a real lvalue.
		if inner, ok := n.E.(*frontend.Unary); ok && inner.Op == "*" {
			if _, err := c.genExprT(inner.E); err != nil {
				return frontend.TInt, err
			}
		} else {
			if err := c.genLValue(n.E); err != nil {
				return frontend.TInt, err
			}
			c.emit("mov rax, r10")
		}
		c.resTyp = frontend.TInt
		c.resSigned = false
		c.resW = 8 // address-of yields a pointer value
		return frontend.TInt, nil
	case "*":
		// PEXT (opt>=1): fold `*(p ± K)` into p's value plus a constant byte
		// displacement, then load straight through it. genExprT would
		// otherwise run the full pointer-arithmetic recipe -- materialise K,
		// sign-extend, imul by the element width, add -- seven instructions to
		// read a neighbouring element, where gcc emits one. Gated off at -O0
		// to honour the byte-identical -O0 guardrail.
		if c.opt >= 1 {
			if pe, disp, ok := c.f2PtrConstOffset(n.E); ok {
				// A register-cached pointer needs no evaluation at all.
				if id, isID := pe.(*frontend.Ident); isID {
					if vi, ok2 := c.lookupVar(id.Name); ok2 && vi.reg != "" {
						c.emitLeaDisp(vi.reg, disp)
					} else {
						if _, err := c.genExprT(pe); err != nil {
							return frontend.TInt, err
						}
						c.emitLeaDisp("rax", disp)
					}
				} else {
					if _, err := c.genExprT(pe); err != nil {
						return frontend.TInt, err
					}
					c.emitLeaDisp("rax", disp)
				}
				ec := c.elemClassOf(n.E)
				width := c.elemWidthOf(n.E)
				signed := c.elemSignedOf(n.E)
				c.genLoadElem("r10", width, ec, signed)
				return c.resTyp, nil
			}
		}
		// Dereference: n.E evaluates to the pointer value (in rax).
		if _, err := c.genExprT(n.E); err != nil {
			return frontend.TInt, err
		}
		// Dereferencing a pointer to FUNCTION yields the function designator,
		// whose value IS that same address -- there is nothing to load. This
		// is what makes "(*fp)(x)" branch to the same place as "fp(x)".
		if t := c.exprType(n.E); t != nil && t.IsPtr() && t.Elem != nil && t.Elem.IsFunc() {
			c.resTyp = frontend.TInt
			c.resSigned = false
			c.resW = 8
			return frontend.TInt, nil
		}
		ec := c.elemClassOf(n.E)
		width := c.elemWidthOf(n.E)
		signed := c.elemSignedOf(n.E)
		c.genLoadElem("rax", width, ec, signed)
		return c.resTyp, nil
	}
	t, err := c.genExprT(n.E)
	if err != nil {
		return frontend.TInt, err
	}
	if n.Op == "-" {
		if t == frontend.TDouble {
			// xmm0 = -xmm0  => 0.0 - xmm0
			c.emit("xorpd xmm1, xmm1")
			c.emit("subsd xmm1, xmm0")
			c.emit("movsd xmm0, xmm1")
		} else {
			// Integer promotion: a narrow operand (char/short) negates as a
			// signed int; an int keeps its own signedness; an 8-byte value
			// (long/unsigned long) keeps the full 64-bit result.
			w := c.resW
			if w < 4 {
				w = 4
			}
			c.resW = w
			if w == 4 {
				// T1.6: negate the low 32 bits only -- a 32-bit `neg eax`
				// wraps at 32 and zero-extends to rax, which is exactly the
				// materialized-int invariant (low 32 valid, high 32 = 0).
				// `neg rax` would set the high 32 to FFFF and corrupt it.
				c.emit("neg eax")
			} else {
				c.emit("neg rax")
			}
		}
		return t, nil
	}
	// "~" -- one's complement. The operand has already been checked to be an
	// integer; `not` flips the low 32 bits for an int (32-bit form zero-extends
	// to rax) and all 64 bits for an 8-byte value.
	if n.Op == "~" {
		w := c.resW
		if w < 4 {
			w = 4
		}
		c.resW = w
		if w == 4 {
			c.emit("not eax")
		} else {
			c.emit("not rax")
		}
		c.resTyp = frontend.TInt
		return frontend.TInt, nil
	}
	// "!" -- force the operand to int, then rax = (rax == 0)
	if err := c.ensureType(frontend.TInt); err != nil {
		return frontend.TInt, err
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
	c.resTyp = frontend.TInt
	c.resSigned = true
	c.resW = 4 // !x yields a (signed) int
	return frontend.TInt, nil
}

// f2IndexRegConst extends the F2 register-index optimisation to the common
// idiom "a[var ± const]" / "a[const + var]" (and "a[var - const]"), where var
// is a register-cached int-class local and the offset is a small integer
// literal. It returns the variable's callee-save home register and the
// constant offset in index units; the frontend.Index addressing code folds
// offset*elemWidth into the final byte-offset add, replacing the
// materialisation of var±const as a separate spilled value (the redundant
// j+1 recompute + slot spill + movsxd seen in a bubble-sort inner loop).
//
// Only the +1 coefficient forms qualify -- "const - var" would require
// negating the index register, which is not folded. The offset magnitude is
// capped at 1<<20 so the byte product always fits a sign-extended imm32.
// Returns ("", 0) when e is not one of those forms or var is not
// register-cached (the caller then falls back to the generic spill path).
// f2PtrConstOffset recognises a pointer expression of the form
// `p + K` / `K + p` / `p - K` where K is a small integer literal, and returns
// the pointer sub-expression together with the equivalent byte displacement
// (K already scaled by the pointee width, sign applied). Anything else -- a
// non-literal offset, a non-pointer operand, an out-of-range constant, a huge
// element width -- reports false so the caller keeps the generic path.
func (c *CG) f2PtrConstOffset(e frontend.Expr) (frontend.Expr, int64, bool) {
	b, ok := e.(*frontend.Binary)
	if !ok {
		return nil, 0, false
	}
	var ptr frontend.Expr
	var lit *frontend.NumLit
	neg := false
	switch b.Op {
	case "+":
		// p + K  OR  K + p
		if l, ok := b.L.(*frontend.NumLit); ok && !l.IsFloat {
			if c.exprType(b.R) != nil && ptrish(c.exprType(b.R)) {
				ptr, lit = b.R, l
			}
		} else if r, ok := b.R.(*frontend.NumLit); ok && !r.IsFloat {
			if c.exprType(b.L) != nil && ptrish(c.exprType(b.L)) {
				ptr, lit = b.L, r
			}
		}
	case "-":
		// p - K only; K - p would need a negation the lea cannot express.
		if r, ok := b.R.(*frontend.NumLit); ok && !r.IsFloat {
			if c.exprType(b.L) != nil && ptrish(c.exprType(b.L)) {
				ptr, lit, neg = b.L, r, true
			}
		}
	default:
		return nil, 0, false
	}
	if ptr == nil || lit == nil || lit.BigWords != nil {
		return nil, 0, false
	}
	if lit.Val < -(1<<20) || lit.Val > (1<<20) {
		return nil, 0, false
	}
	ew := int64(c.ptrElemWidth(c.exprType(ptr)))
	disp := lit.Val * ew
	if neg {
		disp = -disp
	}
	// The displacement must survive the sign-extended imm32 form.
	if disp > (1<<31)-1 || disp < -(1<<31) {
		return nil, 0, false
	}
	return ptr, disp, true
}

// ptrish reports whether t is a pointer (or an array that decays to one), the
// two forms pointer arithmetic and subscripting stride over.
func ptrish(t *frontend.Type) bool {
	return t != nil && (t.IsPtr() || t.IsArray())
}

// emitLeaDisp leaves base+disp in r10. A zero displacement degenerates to a
// plain mov (lea r10, [rax+0] is legal but wasteful, and goa's parser is
// happier with the short form).
func (c *CG) emitLeaDisp(base string, disp int64) {
	switch {
	case disp == 0:
		c.emit("mov r10, %s", base)
	case disp > 0:
		c.emit("lea r10, [%s+%d]", base, disp)
	default:
		c.emit("lea r10, [%s-%d]", base, -disp)
	}
}

func (c *CG) f2IndexRegConst(e frontend.Expr) (string, int64, string) {
	b, ok := e.(*frontend.Binary)
	if !ok {
		return "", 0, ""
	}
	var id *frontend.Ident
	var lit *frontend.NumLit
	neg := false
	switch b.Op {
	case "+":
		// var + const  OR  const + var
		if l, ok := b.L.(*frontend.Ident); ok {
			if r, ok2 := b.R.(*frontend.NumLit); ok2 && !r.IsFloat {
				id, lit = l, r
			}
		} else if l, ok := b.L.(*frontend.NumLit); ok && !l.IsFloat {
			if r, ok2 := b.R.(*frontend.Ident); ok2 {
				id, lit = r, l
			}
		}
	case "-":
		// var - const only; const - var is excluded (would need negation).
		if l, ok := b.L.(*frontend.Ident); ok {
			if r, ok2 := b.R.(*frontend.NumLit); ok2 && !r.IsFloat {
				id, lit = l, r
				neg = true
			}
		}
	default:
		return "", 0, ""
	}
	if id == nil || lit == nil || lit.BigWords != nil {
		return "", 0, ""
	}
	if lit.Val < -(1<<20) || lit.Val > (1<<20) {
		return "", 0, ""
	}
	vi, ok := c.lookupVar(id.Name)
	if !ok || vi.reg == "" {
		return "", 0, ""
	}
	off := lit.Val
	if neg {
		off = -off
	}
	return vi.reg, off, id.Name
}

// genLValue emits code that leaves the address of the lvalue e in r10.
func (c *CG) genLValue(e frontend.Expr) error {
	// Most lvalues are not bit-fields; the frontend.MemberExpr branch below re-arms
	// these when it addresses one.
	c.lvBitWidth, c.lvBitOff, c.lvBitUnit, c.lvBitSigned = 0, 0, 0, false
	switch n := e.(type) {
	case *frontend.Ident:
		// Locals and globals shadow a same-named function (C block scoping,
		// same order as the value path and the checker). A function-local
		// name is resolved FIRST so it is never mistaken for a file-scope
		// thread-local global of the same spelling. Only after no variable
		// matches may this name a function: "&add" is just another spelling
		// of "add"; both yield the function's address.
		if vi, ok := c.lookupVar(n.Name); ok {
			if vi.reg != "" {
				// Reached only if a register-cached local had its address taken,
				// which findAddressTaken should have prevented.
				return fmt.Errorf("cannot take address of register-allocated variable %q", n.Name)
			}
			c.emit("lea r10, [rbp%+d]", vi.off)
			return nil
		}
		// File-scope names. A static local shadows a same-named TLS global
		// within its function, but a static thread-local local still needs the
		// segment reach (also registered in tlsVars).
		if lab, ok2 := c.lookupStatic(n.Name); ok2 {
			if _, isTLS := c.tlsVars[n.Name]; !isTLS {
				c.emit("lea r10, [rip+%s]", lab)
				return nil
			}
		}
		if _, ok := c.tlsVars[n.Name]; ok {
			// Thread-local variable: its address is reached through the
			// fs/gs segment, not a rip-relative .data label.
			if err := c.genTLSAddr(n.Name); err != nil {
				return err
			}
			return nil
		}
		if lab, ok2 := c.lookupStatic(n.Name); ok2 {
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
		if sym, ok2 := c.funcAddrSym(n.Name); ok2 {
			c.emit("lea r10, [rip+%s]", sym)
			return nil
		}
		return fmt.Errorf("undefined variable %q", n.Name)
	case *frontend.CompoundLit:
		// The unnamed object's address. Its initialisation runs HERE so that
		// every consumer (a struct argument, &x, .member) sees initialised
		// bytes; see the value path in genExprT for the decay/load cases.
		off, ok := c.clOff[n]
		if !ok {
			return fmt.Errorf("line %d: internal: compound literal has no frame slot", n.Line)
		}
		if n.Typ.IsArray() || isAgg(n.Typ) {
			if err := c.genBraceInitLocal(n.Typ, n.Init, off); err != nil {
				return err
			}
		} else {
			// Scalar: initialise from its single element; "{}" (C23) zeroes.
			vi := varInfo{off: off, typ: n.Typ}
			if len(n.Init.Elems) == 1 && n.Init.Elems[0].Desig == "" && n.Init.Elems[0].DesigIdx < 0 {
				if _, err := c.genExprT(n.Init.Elems[0].E); err != nil {
					return err
				}
				if err := c.ensureType(n.Typ.Class()); err != nil {
					return err
				}
				c.storeVar(vi)
			} else {
				c.emit("lea r10, [rbp%+d]", off)
				c.zeroBytes("r10", c.slotWidth(n.Typ))
			}
		}
		c.emit("lea r10, [rbp%+d]", off)
		return nil
	case *frontend.Unary:
		if n.Op != "*" {
			return fmt.Errorf("expression is not an lvalue")
		}
		// PEXT (opt>=1): `*(p ± K)` / `*(K + p)` is just p's value plus a
		// constant byte displacement. The generic path below runs the whole
		// pointer-arithmetic recipe instead -- materialise K, sign-extend it,
		// imul by the element width, add -- which costs seven instructions to
		// fetch a neighbour element. Folding the displacement into a single
		// lea turns the bubble-sort idiom `*(p+1)` into one instruction, the
		// same shape gcc emits. Gated off at -O0 to honour the
		// byte-identical -O0 guardrail.
		if c.opt >= 1 {
			if pe, disp, ok := c.f2PtrConstOffset(n.E); ok {
				// A register-cached pointer variable needs no evaluation at
				// all: the callee-save already holds it.
				if id, isID := pe.(*frontend.Ident); isID {
					if vi, ok2 := c.lookupVar(id.Name); ok2 && vi.reg != "" {
						c.emitLeaDisp(vi.reg, disp)
						return nil
					}
				}
				if _, err := c.genExprT(pe); err != nil {
					return err
				}
				c.emitLeaDisp("rax", disp)
				return nil
			}
		}
		// The address of *p is simply the pointer value p holds.
		if _, err := c.genExprT(n.E); err != nil {
			return err
		}
		c.emit("mov r10, rax")
		return nil
	case *frontend.Index:
		// Element address = base_address + index*8. Compute the index first
		// and spill it, because evaluating the base may call a function and
		// clobber the volatile registers.
		//
		// F2: when the index is a register-cached int variable (varInfo.reg,
		// a callee-save home assigned by T2.1 that survives calls), skip the
		// genExprT + spill + reload entirely: the value is already live in the
		// callee-save across the base evaluation, and its low dword (or full
		// width for long/pointer indices) is consumed directly below.
		var idxReg string
		var idxConst int64
		var idxName string
		if !f2IdxRegSkip {
			if id, ok := n.Idx.(*frontend.Ident); ok {
				if vi, ok2 := c.lookupVar(id.Name); ok2 && vi.reg != "" {
					idxReg = vi.reg
					idxName = id.Name
				}
			}
			// F2-EXT (opt>=1): also fold a small constant offset off an
			// index of the form a[var±const] / a[const+var] into the final
			// byte-offset add, instead of materialising var±const as a
			// separate spilled value (the redundant j+1 recompute + slot
			// spill + movsxd in a bubble-sort inner loop). Gated off at -O0
			// to honour the byte-identical -O0 guardrail; the bare-frontend.Ident F2
			// path above stays always-on.
			if idxReg == "" && c.opt >= 1 {
				idxReg, idxConst, idxName = c.f2IndexRegConst(n.Idx)
			}
		}
		if idxReg == "" {
			if _, err := c.genExprT(n.Idx); err != nil {
				return err
			}
		}
		// N11: capture the index's own width/sign -- a signed int index is
		// materialized (high 32 = 0), so the 64-bit scaled add below needs
		// movsxd for negative indexes (p[-3]). An unsigned int index must stay
		// zero-extended (large indexes >= 2^31 address real memory), and a long
		// index is already full width.
		var iw int
		var is bool
		var ip bool
		if idxReg != "" {
			// F2 direct path: the index type comes from the variable's own
			// declaration, not from an evaluation.
			if vi, ok := c.lookupVar(idxName); ok {
				iw = c.semWOf(vi.typ)
				is = vi.typ != nil && vi.typ.Kind == frontend.KInt && vi.typ.Signed
				ip = vi.ptr
			}
		} else {
			iw, is, ip = c.resW, c.resSigned, c.resPtr
		}
		islot := 0
		if idxReg == "" {
			// T1.6 (C4): a ptrCapable index holds pointer bits; the movsxd below
			// is skipped so the full 64-bit index reaches the scaled add.
			c.tmpDepth++
			islot = c.tmpSlot(c.tmpDepth)
			c.emit("mov [rbp%+d], rax", islot)
		}
		if id, ok := n.Base.(*frontend.Ident); ok {
			// Local scope wins (C block scoping); only then file-scope names.
			// A static local shadows a same-named TLS global, but a static
			// thread-local local still needs the segment reach.
			if vi, ok2 := c.lookupVar(id.Name); ok2 {
				if vi.typ.IsArray() {
					c.emit("lea r10, [rbp%+d]", vi.off)
				} else {
					c.loadVar(vi) // rax = pointer value
					c.emit("mov r10, rax")
				}
			} else if lab, ok3 := c.lookupStatic(id.Name); ok3 {
				if _, isTLS := c.tlsVars[id.Name]; !isTLS {
					/* A static local or a global is reached by its ADDRESS only
					 * when it is an array (element 0 decays to the object's
					 * address). For a scalar the index applies to the scalar's
					 * VALUE, so a pointer has to be loaded first -- `lea` on the
					 * slot would hand back the 8-byte slot, not the string it
					 * points at. Automatic variables above do exactly this with
					 * loadVar; a static must do the same by hand. The declared
					 * type of a static local is recorded under its label in
					 * globalTyp (see the static-local branch of genFunc). */
					st := c.globalTyp[lab]
					if st != nil && !st.IsArray() && st.Kind == frontend.KPtr {
						c.emit("mov r10, [rip+%s]", lab)
					} else {
						c.emit("lea r10, [rip+%s]", lab)
					}
				} else if err := c.genTLSAddr(id.Name); err != nil {
					return err
				}
			} else if _, okTLS := c.tlsVars[id.Name]; okTLS {
				// Thread-local array/struct: reach it through the segment.
				if err := c.genTLSAddr(id.Name); err != nil {
					return err
				}
			} else if c.globals[id.Name] {
				/* Same reasoning for a file-scope object. */
				c.useLibGlobal(id.Name)
				glab := c.globalLab[id.Name]
				gt := c.globalTyp[id.Name]
				if gt != nil && !gt.IsArray() && gt.Kind == frontend.KPtr {
					c.emit("mov r10, [rip+%s]", glab)
				} else {
					c.emit("lea r10, [rip+%s]", glab)
				}
			} else {
				return fmt.Errorf("undefined variable %q", id.Name)
			}
		} else if u, ok := n.Base.(*frontend.Unary); ok && u.Op == "*" {
			if _, err := c.genExprT(u.E); err != nil {
				return err
			}
			c.emit("mov r10, rax")
		} else if ix, ok := n.Base.(*frontend.Index); ok {
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
		if idxReg != "" {
			// F2: consume the index from its home register -- the value is
			// live in the callee-save across the base evaluation (which may
			// call). A signed int sign-extends from the low dword; an unsigned
			// int zero-extends (mov r11d); a long/pointer index is full width.
			l32 := low32Reg(idxReg)
			switch {
			case iw == 4 && is && !ip:
				c.emit("movsxd r11, %s", l32)
			case iw == 4 && !is:
				c.emit("mov r11d, %s", l32)
			case iw == 8 || ip:
				// long / pointer / ptrCapable index: full 64-bit value.
				c.emit("mov r11, %s", idxReg)
			case iw == 1 || iw == 2:
				// narrow index: the home register carries only the raw low
				// 8/16 bits (loadVar extends on read), so copy then sign/zero
				// extend -- a plain full-width copy would drag garbage upper bits
				// into the 64-bit SIB index (same bug class as a non-F2 short load).
				c.emit("mov r11, %s", idxReg)
				c.extendRegNarrow("r11", iw, is)
			}
		} else if iw == 4 && is && !ip {
			// N11: sign-extend a signed int index from the slot's low dword
			// (the spilled materialized value) for the 64-bit scale-and-add.
			c.emit("movsxd r11, dword [rbp%+d]", islot)
		} else {
			c.emit("mov r11, [rbp%+d]", islot)
		}
		if idxReg == "" {
			c.tmpDepth--
		}
		// byte offset = index * element_width. Char elements pack one byte
		// per slot (string literals, char arrays, char* buffers); everything
		// else keeps the 8-byte slot stride. goa supports imul-with-immediate,
		// so a single scaled multiply replaces the old triple doubling-add.
		ew := c.elemWidthOf(n.Base)
		// Scale 1/2/4/8 are the only encodable SIB scales; for those a single
		// scaled-index LEA computes base+index*ew with no separate multiply or
		// add (this is what sibFold / LLVM produce for a bare var index too,
		// e.g. s[n] -> lea r10,[r10+r11*1]). Any other element width -- rare,
		// only for non-power-of-two structs -- keeps the imul/add fallback.
		scaledOk := ew == 1 || ew == 2 || ew == 4 || ew == 8
		if idxReg != "" && idxConst != 0 {
			disp := idxConst * int64(ew)
			if scaledOk {
				// F2-EXT: fold var±const directly into a scaled-index LEA,
				// recovering the single-instruction addressing sibFold would
				// otherwise produce for a bare var index (a[j+1] -> lea into
				// [base + r11*ew ± disp]) instead of materialising var±const
				// as a spilled value.
				sign := "+"
				adisp := disp
				if disp < 0 {
					sign = "-"
					adisp = -disp
				}
				c.emit("lea r10, [r10 + r11*%d %s %d]", ew, sign, adisp)
			} else {
				c.emit("imul r11, %d", ew)
				c.emit("add r10, r11")
				if disp >= 0 {
					c.emit("add r10, %d", disp)
				} else {
					c.emit("sub r10, %d", -disp)
				}
			}
		} else {
			if scaledOk {
				c.emit("lea r10, [r10 + r11*%d]", ew)
			} else {
				c.emit("imul r11, %d", ew)
				c.emit("add r10, r11")
			}
		}
		return nil
	case *frontend.MemberExpr:
		// Resolve the struct/union type behind the base so we can look the
		// member offset up. For "->" the base is a pointer to struct.
		var st *frontend.Type
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
		if st == nil || (st.Kind != frontend.KStruct && st.Kind != frontend.KUnion) {
			return fmt.Errorf("line %d: member access %q on non-struct type %v", n.Line, n.Name, st)
		}
		off := 0
		found := false
		for _, m := range st.Members {
			if m.Name == n.Name {
				off = m.Offset
				if m.BitWidth > 0 {
					// A bit-field member: r10 points at the START of its
					// storage unit after the add below; the field lives at
					// bit offset BitOff inside it. Consumers (frontend.MemberExpr
					// loads, assignment stores, inc/dec) branch on
					// lvBitWidth and use the bit address, not a plain load.
					c.lvBitWidth = m.BitWidth
					c.lvBitOff = m.BitOff
					c.lvBitUnit = frontend.Sizeof(m.Type)
					c.lvBitSigned = m.Type.Kind == frontend.KInt && m.Type.Signed
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
	return fmt.Errorf("expression is not an lvalue (%T)", e)
}

// ptrArithElem returns the element type of a pointer-arithmetic expression
// ("p + n", "p - n" or "n + p"): the result still points into the same array,
// so its element type is the pointer operand's. It returns nil for anything
// that is not pointer arithmetic -- including "p - q", whose result is an
// integer, not a pointer.
func (c *CG) ptrArithElem(e frontend.Expr) *frontend.Type {
	b, ok := e.(*frontend.Binary)
	if !ok || (b.Op != "+" && b.Op != "-") {
		return nil
	}
	for _, sub := range []frontend.Expr{b.L, b.R} {
		if t := c.exprType(sub); t != nil && (t.IsPtr() || t.IsArray()) && t.Elem != nil {
			return t.Elem
		}
	}
	return nil
}

// elemClassOf returns the codegen class (int vs double) of the value obtained
// by dereferencing / indexing e. It inspects the structured type behind local
// variables and the shape of nested dereferences / subscripts.
func (c *CG) elemClassOf(e frontend.Expr) frontend.CType {
	switch n := e.(type) {
	case *frontend.Ident:
		vi, ok := c.lookupVar(n.Name)
		if !ok {
			// A static local shadows a same-named global; its type lives
			// under its .data label, not the source name.
			if lab, ok2 := c.lookupStatic(n.Name); ok2 {
				if gt := c.globalTyp[lab]; gt != nil {
					if gt.Kind == frontend.KDouble {
						return frontend.TDouble
					}
					if gt.IsPtr() && gt.Elem != nil {
						return gt.Elem.Class()
					}
					if gt.IsArray() && gt.Elem != nil {
						return gt.Elem.Class()
					}
				}
				return frontend.TInt
			}
			if gt := c.globalTyp[n.Name]; gt != nil {
				if gt.Kind == frontend.KDouble {
					return frontend.TDouble
				}
				if gt.IsPtr() && gt.Elem != nil {
					return gt.Elem.Class()
				}
				if gt.IsArray() && gt.Elem != nil {
					return gt.Elem.Class()
				}
			}
			return frontend.TInt
		}
		if vi.typ.Kind == frontend.KDouble {
			return frontend.TDouble
		}
		if vi.typ.IsPtr() && vi.typ.Elem != nil {
			return vi.typ.Elem.Class()
		}
		if vi.typ.IsArray() && vi.typ.Elem != nil {
			return vi.typ.Elem.Class()
		}
		return frontend.TInt
	case *frontend.Unary:
		if n.Op == "&" {
			// Mirror elemWidthOf: *(&d) on a double is a double element and
			// has to load through xmm0, not through rax.
			if t := c.exprType(n.E); t != nil {
				if (t.IsPtr() || t.IsArray()) && t.Elem != nil {
					return t.Elem.Class()
				}
				return t.Class()
			}
			return frontend.TInt
		}
		if n.Op == "*" {
			return c.elemClassOf(n.E)
		}
		return frontend.TInt
	case *frontend.Index:
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
		return frontend.TInt
	case *frontend.MemberExpr:
		if mt := c.memberType(n.Base, n.Name); mt != nil {
			return mt.Class()
		}
		return frontend.TInt
	case *frontend.IncDecExpr:
		return c.elemClassOf(n.E)
	case *frontend.CastExpr:
		// A cast re-types what a subsequent dereference/subscript reads:
		// *(double *)p is a double element, *(int *)p an int one. Without
		// this, both were reported as frontend.TInt and the double load used the
		// integer path (mov instead of movsd).
		if n.Typ != nil && (n.Typ.IsPtr() || n.Typ.IsArray()) && n.Typ.Elem != nil {
			return n.Typ.Elem.Class()
		}
		return frontend.TInt
	}
	return frontend.TInt
}

// tlsAlignOf returns the alignment used to lay out a thread-local variable in
// the .tls section. goc keeps every variable in an 8-byte-aligned slot (scalars
// are emitted as an 8-byte dq by emitGlobalVar), so all TLS variables are simply
// 8-aligned -- this matches how they are later reached through the fs/gs segment.
func (c *CG) tlsAlignOf(t *frontend.Type) int {
	return 8
}

// tlsAlignedSize returns the exact number of bytes the linker will write for t,
// so the per-variable offset bookkeeping stays in lockstep with the emitted
// .tls image. It is the link package's rule, not a second copy of it: the
// offsets computed here are the ones the .tls section has to match.
// tlsAlignedSize is the stack slot size for t. It forwards to the link package
// rather than repeating the rule: the generator lays out a local and the stub
// binds the same variable's address, and two independent copies of "how wide is
// this type" would disagree silently -- the label would land off its own bytes
// and the program would corrupt memory rather than fail to link.
func (c *CG) tlsAlignedSize(t *frontend.Type) int {
	return link.TLSAlignedSize(t)
}

// tlsPlace aligns the running .tls offset to t's alignment, records the variable
// there, and returns its offset, advancing the running offset past it.
func (c *CG) tlsPlace(t *frontend.Type) int {
	align := c.tlsAlignOf(t)
	off := (c.tlsBytes + align - 1) &^ (align - 1)
	c.tlsBytes = off + c.tlsAlignedSize(t)
	return off
}

// typeWidth returns the byte width used to lay out / step over a value of type
// t: char=1, short=2, int/long=4/8, double/pointer=8, array=element*len,
// struct/union=its computed Size. The rule itself lives in the link package,
// which lays out the data sections with it: a second copy here could disagree
// with the image and put every access to a struct at the wrong offset.
// typeWidth is the size in bytes of a type as code generation sees it. Like
// tlsAlignedSize it forwards to link.TypeWidth: the width decides both what a
// load emits and where the symbol is bound, so the two phases must agree by
// construction rather than by luck.
func (c *CG) typeWidth(t *frontend.Type) int {
	return link.TypeWidth(t)
}

// exprIsPointer reports whether e has pointer type.
func (c *CG) exprIsPointer(e frontend.Expr) bool {
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
func (c *CG) memberType(base frontend.Expr, name string) *frontend.Type {
	t := c.exprType(base)
	// "->" on an array-typed base is legal C too: "printbuffer buffer[1]"
	// decays to a pointer, so "buffer->field" addresses element 0. Both the
	// pointer and the array spelling therefore unwrap to the element type.
	if t != nil && (t.Kind == frontend.KPtr || t.Kind == frontend.KArr) && t.Elem != nil {
		t = t.Elem
	}
	if t == nil || (t.Kind != frontend.KStruct && t.Kind != frontend.KUnion) {
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
func (c *CG) exprType(e frontend.Expr) *frontend.Type {
	switch n := e.(type) {
	case *frontend.CompoundLit:
		// The unnamed object's own declared type (arrays included: callers
		// decide between value-address and decay handling).
		return n.Typ
	case *frontend.VaArgExpr:
		// va_arg(ap, T) has the type T the caller asked for. Without this a
		// `long double v = va_arg(ap, long double)` read a nil type off the
		// initialiser, failed the isLDExpr test, and went down the scalar
		// path -- to_ll/from_ll round-tripping 16 bytes of binary128 through
		// an int64 and destroying the value.
		return n.Typ
	case *frontend.GenericExpr:
		// The selection's type is the type of the chosen branch (the
		// controlling expression's own type is irrelevant after the pick).
		if n.Chosen != nil {
			return c.exprType(n.Chosen)
		}
		return nil
	case *frontend.CondExpr:
		// A conditional's type is the common type of its arms. This branch
		// exists for the WIDE types only -- int/double arms still come back
		// nil, which is what every existing caller was written against --
		// because a nil here is not neutral for them: "c = (a < 0 ? -a : a)"
		// with a long double `a` read a nil type off the right-hand side,
		// failed isLDExpr, and took the SCALAR assignment path, which round-
		// trips 16 bytes of binary128 through an int64 (to_ll/from_ll).
		// 0.5 came out as 0.0. The arms' own codegen already leaves the
		// value at r10 with resBig set; the type is all that was missing.
		tt, et := c.exprType(n.Then), c.exprType(n.Else)
		if isLD(tt) || isLD(et) || c.isLDExpr(n.Then) || c.isLDExpr(n.Else) {
			return frontend.LongDoubleType()
		}
		if frontend.IsBig(tt) || frontend.IsBig(et) {
			if frontend.IsBig(tt) {
				return tt
			}
			return et
		}
		return nil
	case *frontend.Ident:
		if vi, ok := c.lookupVar(n.Name); ok {
			return vi.typ
		}
		// A static local's type is registered under its .data label, not its
		// source name (which may even collide with a file-scope global).
		if lab, ok := c.lookupStatic(n.Name); ok {
			return c.globalTyp[lab]
		}
		// A function designator decays to a pointer to that function.
		if fd, ok := c.funcDefs[n.Name]; ok {
			ft := frontend.FuncType(fd.Ret, fd.ParamTypes)
			ft.Variadic = fd.Variadic
			return frontend.PtrType(ft)
		}
		return c.globalTyp[n.Name]
	case *frontend.Unary:
		// A unary '-'/~ on a _BitInt keeps the operand's own type. Without
		// this branch exprType returned nil and the genExprT big-value
		// intercept never fired for unary expressions.
		if (n.Op == "-" || n.Op == "~") && frontend.IsBig(c.exprType(n.E)) {
			return c.exprType(n.E)
		}
		// A unary '-' on a long double keeps the operand's type, for the
		// same reason (the intercept must fire and goc_tf_neg must run).
		if n.Op == "-" && isLD(c.exprType(n.E)) {
			return c.exprType(n.E)
		}
		// ...and the same for a NEGATED long double literal ("-2.5L"): the
		// literal is in no type table, so the branch above misses it and
		// the expression's type came out nil -- the value then fell into
		// genUnary's integer `neg` and "-2.5L" evaluated to +2.0.
		if n.Op == "-" && c.isLDExpr(n.E) {
			return frontend.LongDoubleType()
		}
		if n.Op == "*" {
			if t := c.exprType(n.E); t != nil && t.IsPtr() {
				return t.Elem
			}
		}
		if n.Op == "&" {
			// "&x" is a pointer to x's type. Without this branch every
			// "(&s)->member" resolved to a nil base type and failed with
			// "member access on non-struct type" -- the C spelling a macro
			// like `#define at(b) ((b)->field)` produces when it is handed
			// "&local" instead of a pointer variable.
			//
			// A function designator already decays to a pointer to the
			// function, so "&f" must keep that same type instead of
			// becoming a pointer to a pointer to a function.
			if t := c.exprType(n.E); t != nil {
				if t.IsFunc() {
					return frontend.PtrType(t)
				}
				if t.IsPtr() && t.Elem != nil && t.Elem.IsFunc() {
					return t
				}
				return frontend.PtrType(t)
			}
		}
	case *frontend.Index:
		if t := c.exprType(n.Base); t != nil {
			if t.IsPtr() || t.IsArray() {
				return t.Elem
			}
		}
	case *frontend.Binary:
		// Pointer arithmetic keeps a pointer type, so "(p + 1) - base" is
		// still pointer-minus-pointer. Without this branch exprType returned
		// nil for any frontend.Binary, codegen classified the left operand as an
		// integer and took the "integer - pointer" swap path instead -- the
		// difference came out negated, and cJSON's parse_string decided
		// every escape sequence ran off the end of its buffer.
		return c.binaryType(n)
	case *frontend.IncDecExpr:
		return c.exprType(n.E)
	case *frontend.TmpLoad:
		// Internal node produced only by genCompoundAssign: the type is the
		// parked lvalue's static type.
		return n.Typ
	case *frontend.MemberExpr:
		return c.memberType(n.Base, n.Name)
	case *frontend.CastExpr:
		return n.Typ
	case *frontend.Call:
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
	case *frontend.IndirectCall:
		// A UFCS method call types like the direct call it was rewritten to.
		if n.UFCS != nil {
			if fd, ok := c.funcDefs[n.UFCS.Name]; ok {
				return fd.Ret
			}
			return nil
		}
		if ft := frontend.FuncTypeOf(c.exprType(n.Fn)); ft != nil {
			return ft.Ret
		}
		return nil
	}
	return nil
}

// binaryType mirrors the checker's rules (checkBinary) so codegen can
// classify an expression that is itself arithmetic: a _BitInt operation
// yields its converted bit-precise result type (frontend.BigArithResult), a pointer
// plus or minus an integer is still a pointer, and pointer minus pointer is
// an integer difference. Anything else is left nil, which every caller
// already treats as "ordinary integer".
func (c *CG) binaryType(n *frontend.Binary) *frontend.Type {
	lt := c.exprType(n.L)
	rt := c.exprType(n.R)
	// C23 bit-precise arithmetic: the result type comes from the usual
	// arithmetic conversions over the _BitInt operands.
	if frontend.IsBig(lt) || frontend.IsBig(rt) {
		switch n.Op {
		case "+", "-", "*", "/", "%", "<<", ">>", "&", "|", "^":
			if (lt == nil || lt.IsIntClass() || frontend.IsBig(lt)) && (rt == nil || rt.IsIntClass() || frontend.IsBig(rt)) {
				lt2 := lt
				if lt2 == nil {
					lt2 = frontend.IntType()
				}
				rt2 := rt
				if rt2 == nil {
					rt2 = frontend.IntType()
				}
				return frontend.BigArithResult(n.Op, lt2, rt2)
			}
		}
		return nil
	}
	// Long double arithmetic: the result is long double whatever the other
	// operand is (the usual arithmetic conversions convert it). A literal
	// operand is in no type table, so a 1.5L operand is recognised by its
	// own flag -- without that, "LITS(1.0) / LITS(3.0)" would classify as an
	// integer operation and the scalar path would divide addresses.
	if n.Op == "+" || n.Op == "-" || n.Op == "*" || n.Op == "/" {
		ldOperand := func(t *frontend.Type, e frontend.Expr) bool {
			if isLD(t) {
				return true
			}
			if nl, ok := e.(*frontend.NumLit); ok {
				return nl.IsLongDouble
			}
			return false
		}
		if ldOperand(lt, n.L) || ldOperand(rt, n.R) {
			return frontend.LongDoubleType()
		}
	}
	if n.Op != "+" && n.Op != "-" {
		return nil
	}
	// A numeric literal is not in any type table, so exprType reports nil for
	// it; on one side of a pointer operation that missing type is the
	// integer it actually is.
	intish := func(t *frontend.Type) bool { return t == nil || (!t.IsPtr() && !t.IsArray()) }
	if lt != nil && lt.IsPtr() && rt != nil && rt.IsPtr() {
		if n.Op == "-" {
			return frontend.IntType() // pointer difference
		}
		return nil
	}
	// An array operand decays to a pointer to its element, exactly as it does
	// in every other value context: "a + 2" has type int*, not int[4]. Keeping
	// the array type made "(a + 2)[0]" look like an array lvalue and genLValue
	// tried to take its address ("expression is not an lvalue").
	decay := func(t *frontend.Type) *frontend.Type {
		if t != nil && t.IsArray() && t.Elem != nil {
			return frontend.PtrType(t.Elem)
		}
		return t
	}
	if lt != nil && (lt.IsPtr() || lt.IsArray()) && intish(rt) {
		return decay(lt)
	}
	if rt != nil && (rt.IsPtr() || rt.IsArray()) && intish(lt) {
		return decay(rt)
	}
	return nil
}

// elemWidthOf returns the byte width of the element referenced by a pointer or
// array expression e (1 for char, 2 for short, 4 for int, 8 otherwise). For a
// bare scalar (the value itself, not a container) it returns that scalar's
// storage width -- but the common use is indexing a pointer/array, where the
// stride must be the *element* width, never the 8-byte pointer width.
func (c *CG) elemWidthOf(e frontend.Expr) int {
	switch n := e.(type) {
	case *frontend.StrLit:
		// A string literal is a char[N] (or wchar_t[N] when wide) array:
		// indexing it ("hello"[i] / L"hi"[i]) strides and loads one element,
		// not the 8-byte default a bare expression gets. Without this case
		// "hello"[0] read a whole quadword of neighbouring memory (P0.8).
		if n.Wide {
			return 2
		}
		return 1
	case *frontend.Ident:
		vi, ok := c.lookupVar(n.Name)
		if !ok {
			// A static local shadows a same-named global; its type lives
			// under its .data label, not the source name.
			if lab, ok2 := c.lookupStatic(n.Name); ok2 {
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
	case *frontend.Unary:
		if n.Op == "&" {
			// *(&x) -- the shape every "atomic_load(&x)"-style macro expands
			// to -- reads x itself, so the pointee of &x is x's own type.
			// Falling through to the 8-byte default made *(&s.c) on a char
			// member read three neighbours as well (67305985 instead of 1).
			if t := c.exprType(n.E); t != nil {
				if (t.IsPtr() || t.IsArray()) && t.Elem != nil {
					return c.typeWidth(t.Elem)
				}
				return c.typeWidth(t)
			}
			return 8
		}
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
	case *frontend.Index:
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
	case *frontend.MemberExpr:
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
	case *frontend.IncDecExpr:
		return c.elemWidthOf(n.E)
	case *frontend.Binary:
		// Pointer arithmetic keeps the pointer's element type, so
		// "(p + n)[i]" strides and loads that element -- 1 byte for
		// "unsigned char *", not the 8-byte default. This is the shape a
		// macro like `#define at(b) ((b)->content + (b)->offset)` expands
		// to, and getting it wrong made every "switch (at(b)[i])" read a
		// whole quadword and fall through to its default label.
		if et := c.ptrArithElem(n); et != nil {
			return c.typeWidth(et)
		}
		return 8
	case *frontend.CastExpr:
		// A cast to a pointer/array type changes what a subsequent
		// dereference or subscript reads: *(int *)p loads 4 bytes at the
		// pointed-to address, NOT the 8-byte pointer slot, and
		// ((int *)p)[i] strides 4 bytes. Only a cast to a scalar (which
		// dereferencing cannot apply to) falls back to its own width.
		if n.Typ != nil && (n.Typ.IsPtr() || n.Typ.IsArray()) && n.Typ.Elem != nil {
			return c.typeWidth(n.Typ.Elem)
		}
		return c.typeWidth(n.Typ)
	}
	return 8
}

// ptrElemWidth returns the byte stride of the element a pointer points at
// (used to scale pointer arithmetic). A void pointer strides one byte. An
// array used as a value has decayed to a pointer to element 0, so its stride
// is the array element width too.
func (c *CG) ptrElemWidth(t *frontend.Type) int {
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
func (c *CG) elemSignedOf(e frontend.Expr) bool {
	if _, ok := e.(*frontend.StrLit); ok {
		return true // string literal elements are char; goc's char is signed
	}
	t := c.exprType(e)
	if t == nil {
		return false
	}
	if t.IsPtr() && t.Elem != nil {
		return t.Elem.Kind == frontend.KInt && t.Elem.Signed
	}
	if t.IsArray() && t.Elem != nil {
		return t.Elem.Kind == frontend.KInt && t.Elem.Signed
	}
	if t.Kind == frontend.KInt {
		return t.Signed
	}
	return false
}

// low32Reg returns the 32-bit name of a 64-bit register (rax->eax, r12->r12d).
// Used wherever a value is sign/zero-extended from the low dword of a
// register-cached variable (F2 index consumption). rbx's low dword is ebx,
// NOT "rbxd".
func low32Reg(r string) string {
	if len(r) >= 2 && r[0] == 'r' && r[1] >= 'a' && r[1] <= 'z' {
		return "e" + r[1:]
	}
	return r + "d"
}

// genLoadElem emits code that loads the value at the address held in reg into
// rax (int-class, width-aware) or xmm0 (double). For a 1-byte element the byte
// is zero- or sign-extended into rax depending on signedness.
func (c *CG) genLoadElem(reg string, width int, class frontend.CType, signed bool) {
	if class == frontend.TDouble {
		if width == 4 {
			// A float element/member: 4 bytes on the wire, widened to the
			// double every expression carries.
			c.emit("movss xmm0, [%s]", reg)
			c.emit("cvtss2sd xmm0, xmm0")
		} else {
			c.emit("movsd xmm0, [%s]", reg)
		}
		c.resTyp = frontend.TDouble
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
		// T1.6: a dword load already zero-extends to rax, which IS the
		// materialized int (low 32 valid, high 32 = 0). No shl/sar sign
		// extension here any more; consumers that need the signed 64-bit
		// value (double, (long), int-vs-long compare, printf slot) movsxd.
		c.emit("mov eax, [%s]", reg)
		c.resW = 4
	default:
		c.emit("mov rax, [%s]", reg)
		c.resW = 8
	}
	c.resTyp = frontend.TInt
	c.resSigned = signed
}

// genStoreElem emits code that stores the value currently in rax (int-class) or
// xmm0 (double) into the address held in reg, honouring the element width.
// valW/valSigned are the VALUE's own width/signedness captured when it was
// evaluated -- NOT c.resW/c.resSigned at the store point, which the intervening
// lvalue-address computation (array indexing etc.) may have clobbered.
func (c *CG) genStoreElem(reg string, width int, class frontend.CType, valW int, valSigned bool) {
	if class == frontend.TDouble {
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
	// T1.6 (N17): a materialized signed int (resW==4, low 32 valid, high 32=0)
	// being stored into an 8-byte (long/pointer) slot must be sign-extended
	// first, or a negative int lands as a huge positive when the slot is later
	// read back as a 64-bit signed value. This covers array initialisers and any
	// other path that lowers an int into a wider slot. Unsigned ints are already
	// zero-extended (high 32 = 0), and width-4 int slots keep the materialised
	// form, so only the signed widening case needs this. A pointer value has
	// valW==8 and must NEVER be narrowed (the atexit crash: a function pointer
	// truncated to 32 bits when the stale c.resW of an index expression was
	// consulted instead of the value's own width).
	if width == 8 && valW == 4 && valSigned {
		c.emit("movsxd rax, eax")
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
	c.genLoadElem("r10", unitW, frontend.TInt, false) // rax = zero-extended storage unit
	c.emit("shr rax, %d", bitOff)                     // field now in the low bitW bits
	if signed {
		// Sign-extend: move the field to the top, then arithmetic-shift back.
		c.emit("shl rax, %d", 64-bitW)
		c.emit("sar rax, %d", 64-bitW)
	} else {
		// Zero-extend: shift the field to the top and back.
		c.emit("shl rax, %d", 64-bitW)
		c.emit("shr rax, %d", 64-bitW)
	}
	c.resTyp = frontend.TInt
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
	c.genLoadElem("r10", unitW, frontend.TInt, false)
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
	c.genStoreElem("r10", unitW, frontend.TInt, 8, false)
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
	// A wide aggregate (a _BitInt(524288) is 64 KiB, a big struct can be
	// larger still) would expand into thousands of MOV pairs. Past a few
	// words, call memcpy instead. r10/r11 are volatile across a call, so
	// they are parked in scratch slots and restored afterwards.
	if n > 64 {
		ar := c.argRegs()
		c.tmpDepth++
		s1 := c.tmpSlot(c.tmpDepth)
		c.tmpDepth++
		s2 := c.tmpSlot(c.tmpDepth)
		c.emit("mov [rbp%+d], r10", s1)
		c.emit("mov [rbp%+d], r11", s2)
		c.emit("mov %s, %s", ar[0], dst)
		c.emit("mov %s, %s", ar[1], src)
		c.emit("mov %s, %d", ar[2], n)
		c.need["memcpy"] = true
		c.emitCall("memcpy")
		c.emit("mov r10, [rbp%+d]", s1)
		c.emit("mov r11, [rbp%+d]", s2)
		c.tmpDepth -= 2
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

// isAgg reports whether t is a struct/union aggregate (or a C23 _BitInt,
// which rides the same by-address value model), i.e. a value that is never
// loaded into a register but always handled by address + copyBytes. The rule
// is the link package's, so a global laid out there and the code addressing it
// here cannot disagree about what counts as an aggregate.
func isAgg(t *frontend.Type) bool {
	return link.IsAgg(t)
}

// regCapable reports whether a value of type t may live in a callee-save GPR
// rather than a frame slot: integer-class scalars only. Floating values ride
// in XMM registers, arrays and aggregates own contiguous storage, and a
// _BitInt travels by address. This is the single rule shared by the local and
// the parameter register allocators (T2.1 R2/R3) so the two can never drift.
func regCapable(t *frontend.Type) bool {
	// An _Atomic object must live in memory: its read-modify-writes are
	// LOCK-prefixed instructions on its address, and a register copy would
	// make them invisible to any other thread (or to a signal handler) that
	// observes the object.
	return t != nil && !t.IsArray() && !t.IsFloating() && !t.Atomic &&
		t.Kind != frontend.KStruct && t.Kind != frontend.KUnion && t.Kind != frontend.KBitInt
}

// t21BodyStats is what R2 needs to know about a function body: how big it is,
// whether it calls anything (a leaf is the only shape inlineCalls will
// expand), and which names it calls.
type t21BodyStats struct {
	stmts   int
	hasCall bool
	// hasAgg is set when the function declares or takes an array, struct,
	// union or _BitInt. Such a function can acquire a call the AST never
	// shows: genBraceInit / copyBytes lower big aggregate copies and zero
	// fills into emitCall("memcpy") / emitCall("memset"), and those take
	// their 3rd/4th arguments in r8/r9 on Win64.
	hasAgg  bool
	callees map[string]bool
}

// t21HasAggType reports whether t is aggregate data in the sense above.
func t21HasAggType(t *frontend.Type) bool {
	return t != nil && (t.IsArray() || t.IsStruct() || t.IsUnion() || t.Kind == frontend.KBitInt)
}

// NOTE (measured, 2026-10-03): the implicit `static const char __func__[]`
// the parser prepends to every function body is a char ARRAY, so it makes
// t21HasAggType -- and therefore bst.hasAgg -- true for every function in the
// program, which silently disables the r8/r9 leaf pool extension (its only
// consumer) everywhere. Exempting it by name looks like an obvious bug fix,
// but it was measured and REVERTED: enabling the extension puts fib_iter's
// loop bound in r8 and made the iterative fib benchmark 12.7% SLOWER
// (1.15s -> 1.30s) despite removing a memory reload per iteration. Do not
// re-enable without re-measuring on real workloads.
const injectedFuncIdent = "__func__"

// t21BodyScan walks f's body counting statements and collecting callees.
func t21BodyScan(f *frontend.FuncDecl) t21BodyStats {
	st := t21BodyStats{callees: map[string]bool{}}
	for _, pt := range f.ParamTypes {
		if t21HasAggType(pt) {
			st.hasAgg = true
		}
	}
	var walkS func(frontend.Stmt)
	var walkE func(frontend.Expr)
	walkE = func(e frontend.Expr) {
		switch n := e.(type) {
		case *frontend.Call:
			st.hasCall = true
			if n.Name != "" {
				st.callees[n.Name] = true
			}
			for _, a := range n.Args {
				walkE(a)
			}
		case *frontend.IndirectCall:
			// An indirect callee names nothing, but the function still is not
			// a leaf, so it is not an inline candidate.
			st.hasCall = true
			for _, a := range n.Args {
				walkE(a)
			}
		case *frontend.Binary:
			walkE(n.L)
			walkE(n.R)
		case *frontend.Unary:
			walkE(n.E)
		case *frontend.CastExpr:
			walkE(n.E)
		case *frontend.AssignExpr:
			walkE(n.Lhs)
			walkE(n.Rhs)
		case *frontend.IncDecExpr:
			walkE(n.E)
		case *frontend.Index:
			walkE(n.Base)
			walkE(n.Idx)
		case *frontend.MemberExpr:
			walkE(n.Base)
		case *frontend.CondExpr:
			walkE(n.Cond)
			walkE(n.Then)
			walkE(n.Else)
		case *frontend.CommaExpr:
			walkE(n.Left)
			walkE(n.Right)
		}
	}
	walkS = func(s frontend.Stmt) {
		switch n := s.(type) {
		case *frontend.Block:
			for _, st := range n.Stmts {
				walkS(st)
			}
		case *frontend.DeclList:
			st.stmts += len(n.Decls)
			for _, d := range n.Decls {
				if t21HasAggType(d.Typ) {
					st.hasAgg = true
				}
				if d.Init != nil {
					walkE(d.Init)
				}
			}
		case *frontend.DeclStmt:
			st.stmts++
			if t21HasAggType(n.Typ) {
				st.hasAgg = true
			}
			if n.Init != nil {
				walkE(n.Init)
			}
		case *frontend.ExprStmt:
			st.stmts++
			walkE(n.E)
		case *frontend.AssignStmt:
			st.stmts++
			walkE(n.Lhs)
			walkE(n.Rhs)
		case *frontend.IfStmt:
			st.stmts++
			walkE(n.Cond)
			walkS(n.Then)
			if n.Else != nil {
				walkS(n.Else)
			}
		case *frontend.WhileStmt:
			st.stmts++
			walkE(n.Cond)
			walkS(n.Body)
		case *frontend.DoWhileStmt:
			st.stmts++
			walkS(n.Body)
			walkE(n.Cond)
		case *frontend.ForStmt:
			st.stmts++
			if n.Init != nil {
				walkS(n.Init)
			}
			if n.Cond != nil {
				walkE(n.Cond)
			}
			if n.Post != nil {
				walkE(n.Post)
			}
			walkS(n.Body)
		case *frontend.ReturnStmt:
			st.stmts++
			if n.E != nil {
				walkE(n.E)
			}
		case *frontend.SwitchStmt:
			st.stmts++
			walkE(n.Src)
			walkS(n.Body)
		case *frontend.LabelStmt:
			walkS(n.Stmt)
		}
	}
	walkS(f.Body)
	return st
}

// t21InlineLikely reports whether f is one of the small call-free functions
// inlineCalls expands at -O1, AND somebody actually calls it. Register-homing
// such a function's parameters puts a `mov rbx, rcx` in its body, and the
// inliner rightly refuses any template that writes a callee-save register (it
// would corrupt the caller's own register-homed locals). For a function that
// is about to be inlined, dropping a parameter reload is worth far less than
// keeping the call itself expandable -- inlining deletes the whole body.
//
// Both conditions matter, and the second is why a plain size threshold is the
// wrong tool. Measured on bench2, raising the statement bound made things
// WORSE, not better (4 -> net 0, 12 -> +208, 30 -> +214 instructions): a wide
// threshold parameter-homes functions that were only medium-sized, and the
// inlining it destroys costs more than the reloads it saves. So decide by
// call graph instead: an uncalled function is never inlined and is therefore
// always safe, a caller (recursive or not) is never a leaf, and only a small
// leaf that is actually called is at risk.
func (c *CG) t21InlineLikely(f *frontend.FuncDecl, st t21BodyStats) bool {
	const maxInlineStmts = 4
	if st.hasCall || st.stmts > maxInlineStmts {
		return false // not leaf, or too big to be worth inlining anyway
	}
	return c.t21Called[f.Name]
}

// zeroBytes emits code that stores n zero bytes at the memory pointed to by
// dst (a register holding an address). Used to default-initialise struct and
// union locals that have no initialiser.
func (c *CG) zeroBytes(dst string, n int) {
	if n <= 0 {
		return
	}
	// Wide aggregates go through memset (see copyBytes) -- a _BitInt(524288)
	// zeroed with MOVs would be 8k instructions per declaration.
	if n > 64 {
		ar := c.argRegs()
		c.tmpDepth++
		s1 := c.tmpSlot(c.tmpDepth)
		c.emit("mov [rbp%+d], r10", s1)
		c.emit("mov %s, %s", ar[0], dst)
		c.emit("mov %s, 0", ar[1])
		c.emit("mov %s, %d", ar[2], n)
		c.need["memset"] = true
		c.emitCall("memset")
		c.emit("mov r10, [rbp%+d]", s1)
		c.tmpDepth--
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
func (c *CG) genBraceInitLocal(t *frontend.Type, bi *frontend.BraceInit, off int) error {
	c.emit("lea r10, [rbp%+d]", off)
	c.zeroBytes("r10", c.typeWidth(t))
	// C23 _BitInt braced init: "{}" zero-fills; "{v}" converts the single
	// value. Designators do not apply (there are no members).
	if frontend.IsBig(t) {
		if len(bi.Elems) == 1 && bi.Elems[0].Desig == "" && bi.Elems[0].DesigIdx < 0 {
			if _, err := c.genExprT(bi.Elems[0].E); err != nil {
				return err
			}
			if err := c.ensureType(frontend.TInt); err != nil {
				return err
			}
			// N17: materialized signed int initialiser consumed by
			// bi_from_i64 must be sign-extended first.
			if c.resW == 4 && c.resSigned && t.Signed {
				c.emit("movsxd r11, eax")
			} else {
				c.emit("mov r11, rax")
			}
			c.emit("lea r10, [rbp%+d]", off)
			sg := int64(0)
			if t.Signed {
				sg = 1
			}
			c.callBigLib("__goclib_bi_from_i64", []bigArg{
				{reg: "r10"}, {reg: "r11"}, {imm: int64(bigWordsOf(t))}, {imm: sg},
			})
		} else if len(bi.Elems) > 1 {
			return fmt.Errorf("too many initialisers for %s", t)
		}
		return nil
	}
	return c.braceWalkLocal(t, bi, off)
}

// braceWalkLocal stores the explicit elements of bi for the aggregate t based
// at frame offset off. Every leaf lands at its layout offset; uncovered bytes
// were already zeroed by the caller.
func (c *CG) braceWalkLocal(t *frontend.Type, bi *frontend.BraceInit, off int) error {
	if t.IsArray() {
		ew := c.typeWidth(t.Elem)
		hasDesig := false
		for _, el := range bi.Elems {
			if el.DesigIdx >= 0 {
				hasDesig = true
				break
			}
		}
		for i, el := range bi.Elems {
			idx := el.DesigIdx
			if !hasDesig {
				idx = i
			}
			if idx < 0 || idx >= t.Len {
				continue
			}
			if err := c.braceElemLocal(t.Elem, el.E, off+idx*ew); err != nil {
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
				mi := frontend.MemberIndex(t, el.Desig)
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
		vis := frontend.PosMembers(t)
		for i, el := range bi.Elems {
			if i >= len(vis) {
				break
			}
			if el.DesigIdx >= 0 {
				return fmt.Errorf("array designator \"[%d] =\" is only valid in an array initialiser", el.DesigIdx)
			}
			if el.Desig != "" {
				return fmt.Errorf("cannot mix positional and designated (\".%s =\") initialisers", el.Desig)
			}
			m := vis[i]
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
		vis := frontend.PosMembers(t)
		var m *frontend.Member
		if len(vis) > 0 {
			m = vis[0]
		}
		if el.DesigIdx >= 0 {
			return fmt.Errorf("array designator \"[%d] =\" is only valid in an array initialiser", el.DesigIdx)
		}
		if el.Desig != "" {
			if mi := frontend.MemberIndex(t, el.Desig); mi >= 0 {
				m = t.Members[mi]
			}
		}
		if m == nil {
			return nil
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
func (c *CG) braceElemLocal(t *frontend.Type, e frontend.Expr, off int) error {
	if nbi, ok := e.(*frontend.BraceInit); ok {
		return c.braceWalkLocal(t, nbi, off)
	}
	w := c.typeWidth(t)
	if sl, ok := e.(*frontend.StrLit); ok && t.IsArray() && ((sl.Wide && t.Elem.Width == 2) || (!sl.Wide && t.Elem.IsChar())) {
		copied := len(sl.Bytes)
		if sl.Wide {
			copied += 2
		} else {
			copied += 1
		}
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
	if isLD(t) {
		// The tf intercept leaves the value's ADDRESS in r10 (a literal's
		// .rdata slot or a claimed carrier); the scalar store below would
		// push 8 bytes of xmm0 and leave 8 zero bytes -- observed as every
		// element of "long double vals[] = {...}" being garbage. Copy the
		// 16 bytes, then free the carrier exactly like tfOperand does.
		c.emit("lea r11, [rbp%+d]", off)
		c.copyBytes("r11", "r10", 16)
		c.releaseResBig()
		return nil
	}
	if err := c.ensureType(t.Class()); err != nil {
		return err
	}
	if t.Kind == frontend.KBool {
		c.normalizeBool()
	}
	c.emit("lea r10, [rbp%+d]", off)
	c.genStoreElem("r10", w, t.Class(), c.resW, c.resSigned)
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
func (c *CG) structSrcAddr(e frontend.Expr, t *frontend.Type) error {
	switch call := e.(type) {
	case *frontend.Call:
		if isAgg(t) {
			if _, err := c.genExprT(call); err != nil {
				return err
			}
			if c.resStruct {
				c.emit("lea r10, [rbp%+d]", c.tmpSlotBlock(c.resStructK, c.resStructSl))
				return nil
			}
			// A long double call arrives through the tf128 intercept, which
			// has already transferred the result buffer to the resBig
			// carrier -- r10 holds its address.
			if c.resBig {
				return nil
			}
			return fmt.Errorf("call %q does not produce a struct value", call.Name)
		}
	case *frontend.IndirectCall:
		if isAgg(t) {
			if _, err := c.genExprT(call); err != nil {
				return err
			}
			if c.resStruct {
				c.emit("lea r10, [rbp%+d]", c.tmpSlotBlock(c.resStructK, c.resStructSl))
				return nil
			}
			if c.resBig {
				return nil
			}
			return fmt.Errorf("function-pointer call does not produce a struct value")
		}
	}
	// A _BitInt or long double value expression: genExprT's intercepts
	// always leave the value address in r10 (lvalues directly; computed
	// values in their own claimed buffer). The ld test also looks at the
	// node itself: a literal is in no type table (exprType reports nil), so
	// "long double x = 1.5L" reaches here with a nil type.
	if frontend.IsBig(t) || isLD(t) || c.isLDExpr(e) {
		_, err := c.genExprT(e)
		return err
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

// releaseCallResultBuffer frees a struct-return call's result buffer after a
// SCALAR member of it has been loaded into a register ("f().x"): the value is
// in rax/xmm0, so the buffer is dead -- but the claim must be unwound HERE,
// while tmpDepth still includes it. Consumers roll tmpDepth back to their
// entry depth without checking the flag (genCompoundAssign's restore did),
// and the statement boundary then releases unconditionally -- so a claim left
// set was subtracted twice and drove tmpDepth negative. Negative slot indices
// handed out addresses inside live locals: "MIX(f(a).hi); r = g(b);" parked
// format pointers into r's own bytes and every half of r printed an image
// address. Aggregate members and array decays must NOT release here: their
// consumers still need the buffer's bytes through the address in r10.
func (c *CG) releaseCallResultBuffer() {
	c.releaseResStruct()
}

// lvalueWidth returns the byte width of the value stored at the lvalue e. For
// a bare scalar variable this is its slot width (char=1, short=2, int=4,
// pointer=8); for a member/pointer-deref/array-subscript it is the layout
// width of the stored object. A pointer-typed lvalue ALWAYS stores at pointer
// width (8): the value being stored is the pointer itself, not the thing it
// points at. Getting this wrong truncates `s.charPtrField = "str"` to a
// single byte (elemWidthOf returns the element *stride*, which is the right
// answer for p[i] indexing but wrong for storing the pointer).
func (c *CG) lvalueWidth(e frontend.Expr) int {
	if id, ok := e.(*frontend.Ident); ok {
		if vi, ok2 := c.lookupVar(id.Name); ok2 {
			if vi.typ.IsPtr() {
				return 8
			}
			if vi.typ.Kind == frontend.KInt || vi.typ.Kind == frontend.KFloat {
				// char/short stay narrow; int becomes an 8-byte slot so it can
				// hold a 64-bit pointer that was stored in an int variable.
				// float is a genuine 4-byte IEEE single.
				return c.slotWidth(vi.typ)
			}
			if vi.typ.Kind == frontend.KStruct || vi.typ.Kind == frontend.KUnion || vi.typ.Kind == frontend.KBitInt {
				return vi.typ.Size
			}
			return 8
		}
		// A static local shadows a same-named global; its type lives under
		// its .data label, not the source name.
		if lab, ok2 := c.lookupStatic(id.Name); ok2 {
			if gt := c.globalTyp[lab]; gt != nil {
				if gt.IsPtr() {
					return 8
				}
				if gt.Kind == frontend.KFloat {
					return 4
				}
				if gt.Kind == frontend.KStruct || gt.Kind == frontend.KUnion || gt.Kind == frontend.KBitInt {
					return gt.Size
				}
			}
			return 8
		}
		// A thread-local variable lives in .tls; its type lives in tlsVars.
		if t, ok2 := c.tlsVars[id.Name]; ok2 {
			if t.typ != nil && t.typ.Kind == frontend.KFloat {
				return 4
			}
			return 8
		}
		if c.globals[id.Name] {
			// A float global occupies 4 bytes in .data (emitted as four db
			// values); everything else is a quad.
			if gt := c.globalTyp[id.Name]; gt != nil && gt.Kind == frontend.KFloat {
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
func (c *CG) lvalueClass(e frontend.Expr) frontend.CType {
	if id, ok := e.(*frontend.Ident); ok {
		if vi, ok2 := c.lookupVar(id.Name); ok2 {
			return vi.typ.Class()
		}
		// A static local shadows a same-named global; its type lives under
		// its .data label, not the source name.
		if lab, ok2 := c.lookupStatic(id.Name); ok2 {
			if gt := c.globalTyp[lab]; gt != nil {
				return gt.Class()
			}
			return frontend.TInt
		}
		// A thread-local variable lives in .tls; its type lives in tlsVars.
		if t, ok2 := c.tlsVars[id.Name]; ok2 {
			if t.typ != nil {
				return t.typ.Class()
			}
			return frontend.TInt
		}
		if c.globals[id.Name] {
			if gt := c.globalTyp[id.Name]; gt != nil {
				return gt.Class()
			}
			return frontend.TInt
		}
		return frontend.TInt
	}
	return c.elemClassOf(e)
}

// genVaArg implements va_arg(ap, T): read 8 bytes at the cursor held in ap,
// advance the cursor by 8, and leave the value in rax (int-class) or xmm0
// (double). The cursor is a va_list = char* = an address stored in an int
// slot, so reading and writing it are ordinary integer operations. Doubles are
// stored bitwise in the 8-byte slot (the caller spilled them with movq), so we
// reload the bits into xmm0 with movq xmm0, rax.
func (c *CG) genVaArg(n *frontend.VaArgExpr) (frontend.CType, error) {
	if err := c.genLValue(n.Ap); err != nil {
		return frontend.TInt, err
	}
	c.emit("mov rcx, [r10]") // rcx = current cursor
	// Long double rides the shared by-address carrier (r10 = value address).
	// The caller passed it as ONE general-purpose slot holding a pointer to
	// the 16 bytes (the aggregate hidden-pointer convention, see genCall's
	// isAgg branch, which is what a long double argument reaches), so the
	// read is: take the pointer out of the slot, copy the 16 bytes it names
	// into a fresh temporary, and describe that temporary as the value.
	// Copying (rather than handing the caller's buffer straight back) keeps
	// the carrier contract simple: resBigK names a buffer this frame owns,
	// released by the ordinary statement-boundary path like every other
	// long double temporary.
	if n.Typ.Kind == frontend.KLongDouble {
		c.emit("mov rax, [rcx]")
		c.emit("add rcx, 8")
		c.emit("mov [r10], rcx")
		k, _, off := c.tfTemp()
		c.emit("mov r11, [rax]")
		c.emit("mov r10, [rax+8]")
		c.emit("mov [rbp%+d], r11", off)
		c.emit("mov [rbp%+d], r10", off+8)
		c.emit("lea r10, [rbp%+d]", off)
		c.resBig = true
		c.resBigT = n.Typ
		c.resBigK = k
		c.resBigSl = tfWords
		c.resTyp = frontend.TInt
		c.resW = 8
		c.resSigned = false
		return frontend.TInt, nil
	}
	if n.Typ.Class() == frontend.TDouble {
		c.emit("mov rax, [rcx]")
		c.emit("movq xmm0, rax")
		c.emit("add rcx, 8")
		c.emit("mov [r10], rcx")
		c.resTyp = frontend.TDouble
		c.resSigned = false
		c.resW = 8
		return frontend.TDouble, nil
	}
	c.emit("mov rax, [rcx]")
	c.emit("add rcx, 8")
	c.emit("mov [r10], rcx")
	c.resTyp = n.Typ.Class()
	c.resSigned = n.Typ.Kind == frontend.KInt && n.Typ.Signed
	c.resW = c.semWOf(n.Typ)
	return c.resTyp, nil
}

// loadDoubleConst materialises a double constant into xmm0, reusing the
// program-level constant pool so e.g. the 1.0 used by ++/-- on a double is
// emitted once, exactly like frontend.NumLit doubles.
func (c *CG) loadDoubleConst(v float64) {
	lab, ok := c.doubleLab[v]
	if !ok {
		lab = fmt.Sprintf("%sLD%dx", c.staticPrefix, len(c.doubles))
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
func (c *CG) genIncDec(n *frontend.IncDecExpr) (frontend.CType, error) {
	et := c.exprType(n.E)
	step := 1
	if et != nil && et.IsPtr() && et.Elem != nil {
		step = c.typeWidth(et.Elem)
	}
	signed := false
	if et != nil && et.Kind == frontend.KInt {
		signed = et.Signed
	}
	resW := 4
	if et != nil {
		resW = c.semWOf(et)
	}

	// Fast path: a register-cached scalar local (pointers are never
	// register-allocated because they can be address-taken).
	if id, ok := n.E.(*frontend.Ident); ok {
		if vi, ok2 := c.lookupVar(id.Name); ok2 && vi.reg != "" {
			// -- must step down, not up. Pick the right mnemonic once so all
			// four fast-path sites below emit inc/dec and add/sub correctly.
			opInc := "inc"
			opAdd := "add"
			if n.Op == "--" {
				opInc = "dec"
				opAdd = "sub"
			}
			if et != nil && et.Kind == frontend.KBool {
				// _Bool keeps exactly 0/1: normalise the register after the
				// increment/decrement. Prefix returns the new (normalised)
				// value; postfix returns the old value.
				c.emit("mov rax, %s", vi.reg) // old value
				if n.Prefix {
					if step == 1 {
						c.emit("%s %s", opInc, vi.reg)
					} else {
						c.emit("%s %s, %d", opAdd, vi.reg, step)
					}
					c.emit("mov rax, %s", vi.reg)
					c.normalizeBool()
					c.emit("mov %s, rax", vi.reg)
				} else {
					c.emit("mov rdx, rax") // save old value
					if step == 1 {
						c.emit("%s %s", opInc, vi.reg)
					} else {
						c.emit("%s %s, %d", opAdd, vi.reg, step)
					}
					c.emit("mov rax, %s", vi.reg)
					c.normalizeBool()
					c.emit("mov %s, rax", vi.reg)
					c.emit("mov rax, rdx") // restore old value as the result
				}
				c.resTyp = frontend.TInt
				c.resSigned = false
				c.resW = 1
				return frontend.TInt, nil
			}
			if n.Prefix {
				if resW == 4 {
					// N7: 32-bit inc/dec wraps at 32 and zero-extends to rax --
					// the materialized-int invariant -- so no canonInt round-trip
					// is needed. (resW==8 long/pointer vars keep the 64-bit form.)
					reg32 := gpReg32(vi.reg)
					if step == 1 {
						c.emit("%s %s", opInc, reg32)
					} else {
						c.emit("%s %s, %d", opAdd, reg32, step)
					}
					c.emit("mov rax, %s", vi.reg)
				} else {
					if step == 1 {
						c.emit("%s %s", opInc, vi.reg)
					} else {
						c.emit("%s %s, %d", opAdd, vi.reg, step)
					}
					c.emit("mov rax, %s", vi.reg)
				}
			} else {
				c.emit("mov rax, %s", vi.reg) // old value (already canonical)
				if resW == 4 {
					// N7: same 32-bit step on the cached register; rax keeps the
					// old (materialized) value as the postfix result.
					reg32 := gpReg32(vi.reg)
					if step == 1 {
						c.emit("%s %s", opInc, reg32)
					} else {
						c.emit("%s %s, %d", opAdd, reg32, step)
					}
				} else {
					if step == 1 {
						c.emit("%s %s", opInc, vi.reg)
					} else {
						c.emit("%s %s, %d", opAdd, vi.reg, step)
					}
				}
			}
			c.resTyp = frontend.TInt
			c.resSigned = signed
			c.resW = resW
			return frontend.TInt, nil
		}
	}

	// General lvalue path: compute the address, load the current value, modify
	// it, and store it back.
	if err := c.genLValue(n.E); err != nil {
		return frontend.TInt, err
	}
	width := c.lvalueWidth(n.E)
	double := false
	if t := c.exprType(n.E); t != nil && t.IsFloating() {
		double = true
	}
	if double {
		// width is 4 for a float lvalue: genLoadElem widens it to a double
		// and genStoreElem rounds the updated value back to a single.
		c.genLoadElem("r10", width, frontend.TDouble, false)
		c.emit("movsd xmm1, xmm0") // keep current for postfix restore
		c.loadDoubleConst(1.0)
		if n.Op == "++" {
			c.emit("addsd xmm0, xmm1")
		} else {
			c.emit("subsd xmm0, xmm1")
		}
		c.genStoreElem("r10", width, frontend.TDouble, 0, false)
		if !n.Prefix {
			c.emit("movsd xmm0, xmm1")
		}
		c.resTyp = frontend.TDouble
		c.resSigned = false
		c.resW = 8
		return frontend.TDouble, nil
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
		c.resTyp = frontend.TInt
		c.resSigned = c.lvBitSigned
		c.resW = resW
		if resW == 4 {
			c.canonInt(c.lvBitSigned)
		}
		return frontend.TInt, nil
	}
	// C11 _Atomic: an increment of an atomic object has to be ONE locked
	// instruction -- the load/modify/store sequence below leaves a window in
	// which another thread's update is lost. LOCK XADD is a fetch-and-add:
	// the register comes away with the previous value and memory with the new
	// one, so a prefix result just adds the step back on.
	// ...but on the object's own width, not the 8-byte slot's: a qword
	// fetch-and-add on an _Atomic int member would clobber whatever follows
	// it in the struct.
	if et != nil && et.Atomic && !double && c.lvBitWidth == 0 {
		if aw := c.typeWidth(et); aw == 1 || aw == 2 || aw == 4 || aw == 8 {
			wn := [...]string{"", "byte", "word", "", "dword", "", "", "", "qword"}[aw]
			areg := [...]string{"", "al", "ax", "", "eax", "", "", "", "rax"}[aw]
			delta := step
			if n.Op == "--" {
				delta = -step
			}
			c.emit("mov %s, %d", areg, delta)
			c.emit("lock xadd %s [r10], %s", wn, areg)
			// XADD writes only the narrow register (AL / AX), so the rest of
			// rax is stale; widen the old value as genLoadElem would have.
			if aw == 1 {
				c.emit("mov%sx rax, al", map[bool]string{true: "s", false: "z"}[signed])
			} else if aw == 2 {
				c.emit("mov%sx rax, ax", map[bool]string{true: "s", false: "z"}[signed])
			}
			if n.Prefix {
				if aw == 8 {
					c.emit("add rax, %d", delta)
				} else {
					c.emit("add eax, %d", delta)
				}
			}
			c.resTyp = frontend.TInt
			c.resSigned = signed
			c.resW = resW
			if resW == 4 {
				c.canonInt(signed)
			}
			return frontend.TInt, nil
		}
	}
	c.genLoadElem("r10", width, frontend.TInt, signed)
	os := 0
	saved := false
	if !n.Prefix {
		c.tmpDepth++
		os = c.tmpSlot(c.tmpDepth)
		c.emit("mov [rbp%+d], rax", os)
		saved = true
	}
	// N8: a width-4 lvalue loads as the materialized int (mov eax); stepping
	// with a 32-bit inc/dec wraps at 32 and zero-extends to rax, preserving the
	// invariant without a canonInt. char/short (extended by genLoadElem) and
	// long/pointer (full 64-bit) keep the 64-bit step.
	if width == 4 && n.Op == "++" {
		if step == 1 {
			c.emit("inc eax")
		} else {
			c.emit("add eax, %d", step)
		}
	} else if width == 4 {
		if step == 1 {
			c.emit("dec eax")
		} else {
			c.emit("sub eax, %d", step)
		}
	} else if n.Op == "++" {
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
	if et != nil && et.Kind == frontend.KBool {
		c.normalizeBool()
	}
	c.genStoreElem("r10", width, frontend.TInt, c.resW, c.resSigned)
	if saved {
		c.emit("mov rax, [rbp%+d]", os)
		c.tmpDepth--
	}
	c.resTyp = frontend.TInt
	c.resSigned = signed
	c.resW = resW
	return frontend.TInt, nil
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

/*
 * emitCompareDbl is emitCompare for a comparison whose ucomisd has already
 * been issued. ucomisd reports "unordered" (an operand was NaN) by setting
 * PF alongside ZF, and every C comparison must read that: an unordered pair
 * is neither equal nor less nor greater, so `==`, `<`, `<=`, `>`, `>=` are
 * all false for NaN -- while `!=` is the one comparison that is TRUE for it,
 * which is exactly what makes the classic `x != x` NaN test work.
 *
 * Without the PF test, `je` sees ZF=1 for an unordered pair and reports
 * NaN == NaN as true, so isnan() silently answers 0 for every value.
 */
func (c *CG) emitCompareDbl(jmpIfTrue string) {
	lTrue := c.newLabel("cmp")
	lFalse := c.newLabel("cmpf")
	lEnd := c.newLabel("cmpe")
	if jmpIfTrue == "jne" {
		c.emit("jp %s", lTrue) // unordered -> not equal -> 1
	} else {
		c.emit("jp %s", lFalse) // unordered -> 0 for every ordered test
	}
	c.emit("%s %s", jmpIfTrue, lTrue)
	c.line(lFalse + ":\n")
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

func (c *CG) genBinary(n *frontend.Binary) (frontend.CType, error) {
	switch n.Op {
	case "&&":
		lFalse := c.newLabel("andf")
		lEnd := c.newLabel("andd")
		if err := c.genTruth(n.L); err != nil {
			return frontend.TInt, err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lFalse)
		if err := c.genTruth(n.R); err != nil {
			return frontend.TInt, err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lFalse)
		c.emit("mov rax, 1")
		c.emit("jmp %s", lEnd)
		c.line(lFalse + ":\n")
		c.emit("mov rax, 0")
		c.line(lEnd + ":\n")
		c.resTyp = frontend.TInt
		c.resSigned = true
		c.resW = 4
		return frontend.TInt, nil
	case "||":
		lTrue := c.newLabel("ort")
		lEnd := c.newLabel("ore")
		if err := c.genTruth(n.L); err != nil {
			return frontend.TInt, err
		}
		c.emit("cmp rax, 0")
		c.emit("jne %s", lTrue)
		if err := c.genTruth(n.R); err != nil {
			return frontend.TInt, err
		}
		c.emit("cmp rax, 0")
		c.emit("jne %s", lTrue)
		c.emit("mov rax, 0")
		c.emit("jmp %s", lEnd)
		c.line(lTrue + ":\n")
		c.emit("mov rax, 1")
		c.line(lEnd + ":\n")
		c.resTyp = frontend.TInt
		c.resSigned = true
		c.resW = 4
		return frontend.TInt, nil
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
		return frontend.TInt, err
	}
	// Signedness of the LEFT operand drives signed vs unsigned division,
	// remainder, and right-shift (the dividend and the value being shifted
	// are always the left operand). The right operand is only a count.
	leftSigned := c.resSigned
	leftW := c.resW
	// T1.6 (C4): whether each operand is a ptrCapable load (pointer bits in
	// the high 32) -- the narrow sites below skip movsxd for such operands.
	leftPtr := c.resPtr
	if lt == frontend.TDouble {
		c.emit("movsd [rbp%+d], xmm0", off)
	} else {
		c.emit("mov [rbp%+d], rax", off)
	}
	rt, err := c.genExprT(n.R)
	if err != nil {
		return frontend.TInt, err
	}
	c.tmpDepth--
	// For symmetric comparisons the two operands normally share signedness,
	// so the right operand's flag is an acceptable proxy there.
	rightSigned := c.resSigned
	rightPtr := c.resPtr
	rightW := c.resW

	// The right operand is now in rax (int) or xmm0 (double).
	isDbl := lt == frontend.TDouble || rt == frontend.TDouble

	// loadLeftDbl puts the spilled left operand into xmm0 as a double.
	loadLeftDbl := func() {
		if lt == frontend.TDouble {
			c.emit("movsd xmm0, [rbp%+d]", off)
		} else {
			c.emit("mov rax, [rbp%+d]", off)
			// T1.6 (N22): sign-extend a signed materialized int left operand
			// (low 32 valid, high 32 = 0) before cvtsi2sd; a ptrCapable
			// operand (C4) passes through unextended.
			if leftW == 4 && leftSigned && !leftPtr {
				c.emit("movsxd rax, eax")
			}
			c.emit("cvtsi2sd xmm0, rax")
		}
	}

	switch n.Op {
	case "+", "-", "*", "/":
		if isDbl {
			if err := c.ensureType(frontend.TDouble); err != nil { // right -> xmm0
				return frontend.TInt, err
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
			c.resTyp = frontend.TDouble
			return frontend.TDouble, nil
		}
		// Usual arithmetic conversions decide the promoted width/sign of the
		// operation. Only a 4-byte result is canonicalised afterwards; an
		// 8-byte result (long/unsigned long) keeps its full 64-bit value.
		w, resSign := promotedArith(leftW, leftSigned, rightW, rightSigned)
		c.emit("mov r10, [rbp%+d]", off) // left -> r10, right -> rax
		// T1.6 (N17 at consumption): a 64-bit operation must see TRUE 64-bit
		// signed operands. A materialized signed int (resW==4, resSigned, high
		// 32 = 0) would read as a huge positive -- `mp + (mp<10 ? 3 : -9)` in
		// goclib's time.c degraded -9 (neg eax) to 0x00000000FFFFFFF7 and gmtime
		// came out one year early. movsxd restores the sign; unsigned ints are
		// already zero-extended and need nothing; pointer (8-byte) sides are
		// full width. This single site covers the + - * / arithmetic AND the
		// pointer-arithmetic branch below (which returns before the switch).
		if w == 8 {
			if rightW == 4 && rightSigned && !rightPtr {
				c.emit("movsxd rax, eax")
			}
			if leftW == 4 && leftSigned && !leftPtr {
				c.emit("movsxd r10, r10d")
			}
		}
		// ---- pointer arithmetic ----
		// A pointer value is carried as an 8-byte integer in rax/r10. When an
		// operand has pointer type we stride the integer side by the pointed-to
		// element size, exactly as array subscripting does.
		ltType := c.exprType(n.L)
		rtType := c.exprType(n.R)
		// An array used as an operand is a value context, so it has already
		// decayed to a pointer to element 0 (see loadVar); treat frontend.KArr exactly
		// like frontend.KPtr for pointer arithmetic. "a + 2" must step by the element
		// width, not by 2 bytes.
		lPtr := ltType != nil && (ltType.IsPtr() || ltType.IsArray())
		rPtr := rtType != nil && (rtType.IsPtr() || rtType.IsArray())
		if lPtr || rPtr {
			if n.Op == "*" || n.Op == "/" {
				return frontend.TInt, fmt.Errorf("operator %q is not defined for pointers", n.Op)
			}
			if lPtr && rPtr {
				if n.Op != "-" {
					return frontend.TInt, fmt.Errorf("only subtraction is defined for two pointers")
				}
				ew := c.ptrElemWidth(ltType)
				c.emit("sub r10, rax") // byte difference (left - right)
				c.emit("mov rax, r10")
				if ew != 1 {
					c.emit("cqo")
					c.emit("mov r11, %d", ew)
					c.emit("idiv r11")
				}
				c.resTyp = frontend.TInt
				c.resSigned = true
				c.resW = 8
				return frontend.TInt, nil
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
			c.resTyp = frontend.TInt
			c.resSigned = false
			c.resW = 8
			return frontend.TInt, nil
		}
		switch n.Op {
		case "+":
			if w == 4 {
				c.emit("add eax, r10d") // 32-bit wrap + zero-extend (materialized)
			} else {
				c.emit("add rax, r10")
			}
		case "-":
			if w == 4 {
				c.emit("sub r10d, eax")
				c.emit("mov eax, r10d")
			} else {
				c.emit("sub r10, rax")
				c.emit("mov rax, r10")
			}
		case "*":
			if w == 4 {
				c.emit("imul eax, r10d")
			} else {
				c.emit("imul rax, r10")
			}
		case "/":
			if w == 4 {
				// T1.6: 32-bit division. The materialized dividend/divisor
				// carry their value in the low 32 bits (zero-extended). cdq
				// sign-extends eax into edx:eax, idiv r11d reads r11d as a
				// signed divisor, quotient lands in eax (zero-extended).
				c.emit("mov r11d, eax") // divisor low 32
				c.emit("mov eax, r10d") // dividend low 32
				if resSign {
					c.emit("cdq")
					c.emit("idiv r11d")
				} else {
					c.emit("xor edx, edx")
					c.emit("div r11d")
				}
			} else {
				c.emit("mov r11, rax") // divisor
				c.emit("mov rax, r10") // dividend
				if resSign {
					c.emit("cqo")
					c.emit("idiv r11")
				} else {
					c.emit("xor rdx, rdx")
					c.emit("div r11")
				}
			}
		}
		c.resSigned = resSign
		c.resW = w
		c.resTyp = frontend.TInt
		return frontend.TInt, nil
	case "%":
		if isDbl {
			return frontend.TInt, fmt.Errorf("%% requires integer operands")
		}
		w, resSign := promotedArith(leftW, leftSigned, rightW, rightSigned)
		c.emit("mov r10, [rbp%+d]", off) // left -> r10, right stays in rax
		if w == 4 {
			c.emit("mov r11d, eax") // divisor low 32
			c.emit("mov eax, r10d") // dividend low 32
			if resSign {
				c.emit("cdq")
				c.emit("idiv r11d")
			} else {
				c.emit("xor edx, edx")
				c.emit("div r11d")
			}
			c.emit("mov eax, edx") // remainder low 32 (zero-extended)
		} else {
			// T1.6 (N17): narrow materialized signed int operands before the
			// 64-bit idiv -- see the + - * / site above.
			if rightW == 4 && rightSigned && !rightPtr {
				c.emit("movsxd rax, eax")
			}
			c.emit("mov r11, rax") // divisor
			if leftW == 4 && leftSigned && !leftPtr {
				c.emit("movsxd r10, r10d")
			}
			c.emit("mov rax, r10") // dividend
			if resSign {
				c.emit("cqo")
				c.emit("idiv r11")
			} else {
				c.emit("xor rdx, rdx")
				c.emit("div r11")
			}
			c.emit("mov rax, rdx")
		}
		c.resSigned = resSign
		c.resW = w
		c.resTyp = frontend.TInt
		return frontend.TInt, nil
	case "<<", ">>":
		if isDbl {
			return frontend.TInt, fmt.Errorf("%q requires integer operands", n.Op)
		}
		// C: each shift operand is individually integer-promoted and the
		// result type is the promoted LEFT operand's type; the right operand
		// is only a count. So width/sign come from the left side alone --
		// promotedArith must NOT be used here (int << someLong is still int).
		lw, ls := leftW, leftSigned
		if lw < 4 {
			lw, ls = 4, true
		}
		// T1.6: constant int shifts fold at compile time. A 32-bit `shl eax,
		// cl` masks the count to 5 bits, so a constant count in [32,63] would
		// shift by (count & 31) instead of losing all bits; folding with
		// 64-bit shift + wrap-to-32 semantics reproduces both the pre-C1
		// output and gcc's constant fold (lllit: `1 << 32` is 0, not 1).
		// Counts outside [0,63] (UB) and non-constant operands stay on the
		// runtime path, which still applies x86 count masking as before.
		// Long (w==8) shifts are not folded: foldConstInit's Go shift of a
		// negative-encoded unsigned long literal would be an arithmetic shift,
		// and the runtime 64-bit path already matches there.
		if lw == 4 {
			if lv, ok1 := foldConstInit(n.L); ok1 {
				if cv, ok2 := foldConstInit(n.R); ok2 && cv >= 0 && cv <= 63 {
					var folded int64
					switch n.Op {
					case "<<":
						folded = lv << uint(cv)
					case ">>":
						if ls {
							folded = lv >> uint(cv)
						} else {
							folded = int64(uint32(lv) >> uint(cv))
						}
					}
					c.emit("mov eax, %d", int32(folded))
					c.resSigned = ls
					c.resW = 4
					c.resTyp = frontend.TInt
					return frontend.TInt, nil
				}
			}
		}
		// Shift count must live in cl (low 8 bits of rcx); the left operand
		// rides in a frame temporary.
		if lw == 4 {
			c.emit("mov ecx, eax")           // count low 8 (zero-extends rcx)
			c.emit("mov eax, [rbp%+d]", off) // left low 32
			switch n.Op {
			case "<<":
				c.emit("shl eax, cl") // 32-bit shift wraps at 32
			case ">>":
				if ls {
					c.emit("sar eax, cl") // arithmetic shift reads bit 31
				} else {
					c.emit("shr eax, cl") // logical shift
				}
			}
		} else {
			c.emit("mov rcx, rax")           // count
			c.emit("mov rax, [rbp%+d]", off) // left
			switch n.Op {
			case "<<":
				c.emit("shl rax, cl")
			case ">>":
				if ls {
					c.emit("sar rax, cl")
				} else {
					c.emit("shr rax, cl")
				}
			}
		}
		c.resSigned = ls
		c.resW = lw
		c.resTyp = frontend.TInt
		return frontend.TInt, nil
	case "&", "|", "^":
		if isDbl {
			return frontend.TInt, fmt.Errorf("%q requires integer operands", n.Op)
		}
		w, resSign := promotedArith(leftW, leftSigned, rightW, rightSigned)
		c.emit("mov r11, [rbp%+d]", off) // left
		if w == 4 {
			switch n.Op {
			case "&":
				c.emit("and eax, r11d")
			case "|":
				c.emit("or eax, r11d")
			case "^":
				c.emit("xor eax, r11d")
			}
		} else {
			// T1.6 (N17): narrow materialized signed int operands before the
			// 64-bit and/or/xor -- see the + - * / site above. rax = right,
			// r11 = left (reloaded from the spill slot).
			if rightW == 4 && rightSigned && !rightPtr {
				c.emit("movsxd rax, eax")
			}
			if leftW == 4 && leftSigned && !leftPtr {
				c.emit("movsxd r11, r11d")
			}
			switch n.Op {
			case "&":
				c.emit("and rax, r11")
			case "|":
				c.emit("or rax, r11")
			case "^":
				c.emit("xor rax, r11")
			}
		}
		c.resSigned = resSign
		c.resW = w
		c.resTyp = frontend.TInt
		return frontend.TInt, nil
	case "<", ">", "<=", ">=", "==", "!=":
		var jmp string
		if isDbl {
			if err := c.ensureType(frontend.TDouble); err != nil {
				return frontend.TInt, err
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
			// NaN-aware: emitCompareDbl reads PF (set by ucomisd for an
			// unordered pair) as well as the condition flags.
			c.emitCompareDbl(jmp)
			c.resTyp = frontend.TInt
			c.resSigned = true
			c.resW = 4 // a comparison yields a (signed) int
			return frontend.TInt, nil
		} else {
			w, resSign := promotedArith(leftW, leftSigned, rightW, rightSigned)
			c.emit("mov r10, [rbp%+d]", off)
			if w == 4 {
				// T1.6: both sides are materialized ints (low 32 valid, high
				// 32 = 0). A 32-bit `cmp r10d, eax` sets SF/OF (signed) and
				// CF (unsigned) on the low 32 bits -- exactly what jl/jg/jb/ja
				// need. No sign extension required.
				c.emit("cmp r10d, eax")
			} else {
				// w == 8: a 64-bit compare. A signed int-width side (resW==4)
				// is a materialized value whose high 32 is zero, so it must
				// be sign-extended before the 64-bit compare or a negative int
				// would read as a huge positive. Width 1/2 sides are already
				// sign/zero-extended by loadVar; long sides are full width;
				// ptrCapable sides (C4) hold pointer bits, not int values.
				if leftW == 4 && leftSigned && !leftPtr {
					c.emit("movsxd r10, r10d")
				}
				if rightW == 4 && rightSigned && !rightPtr {
					c.emit("movsxd rax, eax")
				}
				c.emit("cmp r10, rax")
			}
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
		c.resTyp = frontend.TInt
		c.resSigned = true
		c.resW = 4 // a comparison yields a (signed) int
		return frontend.TInt, nil
	}
	return frontend.TInt, fmt.Errorf("unsupported operator %q", n.Op)
}

// argSlot remembers a call argument's frame temporary slot and its type.
type argSlot struct {
	slot int
	typ  frontend.CType
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
	if lib := common.Store(c.linux); lib != nil {
		if _, ok := lib.Funcs[name]; ok {
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
func (c *CG) genCallExpr(n *frontend.Call) (frontend.CType, error) {
	// va_start / va_end / va_copy are compiler builtins, not real functions.
	// va_start seeds the va_list cursor with the address of the first variadic
	// slot; va_end is a no-op in goc's flat-cursor model; va_copy duplicates the
	// cursor so a second pass over the same arguments starts over.
	//
	// va_copy is a builtin rather than a macro in <stdarg.h> because the size
	// of the copy is ABI-defined. goc's own va_list is a char* cursor on every
	// target it generates today, so here the copy is eight bytes -- but the
	// lowering has to be written in terms of the va_list's real size, or the
	// day va_list becomes the 24-byte SysV __va_list_tag this silently copies
	// 8 of 24 bytes and the copy shares (and trashes) the original's register
	// save area. stdarg.h only defines the macro on targets where the pointer
	// form is correct, so on Linux this name reaches here intact.
	if n.Name == "va_start" || n.Name == "va_end" || n.Name == "va_copy" {
		if n.Name == "va_start" {
			if len(n.Args) < 1 {
				return frontend.TInt, fmt.Errorf("va_start requires at least the va_list argument")
			}
			if err := c.genLValue(n.Args[0]); err != nil {
				return frontend.TInt, err
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
		} else if n.Name == "va_copy" {
			// Both cursors are addressed as values. Evaluate the SOURCE
			// first and keep it in a scratch register across the destination
			// evaluation: `va_copy(a, b)` must read b before a is written,
			// otherwise self-copy or aliasing arguments lose the old value.
			if len(n.Args) < 2 {
				return frontend.TInt, fmt.Errorf("va_copy requires a destination and a source va_list")
			}
			if err := c.genLValue(n.Args[1]); err != nil {
				return frontend.TInt, err
			}
			c.emit("mov r11, [r10]")
			if err := c.genLValue(n.Args[0]); err != nil {
				return frontend.TInt, err
			}
			c.emit("mov [r10], r11")
		}
		c.resTyp = frontend.TInt
		return frontend.TInt, nil
	}
	// The <stdatomic.h> fetch family, atomic_exchange and the
	// compare-exchange form: each is one locked instruction, so they are
	// builtins rather than goclib calls (see frontend/atomic.go).
	if _, user := c.funcs[n.Name]; !user {
		if ab, ok := frontend.LookupAtomicBuiltin(n.Name); ok {
			return c.genAtomicBuiltin(n, ab)
		}
		// The marker builtins carry optimiser information and no runtime work:
		// see src/frontend/marker.go.
		if ms, ok := frontend.LookupMarkerBuiltin(n.Name); ok {
			return c.genMarkerBuiltin(n, ms)
		}
		// GCC's overflow-checked arithmetic: see src/frontend/overflow.go.
		if ob, ok := frontend.LookupOverflowBuiltin(n.Name); ok {
			return c.genOverflowBuiltin(n, ob)
		}
	}
	// Constant-format printf specialisation, in two steps. A format string
	// literal with no '%' and no extra arguments turns the format engine into a
	// pure echo, so call fwrite directly. A literal format that is "lite" --
	// only %s, integers, %c and %f, with no field width, precision or flags --
	// is routed to vfmt_lite, which writes straight to the OS handle. Both
	// keep vfmt, the FILE layer and the float exponent machinery
	// (log/frexp/fmod/...) out of binaries that do not need them. Anything
	// richer (width, precision, %e/%g/%a, %p) keeps the real printf; the
	// decision is compile-time, so a run-time probe is never needed. See
	// constantFormatLite and the vfmt_lite comment in stdio.c.
	if repl := common.SpecializePrintfCall(n, c.printfQueries()); repl != nil {
		return c.genCallExpr(repl)
	}
	// A call whose name designates a VARIABLE holding a function pointer is an
	// indirect call: C spells it exactly like a direct call ("fp(x)"), but the
	// address has to be loaded from the variable at run time. A local (or
	// global) of function-pointer type shadows a same-named function, so the
	// variable is consulted before the direct-call table.
	if e, ft, ok := c.fnPtrVar(n.Name); ok {
		return c.genCall("", e, ft, n.Args)
	}
	return c.genCall(n.Name, nil, nil, n.Args)
}

// common.PrintfQueries adapts the assembly generator's symbol tables to the questions
// specializePrintfCall asks. Only the program's own declarations count: the C
// runtime shares these tables but must not make every library function look
// user-shadowed.
func (c *CG) printfQueries() common.PrintfQueries {
	return common.PrintfQueries{
		UserDefines:   func(name string) bool { return c.funcs[name] },
		ShadowedByVar: func(name string) bool { _, _, ok := c.fnPtrVar(name); return ok },
	}
}

// needsFullExit reports whether the entry stub must terminate through the
// full C exit (atexit handlers + std*-stream flush) instead of the bare
// __goclib_exit. Called after the first goclib fixpoint, so the need set is
// complete: everything that writes stdout/stderr goes through the
// __goclib_stdout/__goclib_stderr accessors, and atexit/exit (the program's
// own call) require the full teardown by definition. The print builtin is
// deliberately absent: __goclib_write is an unbuffered direct OS write, so
// it never needs a flush.
func (c *CG) needsFullExit() bool {
	for _, name := range [...]string{"__goclib_stdout", "__goclib_stderr", "atexit", "exit"} {
		if c.need[name] {
			return true
		}
	}
	return false
}

// fnPtrVar resolves a name that designates a local variable, parameter or
// global holding a function pointer. It returns the callee expression (the
// variable read itself) together with the static function type behind it.
func (c *CG) fnPtrVar(name string) (frontend.Expr, *frontend.Type, bool) {
	if vi, ok := c.lookupVar(name); ok {
		if ft := frontend.FuncTypeOf(vi.typ); ft != nil {
			return &frontend.Ident{Name: name}, ft, true
		}
	}
	if gt, ok := c.globalTyp[name]; ok {
		if ft := frontend.FuncTypeOf(gt); ft != nil {
			return &frontend.Ident{Name: name}, ft, true
		}
	}
	return nil, nil, false
}

// genIndirectCall emits a call through a computed function address: (*fp)(x),
// tab[i](x), s.cb(x). The callee expression is evaluated once, before the
// arguments, so that evaluating an argument cannot clobber it.
func (c *CG) genIndirectCall(n *frontend.IndirectCall) (frontend.CType, error) {
	// A checker-resolved method call (UFCS) is a plain direct call to the
	// named function with the receiver already prepended to the arguments.
	if n.UFCS != nil {
		return c.genCallExpr(n.UFCS)
	}
	return c.genCall("", n.Fn, frontend.FuncTypeOf(c.exprType(n.Fn)), n.Args)
}

// genCall emits a call and returns the callee's result type. name selects a
// directly called symbol; when fnExpr is non-nil the call is indirect and the
// address comes from evaluating that expression (ft is the static function type
// behind the pointer, nil when it could not be recovered -- such a call then
// behaves like an untyped extern returning int). Both paths marshall arguments
// identically; only the branch differs.
func (c *CG) genCall(name string, fnExpr frontend.Expr, ft *frontend.Type, args []frontend.Expr) (frontend.CType, error) {
	indirect := fnExpr != nil
	diag := name
	if indirect {
		diag = "function pointer"
	}
	// Syscall stubs want r10, not rcx, as argument 4 -- see callArgRegs.
	argRegs := c.callArgRegs(name, indirect)
	argXMM := c.argXMM()
	nargs := len(args)
	if nargs > maxArgs {
		return frontend.TInt, fmt.Errorf("%s: too many arguments (max %d)", diag, maxArgs)
	}
	// Struct/union return: the caller allocates a temporary result buffer,
	// passes its address as the hidden first argument (argRegs[0]) and every
	// user argument shifts one register slot to the right.
	retT := (*frontend.Type)(nil)
	varargs := false
	var paramTypes []*frontend.Type
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
		// The 32-byte Windows shadow is now reserved once in the prologue, so
		// only calls that actually spill arguments past the register window
		// need a per-call reservation -- and they still need the shadow region
		// above those spilled args, so the +32 stays here for that case.
		if !c.linux {
			extra += 32
		}
	}
	if extra > 0 && extra%16 != 0 {
		extra += 8
	}

	// Resolve the callee. Direct calls only: an indirect target is a run-time
	// value, not a symbol we can book here.
	target := name
	if !indirect {
		if !c.funcs[name] && name != "main" {
			if lib := common.Store(c.linux); lib != nil {
				if _, ok := lib.Funcs[name]; ok {
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
			return frontend.TInt, err
		}
		c.tmpDepth++
		tgtSlot = c.tmpDepth
		c.emit("mov [rbp%+d], rax", c.tmpSlot(tgtSlot))
		consumed++
	}
	for i := 0; i < nargs; i++ {
		pt := (*frontend.Type)(nil)
		if !varargs && i < len(paramTypes) {
			pt = paramTypes[i]
		}
		at := c.exprType(args[i])
		if at == nil && c.isLDExpr(args[i]) {
			// A long double literal ("printf("%Lf", 1.5L)") is in no type
			// table, so exprType is nil and the argument fell through the
			// aggregate branch into the scalar path -- which passed the
			// value's LOW 8 BYTES where the callee's va_arg expected a
			// pointer to the 16-byte value. Give it its real type so the
			// by-address marshalling below runs.
			at = frontend.LongDoubleType()
		}
		// Long double parameter: the argument is materialised as binary128
		// (converted from whatever type it has) and its address is passed,
		// exactly like a struct argument's hidden pointer. The operand's
		// temporary stays live until the call; consumed covers its 2 slots
		// plus the parked pointer slot.
		if isLD(pt) {
			_, sl, _, err := c.tfOperand(args[i])
			if err != nil {
				return frontend.TInt, err
			}
			c.tmpDepth++
			c.emit("mov [rbp%+d], r10", c.tmpSlot(c.tmpDepth))
			slots[i] = argSlot{slot: c.tmpDepth, typ: frontend.TInt}
			consumed += 1 + sl
			continue
		}
		// _BitInt parameter: the value travels by address. A scalar argument
		// is converted via from_i64; a mismatched-width _BitInt argument is
		// widened/truncated into a parameter-width temporary first.
		if frontend.IsBig(pt) {
			pw := bigWordsOf(pt)
			// keep a computed argument's own buffer alive until the call
			protect := func() {
				if c.resBig && c.resBigSl > 0 {
					c.tmpDepth += c.resBigSl
					consumed += c.resBigSl
					c.resBig = false
				} else {
					c.resBig = false
				}
			}
			if frontend.IsBig(at) {
				if err := c.structSrcAddr(args[i], at); err != nil {
					return frontend.TInt, err
				}
				protect()
				if at.Bits != pt.Bits {
					// convert into a parameter-width temporary
					k, off := c.claimBig(pw)
					name := "__goclib_bi_widen_u"
					if at.Signed {
						name = "__goclib_bi_widen_s"
					}
					c.callBigLib(name, []bigArg{
						{addrOff: off}, {reg: "r10"}, {imm: int64(pw)}, {imm: int64(bigWordsOf(at))},
					})
					// the temporary must stay live until the call; it is
					// released together with the argument spill slots
					consumed += pw
					c.tmpDepth++
					c.emit("lea r10, [rbp%+d]", c.tmpSlotBlock(k, pw))
					c.emit("mov [rbp%+d], r10", c.tmpSlot(c.tmpDepth))
				} else {
					c.tmpDepth++
					c.emit("mov [rbp%+d], r10", c.tmpSlot(c.tmpDepth))
				}
				slots[i] = argSlot{slot: c.tmpDepth, typ: frontend.TInt}
				consumed++
				continue
			}
			// scalar argument converted to the _BitInt parameter
			if _, err := c.genExprT(args[i]); err != nil {
				return frontend.TInt, err
			}
			if err := c.ensureType(frontend.TInt); err != nil {
				return frontend.TInt, err
			}
			// N17: materialized signed int argument consumed by bi_from_i64
			// must be sign-extended first (signed _BitInt parameter).
			if c.resW == 4 && c.resSigned && pt.Signed {
				c.emit("movsxd rax, eax")
			}
			k, off := c.claimBig(pw)
			s := int64(0)
			if pt.Signed {
				s = 1
			}
			c.callBigLib("__goclib_bi_from_i64", []bigArg{
				{addrOff: off}, {reg: "rax"}, {imm: int64(pw)}, {imm: s},
			})
			consumed += pw
			c.tmpDepth++
			c.emit("lea r10, [rbp%+d]", c.tmpSlotBlock(k, pw))
			c.emit("mov [rbp%+d], r10", c.tmpSlot(c.tmpDepth))
			slots[i] = argSlot{slot: c.tmpDepth, typ: frontend.TInt}
			consumed++
			continue
		}
		// Struct/union arguments are passed by hidden pointer: evaluate the
		// ADDRESS of the value into r10 and spill that address into one GP
		// slot (the callee copies the bytes into its own local slot).
		if isAgg(at) {
			if err := c.structSrcAddr(args[i], at); err != nil {
				return frontend.TInt, err
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
			if c.resBig && c.resBigSl > 0 {
				// Same protection for a long double value: its bytes live in
				// a claimed temporary and we are passing that temporary's
				// ADDRESS, so later argument evaluation must not reuse it
				// before the call consumes it.
				c.tmpDepth += c.resBigSl
				consumed += c.resBigSl
			}
			// A long double LVALUE (or a literal: both carry resBigSl == 0)
			// needs no slot protection -- its address points at a local or
			// at .rdata -- but resBig itself MUST still be cleared. Left
			// set, the call's int result was consumed by ensureType's LD
			// branch: "int n = sprintf(buf, "%Lf", x)" ran goc_tf_to_ll on
			// whatever r10 held after the call and got 0 (and, for a
			// literal argument, a wild r10 segfaulted the process).
			c.resBig = false
			c.tmpDepth++
			c.emit("mov [rbp%+d], r10", c.tmpSlot(c.tmpDepth))
			slots[i] = argSlot{slot: c.tmpDepth, typ: frontend.TInt}
			consumed++
			continue
		}
		t, err := c.genExprT(args[i])
		if err != nil {
			return frontend.TInt, err
		}
		if c.resBig {
			// A _BitInt argument passed to a scalar parameter: truncate to the
			// low 64 bits (the two's-complement low word is the int64 value).
			c.emit("mov rax, [r10]")
			c.tmpDepth -= c.resBigSl
			c.resBig = false
			t = frontend.TInt
			c.resTyp = frontend.TInt
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
				f32 = pt.Kind == frontend.KFloat
				if t == frontend.TInt {
					// T1.6 (N22): sign-extend a signed materialized int
					// argument before cvtsi2sd (negative int -> double);
					// a ptrCapable argument (C4) passes unextended.
					if c.resW == 4 && c.resSigned && !c.resPtr {
						c.emit("movsxd rax, eax")
					}
					c.emit("cvtsi2sd xmm0, rax")
					t = frontend.TDouble
					c.resTyp = frontend.TDouble
				}
			}
		}
		c.tmpDepth++
		if t == frontend.TDouble && !varargs {
			c.emit("movsd [rbp%+d], xmm0", c.tmpSlot(c.tmpDepth))
		} else {
			if t == frontend.TDouble { // varargs: double bits ride in a GP slot
				c.emit("movq rax, xmm0")
			} else if varargs && t == frontend.TInt && c.resW == 4 && c.resSigned {
				// T1.6 (N13, printf %d): vfmt reads a vararg int as a full
				// 8-byte signed long (va_arg(ap, long)) and negates on the
				// sign bit. A materialized signed int only has the low 32 bits
				// valid (high 32 = 0), so it must be sign-extended into the
				// 8-byte slot or a negative int prints as a huge positive.
				c.emit("movsxd rax, eax")
			} else if !varargs && t == frontend.TInt && c.resW == 4 && c.resSigned &&
				i < len(paramTypes) && paramTypes[i] != nil &&
				paramTypes[i].Kind == frontend.KInt && paramTypes[i].Signed &&
				paramTypes[i].Width == 8 {
				// T1.6 (C3, N13): an int argument passed to a signed LONG
				// parameter is a C widening conversion and must be sign-extended
				// into the 8-byte slot (the callee consumes a true 64-bit long;
				// libmisc: labs(-7) printed 4294967289 without it). An int
				// argument passed to an INT parameter is NOT widened: per the
				// unified ABI the value travels materialized (low 32 bits valid,
				// high 32 = 0) and the callee's int consumption is 32-bit-aware
				// (32-bit ops, N5b movsxd, N17 stores, N11 index, N21 switch,
				// N22 double) -- no movsxd, matching the mingw/gcc convention
				// that a 32-bit argument's upper bits are irrelevant. Unsigned
				// params read the materialized (zero-extended) form, which is
				// exactly the C conversion.
				c.emit("movsxd rax, eax")
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
		if s.typ == frontend.TDouble {
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
		c.emit("lea %s, [rbp%+d]", argRegs[0], c.tmpSlotBlock(resK, resSl))
	}
	if indirect {
		// Reload the target address last: marshalling the arguments above is
		// free to clobber rax, but nothing between here and the call needs it.
		c.emit("mov rax, [rbp%+d]", c.tmpSlot(tgtSlot))
		c.emitCall("")
	} else {
		c.emitCall(target)
	}
	// Windows API imports return 32-bit values (BOOL/DWORD/int) in EAX; the
	// upper 32 bits of RAX are not guaranteed to be zero, unlike goclib
	// functions which leave a clean 64-bit RAX. goc's register model treats
	// every int-class result as a full 64-bit word, so widen the result to
	// match the declared return type: sign-extend for signed int, zero-extend
	// for unsigned (DWORD/UINT). 8-byte returns (HANDLE, LONG, pointers) are
	// left untouched.
	if !c.linux && !indirect {
		if f, ok := c.funcDefs[name]; ok && common.DLLNames[name] != "" &&
			f.Ret != nil && f.Ret.Kind == frontend.KInt && f.Ret.Width < 8 {
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
	if retT != nil && retT.Kind == frontend.KFloat {
		c.emit("cvtss2sd xmm0, xmm0")
	}
	// Result type: user functions declare it; goclib and extern calls return int.
	ret := frontend.TInt
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
		case retT.Kind == frontend.KInt:
			c.resSigned = retT.Signed
			c.resW = retT.Width
		case retT.Kind == frontend.KPtr || retT.Kind == frontend.KFunc:
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
