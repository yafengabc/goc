package main

import (
	"embed"
	"fmt"
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
	sb          strings.Builder
	strs        []StrLit
	strLab      map[*StrLit]string
	doubles     []float64
	doubleLab   map[float64]string
	label       int
	vars        map[string]varInfo // per-function: param = +off, local = -off
	localBytes  int                // bytes consumed by stack-resident locals (incl. array padding)
	regArea     int                // bytes reserved just below rbp for saved callee-save regs
	globals     map[string]bool    // names of program-level (global/static) variables
	globalLab   map[string]string  // name -> .data label for a global variable
	globalTyp   map[string]*Type   // name -> declared type of a global variable
	usedRegs    []string           // callee-save registers actually used as local homes
	tmpDepth    int                // live expression-temporary slots
	funcs       map[string]bool    // user-defined functions (by name)
	funcDefs    map[string]*FuncDecl
	calls       map[string]bool // functions called that are not defined here
	need        map[string]bool // goclib functions this program actually uses
	linux       bool            // true -> SysV ABI + ELF output
	curRet      *Type           // return type of the function being generated
	curParam    []*Type         // parameter types of the current function
	resTyp      CType           // type of the value left by the last genExprT
	resSigned   bool            // signedness of the last genExprT result (int-class only)
	resW        int             // semantic width of the last genExprT result: 1/2/4 (int-class), 8 (long/pointer/double)
	tmpSgn      []bool          // signedness of each expression-temporary slot
	loops       []loopLabels    // active loop targets for break/continue
	saveBaseOff int             // rbp offset of the variadic save area (0 if none)
	nFixed      int             // number of named params before "..." in the current fn
	sretSlot    int // rbp offset of this function's hidden sret-pointer slot (0 = returns a scalar)
	resStruct   bool    // the last call returned a struct; its value is in a tmp result buffer
	resStructSz int     // size in bytes of that struct
	resStructK  int     // tmpSlot index of the first result-buffer slot
	resStructSl int     // number of tmp slots occupied by the result buffer
}

