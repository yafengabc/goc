package main

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CG emits x86-64 assembly in the Intel syntax that a0 (our own assembler)
// understands. No gcc anywhere in the pipeline: c0 -> .asm -> a0 -> .exe.
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
	sb        strings.Builder
	strs      []StrLit
	strLab    map[*StrLit]string
	doubles   []float64
	doubleLab map[float64]string
	label     int
	vars      map[string]varInfo // per-function: param = +off, local = -off
	localCnt  int                // number of stack-resident locals in current function
	regArea   int                // bytes reserved just below rbp for saved callee-save regs
	usedRegs  []string           // callee-save registers actually used as local homes
	tmpDepth  int                // live expression-temporary slots
	funcs     map[string]bool    // user-defined functions (by name)
	funcDefs  map[string]*FuncDecl
	calls     map[string]bool // functions called that are not defined here
	need      map[string]bool // clib functions this program actually uses
	linux     bool            // true -> SysV ABI + ELF output
	curRet   *Type  // return type of the function being generated
	curParam []*Type // parameter types of the current function
	resTyp    CType           // type of the value left by the last genExprT
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

// maxArgs is the number of integer arguments c0 can pass. Four go in
// RCX/RDX/R8/R9; the rest are spilled onto the stack at [rsp+32] and up,
// which is what lets printf() take more than three varargs.
const maxArgs = 8

// scratchSlots is the number of 8-byte stack slots reserved for spilling
// the left operand of nested binary expressions. 32 is plenty for toy code.
const scratchSlots = 32

// externDLL resolves an imported symbol to the DLL that exports it.
// Anything not listed here is a hard error rather than a silent guess.
// The C library itself (printf, strlen, malloc, ...) lives in clib_win/ and is
// implemented in pure assembly on top of kernel32, so there is no msvcrt
// anywhere in the pipeline.
var externDLL = map[string]string{
	"GetStdHandle":   "kernel32",
	"WriteFile":      "kernel32",
	"ExitProcess":    "kernel32",
	"GetProcessHeap": "kernel32",
	"HeapAlloc":      "kernel32",
	"HeapFree":       "kernel32",
	"MessageBoxA":    "user32",
	"MessageBoxW":    "user32",
}

// externLinux lists the syscall names an ELF-target program may reach for.
// a0 turns each into a `mov rax,N; syscall; ret` stub, so the C code never
// links against a library.
var externLinux = map[string]bool{
	"read": true, "write": true, "open": true, "close": true,
	"lseek": true, "mmap": true, "munmap": true, "brk": true, "ioctl": true,
	"writev": true, "nanosleep": true, "getpid": true, "kill": true,
	"exit": true, "exit_group": true, "gettimeofday": true, "clock_gettime": true,
}

// ---------------------------------------------------------------------------
// clib: the C library, as assembly
// ---------------------------------------------------------------------------
//
// The .asm files under clib_win/ and clib_linux/ are embedded into c0 at
// build time. Each function sits in a
// block marked with "; @func <name>" and can declare what it needs:
//
//	; @deps __clib_write strlen   other clib functions to pull in
//	; @extern WriteFile           imports to declare
//
// A block marked "; @data" holds that file's static data. c0 emits only the
// functions a program actually calls (plus their transitive deps), so a
// hello-world does not pay for malloc.
//
// Adding a function is just dropping it into a file under clib_win/ or
// clib_linux/.

// There are two clibs. clib_win/ is the Windows one (kernel32 WriteFile, heap
// via GetProcessHeap); clib_linux/ is the Linux one (write/brk syscalls, SysV
// argument order). Same C names, different bodies.
//
//go:embed clib_win/*.asm
var clibWinFS embed.FS

//go:embed clib_linux/*.asm
var clibLinuxFS embed.FS

type clibFunc struct {
	name string
	deps []string // other clib functions required
	exts []string // imported symbols required
	body string
	file string
}

// clibDataBlock is a chunk of static data emitted only when at least one of
// the functions named in `needs` made it into the program. That keeps
// printf's 512-byte output buffer out of a program that only calls putchar.
type clibDataBlock struct {
	file  string
	needs []string
	text  string
}

var (
	clibFuncsWin    = map[string]*clibFunc{}
	clibOrderWin    []string // source order, for stable output
	clibBlocksWin   []clibDataBlock
	clibFuncsLinux  = map[string]*clibFunc{}
	clibOrderLinux  []string
	clibBlocksLinux []clibDataBlock
	clibErr         error
)

