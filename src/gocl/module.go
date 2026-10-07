package gocl

import (
	"goc/frontend"
	"sort"
	"strconv"
	"strings"
)

// LLVM IR module scaffolding: type rendering, name mangling and the global
// table.
//
// The IR is emitted as text rather than built through LLVM's C API. That is not
// a shortcut -- it is what makes the backend testable without a shared library
// present, keeps the output readable for debugging, and lets the same module go
// straight to LLVMVerifyModule for a real syntax check before anything is
// emitted. LLVM parses the text with the same parser it uses for .ll files, so
// what is verified here is exactly what would be compiled.

// irMod accumulates one LLVM IR module.
type irMod struct {
	structs map[string]bool           // struct type names already defined
	structN map[*frontend.Type]string // C struct type -> LLVM name
	seq     int

	globals []irGlobal
	// prototypes are emitted as `declare` so a call can be resolved even when
	// the definition lives in the object goa's own assembler produces (the C
	// runtime) or in another translation unit.
	protos map[string]bool
	// prototypeLines holds the `declare` statements, in insertion order, so a
	// call can be resolved before the definition appears later in the file.
	prototypeLines []string
	// typeLines holds the struct type definitions, emitted before anything
	// that might refer to them.
	typeLines []string
	// extSig de-duplicates external declarations by name and arity.
	extSig map[string]bool
	// extRet remembers a declared return type per external.
	extRet map[string]*frontend.Type
	// strings interns string constants, mapping their contents to a global.
	strings map[string]string
	// consts interns every folded constant by its rendered form, so two globals
	// initialised identically share one object instead of storing it twice.
	consts map[string]string
	// funcBodies holds each finished definition; they are appended after the
	// declarations so a function may call one defined later in the file.
	funcBodies []string
	// skipped names functions this module deliberately does not define
	// because another generator does. They still get declared, so calls to
	// them type-check and link.
	defined map[string]bool
	// symKind says what a C identifier written in a static initialiser names:
	// "global" for a file-scope or runtime object (emitted as G_<name>),
	// "func" for a function or a prototype. It is the only way "&g" in an
	// initialiser can be rendered as the address of the right symbol -- the
	// initialiser is lowered before the definitions exist, and a name that is
	// not in here is not something whose address can be taken.
	symKind map[string]string
	// optSize asks the optimiser for a size-oriented result. LLVM 21 removed
	// the `Os` pipeline and replaced it with the optsize attribute on the
	// functions to shrink, run under O2 -- so this is what -Os now means, and
	// the pipeline string alone can no longer ask for it.
	optSize bool
	// linux selects the ELF target triple and the SysV x86-64 data layout. The
	// IR front end owns this so the module it emits names the machine LLVM must
	// lower to -- goc's own code generator switches its whole register file on
	// the same flag, and the LLVM backend has to agree or the object describes a
	// different ABI than the C runtime it is linked against.
	linux bool
	// tlsOffsets maps a C thread-local global's name to its byte offset inside
	// the .tls section. The linker lays the variable out there and provides the
	// __goc_tls_slot helper to reach it; the IR references the variable through
	// that helper with this offset, so the access code and the storage always
	// agree on where the variable lives.
	tlsOffsets map[string]int64
}

type irGlobal struct {
	name string
	ty   string
	init string // constant expression, or "" for zero
	// external marks a global this module only references; it is emitted as a
	// declaration and the definition comes from the other generator.
	external bool
	// constant marks a private object holding folded initialiser bytes, not a
	// variable the program names.
	constant bool
}

func newIRMod(linux bool) *irMod {
	m := &irMod{
		structs:    map[string]bool{},
		structN:    map[*frontend.Type]string{},
		protos:     map[string]bool{},
		defined:    map[string]bool{},
		extSig:     map[string]bool{},
		extRet:     map[string]*frontend.Type{},
		strings:    map[string]string{},
		consts:     map[string]string{},
		linux:      linux,
		tlsOffsets: map[string]int64{},
	}
	return m
}