// loopLabels records the break/continue targets of the innermost loop.
type loopLabels struct {
	breakLbl string
	contLbl  string
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

// maxArgs is the number of integer arguments goc can pass. Four go in
// RCX/RDX/R8/R9; the rest are spilled onto the stack at [rsp+32] and up,
// which is what lets printf() take more than three varargs.
const maxArgs = 8

// scratchSlots is the number of 8-byte stack slots reserved for spilling
// the left operand of nested binary expressions. 32 is plenty for toy code.
const scratchSlots = 32

// externDLL resolves an imported symbol to the DLL that exports it.
// Anything not listed here is a hard error rather than a silent guess.
// The C library itself (printf, strlen, malloc, ...) is implemented in pure
// assembly on top of kernel32 (Windows) or syscalls (Linux). The one source is
// goclib/goclib.asm, conditionally compiled per target (see selectPlatform); the
// portable C version goclib/goclib.c is the intended replacement once goc can
// compile it, but goc cannot today. There is no msvcrt anywhere in the pipeline.
var externDLL = map[string]string{
	// kernel32
	"GetStdHandle":        "kernel32",
	"WriteFile":           "kernel32",
	"ReadFile":            "kernel32",
	"ExitProcess":         "kernel32",
	"GetProcessHeap":      "kernel32",
	"HeapAlloc":           "kernel32",
	"HeapFree":            "kernel32",
	"GetLastError":        "kernel32",
	"SetLastError":        "kernel32",
	"GetFileType":         "kernel32",
	"GetConsoleMode":      "kernel32",
	"SetConsoleMode":      "kernel32",
	"WriteConsoleA":       "kernel32",
	"CloseHandle":         "kernel32",
	"FlushFileBuffers":    "kernel32",
	"GetModuleHandleA":    "kernel32",
	"GetModuleFileNameA":  "kernel32",
	"GetCommandLineA":     "kernel32",
	"GetEnvironmentVariableA": "kernel32",
	"SetEnvironmentVariableA": "kernel32",
	"GetCurrentDirectoryA":    "kernel32",
	"SetCurrentDirectoryA":    "kernel32",
	"GetTempPathA":        "kernel32",
	"GetComputerNameA":    "kernel32",
	"LoadLibraryA":        "kernel32",
	"FreeLibrary":         "kernel32",
	"GetTickCount":        "kernel32",
	"Sleep":               "kernel32",
	// user32
	"GetSystemMetrics":    "user32",
	"MessageBoxA":         "user32",
	"MessageBoxW":         "user32",
	"FindWindowA":         "user32",
	"GetWindowTextA":      "user32",
	"GetWindowTextLengthA": "user32",
	"SetWindowTextA":      "user32",
	"GetForegroundWindow": "user32",
	"GetDesktopWindow":    "user32",
	"IsWindow":            "user32",
	"EnableWindow":        "user32",
	"ShowWindow":          "user32",
	"SetFocus":            "user32",
	"SetCursorPos":        "user32",
	"GetSysColor":         "user32",
	"GetDoubleClickTime":  "user32",
	"SendMessageA":        "user32",
	"PostMessageA":        "user32",
	"GetDC":               "user32",
	"ReleaseDC":           "user32",
	// gdi32
	"GetStockObject":      "gdi32",
	"SelectObject":        "gdi32",
	"SetBkColor":          "gdi32",
	"SetTextColor":        "gdi32",
	"TextOutA":            "gdi32",
	"LineTo":              "gdi32",
	"Rectangle":           "gdi32",
	"Ellipse":             "gdi32",
	"PatBlt":              "gdi32",
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
// The .asm file goclib/goclib.asm is embedded into goc at build time. It is ONE
// source that carries BOTH platforms, selected per target by conditional
// compilation: the Windows body lives under "#if defined(_WIN64)" and the
// Linux body under the matching "#else" (see selectPlatform). This is the same
// trick a normal compiler uses for inline asm — write the portable parts once,
// isolate the OS-specific bits behind #ifdef.
//
// Inside the selected branch each function sits in a
// block marked with "; @func <name>" and can declare what it needs:
//
//	; @deps __goclib_write strlen   other goclib functions to pull in
//	; @extern WriteFile           imports to declare
//
// A block marked "; @data" holds that file's static data. goc emits only the
// functions a program actually calls (plus their transitive deps), so a
// hello-world does not pay for malloc.
//
// The genuinely cross-platform algorithms (strlen, strcpy, printf formatting,
// strtol, rand, ...) are also written in portable C in goclib/goclib.c, ready to
// REPLACE this assembly once goc supports char/pointer/globals/for (stage 5).
// Until then the assembly below is the backend that actually runs.

//go:embed goclib/*.asm
var goclibFS embed.FS

type goclibFunc struct {
	name string
	deps []string // other goclib functions required
	exts []string // imported symbols required
	body string
	file string
}

// goclibDataBlock is a chunk of static data emitted only when at least one of
// the functions named in `needs` made it into the program. That keeps
// printf's 512-byte output buffer out of a program that only calls putchar.
type goclibDataBlock struct {
	file  string
	needs []string
	text  string
}

var (
	goclibFuncsWin    = map[string]*goclibFunc{}
	goclibOrderWin    []string // source order, for stable output
	goclibBlocksWin   []goclibDataBlock
	goclibFuncsLinux  = map[string]*goclibFunc{}
	goclibOrderLinux  []string
	goclibBlocksLinux []goclibDataBlock
	goclibErr         error
)

// goclibStore picks the tables for a target.
func goclibStore(linux bool) (map[string]*goclibFunc, *[]string, *[]goclibDataBlock) {
	if linux {
		return goclibFuncsLinux, &goclibOrderLinux, &goclibBlocksLinux
	}
	return goclibFuncsWin, &goclibOrderWin, &goclibBlocksWin
}

func init() { goclibErr = loadClib() }

func loadClib() error {
	// The same embedded file is loaded twice, once per target; selectPlatform
	// strips the branch that does not apply before the @func blocks are parsed.
	if err := loadClibDir(goclibFS, "goclib", false); err != nil {
		return err
	}
	return loadClibDir(goclibFS, "goclib", true)
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
		// Drop the platform branch that does not apply to this target so that
		// one file carries both Windows and Linux bodies.
		src := selectPlatform(string(b), linux)
		if err := parseClib(f, src, linux); err != nil {
			return fmt.Errorf("%s/%s: %v", dir, f, err)
		}
	}
	return nil
}

// goclibAlias maps a C name to a different symbol on Linux. Needed where the C
// name would collide with an goa syscall stub of the same name.
var goclibAliasLinux = map[string]string{
	"exit": "__goclib_exit",
}

// selectPlatform evaluates a small subset of C conditional compilation over the
// goclib assembly source and returns only the lines that apply to the current
// target. goa assembly has no '#' lines of its own, so the directives are
// unambiguous. Supported:
//
//	#if defined(_WIN64)      #elif defined(__linux__)
//	#else                    #endif
//	#if 0  #if 1
//
// and the boolean operators ! && || ( ) inside the expressions. This is what
// lets one goclib.asm carry both platforms, selected at load time — the same
// trick a normal compiler uses for inline assembly.
// goclibFrame tracks one #if/#else chain level during platform selection.
type goclibFrame struct{ active, taken bool }

func selectPlatform(src string, linux bool) string {
	def := map[string]bool{"_WIN64": !linux, "__linux__": linux, "__x86_64__": true}
	lines := strings.Split(src, "\n")
	stack := []goclibFrame{{active: true}}
	out := make([]string, 0, len(lines))
	eval := func(expr string) bool {
		toks := ceLex(expr)
		v, _ := ceParse(toks, 0, def)
		return v
	}
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "#if "):
			c := eval(strings.TrimSpace(t[3:]))
			stack = append(stack, goclibFrame{active: topActive(stack) && c, taken: c})
			continue
		case strings.HasPrefix(t, "#elif "):
			f := &stack[len(stack)-1]
			if !f.taken {
				c := eval(strings.TrimSpace(t[5:]))
				f.taken = c
				f.active = topActive(stack[:len(stack)-1]) && c
			} else {
				f.active = false
			}
			continue
		case t == "#else":
			f := &stack[len(stack)-1]
			f.active = topActive(stack[:len(stack)-1]) && !f.taken
			f.taken = true
			continue
		case t == "#endif":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if topActive(stack) {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

func topActive(s []goclibFrame) bool {
	if len(s) == 0 {
		return true
	}
	return s[len(s)-1].active
}

// ceLex tokenises a #if/#elif boolean expression into a flat token slice.
func ceLex(s string) []string {
	var toks []string
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}
		switch c {
		case '(', ')', '!':
			toks = append(toks, string(c))
			i++
		case '&':
			toks = append(toks, "&&")
			i += 2
		case '|':
			toks = append(toks, "||")
			i += 2
		default:
			if isIdentStart(c) {
				j := i
				for j < n && isIdentChar(s[j]) {
					j++
				}
				toks = append(toks, s[i:j])
				i = j
			} else if c >= '0' && c <= '9' {
				j := i
				for j < n && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				toks = append(toks, s[i:j])
				i = j
			} else {
				i++ // ignore any other character
			}
		}
	}
	return toks
}