// clibStore picks the tables for a target.
func clibStore(linux bool) (map[string]*clibFunc, *[]string, *[]clibDataBlock) {
	if linux {
		return clibFuncsLinux, &clibOrderLinux, &clibBlocksLinux
	}
	return clibFuncsWin, &clibOrderWin, &clibBlocksWin
}

func init() { clibErr = loadClib() }

func loadClib() error {
	if err := loadClibDir(clibWinFS, "clib_win", false); err != nil {
		return err
	}
	return loadClibDir(clibLinuxFS, "clib_linux", true)
}

func loadClibDir(fs embed.FS, dir string, linux bool) error {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".asm") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, f := range files {
		b, err := fs.ReadFile(dir + "/" + f)
		if err != nil {
			return err
		}
		if err := parseClib(f, string(b), linux); err != nil {
			return fmt.Errorf("%s/%s: %v", dir, f, err)
		}
	}
	return nil
}

// clibAlias maps a C name to a different symbol on Linux. Needed where the C
// name would collide with an a0 syscall stub of the same name.
var clibAliasLinux = map[string]string{
	"exit": "__clib_exit",
}

func parseClib(file, src string, linux bool) error {
	funcs, order, blocks := clibStore(linux)
	var cur *clibFunc
	var data []string
	var dataNeeds []string
	inData := false
	flush := func() error {
		if inData {
			*blocks = append(*blocks, clibDataBlock{
				file:  file,
				needs: dataNeeds,
				text:  strings.Join(data, "\n"),
			})
			data, dataNeeds, inData = nil, nil, false
			return nil
		}
		if cur != nil {
			if _, dup := funcs[cur.name]; dup {
				return fmt.Errorf("duplicate function %q", cur.name)
			}
			funcs[cur.name] = cur
			*order = append(*order, cur.name)
			cur = nil
		}
		return nil
	}
	for _, raw := range strings.Split(src, "\n") {
		t := strings.TrimSpace(raw)
		switch {
		case t == "; @end":
			if err := flush(); err != nil {
				return err
			}
		case t == "; @data" || strings.HasPrefix(t, "; @data "):
			if err := flush(); err != nil {
				return err
			}
			// "; @data printf,sprintf" -- emit this block only if one of
			// those functions is linked in.
			for _, n := range strings.Split(strings.TrimSpace(strings.TrimPrefix(t, "; @data")), ",") {
				if n = strings.TrimSpace(n); n != "" {
					dataNeeds = append(dataNeeds, n)
				}
			}
			inData = true
		case strings.HasPrefix(t, "; @func"):
			if err := flush(); err != nil {
				return err
			}
			name := strings.TrimSpace(strings.TrimPrefix(t, "; @func"))
			if name == "" {
				return fmt.Errorf("@func without a name")
			}
			cur = &clibFunc{name: name, file: file}
		case strings.HasPrefix(t, "; @deps"):
			if cur == nil {
				return fmt.Errorf("@deps outside a function")
			}
			cur.deps = append(cur.deps, strings.Fields(strings.TrimPrefix(t, "; @deps"))...)
		case strings.HasPrefix(t, "; @extern"):
			if cur == nil {
				return fmt.Errorf("@extern outside a function")
			}
			cur.exts = append(cur.exts, strings.Fields(strings.TrimPrefix(t, "; @extern"))...)
		default:
			if inData {
				data = append(data, raw)
			} else if cur != nil {
				cur.body += raw + "\n"
			}
		}
	}
	return flush()
}