// dso returns the "dso_local" attribute for the Linux target. A static ELF
// image is one non-preemptible unit: every global and function the module
// defines or references lives in the same executable, so marking it dso_local
// tells LLVM to bind references PC-relative (R_X86_64_PC32) instead of through a
// GOT (R_X86_64_GOTPCRELX), which a no-GOT static image cannot satisfy.
func (m *irMod) dso() string {
	if m.linux {
		return "dso_local "
	}
	return ""
}

// String returns the assembled module: type definitions, then globals, then
// prototypes, then the function bodies.
// Externals lists the globals this module declared but did not define, which
// is exactly the set the entry stub has to give storage to.
//
// A global lands here when its initialiser is a relocation -- the address of
// another global, or of a function -- because a COFF or ELF object cannot carry
// a relocation in an initialiser without a fixup, and goa emits none. The
// alternative would be a linker script; emitting the slot and letting the stub
// write it at startup keeps the object self-contained.
//
// It matters that this set is small and specific. A global the module *does*
// define must not be given storage by the stub as well: two definitions of one
// symbol, and the program reads whichever the linker ordered first.
func (m *irMod) Externals() []string {
	var out []string
	for _, g := range m.globals {
		if g.external {
			out = append(out, g.name)
		}
	}
	sort.Strings(out)
	return out
}

func (m *irMod) String() string {
	// The layout string describes the machine goa's own assembler targets, so a
	// struct laid out by one half of the compiler is read identically by the
	// other. On Windows it is the m:w (MS ABI) layout; on Linux the SysV one
	// (m:e), because the two differ in struct-by-value passing and aggregate
	// alignment -- getting this wrong makes a struct return or a >8-byte
	// argument land at the wrong offset.
	layout := "e-m:w-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
	triple := "x86_64-pc-windows-msvc"
	if m.linux {
		layout = "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
		triple = "x86_64-pc-linux-gnu"
	}

	var b strings.Builder
	b.WriteString("target datalayout = \"" + layout + "\"\n")
	b.WriteString("target triple = \"" + triple + "\"\n\n")
	for _, t := range m.typeLines {
		b.WriteString(t + "\n")
	}
	if len(m.typeLines) > 0 {
		b.WriteString("\n")
	}
	for _, g := range m.globals {
		if g.external {
			b.WriteString("@" + g.name + " = external " + m.dso() + "global " + g.ty + "\n")
			continue
		}
		init := g.init
		if init == "" {
			init = "zeroinitializer"
		}
		b.WriteString("@" + g.name + " = " + m.dso() + "global " + g.ty + " " + init + ", align " +
			itoa(alignOfLlir(g.ty)) + "\n")
	}
	if len(m.globals) > 0 {
		b.WriteString("\n")
	}
	// A thread-local variable is reached through the linker-provided
	// __goc_tls_slot helper (see call.go lvalue / expression.go ident), which
	// returns the variable's per-thread address from its .tls offset. The
	// helper's body is assembled by the entry stub; this declares it so the IR
	// can call it.
	if len(m.tlsOffsets) > 0 {
		b.WriteString("declare ptr @__goc_tls_slot(i64)\n\n")
	}
	for _, p := range m.prototypeLines {
		b.WriteString(p + "\n")
	}
	if len(m.prototypeLines) > 0 {
		b.WriteString("\n")
	}
	for _, f := range m.funcBodies {
		b.WriteString(f)
		b.WriteString("\n")
	}
	// A static ELF image has no GOT or PLT, so the Linux module must ask LLVM for
	// non-PIE code: a PIC Level and PIE Level of 0 make every reference to a
	// static symbol a plain PC-relative relocation (R_X86_64_PC32) instead of a
	// GOTPCRELX one. Without this, x86_64-pc-linux-gnu defaults to PIE and
	// `&"..string.."` in the codegen becomes a GOT-relative load gocld cannot
	// resolve (it builds no GOT), failing the link with "unsupported relocation
	// type 42".
	if m.linux {
		b.WriteString("!0 = !{i32 1, !\"PIC Level\", i32 0}\n")
		b.WriteString("!1 = !{i32 1, !\"PIE Level\", i32 0}\n")
		b.WriteString("!llvm.module.flags = !{!0, !1}\n")
	}
	return b.String()
}