// ceParse evaluates a token slice as a boolean expression.
//
//	or   := and ('||' and)*
//	and  := not ('&&' not)*
//	not  := '!' not | primary
//	prim := '(' or ')' | 'defined' ('(' ident ')' | ident) | '1' | '0'
func ceParse(toks []string, i int, def map[string]bool) (bool, int) {
	return ceOr(toks, i, def)
}
func ceOr(toks []string, i int, def map[string]bool) (bool, int) {
	left, i := ceAnd(toks, i, def)
	for i < len(toks) && toks[i] == "||" {
		i++
		right, ni := ceAnd(toks, i, def)
		i = ni
		left = left || right
	}
	return left, i
}
func ceAnd(toks []string, i int, def map[string]bool) (bool, int) {
	left, i := ceNot(toks, i, def)
	for i < len(toks) && toks[i] == "&&" {
		i++
		right, ni := ceNot(toks, i, def)
		i = ni
		left = left && right
	}
	return left, i
}
func ceNot(toks []string, i int, def map[string]bool) (bool, int) {
	if i < len(toks) && toks[i] == "!" {
		v, ni := ceNot(toks, i+1, def)
		return !v, ni
	}
	return cePrimary(toks, i, def)
}
func cePrimary(toks []string, i int, def map[string]bool) (bool, int) {
	if i >= len(toks) {
		return false, i
	}
	t := toks[i]
	switch {
	case t == "(":
		v, ni := ceOr(toks, i+1, def)
		if ni < len(toks) && toks[ni] == ")" {
			ni++
		}
		return v, ni
	case t == "defined":
		i++
		name := ""
		if i < len(toks) && toks[i] == "(" {
			i++
			if i < len(toks) {
				name = toks[i]
				i++
			}
			if i < len(toks) && toks[i] == ")" {
				i++
			}
		} else if i < len(toks) {
			name = toks[i]
			i++
		}
		return def[name], i
	case t == "1":
		return true, i + 1
	case t == "0":
		return false, i + 1
	default:
		return false, i + 1
	}
}