// clibUsed expands the set of needed clib functions into their transitive
// closure, and reports the imports and static data that go with them.
func clibUsed(need map[string]bool, linux bool) (order []string, exts []string, data string, err error) {
	funcs, _, blocks := clibStore(linux)
	seen := map[string]bool{}
	queue := make([]string, 0, len(need))
	for n := range need {
		queue = append(queue, n)
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if seen[n] {
			continue
		}
		f, ok := funcs[n]
		if !ok {
			return nil, nil, "", fmt.Errorf("clib: function %q is not defined for this target", n)
		}
		seen[n] = true
		order = append(order, n)
		queue = append(queue, f.deps...)
	}
	extSet := map[string]bool{}
	for _, n := range order {
		for _, e := range funcs[n].exts {
			extSet[e] = true
		}
	}
	// Static data comes along only if one of the functions that needs it did.
	var db strings.Builder
	for _, b := range *blocks {
		if strings.TrimSpace(b.text) == "" {
			continue
		}
		used := len(b.needs) == 0 // untagged block: always emitted
		for _, n := range b.needs {
			if seen[n] {
				used = true
				break
			}
		}
		if used {
			db.WriteString(b.text)
			db.WriteString("\n")
		}
	}
	for e := range extSet {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	return order, exts, db.String(), nil
}

// clibNames lists the public (non-internal) clib functions, for error messages.
func clibNames(linux bool) []string {
	funcs, order, _ := clibStore(linux)
	_ = funcs
	var out []string
	for _, n := range *order {
		if strings.HasPrefix(n, "__") {
			// Internal name that a C-facing alias points at: report the C name.
			for cName, sym := range clibAliasLinux {
				if sym == n {
					out = append(out, cName)
				}
			}
			continue
		}
		out = append(out, n)
	}
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
	return -(c.regArea + 8*c.localCnt + 8*k)
}

// emit writes one indented instruction line.
func (c *CG) emit(format string, a ...any) {
	c.sb.WriteString("\t" + fmt.Sprintf(format, a...) + "\n")
}

// loadVar emits code that loads variable vi's value into rax (int) or xmm0
// (double), and records the resulting type in c.resTyp.
// movsd (not movq) is used for the XMM <-> memory moves: a0 only knows the
// GP <-> XMM forms of movq.
func (c *CG) loadVar(vi varInfo) {
	if vi.reg != "" {
		// Register-cached int local: value is already in the callee-save.
		c.emit("mov rax, %s", vi.reg)
		c.resTyp = TInt
		return
	}
	if vi.typ != nil && vi.typ.Kind == KDouble {
		c.emit("movsd xmm0, [rbp%+d]", vi.off)
		c.resTyp = TDouble
	} else {
		c.emit("mov rax, [rbp%+d]", vi.off)
		c.resTyp = TInt
	}
}

// storeVar emits code that stores the value currently in rax (int) or xmm0
// (double) into variable vi's slot or home register.
func (c *CG) storeVar(vi varInfo) {
	if vi.reg != "" {
		// Register-cached int local: keep it in the callee-save (which
		// survives function calls, so no spill is needed).
		c.emit("mov %s, rax", vi.reg)
		return
	}
	if vi.typ != nil && vi.typ.Kind == KDouble {
		c.emit("movsd [rbp%+d], xmm0", vi.off)
	} else {
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
		return TInt, nil
	case *Ident:
		vi, ok := c.vars[n.Name]
		if !ok {
			return TInt, fmt.Errorf("undefined variable %q", n.Name)
		}
		if vi.typ.IsArray() {
			// An array used as a value decays to a pointer to element 0.
			c.emit("lea rax, [rbp%+d]", vi.off)
			c.resTyp = TInt
			return TInt, nil
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
		ec := c.elemClassOf(n.Base)
		if ec == TDouble {
			c.emit("movsd xmm0, [r10]")
			c.resTyp = TDouble
			return TDouble, nil
		}
		c.emit("mov rax, [r10]")
		c.resTyp = TInt
		return TInt, nil
	case *Call:
		return c.genCallExpr(n)
	}
	return TInt, fmt.Errorf("unknown expression")
}

// genExpr emits an expression and discards its type (for statement context).
func (c *CG) genExpr(e Expr) error {
	_, err := c.genExprT(e)
	return err
}

// Gen produces the full assembly source for a program. linux selects the
// SysV ABI and the Linux clib; otherwise Windows x64 conventions are used.
func Gen(prog *Program, linux bool) (string, error) {
	if clibErr != nil {
		return "", clibErr
	}
	c := &CG{
		strLab:    map[*StrLit]string{},
		doubleLab: map[float64]string{},
		vars:      map[string]varInfo{},
		funcs:     map[string]bool{},
		funcDefs:  map[string]*FuncDecl{},
		calls:     map[string]bool{},
		need:      map[string]bool{},
		linux:     linux,
	}
	for _, f := range prog.Funcs {
		c.funcs[f.Name] = true
		c.funcDefs[f.Name] = f
	}
	// Prototypes (from #include'd headers) are registered only for call-site
	// double-promotion; they are deliberately NOT added to c.funcs, so a
	// prototype for a clib function still triggers clib inclusion.
	for _, f := range prog.Prototypes {
		c.funcDefs[f.Name] = f
	}

	var body strings.Builder
	c.sb = strings.Builder{}
	for _, f := range prog.Funcs {
		if err := c.genFunc(f); err != nil {
			return "", err
		}
	}
	body.WriteString(c.sb.String())

	if _, ok := c.funcs["main"]; !ok {
		return "", fmt.Errorf("program has no main()")
	}

	// Pull in exactly the clib functions this program calls, plus whatever
	// those depend on.
	order, clibExts, dataBlock, err := clibUsed(c.need, c.linux)
	if err != nil {
		return "", err
	}

	// Every import the program needs: the exit routine for the entry stub,
	// whatever the C code calls directly, and whatever clib pulled in.
	importSet := map[string]bool{}
	if c.linux {
		importSet["exit"] = true
	} else {
		importSet["ExitProcess"] = true
	}
	for name := range c.calls {
		importSet[name] = true
	}
	for _, e := range clibExts {
		importSet[e] = true
	}
	imports := make([]string, 0, len(importSet))
	for name := range importSet {
		if c.linux {
			if !externLinux[name] {
				return "", fmt.Errorf("unknown function %q: not in clib (%s), and not a Linux syscall a0 knows",
					name, strings.Join(clibNames(c.linux), ", "))
			}
			// ELF targets have no DLLs: a0 turns this into a syscall stub.
			imports = append(imports, fmt.Sprintf("extern %s\n", name))
			continue
		}
		dll, ok := externDLL[name]
		if !ok {
			return "", fmt.Errorf("unknown function %q: not in clib (%s), and not in externDLL",
				name, strings.Join(clibNames(c.linux), ", "))
		}
		imports = append(imports, fmt.Sprintf("extern %s, %s\n", name, dll))
	}
	sort.Strings(imports)

	var out strings.Builder
	out.WriteString("; generated by c0 -- assembled by a0, no gcc involved\n")
	out.WriteString("section .text\n")
	out.WriteString("global _start\n")
	out.WriteString("\n")
	for _, e := range imports {
		out.WriteString(e)
	}
	out.WriteString("\n")
	// Entry stub: align the stack, run main, and hand its return value to the
	// platform's exit routine. Linux needs no shadow space and exits through
	// the `exit` syscall stub; Windows uses ExitProcess.
	out.WriteString("_start:\n")
	out.WriteString("\tand rsp, -16\n")
	if c.linux {
		out.WriteString("\tcall main\n")
		out.WriteString("\tmov rdi, rax\n")
		out.WriteString("\tcall exit\n\n")
	} else {
		out.WriteString("\tsub rsp, 48\n")
		out.WriteString("\tcall main\n")
		out.WriteString("\tmov rcx, rax\n")
		out.WriteString("\tcall ExitProcess\n\n")
	}
	out.WriteString(body.String())

	if len(order) > 0 {
		out.WriteString("\n; --- clib: only what this program uses ---\n")
		if dataBlock != "" {
			out.WriteString(dataBlock)
			out.WriteString("section .text\n")
		}
		funcs, _, _ := clibStore(c.linux)
		for _, n := range order {
			out.WriteString(funcs[n].body)
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
	return out.String(), nil
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
			for _, a := range n.Args {
				walkExpr(a)
			}
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
		}
	}
	walkStmt(f.Body)
	return taken
}

func (c *CG) genFunc(f *FuncDecl) error {
	c.vars = map[string]varInfo{}
	c.curRet = f.Ret
	c.curParam = f.ParamTypes
	for i, p := range f.Params {
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
	var gather func(Stmt)
	gather = func(s Stmt) {
		switch n := s.(type) {
		case *Block:
			for _, st := range n.Stmts {
				gather(st)
			}
		case *DeclList:
			for _, d := range n.Decls {
				gather(d)
			}
		case *DeclStmt:
			if _, ok := c.vars[n.Name]; !ok {
				decls = append(decls, localDecl{n.Name, n.Typ})
			}
		case *IfStmt:
			gather(n.Then)
			if n.Else != nil {
				gather(n.Else)
			}
		case *WhileStmt:
			gather(n.Body)
		}
	}
	for _, st := range f.Body.Stmts {
		gather(st)
	}

	// A local whose address is taken (&x) cannot live in a register, doubles
	// cannot live in a GPR, and arrays obviously need memory. Everything else
	// that is a small int is a candidate for a callee-save register home.
	addrTaken := c.findAddressTaken(f)
	regPool := []string{"rbx", "r12", "r13", "r14"}
	c.usedRegs = nil
	regOf := map[string]string{}
	ri := 0
	stackDecls := make([]localDecl, 0, len(decls))
	for _, d := range decls {
		intClass := d.typ != nil && !d.typ.IsArray() && d.typ.Kind != KDouble
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
	regArea := 8 * len(c.usedRegs)
	localCount := 0
	for _, d := range stackDecls {
		if d.typ != nil && d.typ.IsArray() {
			ln := d.typ.Len
			if ln < 1 {
				ln = 1
			}
			off := -(regArea + 8*(localCount + ln))
			localCount += ln
			c.vars[d.name] = varInfo{off: off, typ: d.typ}
		} else {
			localCount++
			c.vars[d.name] = varInfo{off: -(regArea + 8*localCount), typ: d.typ}
		}
	}
	c.regArea = regArea
	c.localCnt = localCount
	c.tmpDepth = 0

	// shadow space (Windows only) + callee-save save area + locals +
	// expression temporaries, all kept 16-byte aligned.
	frame := c.shadowSpace() + regArea + 8*localCount + 8*scratchSlots
	if frame%16 != 0 {
		frame += 16 - frame%16
	}

	c.sb.WriteString(f.Name + ":\n")
	c.emit("push rbp")
	c.emit("mov rbp, rsp")
	c.emit("sub rsp, %d", frame)
	// Save only the callee-save registers we actually use as local homes.
	for i, r := range c.usedRegs {
		c.emit("mov [rbp-%d], %s", 8*(i+1), r)
	}

	// Spill the register arguments into this function's frame so the rest of
	// the code can read params from the stack like normal locals. Double
	// parameters arrive in XMM registers and are stored as 8 bytes (movsd).
	argRegs := c.argRegs()
	argXMM := c.argXMM()
	for i := 0; i < len(f.Params) && i < len(argRegs); i++ {
		off := 16 + 8*i
		if f.ParamTypes[i].Kind == KDouble && i < len(argXMM) {
			c.emit("movsd [rbp+%d], %s", off, argXMM[i])
		} else {
			c.emit("mov [rbp+%d], %s", off, argRegs[i])
		}
	}

	for _, st := range f.Body.Stmts {
		if err := c.genStmt(st); err != nil {
			return err
		}
	}
	// Safety epilogue in case a path has no explicit return.
	c.emitEpilogue()
	return nil
}

// emitEpilogue restores the saved callee-save registers, then returns.
// (a0 has no `leave`, so spell it out.)
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
	case *DeclStmt:
		vi := c.vars[n.Name]
		if vi.typ.IsArray() {
			// Arrays are not initialised here; the checker rejects initialisers.
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
		if ec == TDouble {
			c.emit("movsd xmm0, [rbp%+d]", rslot)
			c.emit("movsd [r10], xmm0")
		} else {
			c.emit("mov rax, [rbp%+d]", rslot)
			c.emit("mov [r10], rax")
		}
		c.tmpDepth--
		return nil
	case *ExprStmt:
		if _, err := c.genExprT(n.E); err != nil {
			return err
		}
	case *ReturnStmt:
		if n.E != nil {
			if _, err := c.genExprT(n.E); err != nil {
				return err
			}
		if err := c.ensureType(c.curRet.Class()); err != nil {
			return err
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
			c.sb.WriteString(lElse + ":\n")
			if err := c.genStmt(n.Else); err != nil {
				return err
			}
			c.sb.WriteString(lEnd + ":\n")
		} else {
			c.sb.WriteString(lElse + ":\n")
		}
	case *WhileStmt:
		lTop := c.newLabel("while")
		lEnd := c.newLabel("wend")
		c.sb.WriteString(lTop + ":\n")
		if _, err := c.genExprT(n.Cond); err != nil {
			return err
		}
		if err := c.ensureType(TInt); err != nil {
			return err
		}
		c.emit("cmp rax, 0")
		c.emit("je %s", lEnd)
		if err := c.genStmt(n.Body); err != nil {
			return err
		}
		c.emit("jmp %s", lTop)
		c.sb.WriteString(lEnd + ":\n")
	}
	return nil
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
		return TInt, nil
	case "*":
		// Dereference: n.E evaluates to the pointer value (in rax).
		if _, err := c.genExprT(n.E); err != nil {
			return TInt, err
		}
		ec := c.elemClassOf(n.E)
		if ec == TDouble {
			c.emit("movsd xmm0, [rax]")
			c.resTyp = TDouble
			return TDouble, nil
		}
		c.emit("mov rax, [rax]")
		c.resTyp = TInt
		return TInt, nil
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
	c.sb.WriteString(lTrue + ":\n")
	c.emit("mov rax, 1")
	c.sb.WriteString(lEnd + ":\n")
	return TInt, nil
}

// genLValue emits code that leaves the address of the lvalue e in r10.
func (c *CG) genLValue(e Expr) error {
	switch n := e.(type) {
	case *Ident:
		vi, ok := c.vars[n.Name]
		if !ok {
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
				return fmt.Errorf("undefined variable %q", id.Name)
			}
			if vi.typ.IsArray() {
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
		} else {
			if _, err := c.genExprT(n.Base); err != nil {
				return err
			}
			c.emit("mov r10, rax")
		}
		c.emit("mov r11, [rbp%+d]", islot)
		// byte offset = index * 8 (every slot is 8 bytes). a0 now supports
		// imul-with-immediate, so a single scaled multiply replaces the old
		// triple doubling-add (8 = 2^3) hack.
		c.emit("imul r11, 8")
		c.emit("add r10, r11")
		c.tmpDepth--
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
		return c.elemClassOf(n.Base)
	}
	return TInt
}

// setcc emits "rax = (left OP right)" using a conditional branch, because a0
// does not implement the setcc/movzx pair that gcc's assembler provides.
func (c *CG) emitCompare(jmpIfTrue string) {
	lTrue := c.newLabel("cmp")
	lEnd := c.newLabel("cmpe")
	c.emit("%s %s", jmpIfTrue, lTrue)
	c.emit("mov rax, 0")
	c.emit("jmp %s", lEnd)
	c.sb.WriteString(lTrue + ":\n")
	c.emit("mov rax, 1")
	c.sb.WriteString(lEnd + ":\n")
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
		c.sb.WriteString(lFalse + ":\n")
		c.emit("mov rax, 0")
		c.sb.WriteString(lEnd + ":\n")
		c.resTyp = TInt
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
		c.sb.WriteString(lTrue + ":\n")
		c.emit("mov rax, 1")
		c.sb.WriteString(lEnd + ":\n")
		c.resTyp = TInt
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
		c.emit("mov r10, [rbp%+d]", off) // left -> r10, right -> rax
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
			c.emit("cqo")
			c.emit("idiv r11")
		}
		c.resTyp = TInt
		return TInt, nil
	case "%":
		if isDbl {
			return TInt, fmt.Errorf("%% requires integer operands")
		}
		c.emit("mov r10, [rbp%+d]", off)
		c.emit("mov r11, rax") // divisor
		c.emit("mov rax, r10") // dividend
		c.emit("cqo")
		c.emit("idiv r11")
		c.emit("mov rax, rdx")
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
			c.emit("mov r10, [rbp%+d]", off)
			c.emit("cmp r10, rax")
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
		}
		c.emitCompare(jmp)
		c.resTyp = TInt
		return TInt, nil
	}
	return TInt, fmt.Errorf("unsupported operator %q", n.Op)
}

// argSlot remembers a call argument's frame temporary slot and its type.
type argSlot struct {
	slot int
	typ  CType
}

// variadicFn reports whether a clib function takes printf-style varargs.
// For those, c0 passes every argument in an 8-byte "general-purpose slot"
// (doubles are moved bitwise into rax first), so the __clib_va array lines
// up positionally with the format string: the %d/%f/%s consumers all walk
// the same 8-byte cursor, exactly like the clib prologue expects.
func variadicFn(name string) bool {
	return name == "printf" || name == "sprintf"
}

// genCallExpr emits a call and returns the callee's result type.
//
// Calling conventions:
//   - Typed calls (user functions): each argument goes to the register of
//     its position and type -- doubles to xmm0-3 (Win) / xmm0-7 (SysV),
//     everything else to rcx/rdx/r8/r9 (Win) / rdi/rsi/... (SysV).
//   - Varargs (printf/sprintf): every argument rides in an 8-byte GP slot,
//     double bit patterns included, so __clib_va needs no XMM handling.
//
// Each argument is evaluated and spilled to a frame temporary slot, and only
// loaded into the argument registers right before the call. This is required
// because an argument may itself be a function call whose own argument setup
// would otherwise clobber the values of earlier arguments.
func (c *CG) genCallExpr(n *Call) (CType, error) {
	argRegs := c.argRegs()
	argXMM := c.argXMM()
	nargs := len(n.Args)
	if nargs > maxArgs {
		return TInt, fmt.Errorf("%s: too many arguments (max %d)", n.Name, maxArgs)
	}
	// Anything past the register arguments goes on the stack: at [rsp+32]
	// and up on Windows (above the 32-byte shadow space), at [rsp] and up on
	// Linux. The extra space is rounded up to 16 so RSP stays aligned.
	stackArgs := nargs - len(argRegs)
	if stackArgs < 0 {
		stackArgs = 0
	}
	extra := 0
	if stackArgs > 0 {
		extra = 8 * stackArgs
		if extra%16 != 0 {
			extra += 8
		}
	}

	// Resolve the callee. A clib function may live under a different symbol
	// than its C name (see clibAliasLinux).
	target := n.Name
	if c.linux {
		if a, ok := clibAliasLinux[n.Name]; ok {
			target = a
		}
	}
	if !c.funcs[n.Name] && n.Name != "main" {
		funcs, _, _ := clibStore(c.linux)
		if _, ok := funcs[target]; ok {
			c.need[target] = true
		} else {
			c.calls[n.Name] = true
		}
	}

	varargs := variadicFn(n.Name)
	slots := make([]argSlot, nargs)
	for i := 0; i < nargs; i++ {
		t, err := c.genExprT(n.Args[i])
		if err != nil {
			return TInt, err
		}
		// C's usual conversion at call sites: an int argument passed to a
		// declared double parameter is widened before it is spilled, so the
		// callee (which reads double params from an XMM register) sees the
		// right value.
		if !varargs && t == TInt {
			if fd, ok := c.funcDefs[n.Name]; ok && i < len(fd.ParamTypes) && fd.ParamTypes[i].Kind == KDouble {
				c.emit("cvtsi2sd xmm0, rax")
				t = TDouble
				c.resTyp = TDouble
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
		slots[i] = argSlot{slot: c.tmpDepth, typ: t}
	}
	if extra > 0 {
		c.emit("sub rsp, %d", extra)
	}
	for i := 0; i < nargs; i++ {
		s := slots[i]
		if varargs {
			if i < len(argRegs) {
				c.emit("mov %s, [rbp%+d]", argRegs[i], c.tmpSlot(s.slot))
				continue
			}
			c.emit("mov rax, [rbp%+d]", c.tmpSlot(s.slot))
			c.emit("mov [rsp+%d], rax", c.stackArgOff(i-len(argRegs)))
			continue
		}
		if s.typ == TDouble {
			if i < len(argRegs) && i < len(argXMM) {
				c.emit("movsd %s, [rbp%+d]", argXMM[i], c.tmpSlot(s.slot))
				continue
			}
			c.emit("movsd xmm0, [rbp%+d]", c.tmpSlot(s.slot))
			c.emit("movq rax, xmm0")
			c.emit("mov [rsp+%d], rax", c.stackArgOff(i-len(argRegs)))
			continue
		}
		if i < len(argRegs) {
			c.emit("mov %s, [rbp%+d]", argRegs[i], c.tmpSlot(s.slot))
			continue
		}
		c.emit("mov rax, [rbp%+d]", c.tmpSlot(s.slot))
		c.emit("mov [rsp+%d], rax", c.stackArgOff(i-len(argRegs)))
	}
	c.emit("call %s", target)
	if extra > 0 {
		c.emit("add rsp, %d", extra)
	}
	c.tmpDepth -= nargs

	// Result type: user functions declare it; clib and extern calls return int.
	ret := TInt
	if f, ok := c.funcDefs[n.Name]; ok {
		ret = f.Ret.Class()
	}
	c.resTyp = ret
	return ret, nil
}

// encodeStr renders decoded string bytes as a double-quoted literal with
// escapes, for a0's db directive.
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

// formatDouble renders a float64 as a Go-syntax literal that a0's `dq`
// directive can parse back into IEEE-754 bits ("1.5", "0x1.2p3", ...).
//
// 'g' formatting would print integral doubles as bare integers ("10"), and a0
// parses those via ParseInt -> the integer bit pattern (0xa) instead of the
// double (0x4024000000000000). Force a trailing ".0" so a0's ParseFloat path
// is taken.
func formatDouble(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