// --- types ------------------------------------------------------------------

// llirType renders a C type as an LLVM type.
//
// Integer widths and signedness are preserved because C's usual arithmetic
// conversions depend on both. A pointer is the opaque `ptr`: goc's C subset has
// no typed pointers, and the front end inserts the casts that need. A float
// rides in an i32, matching what the native code generator does, so a value
// means the same thing on both paths.
// boolIr is the IR type of a value that is already a condition.
func boolIr() *frontend.Type { return &frontend.Type{Kind: frontend.KBool} }

func (m *irMod) llirType(t *frontend.Type) string {
	if t == nil {
		return "i32"
	}
	switch t.Kind {
	case frontend.KVoid:
		return "void"
	case frontend.KBool:
		// C's _Bool is a byte, but the IR front end uses this type for the
		// result of a comparison and for a reduced controlling expression,
		// which LLVM models as i1. A _Bool stored to memory is written through
		// an i8 slot by the store path, so nothing else depends on this.
		return "i1"
	case frontend.KBitInt:
		// Not modelled as a distinct type. goc's own generator handles these
		// and the caller keeps such functions off this path.
		return "i64"
	case frontend.KInt:
		switch t.Width {
		case 1:
			return "i8"
		case 2:
			return "i16"
		case 8:
			return "i64"
		default:
			return "i32"
		}
	case frontend.KFloat:
		return "float"
	case frontend.KDouble:
		return "double"
	case frontend.KPtr, frontend.KFunc:
		return "ptr"
	case frontend.KArr:
		return "[" + itoa(lenOfTy(t)) + " x " + m.llirType(t.Elem) + "]"
	case frontend.KStruct, frontend.KUnion:
		return "%" + m.structName(t)
	}
	return "i32"
}

func isArrayTy(s string) bool { return len(s) > 0 && s[0] == '[' }

func alignOfLlir(ty string) int {
	switch {
	case len(ty) > 1 && ty[0] == 'i':
		n := 0
		for i := 1; i < len(ty); i++ {
			if ty[i] < '0' || ty[i] > '9' {
				return 8
			}
			n = n*10 + int(ty[i]-'0')
		}
		// i1/_Bool is a byte in memory; n/8 would give 0, and LLVM rejects
		// an alignment of 0.
		if a := n / 8; a >= 1 {
			return a
		}
		return 1
	case ty == "float":
		return 4
	case ty == "double":
		return 8
	case ty == "ptr":
		return 8
	}
	// An array or a struct is as aligned as its elements. Reporting 1 for
	// "[1024 x i8]" put a char buffer the C runtime hands to a Win32 API on an
	// odd address, and WriteFile wrote through it faulted: the API assumes the
	// alignment its own prototype implies, not the alignment of the element.
	// "[3 x i64]" is the Windows x64 va_list, which is only ever accessed
	// through pointers, so it keeps its own tighter answer above.
	if strings.HasPrefix(ty, "[") {
		if i := strings.Index(ty, " x "); i > 0 {
			return alignOfLlir(ty[i+3 : len(ty)-1])
		}
	}
	if strings.HasPrefix(ty, "%") {
		return 8
	}
	return 1
}