func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentChar(c byte) bool  { return isIdentStart(c) || (c >= '0' && c <= '9') }

func parseClib(file, src string, linux bool) error {
	funcs, order, blocks := goclibStore(linux)
	var cur *goclibFunc
	var data []string
	var dataNeeds []string
	inData := false
	flush := func() error {
		if inData {
			*blocks = append(*blocks, goclibDataBlock{
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
			cur = &goclibFunc{name: name, file: file}
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

// goclibUsed expands the set of needed goclib functions into their transitive
// closure, and reports the imports and static data that go with them.
func goclibUsed(need map[string]bool, linux bool) (order []string, exts []string, data string, err error) {
	funcs, _, blocks := goclibStore(linux)
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
			return nil, nil, "", fmt.Errorf("goclib: function %q is not defined for this target", n)
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

// goclibNames lists the public (non-internal) goclib functions, for error messages.
func goclibNames(linux bool) []string {
	funcs, order, _ := goclibStore(linux)
	_ = funcs
	var out []string
	for _, n := range *order {
		if strings.HasPrefix(n, "__") {
			// Internal name that a C-facing alias points at: report the C name.
			for cName, sym := range goclibAliasLinux {
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
	return -(c.regArea + c.localBytes + 8*k)
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
	case 1:
		if signed {
			c.emit("shl rax, 56")
			c.emit("sar rax, 56")
		} else {
			c.emit("and rax, 0xff")
		}
	case 2:
		if signed {
			c.emit("shl rax, 48")
			c.emit("sar rax, 48")
		} else {
			c.emit("and rax, 0xffff")
		}
	case 4:
		if signed {
			c.emit("shl rax, 32")
			c.emit("sar rax, 32")
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
	return 8
}

// emit writes one indented instruction line.
func (c *CG) emit(format string, a ...any) {
	c.sb.WriteString("\t" + fmt.Sprintf(format, a...) + "\n")
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
	if vi.typ != nil && vi.typ.Kind == KDouble {
		c.emit("movsd xmm0, [rbp%+d]", vi.off)
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
		c.emit("mov %s, rax", vi.reg)
		return
	}
	if vi.typ != nil && vi.typ.Kind == KDouble {
		c.emit("movsd [rbp%+d], xmm0", vi.off)
		return
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
		vi, ok := c.vars[n.Name]
		if !ok {
		if c.globals[n.Name] {
			// Global / static variable: load its value via rip-relative
			// addressing into the .data section. Arrays decay to a pointer to
			// element 0 (mirroring local arrays).
			gt := c.globalTyp[n.Name]
			if gt != nil && gt.IsArray() {
				c.emit("lea rax, [rip+%s]", c.globalLab[n.Name])
				c.resTyp = TInt
				c.resSigned = false
				c.resW = 8
				return TInt, nil
			}
			if isAgg(gt) {
				// A whole struct/union value cannot be loaded into rax;
				// consumers must go through genLValue (see structSrcAddr).
				return TInt, fmt.Errorf("cannot load struct/union value %q directly", n.Name)
			}
			c.emit("mov rax, [rip+%s]", c.globalLab[n.Name])
			c.resTyp = TInt
			c.resSigned = gt != nil && gt.Kind == KInt && gt.Signed
			c.resW = c.semWOf(gt)
			return TInt, nil
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
		// Members are laid out at their C type width (MSVC x64 packs an int
		// member as 4 bytes), not the 8-byte scalar slot width -- loading the
		// slot width would read 4 bytes past a trailing int member. Double
		// members load into xmm0, not rax.
		width := c.typeWidth(t)
		if t.Kind == KDouble {
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
		c.sb.WriteString(lElse + ":\n")
		et, err := c.genExprT(n.Else)
		if err != nil {
			return et, err
		}
		wElse, sElse := c.resW, c.resSigned
		c.sb.WriteString(lEnd + ":\n")
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
		c.resTyp = n.Typ.Class()
		c.resSigned = n.Typ.Kind == KInt && n.Typ.Signed
		c.resW = c.semWOf(n.Typ)
		return c.resTyp, nil
	case *IncDecExpr:
		return c.genIncDec(n)
	case *Call:
		return c.genCallExpr(n)
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
		c.genStoreElem("r10", width, class)
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
func Gen(prog *Program, linux bool) (string, error) {
	if goclibErr != nil {
		return "", goclibErr
	}
	c := &CG{
		strLab:    map[*StrLit]string{},
		doubleLab: map[float64]string{},
		vars:      map[string]varInfo{},
		funcs:     map[string]bool{},
		funcDefs:  map[string]*FuncDecl{},
		calls:     map[string]bool{},
		need:      map[string]bool{},
		globals:   map[string]bool{},
		globalLab: map[string]string{},
		globalTyp: map[string]*Type{},
		linux:     linux,
	}
	for _, g := range prog.Globals {
		c.globals[g.Name] = true
		c.globalLab[g.Name] = "G_" + g.Name
		c.globalTyp[g.Name] = g.Typ
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

	// Pull in exactly the goclib functions this program calls, plus whatever
	// those depend on.
	order, goclibExts, dataBlock, err := goclibUsed(c.need, c.linux)
	if err != nil {
		return "", err
	}

	// Every import the program needs: the exit routine for the entry stub,
	// whatever the C code calls directly, and whatever goclib pulled in.
	importSet := map[string]bool{}
	if c.linux {
		importSet["exit"] = true
	} else {
		importSet["ExitProcess"] = true
	}
	for name := range c.calls {
		importSet[name] = true
	}
	for _, e := range goclibExts {
		importSet[e] = true
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
			return "", fmt.Errorf("unknown function %q: not in goclib (%s), and not in externDLL",
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
		out.WriteString("\n; --- goclib: only what this program uses ---\n")
		if dataBlock != "" {
			out.WriteString(dataBlock)
			out.WriteString("section .text\n")
		}
		funcs, _, _ := goclibStore(c.linux)
		for _, n := range order {
			out.WriteString(funcs[n].body)
		}
	}

	// Program-level (global / static) variables live in a writable .data
	// section, referenced via rip. Only constant integer initialisers are
	// supported today (goclib's globals are all simple constants). Arrays are
	// zero-filled for their full byte size so rip-relative indexing works.
	if len(prog.Globals) > 0 {
		out.WriteString("\nsection .data\n")
		for _, g := range prog.Globals {
			lab := c.globalLab[g.Name]
			if g.Typ != nil && (g.Typ.IsArray() || isAgg(g.Typ)) {
				// typeWidth already returns the full byte size (elem width *
				// len for arrays, the computed Size for structs/unions), so
				// that IS the size to zero-fill.
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
				continue
			}
			val := int64(0)
			if v, ok := foldConstInit(g.Init); ok {
				val = v
			}
			out.WriteString(fmt.Sprintf("%s dq %d\n", lab, val))
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
		case *AssignExpr:
			walkExpr(n.Lhs)
			walkExpr(n.Rhs)
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
		}
	}
	walkStmt(f.Body)
	return taken
}

func (c *CG) genFunc(f *FuncDecl) error {
	c.vars = map[string]varInfo{}
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
		case *ForStmt:
			if n.Init != nil {
				gather(n.Init)
			}
			gather(n.Body)
		}
	}
	for _, st := range f.Body.Stmts {
		gather(st)
	}

	// A local whose address is taken (&x) cannot live in a register, doubles
	// cannot live in a GPR, and arrays obviously need memory. Struct/union
	// aggregates likewise never live in a register. Everything else that is a
	// small int is a candidate for a callee-save register home.
	addrTaken := c.findAddressTaken(f)
	regPool := []string{"rbx", "r12", "r13", "r14"}
	c.usedRegs = nil
	regOf := map[string]string{}
	ri := 0
	stackDecls := make([]localDecl, 0, len(decls))
	for _, d := range decls {
		intClass := d.typ != nil && !d.typ.IsArray() && d.typ.Kind != KDouble &&
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
	frame := pad + regArea + localBytes + 8*scratchSlots + varargSave
	if frame%16 != 0 {
		frame += 16 - frame%16
	}
	// saveBaseOff points at save-area slot 0, which sits just below the
	// expression temporaries. The ABI pad added to frame above pushes it up
	// so the callee's argument-spill slots ([rsp+0..8*len(argRegs)) of the
	// caller) stay below it.
	c.saveBaseOff = -(regArea + localBytes + 8*scratchSlots + varargSave)

	c.sb.WriteString(f.Name + ":\n")
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
		if pt.Kind == KDouble {
			// XMM index: Windows numbers XMM argument registers positionally
			// (shared with the GP slot counter, hidden pointer included);
			// SysV numbers them by FP-argument order only.
			xmmAt := i + regShift
			if c.linux {
				xmmAt = fpIdx
			}
			fpIdx++
			if xmmAt < len(argXMM) {
				c.emit("movsd [rbp%+d], %s", vi.off, argXMM[xmmAt])
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
	// Safety epilogue in case a path has no explicit return.
	c.emitEpilogue()
	return nil
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
	case *DeclStmt:
		vi := c.vars[n.Name]
		if vi.typ.IsArray() {
			// Arrays are not initialised here; the checker rejects initialisers.
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
		if ec == TDouble {
			c.emit("movsd xmm0, [rbp%+d]", rslot)
			c.emit("movsd [r10], xmm0")
		} else {
			c.emit("mov rax, [rbp%+d]", rslot)
			c.genStoreElem("r10", c.lvalueWidth(n.Lhs), ec)
		}
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
		// Register break/continue targets so a break/continue inside the body
		// (or a nested loop) resolves to this loop. continue re-checks the
		// condition at lTop.
		c.loops = append(c.loops, loopLabels{breakLbl: lEnd, contLbl: lTop})
		if err := c.genStmt(n.Body); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
		c.loops = c.loops[:len(c.loops)-1]
		c.emit("jmp %s", lTop)
		c.sb.WriteString(lEnd + ":\n")
	case *ForStmt:
		lTop := c.newLabel("for")
		lEnd := c.newLabel("forend")
		lCont := c.newLabel("forcont")
		if n.Init != nil {
			if err := c.genStmt(n.Init); err != nil {
				return err
			}
		}
		c.sb.WriteString(lTop + ":\n")
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
		if err := c.genStmt(n.Body); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
		c.loops = c.loops[:len(c.loops)-1]
		c.sb.WriteString(lCont + ":\n")
		if n.Post != nil {
			if _, err := c.genExprT(n.Post); err != nil {
				return err
			}
		}
		c.emit("jmp %s", lTop)
		c.sb.WriteString(lEnd + ":\n")
	case *BreakStmt:
		if len(c.loops) == 0 {
			return fmt.Errorf("break outside a loop")
		}
		c.emit("jmp %s", c.loops[len(c.loops)-1].breakLbl)
	case *ContinueStmt:
		if len(c.loops) == 0 {
			return fmt.Errorf("continue outside a loop")
		}
		c.emit("jmp %s", c.loops[len(c.loops)-1].contLbl)
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
		c.resSigned = false
		c.resW = 8 // address-of yields a pointer value
		return TInt, nil
	case "*":
		// Dereference: n.E evaluates to the pointer value (in rax).
		if _, err := c.genExprT(n.E); err != nil {
			return TInt, err
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
	c.sb.WriteString(lTrue + ":\n")
	c.emit("mov rax, 1")
	c.sb.WriteString(lEnd + ":\n")
	c.resTyp = TInt
	c.resSigned = true
	c.resW = 4 // !x yields a (signed) int
	return TInt, nil
}

// genLValue emits code that leaves the address of the lvalue e in r10.
func (c *CG) genLValue(e Expr) error {
	switch n := e.(type) {
	case *Ident:
		vi, ok := c.vars[n.Name]
		if !ok {
			if c.globals[n.Name] {
				// Address of a global: rip-relative lea into .data.
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
				if c.globals[id.Name] {
					// Global array: rip-relative lea of element 0.
					gt := c.globalTyp[id.Name]
					if gt == nil || !gt.IsArray() {
						return fmt.Errorf("cannot index non-array global %q", id.Name)
					}
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
			if err := c.genLValue(n.Base); err != nil {
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
		return c.elemClassOf(n.Base)
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
			return c.elemWidthOf(n.E)
		}
		return 8
	case *Index:
		return c.elemWidthOf(n.Base)
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
		c.emit("movsd xmm0, [%s]", reg)
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
		c.emit("movsd [%s], xmm0", reg)
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
	if call, ok := e.(*Call); ok && isAgg(t) {
		if _, err := c.genExprT(call); err != nil {
			return err
		}
		if !c.resStruct {
			return fmt.Errorf("call %q does not produce a struct value", call.Name)
		}
		c.emit("lea r10, [rbp%+d]", c.tmpSlot(c.resStructK))
		return nil
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

// lvalueWidth returns the byte width of the value stored at the lvalue e. For a
// scalar/pointer this is its slot width (char=1, short=2, int=4, pointer=8);
// for a member/pointer-deref/array-subscript it is the element width.
func (c *CG) lvalueWidth(e Expr) int {
	if id, ok := e.(*Ident); ok {
		if vi, ok2 := c.vars[id.Name]; ok2 {
			if vi.typ.IsPtr() {
				return 8
			}
			if vi.typ.Kind == KInt {
				// char/short stay narrow; int becomes an 8-byte slot so it can
				// hold a 64-bit pointer that was stored in an int variable.
				return c.slotWidth(vi.typ)
			}
			if vi.typ.Kind == KStruct || vi.typ.Kind == KUnion {
				return vi.typ.Size
			}
			return 8
		}
		if c.globals[id.Name] {
			return 8
		}
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
		if c.globals[id.Name] {
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
	if t := c.exprType(n.E); t != nil && t.Kind == KDouble {
		double = true
	}
	if double {
		c.genLoadElem("r10", 8, TDouble, false)
		c.emit("movsd xmm1, xmm0") // keep current for postfix restore
		c.loadDoubleConst(1.0)
		if n.Op == "++" {
			c.emit("addsd xmm0, xmm1")
		} else {
			c.emit("subsd xmm0, xmm1")
		}
		c.genStoreElem("r10", 8, TDouble)
		if !n.Prefix {
			c.emit("movsd xmm0, xmm1")
		}
		c.resTyp = TDouble
		c.resSigned = false
		c.resW = 8
		return TDouble, nil
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
		c.sb.WriteString(lTrue + ":\n")
		c.emit("mov rax, 1")
		c.sb.WriteString(lEnd + ":\n")
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
}

// variadicFn reports whether a goclib function takes printf-style varargs.
// For those, goc passes every argument in an 8-byte "general-purpose slot"
// (doubles are moved bitwise into rax first), so the __goclib_va array lines
// up positionally with the format string: the %d/%f/%s consumers all walk
// the same 8-byte cursor, exactly like the goclib prologue expects.
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
//     double bit patterns included, so __goclib_va needs no XMM handling.
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
	argRegs := c.argRegs()
	argXMM := c.argXMM()
	nargs := len(n.Args)
	if nargs > maxArgs {
		return TInt, fmt.Errorf("%s: too many arguments (max %d)", n.Name, maxArgs)
	}
	// Struct/union return: the caller allocates a temporary result buffer,
	// passes its address as the hidden first argument (argRegs[0]) and every
	// user argument shifts one register slot to the right.
	retT := (*Type)(nil)
	if f, ok := c.funcDefs[n.Name]; ok {
		retT = f.Ret
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

	// Resolve the callee. A goclib function may live under a different symbol
	// than its C name (see goclibAliasLinux).
	target := n.Name
	if c.linux {
		if a, ok := goclibAliasLinux[n.Name]; ok {
			target = a
		}
	}
	if !c.funcs[n.Name] && n.Name != "main" {
		funcs, _, _ := goclibStore(c.linux)
		if _, ok := funcs[target]; ok {
			c.need[target] = true
		} else {
			c.calls[n.Name] = true
		}
	}

	varargs := variadicFn(n.Name)
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
	for i := 0; i < nargs; i++ {
		// Struct/union arguments are passed by hidden pointer: evaluate the
		// ADDRESS of the value into r10 and spill that address into one GP
		// slot (the callee copies the bytes into its own local slot).
		if at := c.exprType(n.Args[i]); isAgg(at) {
			if err := c.structSrcAddr(n.Args[i], at); err != nil {
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
				c.emit("movsd %s, [rbp%+d]", argXMM[xmmAt], c.tmpSlot(s.slot))
				xmmIdx++
				continue
			}
			c.emit("movsd xmm0, [rbp%+d]", c.tmpSlot(s.slot))
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
	c.emit("call %s", target)
	// Windows API imports return 32-bit values (BOOL/DWORD/int) in EAX; the
	// upper 32 bits of RAX are not guaranteed to be zero, unlike goclib
	// functions which leave a clean 64-bit RAX. goc's register model treats
	// every int-class result as a full 64-bit word, so widen the result to
	// match the declared return type: sign-extend for signed int, zero-extend
	// for unsigned (DWORD/UINT). 8-byte returns (HANDLE, LONG, pointers) are
	// left untouched.
	if !c.linux {
		if f, ok := c.funcDefs[n.Name]; ok && externDLL[n.Name] != "" &&
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