// structName defines the LLVM type for a struct or union on first use and
// returns its name. A tagged struct uses its tag so the IR stays readable; an
// anonymous one is numbered, since nothing else can name it.
func (m *irMod) structName(t *frontend.Type) string {
	if n, ok := m.structN[t]; ok {
		return n
	}
	base := "anon"
	if t.Tag != "" {
		base = sanitize(t.Tag)
	}
	// frontend.Structs can refer to one another, so reserve the name before recursing.
	name := base
	if _, taken := m.structs[name]; taken {
		m.seq++
		name = base + "." + itoa(m.seq)
	}
	m.structN[t] = name
	m.structs[name] = true

	var body string
	if t.Kind == frontend.KUnion {
		// A union is as wide as its widest member, and its first member
		// already occupies that width -- only the remainder, if any, needs an
		// explicit pad. Adding the whole width on top of the member doubled
		// it: "union U { char *p; int x; }" came out as "{ ptr, [8 x i8] }",
		// 16 bytes for what C calls 8.
		first, pad := unionLayout(t)
		ty := "i8"
		if first != nil {
			ty = m.llirType(first)
		}
		body = "{ " + ty + " }"
		if pad > 0 {
			body = "{ " + ty + ", [" + itoa(pad) + " x i8] }"
		}
	} else {
		parts := make([]string, 0, len(t.Members))
		for _, mem := range t.Members {
			parts = append(parts, m.llirType(mem.Type))
		}
		if len(parts) == 0 {
			body = "{ i8 }"
		} else {
			body = "{ " + joinStrings(parts, ", ") + " }"
		}
	}
	// No trailing pad member. LLVM lays a struct out from its members per the
	// target datalayout -- inserting the same internal and trailing padding the
	// C ABI does -- so "{ ptr, i32 }" is already 16 bytes with the right
	// alignment. Adding a pad member of our own both inflates the size (a
	// pad rounded up to the struct's alignment made it 24) and puts a member
	// the C source never mentions in the type, which every brace initialiser
	// then fails to fill ("initializer with struct type has wrong # elements").
	m.typeLines = append(m.typeLines, "%"+name+" = type "+body)
	return name
}

// unionLayout describes how a union is laid out in LLVM: the first member
// keeps its own type (so member access can use it directly) and pad is the
// number of extra bytes needed to reach the union's C size. The type emitter
// and the constant initialiser both go through this so they cannot disagree
// about how many members a union has.
func unionLayout(t *frontend.Type) (first *frontend.Type, pad int) {
	if len(t.Members) == 0 {
		return nil, 0
	}
	first = t.Members[0].Type
	wide := frontend.Sizeof(first)
	for _, mem := range t.Members[1:] {
		if s := frontend.Sizeof(mem.Type); s > wide {
			wide = s
		}
	}
	size := t.Size
	if size <= 0 {
		size = wide
	}
	if size > wide {
		wide = size
	}
	if pad = wide - frontend.Sizeof(first); pad < 0 {
		pad = 0
	}
	return first, pad
}

// hasGlobal reports whether name is a module-level variable.
func (m *irMod) hasGlobal(name string) bool {
	for _, g := range m.globals {
		if g.name == name {
			return true
		}
	}
	return false
}

// declareFunc records a `declare` for a function this module calls but does not
// define -- the C runtime and the entry stub come from goa's own assembler, so
// their bodies live in the other half of the link. The signature here is
// deliberately loose (everything as i32) because the call sites that matter go
// through the runtime's real prototypes; what matters for correctness is that
// the symbol is declared exactly once and with a fixed type.
func (m *irMod) declareFunc(name, ret string, params []string) {
	if m.protos[name] {
		return
	}
	m.protos[name] = true
	ps := ""
	if len(params) > 0 {
		ps = strings.Join(params, ", ")
	}
	m.prototypeLines = append(m.prototypeLines,
		"declare "+m.dso()+ret+" @"+name+"("+ps+")")
}

// --- small helpers ----------------------------------------------------------

func itoa(n int) string { return strconv.Itoa(n) }

func joinStrings(a []string, sep string) string {
	var b strings.Builder
	for i, s := range a {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s)
	}
	return b.String()
}

// sanitize turns a C identifier or tag into an LLVM identifier. Names that are
// not already valid -- a leading digit, say -- are prefixed rather than
// rejected, because a mangled name is better than a failed compile.
func sanitize(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' || c == '.' || c == '$' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if ok {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// llvmMisrecognizedMaxMin lists the C library names LLVM's TargetLibraryInfo
// claims for its own libcall expansion, and which therefore must not appear
// verbatim as an LLVM symbol.
//
// The mechanism is name-based and it overrides the body: InstCombine sees
// `call double @fmaximum_num(...)`, decides from the name alone that this is a
// libfunc, and replaces the call with the libcall's semantics -- which for
// fmaximum_num is fmax's, not its own. C23 splits the maxima family in two:
// fmax SKIPS a NaN and returns the other operand, fmaximum_num PROPAGATES it and
// raises a domain error. Collapsing the two turns fmaximum_num(NaN, 1) into 1.0,
// and the miscompile is silent -- no diagnostic, no trap, just a wrong number in
// the program's output.
//
// The substitution is worse than it looks, because it does not need the body to
// be the libfunc's. A definition of `@fmaximum_num` whose body is a bare
// `ret double %p0` -- which returns its first argument and so must answer NaN --
// still gets replaced, and answers 1.0. Nothing about the IR says "max"; only
// the spelling of the symbol does.
//
// Measured on LLVM 22.1.8 (the libLLVM gocl links is 23.x, which behaves the
// same way), and reproducible outside gocl: clang -O2 on
// `fmaximum_num(0.0/0.0, 1.0)` prints 1.000000, because clang lowers the call to
// @llvm.maximumnum.f64 and folds that with the same wrong rule. gcc -O2 gets it
// right. The upstream fix is in flight in the min/max SimplifyDemandedFPClass
// work; until it lands, the portable answer is to keep the name LLVM keys on out
// of the IR entirely.
//
// Only the `_num` pair is renamed. fmax and fmin are *supposed* to skip a NaN, so
// the expansion LLVM performs for them is the semantics the standard asks for,
// and a program that calls them through a function pointer still wants a real
// symbol of that name. The two families also behave identically for every input
// that is not a NaN, so nothing else in the library can tell them apart.
var llvmMisrecognizedMaxMin = map[string]string{
	"fmaximum_num": "goclib_fmaximum_num",
	"fminimum_num": "goclib_fminimum_num",
}

// irFuncSym returns the LLVM symbol a C function name is emitted as. It is the
// C name unchanged for almost everything, and a private spelling for the two
// libfuncs LLVM would otherwise rewrite -- see the table above.
//
// Renaming is safe here because gocl compiles a whole program into one LLVM
// module: goclib's definitions and the user's calls are in the same module, so
// both sides go through this function and agree. Nothing links against these
// symbols from another object, and the image gocld builds keeps whatever spelling
// the IR used.
func irFuncSym(cName string) string {
	if alt, ok := llvmMisrecognizedMaxMin[cName]; ok {
		return alt
	}
	return cName
}

// lenOfTy returns the element count of an array type, or 1.
func lenOfTy(t *frontend.Type) int {
	if t.Len <= 0 {
		return 1
	}
	return t.Len
}

// --- extern tracking --------------------------------------------------------

// noteExtern records a function this module calls but does not define. The C
// runtime and the entry stub come from goa's own assembler, so their bodies
// arrive through the other half of the link; all that is needed here is a
// declaration with the right shape.
func (m *irMod) noteExtern(name string, ret *frontend.Type, params []*frontend.Type) {
	// The return type is recorded on every call, not only the ones that need a
	// `declare` line: callExpr reads it back through externRet to type the
	// `call` instruction, and that has to be void for a void function however
	// this module came to define it. Missing this makes every void callee
	// resolve to int, so the call reads "call i32 @f" against the "void @f"
	// the module defines -- which LLVM reports as a redefinition.
	m.setExternRet(name, ret)
	if m.defined[name] {
		return
	}
	key := name + "/" + itoa(len(params))
	if m.extSig[key] {
		return
	}
	m.extSig[key] = true
	var ps []string
	for _, p := range params {
		ps = append(ps, m.llirType(p))
	}
	r := "i32"
	if ret != nil {
		r = m.llirType(ret)
	}
	// irFuncSym: a declaration has to spell the symbol the call site names. For
	// the renamed libfuncs that is not the C name, and a `declare` under the C
	// name would leave the module with a declaration nobody defines and a call
	// to a symbol nobody declared.
	m.prototypeLines = append(m.prototypeLines,
		"declare "+m.dso()+r+" @"+irFuncSym(name)+"("+strings.Join(ps, ", ")+")")
}

// noteExternGlobal declares a global this module reads or writes but does not
// define. The definition comes from the other generator; without the
// declaration a reference would be an undefined symbol at link time.
func (m *irMod) noteExternGlobal(name, ty string) {
	m.globals = append(m.globals, irGlobal{name: name, ty: ty, external: true})
}

// noteIntrinsic declares one of LLVM's own intrinsics. They are not library
// functions and have no body; the exact signature matters, because a mismatch is
// accepted at parse time and rejected at codegen.
func (m *irMod) noteIntrinsic(name, ret string, params []string) {
	if m.protos[name] {
		return
	}
	m.protos[name] = true
	if ret == "" {
		ret = "void"
	}
	m.prototypeLines = append(m.prototypeLines,
		"declare "+m.dso()+ret+" @"+name+"("+strings.Join(params, ", ")+")")
}

// externRet reports the return type a previous noteExtern recorded, defaulting
// to int so a call whose prototype was never seen still type-checks.
func (m *irMod) externRet(name string) *frontend.Type {
	if t, ok := m.extRet[name]; ok {
		return t
	}
	return frontend.IntType()
}

// setExternRet records a declared return type.
func (m *irMod) setExternRet(name string, t *frontend.Type) {
	if m.extRet == nil {
		m.extRet = map[string]*frontend.Type{}
	}
	m.extRet[name] = t
}

// addString interns a string constant and returns its global name.
func (m *irMod) addString(b []byte) string {
	if m.strings == nil {
		m.strings = map[string]string{}
	}
	key := string(b)
	if n, ok := m.strings[key]; ok {
		return n
	}
	name := ".str." + itoa(len(m.strings))
	m.strings[key] = name
	ty := "[" + itoa(len(b)) + " x i8]"
	m.globals = append(m.globals, irGlobal{name: name, ty: ty, init: cString(b)})
	return name
}

// cStringN renders n bytes as an LLVM [n x i8] constant, NUL-padding or
// truncating the input to fit. An initialiser for `char s[4] = "abc"` has to
// produce exactly four elements, and one for `char *p = "abc"` exactly the
// string's own length plus its terminator.
func cStringN(b []byte, n int) string {
	if n < 0 {
		n = 0
	}
	if len(b) > n {
		b = b[:n]
	}
	padded := make([]byte, n)
	copy(padded, b)
	return cString(padded)
}

// cString renders a byte sequence as an LLVM [N x i8] constant.
func cString(b []byte) string {
	var sb strings.Builder
	sb.WriteString("c\"")
	for _, c := range b {
		switch {
		case c == '"':
			sb.WriteString("\\22")
		case c == '\\':
			sb.WriteString("\\5C")
		case c >= 0x20 && c < 0x7f:
			sb.WriteByte(c)
		default:
			sb.WriteString("\\")
			const hex = "0123456789ABCDEF"
			sb.WriteByte(hex[c>>4])
			sb.WriteByte(hex[c&0xf])
		}
	}
	sb.WriteString("\"")
	return sb.String()
}

// convertTo converts a value to a named LLVM type. It is the counterpart of
// convert for the places where the target is already rendered as text -- a
// function's return type, for instance.
func (e *irEmitter) convertTo(op string, from *frontend.Type, toIR string) string {
	// An array operand has already decayed to a pointer to its first element by
	// the time it is a value. Converting from the array type itself renders
	// "[256 x i8]", which is not what the value is: the operand is the slot's
	// address. Reading it as an array produced "bitcast [256 x i8] %p to ptr"
	// against a pointer, which LLVM rejects.
	if from != nil && from.Kind == frontend.KArr {
		from = frontend.PtrType(from.Elem)
	}
	f := e.ty(from)
	if f == toIR {
		return op
	}
	if isFloatTy(from) {
		v := e.newTmp()
		if f == "double" && toIR == "float" {
			e.line("%s = fptrunc double %s to float", v, op)
			return v
		}
		if f == "float" && toIR == "double" {
			e.line("%s = fpext float %s to double", v, op)
			return v
		}
	}
	// Integer <-> floating point is a value conversion, not a reinterpretation,
	// so it has to happen before the integer-width branch below -- an i1 and a
	// double are both "different shapes", and left to the store-and-load
	// fallback the 1-byte i1 was stored into an 8-byte slot and read back as a
	// double, yielding whatever the adjacent bytes happened to hold. That is
	// how "return (x != x);" in a function returning double came to answer
	// 5368713217 instead of 0.0 or 1.0.
	//
	// An i1 source is signed: an LLVM i1 carries the values 0 and -1, so sitofp
	// turns true into 1.0 and uitofp would turn it into 1.0 as well by luck of
	// the zero-extension, but false into 0.0 either way. sitofp states the
	// intent.
	// An i1 source is neither signed nor unsigned: LLVM's i1 holds 0 and -1,
	// so sitofp yields -1.0 for true and uitofp is not available on i1 at all.
	// Both are wrong here, because C means 1.0. The three-step widen is what
	// clang does and what keeps the value honest: zext to i64 turns the -1 bit
	// pattern into 1, and uitofp then gives 1.0. (sitofp straight off the i1
	// is not merely imprecise, it is actively wrong -- LLVM legalises it into
	// "cmpunordsd + andpd <sign mask>", which hands back -1.0.)
	if isIntIr(f) && isFloatIr(toIR) {
		src := op
		if f == "i1" {
			w := e.newTmp()
			e.line("%s = zext i1 %s to i64", w, op)
			src, f = w, "i64"
		}
		v := e.newTmp()
		opc := "uitofp"
		if from != nil && from.Signed {
			opc = "sitofp"
		}
		e.line("%s = %s %s %s to %s", v, opc, f, src, toIR)
		return v
	}
	if isFloatIr(f) && isIntIr(toIR) {
		v := e.newTmp()
		e.line("%s = %s %s %s to %s", v, fptoiOp(toIR), f, op, toIR)
		return v
	}
	// Integers: same width and different signedness needs no instruction, since
	// both live in the same register and C only reinterprets the bits.
	if len(f) > 1 && f[0] == 'i' && len(toIR) > 1 && toIR[0] == 'i' {
		fw, tw := irIntWidth(f), irIntWidth(toIR)
		switch {
		case fw < tw:
			v := e.newTmp()
			if from != nil && from.Signed {
				e.line("%s = sext %s %s to %s", v, f, op, toIR)
			} else {
				e.line("%s = zext %s %s to %s", v, f, op, toIR)
			}
			return v
		case fw > tw:
			v := e.newTmp()
			e.line("%s = trunc %s %s to %s", v, f, op, toIR)
			return v
		}
		return op
	}
	// A pointer and an integer are not interchangeable by truncation: LLVM has
	// dedicated opcodes for that, and using trunc between them is rejected with
	// "invalid cast opcode". A pointer reaching an integer slot is C's pointer
	// arithmetic on this target (an int is wide enough to hold it).
	if f == "ptr" && len(toIR) > 1 && toIR[0] == 'i' {
		v := e.newTmp()
		e.line("%s = ptrtoint ptr %s to %s", v, op, toIR)
		return v
	}
	if len(f) > 1 && f[0] == 'i' && toIR == "ptr" {
		v := e.newTmp()
		e.line("%s = inttoptr %s %s to ptr", v, f, op)
		return v
	}
	if f == "ptr" || toIR == "ptr" {
		v := e.newTmp()
		if f == "ptr" {
			e.line("%s = bitcast ptr %s to %s", v, op, toIR)
		} else {
			e.line("%s = bitcast %s %s to ptr", v, f, op)
		}
		return v
	}
	// Anything else moves through memory: the two types have different shapes,
	// so a store and a load of the right width is the general answer. The
	// staging slot has to hold the whole source object, which for an aggregate
	// is more than the eight bytes a scalar needs.
	n := 8
	if from != nil {
		if sz := frontend.Sizeof(from); sz > n {
			n = sz
		}
	}
	slot := e.scratchSlot(n)
	e.line("store %s %s, ptr %s, align %d", f, op, slot, alignOfIr(f))
	v := e.newTmp()
	e.line("%s = load %s, ptr %s, align %d", v, toIR, slot, alignOfIr(toIR))
	return v
}

// isIntIr reports whether an IR type name is an integer ("i1", "i32", "i64").
func isIntIr(s string) bool {
	return len(s) > 1 && s[0] == 'i' && s[1] >= '0' && s[1] <= '9'
}

// isFloatIr reports whether an IR type name is a floating-point type.
func isFloatIr(s string) bool {
	return s == "float" || s == "double"
}

// fptoiOp picks the signed or unsigned float-to-integer opcode. An i1 target
// has no width to speak of and only ever holds 0 or -1, so signed is the only
// spelling that can work.
func fptoiOp(toIR string) string {
	if toIR == "i1" {
		return "fptosi"
	}
	if toIR == "i32" {
		return "fptosi"
	}
	return "fptoui"
}

// irIntWidth returns the bit width of an "iN" type, 64 when it cannot be read.
func irIntWidth(ty string) int {
	if len(ty) < 2 || ty[0] != 'i' {
		return 64
	}
	n := 0
	for i := 1; i < len(ty); i++ {
		if ty[i] < '0' || ty[i] > '9' {
			return 64
		}
		n = n*10 + int(ty[i]-'0')
	}
	return n
}

// doGoto branches to a C label. A backward target is already known; a forward
// one is not, so the branch is written through a placeholder and patched once
// the label has been seen.
func (e *irEmitter) doGoto(n *frontend.GotoStmt) {
	if l, ok := e.userLabels[n.Label]; ok {
		e.term("br label %%%s", l)
		return
	}
	// Emit through a named placeholder block so the text can be rewritten in
	// place when the target appears; a real target later replaces the whole
	// line.
	ph := e.newLabel()
	off := e.bodyLen()
	e.term("br label %%%s", ph)
	e.forwards = append(e.forwards, forwardGoto{
		label: n.Label, at: off, placeholder: ph,
	})
}

// blockLen reports the current length of the body buffer, which is where a
// forward goto's branch text will need patching.
func (e *irEmitter) bodyLen() int { return e.body.Len() }

// globalSym returns the assembler-level symbol for a C global, or "" if the
// module does not reference it. The two generators have to name a global
// identically or they end up referring to different storage: goa's assembler
// prefixes every global's label with "G_" so it cannot collide with a function
// of the same name.
func (m *irMod) globalSym(cName string) string {
	sym := "G_" + cName
	for _, g := range m.globals {
		if g.name == sym {
			return sym
		}
	}
	return ""
}

// tlsOffset reports the .tls-section offset of a thread-local global, or false
// if cName is not one. The offset is exactly the one ComputeTLSLayout assigned
// and the linker's .tls image used, so the IR's access and the section's storage
// cannot drift apart.
func (m *irMod) tlsOffset(cName string) (int64, bool) {
	off, ok := m.tlsOffsets[cName]
	return off, ok
}
