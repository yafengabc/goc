package main

import (
	"fmt"
	"os"
)

// typeKeywords are the keywords the parser treats as the start of a type
// specifier.
var typeKeywords = map[string]bool{
	"void": true, "char": true, "int": true, "long": true, "short": true,
	"unsigned": true, "signed": true, "double": true, "float": true,
	"struct": true, "union": true, "enum": true, "_Bool": true, "bool": true, "char8_t": true,
	"_BitInt": true,
}

// qualifierKeywords are type qualifiers that decorate a specifier list but
// carry no codegen meaning for the toy model (const/volatile/restrict).
var qualifierKeywords = map[string]bool{
	"const": true, "volatile": true, "restrict": true,
}

// storageKeywords are the storage-class specifiers C89 defines. register and
// auto are accepted as no-ops (the variable is an ordinary automatic local);
// static and extern are accepted and their storage semantics are implemented
// in codegen (static locals live in .data, extern locals reference globals).
var storageKeywords = map[string]bool{
	"typedef": true, "extern": true, "static": true, "register": true, "auto": true, "inline": true,
}

// typedefs maps a typedef name to the type it aliases. Populated during parsing
// of "typedef" declarations and consulted by isTypeName so later declarations
// can use the alias as a type name.
var typedefs = map[string]*Type{}

// varTypes maps a variable/parameter name to its declared type. Populated as
// declarations and parameter lists are parsed, and consulted by typeof(expr)
// so "typeof(x)" can resolve x's type at parse time (the common case). Complex
// expressions are not resolved -- only a single identifier is supported in MVP.
var varTypes = map[string]*Type{}

// autoDeduceType is the placeholder the parser stamps onto a C23 "auto"
// declaration (type inferred from the initialiser): "auto x = expr;" parses
// with Typ.AutoDeduce == true and the checker replaces it with the deduced
// type before any later phase runs. The flag lives on Type (not on DeclStmt)
// so it survives being combined with qualifiers: parseDeclarationSpecifiers
// stamps the "const" of "const auto x = 5;" onto the placeholder it returns.
var autoDeduceType = IntType()

func init() { autoDeduceType.AutoDeduce = true }

// structs maps a struct/union tag to its (possibly incomplete) type. A forward
// declaration "struct S;" creates the type; a later definition "struct S {...}"
// fills in the members in place so every reference shares the same *Type.
var structs = map[string]*Type{}

// enumConsts maps an enumerator name to its integer value. Enumerators are
// file-scope integer constants in C; the checker, the constant-expression
// evaluator and the code generator all consult this table as a fallback when a
// name is not a variable.
var enumConsts = map[string]int64{}

// isAutoDeduction reports whether the current "auto" keyword opens a C23
// type-inference declaration ("auto name = init;"): the follower is the
// declarator name -- an identifier that is not a typedef alias. Any other
// follower ("auto int x", "auto *p") keeps the classic no-op storage-class
// reading (or fails specifier parsing, exactly as before).
func (p *Parser) isAutoDeduction() bool {
	if p.cur().Kind != TKeyword || p.cur().Text != "auto" {
		return false
	}
	nx := p.peek()
	return nx.Kind == TIdent && typedefs[nx.Text] == nil
}

func isTypeName(tok Token) bool {
	if tok.Kind == TKeyword && typeKeywords[tok.Text] {
		return true
	}
	if tok.Kind == TIdent && typedefs[tok.Text] != nil {
		return true
	}
	return false
}

func isQualifier(tok Token) bool {
	return tok.Kind == TKeyword && qualifierKeywords[tok.Text]
}

func isStorageClass(tok Token) bool {
	return tok.Kind == TKeyword && storageKeywords[tok.Text]
}

// isDeclarationStart reports whether tok can begin a declaration (as opposed to
// an expression/other statement). It widens the test used by parseStmt so that
// the C23 specifier keywords (_Alignas/alignas/thread_local/...) at the start of
// a block-scoped declaration dispatch to parseDeclaration rather than an
// expression parse (which would reject them).
func isDeclarationStart(tok Token) bool {
	if isTypeName(tok) || isQualifier(tok) || isStorageClass(tok) {
		return true
	}
	if tok.Kind == TIdent || tok.Kind == TKeyword {
		switch tok.Text {
		case "_Alignas", "alignas", "typeof", "typeof_unqual",
			"noreturn", "_Noreturn", "thread_local", "_Thread_local":
			return true
		}
	}
	return false
}

// paramDecl is an intermediate result of a declarator: the declared name and
// the full type built from the specifiers plus pointer/array/function suffixes.
type paramDecl struct {
	Name string
	Typ  *Type
	Line int
}

// declResult is what parseDeclarator returns: the name, its (possibly function)
// type, and the parameter names when the type is a function.
type declResult struct {
	name       string
	typ        *Type
	paramNames []string
	variadic   bool
	line       int
}

type Parser struct {
	toks    []Token
	pos     int
	globals []*DeclStmt // top-level variable declarations
}

func Parse(toks []Token) (*Program, error) {
	// Each call parses one translation unit, and the type-name tables below
	// are per-translation-unit state: every TU that needs a header includes
	// it, so the tables must start fresh. Without the reset, compiling the
	// same header twice (the built-in library TUs run before the user's) sees
	// "typedef unsigned long size_t;" with size_t already a type name -- the
	// specifier parser then swallows the alias and the declarator is left
	// with no name.
	for k := range typedefs {
		delete(typedefs, k)
	}
	for k := range structs {
		delete(structs, k)
	}
	for k := range enumConsts {
		delete(enumConsts, k)
	}
	for k := range varTypes {
		delete(varTypes, k)
	}

	// va_list is the cursor type for <stdarg.h> variadic access. goc implements
	// variadics with a contiguous register/stack save area and walks it with a
	// plain char* cursor, so va_list is just a pointer typedef.
	typedefs["va_list"] = PtrType(CharType())
	p := &Parser{toks: toks}
	prog := &Program{}
	for p.cur().Kind != TEOF {
		fd, err := p.parseTopLevel()
		if err != nil {
			return nil, err
		}
		if fd == nil {
			continue // top-level declaration with no definition (e.g. a global)
		}
		if fd.Body == nil {
			prog.Prototypes = append(prog.Prototypes, fd)
		} else {
			prog.Funcs = append(prog.Funcs, fd)
		}
	}
	prog.Globals = p.globals
	return prog, nil
}

// parseTopLevel parses one translation-unit item: a function definition (a
// declarator followed by a block) or a forward declaration / prototype (a
// declarator followed by ';'). The latter is how #include'd system headers
// declare printf, malloc, strlen, ... without a body.
func (p *Parser) parseTopLevel() (*FuncDecl, error) {
	p.skipAttributes()
	if p.cur().Kind == TKeyword && (p.cur().Text == "_Static_assert" || p.cur().Text == "static_assert") {
		if _, err := p.parseStaticAssert(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	// Storage-class specifiers: typedef / extern / static. These precede the
	// type specifier list. "typedef" creates an alias and produces no symbol;
	// extern/static only affect linkage (ignored by the toy model) and fall
	// through to the normal declaration logic.
	storage := ""
	autoInfer := p.isAutoDeduction()
	if autoInfer {
		// C23 type inference: "auto" is the whole type specifier and the
		// declared type comes from the initialiser (legal at file scope too,
		// C23 6.7.1/6.7.9). Consume the keyword here; the checker replaces
		// the autoDeduceType placeholder with the deduced type.
		p.next()
	} else if p.cur().Kind == TKeyword && (p.cur().Text == "typedef" || p.cur().Text == "extern" || p.cur().Text == "static" || p.cur().Text == "register" || p.cur().Text == "auto" || p.cur().Text == "inline") {
		storage = p.next().Text
	}
	if storage == "typedef" {
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, err
		}
		for {
			pd, err := p.parseDeclarator(spec, true, false)
			if err != nil {
				return nil, err
			}
			// The alias keeps the declared type verbatim: "typedef int fn(int)"
			// really does name a function type, and "fn *fp;" is how pointers
			// to it are declared.
			typedefs[pd.name] = pd.typ
			if p.atPunct(",") {
				p.next()
				continue
			}
			break
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return nil, nil
	}

	var spec *Type
	if autoInfer {
		spec = autoDeduceType
	} else {
		s, serr := p.parseDeclarationSpecifiers()
		if serr != nil {
			return nil, serr
		}
		spec = s
	}
	// A bare "struct S {...};" / "union U {...};" / "enum E {...};" type
	// definition declares no symbol: the tag and any enumerators are registered
	// during specifier parsing, and the declaration ends right at ';'.
	if p.atPunct(";") {
		p.next()
		return nil, nil
	}
	d, err := p.parseDeclarator(spec, true, false)
	if err != nil {
		return nil, err
	}
	// C23 6.7.9: the direct declarator of an inference declaration must be a
	// plain identifier ("auto *p", "auto x[3]", "auto f(void)" are all
	// undefined behaviour / errors). parseDeclarator only preserves the
	// AutoDeduce placeholder for that plain form; any pointer/array/function
	// suffix wraps it in a fresh type without the flag.
	if autoInfer && !d.typ.AutoDeduce {
		return nil, fmt.Errorf("line %d: auto requires a plain identifier declarator", d.line)
	}
	// A function definition: declarator followed by a block.
	if p.atPunct("{") {
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		// C99 6.4.2.2: __func__ is implicitly declared at the start of every
		// function as "static const char __func__[] = \"function-name\";".
		// Inject that declaration as the body's first statement so the checker
		// and codegen treat it exactly like user-written code (the static-local
		// machinery is per-function, so the name never collides).
		{
			fnElem := *CharType()
			fnElem.Const = true
			body.Stmts = append([]Stmt{
				&DeclStmt{
					Name:    "__func__",
					Typ:     &Type{Kind: KArr, Elem: &fnElem, Len: 0},
					Init:    &StrLit{Bytes: []byte(d.name)},
					Storage: "static",
					Line:    d.line,
				},
			}, body.Stmts...)
		}
		return &FuncDecl{
			Name:       d.name,
			Ret:        d.typ.Ret,
			Params:     d.paramNames,
			ParamTypes: d.typ.Params,
			Variadic:   d.variadic,
			Body:       body,
			Storage:    storage,
		}, nil
	}
	// A declaration: either a function prototype (no body) or a global
	// variable (with optional initialiser). Both end at ';'.
	if d.typ.Kind == KFunc {
		// An optional ", <dll>" names the import library, mirroring the
		// assembler's `extern Name, dll`. It belongs to the declaration that
		// uses the API instead of a central table, so a source file is
		// self-describing: `extern long MessageBoxA(...), user32;`.
		// A plain identifier is unambiguous here -- the next declarator of a
		// comma list would have to be a name followed by '(' or '['.
		dll := ""
		if p.atPunct(",") {
			p.next()
			if p.cur().Kind != TIdent {
				return nil, fmt.Errorf("line %d: expected a DLL name after ',' in a prototype", p.cur().Line)
			}
			dll = p.next().Text
		}
		if !p.atPunct(";") {
			return nil, fmt.Errorf("line %d: expected ';' after prototype", p.cur().Line)
		}
		p.next()
		return &FuncDecl{
			Name:       d.name,
			Ret:        d.typ.Ret,
			Params:     d.paramNames,
			ParamTypes: d.typ.Params,
			Variadic:   d.variadic,
			DLL:        dll,
			Storage:    storage,
		}, nil
	}
	// Global variable declaration(s), optionally comma-separated:
	//   "T name = expr;", "T a, b = 2, c;" -- each declarator gets its own
	// initialiser; the top-level comma separates declarators, never a comma-op.
	for {
		var init Expr
		if p.atPunct("=") {
			p.next()
			if p.atPunct("{") {
				init, err = p.parseBraceInit()
			} else {
				// An initialiser is an assignment-expression: the top-level comma
				// separates declarators ("int a = 1, b = 2;"), not a comma-op.
				init, err = p.parseAssign()
			}
			if err != nil {
				return nil, err
			}
		}
		p.globals = append(p.globals, &DeclStmt{Name: d.name, Typ: declType(d.typ), Init: init, Storage: storage, IsTLS: spec.IsTLS, Line: d.line})
		// Record the global's type so later file-scope "typeof(name)" can resolve it.
		// An auto-inferred global has no parse-time type yet, so it is not recorded.
		if d.name != "" && !autoInfer {
			varTypes[d.name] = declType(d.typ)
		}
		if !p.atPunct(",") {
			break
		}
		p.next() // consume the ',' between declarators
		d, err = p.parseDeclarator(spec, true, false)
		if err != nil {
			return nil, err
		}
	}
	if !p.atPunct(";") {
		return nil, fmt.Errorf("line %d: expected ';' after global declaration", p.cur().Line)
	}
	p.next()
	return nil, nil
}

func (p *Parser) cur() Token  { return p.toks[p.pos] }
func (p *Parser) peek() Token { return p.toks[p.pos+1] }

func (p *Parser) next() Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) expect(punct string) error {
	if p.cur().Kind != TPunct || p.cur().Text != punct {
		return fmt.Errorf("line %d: expected %q, got %q", p.cur().Line, punct, p.cur().Text)
	}
	p.next()
	return nil
}

func (p *Parser) atPunct(s string) bool {
	return p.cur().Kind == TPunct && p.cur().Text == s
}

// ---------------------------------------------------------------------------
// Type specifiers: void / char / int / long / short / unsigned / signed / double
// ---------------------------------------------------------------------------

func (p *Parser) parseDeclarationSpecifiers() (*Type, error) {
	signed := true
	width := 0
	var fp *Type // set when a floating-point specifier (float/double) is seen
	isVoid := false
	isBool := false
	isConst := false // a "const" qualifier appeared anywhere in the list
	seen := false
	align := 0           // a requested alignment from _Alignas(N) / _Alignas(type)
	isConstExpr := false // a "constexpr" specifier appeared
	isTLS := false       // a _Thread_local / thread_local specifier appeared
	var tdType *Type     // a typedef alias, if this specifier list names one
	for {
		if isQualifier(p.cur()) {
			if p.cur().Text == "const" {
				isConst = true
			}
			p.next()
			continue
		}
		// _Alignas(N) / _Alignas(type): record a requested alignment; the
		// value is stamped onto the resulting type's Align field and honoured
		// by the code generator when it lays out the variable's stack slot.
		if p.cur().Text == "_Alignas" || p.cur().Text == "alignas" {
			a, err := p.parseAlignas()
			if err != nil {
				return nil, err
			}
			if a > align {
				align = a
			}
			continue
		}
		// typeof(type) / typeof(expr) / typeof_unqual(...): a type specifier that
		// yields the type of its operand. The type-operand form (typeof(int))
		// is full; the expression form is restricted to a single identifier that
		// the parser has already seen (resolved via varTypes).
		if p.cur().Text == "typeof" || p.cur().Text == "typeof_unqual" {
			unqual := p.cur().Text == "typeof_unqual"
			p.next()
			if err := p.expect("("); err != nil {
				return nil, err
			}
			var t *Type
			if isTypeName(p.cur()) || isQualifier(p.cur()) {
				spec, err := p.parseDeclarationSpecifiers()
				if err != nil {
					return nil, err
				}
				dt, err := p.parseDeclarator(spec, true, true)
				if err != nil {
					return nil, err
				}
				t = dt.typ
			} else {
				e, err := p.parseAssign()
				if err != nil {
					return nil, err
				}
				switch v := e.(type) {
				case *Ident:
					vt, ok := varTypes[v.Name]
					if !ok {
						return nil, fmt.Errorf("line %d: typeof(%s): type not known at parse time", p.cur().Line, v.Name)
					}
					t = vt
				case *NumLit:
					// typeof(1LL) / typeof(0U) etc.: derive the integer type
					// directly from the literal's width/ signedness.
					w := 4
					if v.Long {
						w = 8
					}
					t = &Type{Kind: KInt, Width: w, Signed: !v.Unsig}
				default:
					return nil, fmt.Errorf("line %d: only typeof(type), typeof(var) and typeof(constant) are supported", p.cur().Line)
				}
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			if unqual {
				t2 := *t
				t2.Const = false
				t = &t2
			}
			return t, nil
		}
		// constexpr: a C23 specifier. The lightweight model accepts it as a
		// no-op qualifier here (the type is flagged ConstExpr); full
		// compile-time function evaluation is intentionally not performed, but
		// the keyword lets modern code parse and compile unchanged.
		if p.cur().Text == "constexpr" {
			isConstExpr = true
			p.next()
			continue
		}
		// C23 type inference when auto follows other specifiers/qualifiers:
		// "static auto x = 5;" / "const auto c = 'a';". The placeholder keeps
		// the qualifiers seen so far; the checker deduces the underlying type
		// from the initialiser. (auto as the FIRST specifier is handled by the
		// storage-class lookahead in parseDeclaration/parseTopLevel.)
		if p.cur().Text == "auto" && p.isAutoDeduction() {
			p.next()
			t := IntType()
			t.AutoDeduce = true
			t.Const = isConst
			return t, nil
		}
		// C23 keyword aliases and forward-compatibility specifiers.
		switch p.cur().Text {
		case "noreturn", "_Noreturn":
			// Function specifier: accepted and ignored (this lightweight model
			// has no missing-return diagnostic to hook into).
			p.next()
			continue
		case "thread_local", "_Thread_local":
			// C11/C23 thread-local storage. The variable gets a per-thread
			// instance, laid out in the .tls section and reached through the
			// FS (Linux) / GS (Windows) segment. Record the flag on the type
			// and let the specifier list continue normally.
			isTLS = true
			p.next()
			// C permits _Thread_local to combine with a storage-class
			// specifier (static/extern) in either order; consume any that
			// follow so the remaining specifier list parses normally.
			for isStorageClass(p.cur()) {
				p.next()
			}
			continue
		case "char8_t":
			// C23 char8_t is an unsigned char (1 byte) with a distinct type.
			p.next()
			width = 1
			signed = false
			seen = true
			continue
		}
		if !isTypeName(p.cur()) {
			// A storage-class keyword (static/extern/...) may follow a
			// _Alignas/thread_local specifier in either order ("_Alignas static
			// int" / "static _Alignas int"); skip it so the remaining type
			// specifiers parse. These are no-ops in this model.
			if isStorageClass(p.cur()) {
				p.next()
				continue
			}
			break
		}
		if p.cur().Text == "struct" || p.cur().Text == "union" {
			t, err := p.parseStructSpecifier(p.cur().Text == "union")
			if err != nil {
				return nil, err
			}
			if isConst {
				// Never mutate the shared tagged type; stamp a copy instead.
				t2 := *t
				t2.Const = true
				return &t2, nil
			}
			return t, nil
		}
		if p.cur().Text == "enum" {
			t, err := p.parseEnumSpecifier()
			if err != nil {
				return nil, err
			}
			if isConst {
				t2 := *t
				t2.Const = true
				t2.IsTLS = isTLS
				return &t2, nil
			}
			t.IsTLS = isTLS
			return t, nil
		}
		// C23 bit-precise integer: "_BitInt(N)" / "unsigned _BitInt(N)".
		// N is an integer constant expression in [1, 4096] (the storage cap
		// this implementation shares with the goclib bitint helpers). The
		// specifier terminates the list, like struct/enum do, because the
		// pending signed/unsigned flag must be consumed here.
		if p.cur().Text == "_BitInt" {
			p.next()
			if err := p.expect("("); err != nil {
				return nil, err
			}
			e, err := p.parseAssign()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			nl, ok := e.(*NumLit)
			if !ok || nl.Val <= 0 {
				return nil, fmt.Errorf("line %d: _BitInt width must be a positive integer constant", p.cur().Line)
			}
			// Implementation limit: 4194304 bits = 65536 words = 512 KiB per
			// value. A 100,000-decimal-digit pi needs ~332,200 bits; a
			// 1,000,000-digit one needs ~3,321,929 bits, so this covers both.
			if nl.Val > 4194304 {
				return nil, fmt.Errorf("line %d: _BitInt width %d exceeds the implementation limit of 4194304", p.cur().Line, nl.Val)
			}
			words := (int(nl.Val) + 63) / 64
			t := &Type{Kind: KBitInt, Bits: int(nl.Val), Size: words * 8, Signed: signed, Align: align, ConstExpr: isConstExpr, IsTLS: isTLS}
			if isConst {
				t.Const = true
			}
			return t, nil
		}
		if p.cur().Text == "_Bool" || p.cur().Text == "bool" {
			p.next()
			seen = true
			isBool = true
			continue
		}
		// A typedef name carries its own (possibly pointer) type; reuse it
		// verbatim rather than re-synthesising one from width/signedness.
		if td := typedefs[p.cur().Text]; td != nil {
			tdType = td
			p.next()
			seen = true
			continue
		}
		k := p.next().Text
		switch k {
		case "void":
			isVoid = true
		case "char":
			width = 1
		case "short":
			width = 2
		case "int":
			if width == 0 {
				width = 4
			}
		case "long":
			width = 8
		case "double":
			fp = DoubleType()
		case "float":
			fp = FloatType()
		case "unsigned":
			signed = false
		case "signed":
			signed = true
		}
		seen = true
	}
	if !seen {
		return nil, fmt.Errorf("line %d: expected type specifier, got %q", p.cur().Line, p.cur().Text)
	}
	if fp != nil {
		if isConst {
			t2 := *fp
			t2.Const = true
			t2.IsTLS = isTLS
			return &t2, nil
		}
		fp.IsTLS = isTLS
		return fp, nil
	}
	if isVoid {
		return VoidType(), nil
	}
	if isBool {
		if isConst {
			return &Type{Kind: KBool, Width: 1, Signed: true, Const: true, Align: align, ConstExpr: isConstExpr, IsTLS: isTLS}, nil
		}
		return &Type{Kind: KBool, Width: 1, Signed: true, Align: align, ConstExpr: isConstExpr, IsTLS: isTLS}, nil
	}
	if tdType != nil {
		// The specifier list was a typedef alias (e.g. va_list, size_t). The
		// pointer/array suffixes are applied later by parseDeclarator, so just
		// hand back the alias's underlying type. A const-qualified typedef use
		// is stamped onto a copy so the alias itself is never polluted. TLS of a
		// typedef'd type is rare; stamp a copy when const so the shared alias is
		// left untouched (non-const typedef TLS is not flagged).
		if isConst {
			t2 := *tdType
			t2.Const = true
			t2.IsTLS = isTLS
			return &t2, nil
		}
		return tdType, nil
	}
	if width == 0 {
		width = 4 // bare "signed"/"unsigned" means int
	}
	t := &Type{Kind: KInt, Width: width, Signed: signed, Align: align, ConstExpr: isConstExpr, IsTLS: isTLS}
	if isConst {
		t.Const = true
	}
	return t, nil
}

// parseAlignas parses a "_Alignas(alignment)" or "_Alignas(type)" specifier and
// returns the requested alignment in bytes. It is invoked from
// parseDeclarationSpecifiers when it sees the _Alignas keyword.
func (p *Parser) parseAlignas() (int, error) {
	p.next() // consume "_Alignas"
	if err := p.expect("("); err != nil {
		return 0, err
	}
	var a int
	if isTypeName(p.cur()) || isQualifier(p.cur()) {
		// _Alignas(type): the alignment is the type's natural alignment.
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return 0, err
		}
		dt, err := p.parseDeclarator(spec, true, true)
		if err != nil {
			return 0, err
		}
		a = alignOf(dt.typ)
	} else {
		// _Alignas(integer-constant).
		v, err := p.constExpr()
		if err != nil {
			return 0, err
		}
		a = v
	}
	if err := p.expect(")"); err != nil {
		return 0, err
	}
	return a, nil
}

// parseStructSpecifier parses a struct or union type specifier:
//
//	"struct"        -> forward reference to (or definition of) tag ""
//	"struct S"      -> named tag reference / definition
//	"struct S {..}" -> definition (reusing a pre-registered incomplete type so
//	                  the body may reference the tag recursively)
//
// The completed type's Size/Align are computed immediately.
func (p *Parser) parseStructSpecifier(isUnion bool) (*Type, error) {
	p.next() // consume "struct"/"union"
	tag := ""
	if p.cur().Kind == TIdent {
		tag = p.next().Text
	}
	if p.atPunct("{") {
		p.next()
		// Pre-register an incomplete type so members can name the tag
		// recursively (e.g. struct Node { struct Node* next; }).
		var t *Type
		if tag != "" {
			if existing, ok := structs[tag]; ok {
				t = existing
			} else {
				t = &Type{}
				structs[tag] = t
			}
		} else {
			t = &Type{}
		}
		members, err := p.parseStructMembers()
		if err != nil {
			return nil, err
		}
		if isUnion {
			t.Kind = KUnion
		} else {
			t.Kind = KStruct
		}
		t.Members = members
		t.Tag = tag
		t.computeLayout()
		return t, nil
	}
	// No body: this is a tag reference (possibly forward declaration).
	if tag == "" {
		return nil, fmt.Errorf("line %d: anonymous struct/union requires a body", p.cur().Line)
	}
	if t, ok := structs[tag]; ok {
		return t, nil
	}
	t := &Type{Tag: tag}
	structs[tag] = t
	return t, nil
}

// parseStructMembers parses the ";"-terminated member declarations between the
// struct braces. Each declaration is a normal type specifier followed by one or
// more comma-separated declarators (names with optional pointer / array
// suffixes). Anonymous members are not supported yet (every member is named).
func (p *Parser) parseStructMembers() ([]*Member, error) {
	var members []*Member
	for !p.atPunct("}") {
		p.skipAttributes()
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, err
		}
		// C11 anonymous struct/union member: a struct/union specifier with a
		// definition body and no declarator ("struct { int x, y; };"). Its
		// named sub-members are promoted into this type's member list
		// (recursively, for nested anonymous members); the anonymous shell
		// itself is kept for size/offset accounting. computeLayout places
		// the shell as an ordinary member and rewrites the promoted members'
		// offsets to absolute positions.
		if (spec.Kind == KStruct || spec.Kind == KUnion) && p.atPunct(";") {
			if spec.Align == 0 {
				return nil, fmt.Errorf("line %d: anonymous struct/union member requires a definition body", p.cur().Line)
			}
			shell := &Member{Name: "", Type: spec}
			members = append(members, shell)
			promoteAnonMembers(&members, spec, shell)
			p.next()
			continue
		}
		for {
			var d declResult
			if p.atPunct(":") {
				// Anonymous bit-field ("int : 3;", "int : 0;"): no declarator
				// name, the width follows the colon. d.typ is the base type.
				d = declResult{typ: spec}
			} else {
				var err error
				d, err = p.parseDeclarator(spec, true, false)
				if err != nil {
					return nil, err
				}
				if d.typ == nil {
					return nil, fmt.Errorf("line %d: expected struct member name", p.cur().Line)
				}
			}
			// Check for bitfield width after declarator name
			bitWidth := 0
			if p.atPunct(":") {
				p.next()
				if p.cur().Kind != TNum {
					return nil, fmt.Errorf("line %d: expected bit width number after ':'", p.cur().Line)
				}
				bitWidth = int(p.cur().Num)
				p.next()
				bt := declType(d.typ)
				if bt == nil || !bt.IsIntClass() {
					return nil, fmt.Errorf("line %d: bit-field base type must be an integer type, got %s", p.cur().Line, bt)
				}
				if bitWidth == 0 {
					if d.name != "" {
						return nil, fmt.Errorf("line %d: named bit-field %q cannot have zero width", p.cur().Line, d.name)
					}
				} else if bitWidth > sizeOf(bt)*8 {
					return nil, fmt.Errorf("line %d: bit-field width %d exceeds storage unit of %d bits", p.cur().Line, bitWidth, sizeOf(bt)*8)
				}
			}
			members = append(members, &Member{Name: d.name, Type: declType(d.typ), BitWidth: bitWidth})
			if p.atPunct(",") {
				p.next()
				continue
			}
			break
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
	}
	if err := p.expect("}"); err != nil {
		return nil, err
	}
	return members, nil
}

// promoteAnonMembers appends the named sub-members of an anonymous struct or
// union member to the parent's member list. Nested anonymous members are
// flattened recursively, so every directly nameable field of the anonymous
// subtree ends up in the parent list. Each promoted member's Offset holds
// its position *within* the anonymous member's type (that type's layout was
// computed when it was parsed); computeLayout adds the shell's absolute
// offset in a final pass.
func promoteAnonMembers(dst *[]*Member, t *Type, shell *Member) {
	for _, sm := range t.Members {
		if sm.Name == "" && (sm.Type.Kind == KStruct || sm.Type.Kind == KUnion) {
			promoteAnonMembers(dst, sm.Type, shell)
			continue
		}
		if sm.Name == "" {
			continue // unnamed padding bit-field: not nameable
		}
		*dst = append(*dst, &Member{Name: sm.Name, Type: sm.Type, Offset: sm.Offset, BitWidth: sm.BitWidth, BitOff: sm.BitOff, AnonBase: shell})
	}
}

// parseEnumSpecifier parses an enum type specifier:
//
//	"enum Tag"        -> reference to (or forward declaration of) an enum type
//	"enum { A, B }"   -> anonymous definition of constants
//	"enum Tag { A=1, B }" -> tagged definition of constants
//
// goc models every enum as a plain signed int (C guarantees the enumerator type
// is int-compatible; the tag is only source-level identity). The enumerator
// names are registered in enumConsts with the usual auto-increment semantics:
// the first enumerator is 0 and each following one without an explicit '=' is
// the previous value plus one.
func (p *Parser) parseEnumSpecifier() (*Type, error) {
	p.next() // consume "enum"
	if p.cur().Kind == TIdent {
		// A tag name ("enum Color { ... }" or "enum Color x;"). Consume it and
		// fall through; every enum is modelled as int.
		p.next()
	}
	// C23 underlying type: "enum Tag : int { ... }" (or even "enum : int { ... }").
	// goc models every enum as a plain int regardless of the requested
	// underlying type, so the specifier is parsed and dropped (it only refines
	// the enumerators' storage width, which goc does not track separately).
	if p.atPunct(":") {
		p.next()
		if _, err := p.parseDeclarationSpecifiers(); err != nil {
			return nil, err
		}
	}
	if !p.atPunct("{") {
		// Tag reference (or forward declaration "enum Tag;"). Every enum is
		// modelled as int; the tag is not otherwise consulted.
		return IntType(), nil
	}
	p.next() // consume "{"
	val := int64(0)
	for {
		if p.cur().Kind != TIdent {
			return nil, fmt.Errorf("line %d: expected enumerator name, got %q", p.cur().Line, p.cur().Text)
		}
		name := p.next().Text
		if p.atPunct("=") {
			p.next()
			v, err := p.constExpr()
			if err != nil {
				return nil, err
			}
			val = int64(v)
		}
		enumConsts[name] = val
		if p.atPunct(",") {
			p.next()
			if p.atPunct("}") {
				break
			}
			val++
			continue
		}
		break
	}
	if err := p.expect("}"); err != nil {
		return nil, err
	}
	return IntType(), nil
}

// parseDeclarator applies pointer prefixes, a direct declarator (name or
// grouping), and array/function suffixes to base. When allowFunc is false the
// function suffix is rejected (it only makes sense at the top level).
func (p *Parser) parseDeclarator(base *Type, allowFunc bool, abstract bool) (declResult, error) {
	for p.atPunct("*") {
		base = PtrType(base)
		p.next()
		// C allows a qualifier-list after the '*' ("const char * const p",
		// "int * volatile q"). Those qualifiers constrain the pointer ITSELF,
		// and goc has nowhere to record that -- a pointer lives in a register
		// or a stack slot, neither of which can be read-only -- so they are
		// parsed and dropped. Dropping them is also what makes a library's
		// const-correct signatures parse at all.
		for isQualifier(p.cur()) {
			p.next()
		}
	}
	var d declResult
	if p.atPunct("(") {
		// Parenthesised declarator: "int (*fp)(int)", "int (*tab[4])(void)".
		// The suffixes that follow the group apply to the OUTER type, not to
		// the inner declarator, which is exactly what makes (*fp)(int) a
		// pointer to a function rather than a function returning a pointer.
		//
		// Following the standard trick: parse the inner declarator once merely
		// to step over it, apply the trailing suffixes to base, then rewind and
		// re-parse the inner part with the suffix-built type as its starting
		// point. The first pass's result is discarded.
		open := p.pos
		p.next() // consume '('
		if _, err := p.parseDeclarator(base, allowFunc, abstract); err != nil {
			return d, err
		}
		if err := p.expect(")"); err != nil {
			return d, err
		}
		tail, err := p.parseTypeSuffixes(base, allowFunc)
		if err != nil {
			return d, err
		}
		after := p.pos
		p.pos = open + 1
		d, err = p.parseDeclarator(tail.typ, allowFunc, abstract)
		if err != nil {
			return d, err
		}
		if err := p.expect(")"); err != nil {
			return d, err
		}
		p.pos = after
		// When the inner declarator carries a function suffix it supplies this
		// declaration's parameter names (e.g. "int (*f(int x))(int)"), so keep
		// whatever the second pass produced.
		return d, nil
	} else if p.cur().Kind == TIdent {
		nameTok := p.next()
		d.name = nameTok.Text
		d.line = nameTok.Line
		d.typ = base
	} else if abstract {
		// Abstract declarator (e.g. a cast like (char*) or (int[4])): no name.
		d.typ = base
	} else {
		return d, fmt.Errorf("line %d: expected declarator name, got %q", p.cur().Line, p.cur().Text)
	}
	// Suffixes: array [...], function (...).
	suf, err := p.parseTypeSuffixes(d.typ, allowFunc)
	if err != nil {
		return d, err
	}
	d.typ = suf.typ
	d.paramNames = suf.paramNames
	d.variadic = suf.variadic
	return d, nil
}

// parseTypeSuffixes applies the array / function suffixes of a declarator to
// base and stops at the first token that starts none. It is a separate pass so
// a parenthesised declarator can have the suffixes behind its closing paren
// folded into the type its inner declarator starts from.
func (p *Parser) parseTypeSuffixes(base *Type, allowFunc bool) (declResult, error) {
	var d declResult
	d.typ = base
	for {
		if p.atPunct("[") {
			// Collect consecutive array lengths first: C applies them
			// right-to-left. "int m[2][3]" is 2 elements of int[3], NOT 3
			// elements of int[2] -- wrapping each dimension as it is read
			// produces the latter, which silently miscomputes every m[i][j]
			// address (m[0][1] landing on m[1][0]'s bytes).
			var dims []int
			for p.atPunct("[") {
				p.next()
				n, err := p.parseArrayLength()
				if err != nil {
					return d, err
				}
				if err := p.expect("]"); err != nil {
					return d, err
				}
				dims = append(dims, n)
			}
			for i := len(dims) - 1; i >= 0; i-- {
				d.typ = ArrType(d.typ, dims[i])
			}
		} else if p.atPunct("(") {
			if !allowFunc {
				return d, fmt.Errorf("line %d: function type not allowed here", p.cur().Line)
			}
			params, names, variadic, err := p.parseParamList()
			if err != nil {
				return d, err
			}
			d.typ = FuncType(d.typ, params)
			d.typ.Variadic = variadic
			d.paramNames = names
			d.variadic = variadic
		} else {
			break
		}
	}
	return d, nil
}

// declType applies C's declaration adjustment to a declarator's type: a
// parameter (or object) declared with function type really holds a pointer to
// that function. Function definitions keep the raw KFunc type, which is why the
// conversion lives here rather than in parseDeclarator.
func declType(t *Type) *Type {
	if t != nil && t.IsFunc() {
		return PtrType(t)
	}
	return t
}

// parseArrayLength parses a small integer constant expression for [N]. An
// empty pair of brackets (incomplete array type, e.g. "int a[]" in a
// parameter list) yields 0; the caller decays such a parameter to a pointer.
func (p *Parser) parseArrayLength() (int, error) {
	if p.atPunct("]") {
		return 0, nil
	}
	v, err := p.constExpr()
	if err != nil {
		return 0, err
	}
	return v, nil
}

// Constant expressions above '+': case labels, enum values and array lengths
// routinely spell values like "(1 << 3)", "(A | B)" or "(N > 0)", so the
// constant parser follows C's precedence all the way up instead of stopping
// at additive. constExpr is the entry point -- each layer parses the next
// tighter one as its operands, exactly as the C grammar does.
func (p *Parser) constExpr() (int, error) {
	v, err := p.constLAnd()
	if err != nil {
		return 0, err
	}
	for p.atPunct("||") {
		p.next()
		r, err := p.constLAnd()
		if err != nil {
			return 0, err
		}
		v = constBool(v != 0 || r != 0)
	}
	return v, nil
}

func (p *Parser) constLAnd() (int, error) {
	v, err := p.constBOr()
	if err != nil {
		return 0, err
	}
	for p.atPunct("&&") {
		p.next()
		r, err := p.constBOr()
		if err != nil {
			return 0, err
		}
		v = constBool(v != 0 && r != 0)
	}
	return v, nil
}

func (p *Parser) constBOr() (int, error) {
	v, err := p.constBXor()
	if err != nil {
		return 0, err
	}
	for p.atPunct("|") {
		p.next()
		r, err := p.constBXor()
		if err != nil {
			return 0, err
		}
		v |= r
	}
	return v, nil
}

func (p *Parser) constBXor() (int, error) {
	v, err := p.constBAnd()
	if err != nil {
		return 0, err
	}
	for p.atPunct("^") {
		p.next()
		r, err := p.constBAnd()
		if err != nil {
			return 0, err
		}
		v ^= r
	}
	return v, nil
}

func (p *Parser) constBAnd() (int, error) {
	v, err := p.constEq()
	if err != nil {
		return 0, err
	}
	for p.atPunct("&") {
		p.next()
		r, err := p.constEq()
		if err != nil {
			return 0, err
		}
		v &= r
	}
	return v, nil
}

func (p *Parser) constEq() (int, error) {
	v, err := p.constRel()
	if err != nil {
		return 0, err
	}
	for p.atPunct("==") || p.atPunct("!=") {
		op := p.next().Text
		r, err := p.constRel()
		if err != nil {
			return 0, err
		}
		if op == "==" {
			v = constBool(v == r)
		} else {
			v = constBool(v != r)
		}
	}
	return v, nil
}

func (p *Parser) constRel() (int, error) {
	v, err := p.constShift()
	if err != nil {
		return 0, err
	}
	for p.atPunct("<") || p.atPunct(">") || p.atPunct("<=") || p.atPunct(">=") {
		op := p.next().Text
		r, err := p.constShift()
		if err != nil {
			return 0, err
		}
		switch op {
		case "<":
			v = constBool(v < r)
		case ">":
			v = constBool(v > r)
		case "<=":
			v = constBool(v <= r)
		default:
			v = constBool(v >= r)
		}
	}
	return v, nil
}

func (p *Parser) constShift() (int, error) {
	v, err := p.constAdd()
	if err != nil {
		return 0, err
	}
	for p.atPunct("<<") || p.atPunct(">>") {
		op := p.next().Text
		r, err := p.constAdd()
		if err != nil {
			return 0, err
		}
		// Mirrors codegen's rule for a real shl/shr: x86 masks the count to
		// 6 bits, so an out-of-range shift would fold to a value the emitted
		// instruction disagrees with. Refuse instead.
		if r < 0 || r > 63 {
			return 0, fmt.Errorf("line %d: shift count %d out of range in constant expression", p.cur().Line, r)
		}
		if op == "<<" {
			v <<= uint(r)
		} else {
			v >>= uint(r)
		}
	}
	return v, nil
}

// constBool renders a comparison/logical result the way C does: 1 or 0.
func constBool(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (p *Parser) constAdd() (int, error) {
	v, err := p.constMul()
	if err != nil {
		return 0, err
	}
	for p.atPunct("+") || p.atPunct("-") {
		op := p.next().Text
		r, err := p.constMul()
		if err != nil {
			return 0, err
		}
		if op == "+" {
			v += r
		} else {
			v -= r
		}
	}
	return v, nil
}

func (p *Parser) constMul() (int, error) {
	v, err := p.constPrim()
	if err != nil {
		return 0, err
	}
	for p.atPunct("*") || p.atPunct("/") || p.atPunct("%") {
		op := p.next().Text
		r, err := p.constPrim()
		if err != nil {
			return 0, err
		}
		if op == "*" {
			v *= r
		} else if op == "/" {
			if r == 0 {
				return 0, fmt.Errorf("line %d: division by zero in array size", p.cur().Line)
			}
			v /= r
		} else {
			if r == 0 {
				return 0, fmt.Errorf("line %d: division by zero in array size", p.cur().Line)
			}
			v %= r
		}
	}
	return v, nil
}

func (p *Parser) constPrim() (int, error) {
	if p.atPunct("(") {
		p.next()
		v, err := p.constExpr()
		if err != nil {
			return 0, err
		}
		if err := p.expect(")"); err != nil {
			return 0, err
		}
		return v, nil
	}
	// nullptr inside a constant expression lowers to the integer 0 (a null
	// pointer constant), so "static_assert(nullptr == 0, ...)" folds.
	if p.cur().Text == "nullptr" {
		p.next()
		return 0, nil
	}
	// _Alignof(Type) inside a constant expression: yields the type's alignment.
	if p.cur().Text == "_Alignof" {
		p.next()
		if p.atPunct("(") && (isTypeName(p.peek()) || isQualifier(p.peek())) {
			p.next()
			spec, err := p.parseDeclarationSpecifiers()
			if err != nil {
				return 0, err
			}
			dt, err := p.parseDeclarator(spec, true, true)
			if err != nil {
				return 0, err
			}
			if err := p.expect(")"); err != nil {
				return 0, err
			}
			return alignOf(dt.typ), nil
		}
		return 0, fmt.Errorf("line %d: only _Alignof(type) is constant-evaluable here", p.cur().Line)
	}
	// sizeof(Type) inside a constant expression (the common static_assert use,
	// e.g. "static_assert(sizeof(int) == 4, ...)").
	if p.cur().Text == "sizeof" {
		p.next()
		if p.atPunct("(") && (isTypeName(p.peek()) || isQualifier(p.peek())) {
			p.next()
			spec, err := p.parseDeclarationSpecifiers()
			if err != nil {
				return 0, err
			}
			dt, err := p.parseDeclarator(spec, true, true)
			if err != nil {
				return 0, err
			}
			if err := p.expect(")"); err != nil {
				return 0, err
			}
			return sizeOf(dt.typ), nil
		}
		return 0, fmt.Errorf("line %d: only sizeof(type) is constant-evaluable here", p.cur().Line)
	}
	if p.atPunct("-") {
		p.next()
		v, err := p.constPrim()
		if err != nil {
			return 0, err
		}
		return -v, nil
	}
	if p.atPunct("+") {
		p.next()
		return p.constPrim()
	}
	if p.atPunct("~") {
		p.next()
		v, err := p.constPrim()
		if err != nil {
			return 0, err
		}
		return ^v, nil
	}
	// '!' yields C's 1/0, so a case label like "case !FOO:" folds to 0/1.
	if p.atPunct("!") {
		p.next()
		v, err := p.constPrim()
		if err != nil {
			return 0, err
		}
		return constBool(v == 0), nil
	}
	if p.cur().Kind == TNum && !p.cur().IsDbl {
		v := int(p.next().Num)
		return v, nil
	}
	// An enumerator name may appear in a constant expression (array size,
	// another enumerator's value): look it up in the enum table.
	if p.cur().Kind == TIdent {
		if v, ok := enumConsts[p.cur().Text]; ok {
			p.next()
			return int(v), nil
		}
	}
	return 0, fmt.Errorf("line %d: expected integer constant, got %q", p.cur().Line, p.cur().Text)
}

// parseParamList parses the (...) of a function declarator. Array parameters
// decay to pointers, matching C. "void" as the sole parameter means empty.
// atEllipsis reports whether the cursor is at a "..." token -- either the single
// "..." preprocessing token (the modern goc lexer form, emitted for "..."
// sequences) or three consecutive '.' punctuation tokens (legacy form).
func (p *Parser) atEllipsis() bool {
	if p.cur().Kind == TPunct && p.cur().Text == "..." {
		return true
	}
	return p.cur().Kind == TPunct && p.cur().Text == "." &&
		p.peek().Kind == TPunct && p.peek().Text == "." &&
		p.toks[p.pos+2].Kind == TPunct && p.toks[p.pos+2].Text == "."
}

// consumeEllipsis advances past a "..." token sequence.
func (p *Parser) consumeEllipsis() {
	if p.cur().Kind == TPunct && p.cur().Text == "..." {
		p.next()
		return
	}
	p.next()
	p.next()
	p.next()
}

// paramIsNamed reports whether the tokens at the cursor (just past a
// parameter's declaration specifiers) introduce the parameter's name. Skipping
// pointer prefixes, an identifier that is not itself a type name means the
// declarator is named; anything else means the parameter is unnamed and its
// declarator must be parsed abstractly.
func (p *Parser) paramIsNamed() bool {
	i := p.pos
	for i < len(p.toks) && p.toks[i].Kind == TPunct && p.toks[i].Text == "*" {
		i++
	}
	if i >= len(p.toks) {
		return false
	}
	t := p.toks[i]
	return t.Kind == TIdent && !isTypeName(t)
}

func (p *Parser) parseParamList() ([]*Type, []string, bool, error) {
	if err := p.expect("("); err != nil {
		return nil, nil, false, err
	}
	var types []*Type
	var names []string
	variadic := false
	if p.atPunct(")") {
		p.next()
		return types, names, variadic, nil
	}
	for {
		// Bare "..." as the only parameter (e.g. f(...)).
		if p.atEllipsis() {
			p.consumeEllipsis()
			variadic = true
			break
		}
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, nil, false, err
		}
		if p.atPunct(")") {
			// "void" alone => empty parameter list
			if spec.IsVoid() {
				p.next()
				return types, names, variadic, nil
			}
			// Otherwise the last parameter is unnamed and ends here; the
			// abstract-declarator parse below records its type and the loop
			// consumes the ')'.
		}
		// A parameter may be unnamed ("int f(int, char*)"): only its type
		// matters, so such a declarator is abstract. Whether a name follows is
		// decided by looking past any pointer prefixes for an identifier that
		// is not itself a type specifier.
		abstract := !p.paramIsNamed()
		pd, err := p.parseDeclarator(spec, true, abstract)
		if err != nil {
			return nil, nil, false, err
		}
		// C's parameter adjustment: an array parameter decays to a pointer to
		// its element type, and a function parameter becomes a pointer to that
		// function (so "int f(int (*cb)(int))" receives an 8-byte pointer).
		if pd.typ.IsArray() {
			pd.typ = PtrType(pd.typ.Elem)
		}
		pd.typ = declType(pd.typ)
		types = append(types, pd.typ)
		names = append(names, pd.name)
		if pd.name != "" {
			varTypes[pd.name] = pd.typ
		}
		if p.atPunct(",") {
			p.next()
			if p.atEllipsis() {
				p.consumeEllipsis()
				variadic = true
				break
			}
			continue
		}
		break
	}
	if err := p.expect(")"); err != nil {
		return nil, nil, false, err
	}
	return types, names, variadic, nil
}

// parseBlock parses a brace-delimited statement block.
func (p *Parser) parseBlock() (*Block, error) {
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	b := &Block{}
	for !p.atPunct("}") {
		if p.cur().Kind == TEOF {
			return nil, fmt.Errorf("line %d: unexpected EOF inside block", p.cur().Line)
		}
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		b.Stmts = append(b.Stmts, st)
	}
	p.next() // consume '}'
	return b, nil
}

func (p *Parser) parseStmt() (Stmt, error) {
	p.skipAttributes()
	t := p.cur()
	var err error
	switch {
	case t.Kind == TKeyword && (t.Text == "_Static_assert" || t.Text == "static_assert"):
		return p.parseStaticAssert()
	case isDeclarationStart(t):
		return p.parseDeclaration()
	case t.Kind == TKeyword && t.Text == "return":
		p.next()
		var e Expr
		if !p.atPunct(";") {
			var err error
			e, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return &ReturnStmt{E: e}, nil
	case t.Kind == TKeyword && t.Text == "if":
		p.next()
		if err := p.expect("("); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		thenS, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		var elseS Stmt
		if p.cur().Kind == TKeyword && p.cur().Text == "else" {
			p.next()
			elseS, err = p.parseStmt()
			if err != nil {
				return nil, err
			}
		}
		return &IfStmt{Cond: cond, Then: thenS, Else: elseS}, nil
	case t.Kind == TKeyword && t.Text == "while":
		p.next()
		if err := p.expect("("); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		body, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		return &WhileStmt{Cond: cond, Body: body}, nil
	case t.Kind == TKeyword && t.Text == "for":
		p.next()
		if err := p.expect("("); err != nil {
			return nil, err
		}
		var init Stmt
		if !p.atPunct(";") {
			if isTypeName(p.cur()) || p.isAutoDeduction() {
				init, err = p.parseDeclaration()
				if err != nil {
					return nil, err
				}
			} else {
				e, eerr := p.parseExpr()
				if eerr != nil {
					return nil, eerr
				}
				if eerr := p.expect(";"); eerr != nil {
					return nil, eerr
				}
				init = &ExprStmt{E: e}
			}
		} else {
			p.next() // empty init
		}
		var cond Expr
		if !p.atPunct(";") {
			cond, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		var post Expr
		if !p.atPunct(")") {
			post, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		body, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		return &ForStmt{Init: init, Cond: cond, Post: post, Body: body}, nil
	case t.Kind == TKeyword && t.Text == "switch":
		p.next()
		if err := p.expect("("); err != nil {
			return nil, err
		}
		src, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		// The body must be a block: case/default labels live inside it as
		// ordinary statements, and codegen needs the flat list to lay out the
		// comparison chain and the case bodies in source order.
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &SwitchStmt{Src: src, Body: body, Line: t.Line}, nil
	case t.Kind == TKeyword && t.Text == "case":
		p.next()
		v, err := p.constExpr()
		if err != nil {
			return nil, fmt.Errorf("line %d: case label must be an integer constant (%v)", t.Line, err)
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		return &CaseStmt{Val: v, Line: t.Line}, nil
	case t.Kind == TKeyword && t.Text == "default":
		p.next()
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		return &DefaultStmt{Line: t.Line}, nil
	case t.Kind == TKeyword && t.Text == "do":
		p.next()
		body, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		if !(p.cur().Kind == TKeyword && p.cur().Text == "while") {
			return nil, fmt.Errorf("line %d: expected \"while\" after the body of do", p.cur().Line)
		}
		p.next()
		if err := p.expect("("); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return &DoWhileStmt{Body: body, Cond: cond}, nil
	case t.Kind == TKeyword && t.Text == "goto":
		p.next()
		if p.cur().Kind != TIdent {
			return nil, fmt.Errorf("line %d: expected a label name after goto", p.cur().Line)
		}
		lab := p.next()
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return &GotoStmt{Label: lab.Text, Line: t.Line}, nil
	case t.Kind == TKeyword && (t.Text == "__asm" || t.Text == "_asm"):
		// Inline assembly. The lexer has already captured the raw block text
		// as a single TAsm token (its body is assembler, not C), so there is
		// nothing to parse here beyond the token itself.
		p.next()
		if p.cur().Kind != TAsm {
			return nil, fmt.Errorf("line %d: expected a '{' block after %s", t.Line, t.Text)
		}
		block := p.cur().Text
		line := p.cur().Line
		p.next()
		return &AsmStmt{Text: block, Line: line}, nil
	case t.Kind == TKeyword && (t.Text == "break" || t.Text == "continue"):
		p.next()
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		if t.Text == "break" {
			return &BreakStmt{}, nil
		}
		return &ContinueStmt{}, nil
	case p.atPunct("{"):
		return p.parseBlock()
	// A labelled statement: "name: stmt". Disambiguated from the ternary
	// operator (which is the only other place a ':' can follow an identifier)
	// by the token after the name: ':' means a label, '?' does not.
	case t.Kind == TIdent && p.peek().Kind == TPunct && p.peek().Text == ":":
		lab := p.next()
		p.next() // consume ':'
		var body Stmt
		// "name: }" -- a label on an empty statement at the end of a block.
		if !p.atPunct("}") {
			body, err = p.parseStmt()
			if err != nil {
				return nil, err
			}
		}
		return &LabelStmt{Name: lab.Text, Stmt: body, Line: t.Line}, nil
	}
	if p.atPunct(";") {
		// Empty statement: a bare ";" (e.g. the body of `while (cond);`).
		p.next()
		return nil, nil
	}

	// Expression statement. Assignment ("a = b;") is now parsed as an
	// AssignExpr inside parseExpr and wrapped in an ExprStmt, sharing the same
	// code path as expression-position assignments.
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	return &ExprStmt{E: e}, nil
}

// parseDeclaration parses one declaration specifier list followed by a
// comma-separated list of initialised declarators: "int a = 1, b[3];".
func (p *Parser) parseDeclaration() (Stmt, error) {
	// Storage-class specifiers: typedef / extern / static / register / auto.
	// "typedef" creates an alias and produces no variable; the rest only shape
	// the lifetime/linkage of the locals below and are otherwise no-ops in this
	// non-optimising, single-translation-unit compiler.
	storage := ""
	autoInfer := p.isAutoDeduction()
	if autoInfer {
		// C23 type inference: "auto name = init;" -- see parseTopLevel. The
		// checker replaces the autoDeduceType placeholder with the deduced
		// type; a "static auto" combination keeps its storage class here.
		p.next()
	} else if p.cur().Kind == TKeyword && (p.cur().Text == "typedef" || p.cur().Text == "extern" || p.cur().Text == "static" || p.cur().Text == "register" || p.cur().Text == "auto" || p.cur().Text == "inline") {
		storage = p.next().Text
	}
	if storage == "typedef" {
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, err
		}
		for {
			pd, err := p.parseDeclarator(spec, true, false)
			if err != nil {
				return nil, err
			}
			typedefs[pd.name] = pd.typ
			if p.atPunct(",") {
				p.next()
				continue
			}
			break
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return &DeclList{}, nil
	}
	var spec *Type
	if autoInfer {
		spec = autoDeduceType
	} else {
		s, serr := p.parseDeclarationSpecifiers()
		if serr != nil {
			return nil, serr
		}
		spec = s
	}
	// A bare type definition inside a block ("struct S {...};" / "enum { A };")
	// declares no variable; the empty DeclList is a harmless no-op statement.
	if p.atPunct(";") {
		p.next()
		return &DeclList{}, nil
	}
	var decls []*DeclStmt
	for {
		pd, err := p.parseDeclarator(spec, true, false)
		// C23 6.7.9: the direct declarator of an inference declaration must be
		// a plain identifier; "auto *p" / "auto x[3]" are not inferred forms.
		if autoInfer && !pd.typ.AutoDeduce {
			return nil, fmt.Errorf("line %d: auto requires a plain identifier declarator", pd.line)
		}
		var init Expr
		if p.atPunct("=") {
			p.next()
			if p.atPunct("{") {
				init, err = p.parseBraceInit()
			} else {
				// assignment-expression, not comma-expression: the top-level
				// comma separates declarators (int a = 1, b = 2;).
				init, err = p.parseAssign()
			}
			if err != nil {
				return nil, err
			}
		}
		decls = append(decls, &DeclStmt{Name: pd.name, Typ: declType(pd.typ), Init: init, Storage: storage, IsTLS: spec.IsTLS, Line: pd.line})
		// Record the declared type so later "typeof(name)" can resolve it.
		// An auto-inferred variable has no parse-time type yet, so it is not
		// recorded (typeof(auto-var) stays unsupported).
		if pd.name != "" && !autoInfer {
			varTypes[pd.name] = declType(pd.typ)
		}
		if p.atPunct(",") {
			p.next()
			continue
		}
		break
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	if len(decls) == 1 {
		return decls[0], nil
	}
	return &DeclList{Decls: decls}, nil
}

// parseStaticAssert handles C11 _Static_assert and the C23 static_assert alias:
//
//	_Static_assert(constant-expr, "message");
//
// The assertion is checked at compile time; a false condition is a hard error
// that aborts compilation, exactly like a failed #if. A true condition produces
// no code (an empty DeclList is a harmless no-op statement).
func (p *Parser) parseStaticAssert() (Stmt, error) {
	p.next() // consume "_Static_assert" / "static_assert"
	if err := p.expect("("); err != nil {
		return nil, err
	}
	val, err := p.constExpr()
	if err != nil {
		return nil, err
	}
	if !p.atPunct(",") {
		return nil, fmt.Errorf("line %d: expected ',' after static_assert condition", p.cur().Line)
	}
	p.next() // consume ','
	// The second operand is a string-literal message (mandatory in C11/C23).
	if p.cur().Kind != TStr {
		return nil, fmt.Errorf("line %d: static_assert requires a string literal message", p.cur().Line)
	}
	msg := string(p.cur().Str)
	p.next()
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	if val == 0 {
		return nil, fmt.Errorf("static_assert failed: %s", msg)
	}
	return &DeclList{}, nil
}

// skipAttributes consumes C23 attribute specifier lists ("[[...]]", possibly
// several in a row) at the current cursor. Attributes are largely ignored by
// goc's toy model; the only visible effect is a compiler warning for
// "deprecated" and "nodiscard". Any other attribute is silently accepted.
func (p *Parser) skipAttributes() {
	for p.atPunct("[") && p.peek().Kind == TPunct && p.peek().Text == "[" {
		p.next() // consume first '['
		p.next() // consume second '['
		for {
			if p.cur().Kind == TEOF {
				return
			}
			if p.atPunct("]") && p.peek().Kind == TPunct && p.peek().Text == "]" {
				p.next() // first ']'
				p.next() // second ']'
				break
			}
			if p.cur().Kind == TIdent {
				switch p.cur().Text {
				case "deprecated":
					fmt.Fprintf(os.Stderr, "line %d: warning: declaration is deprecated\n", p.cur().Line)
				case "nodiscard":
					fmt.Fprintf(os.Stderr, "line %d: warning: return value of function should not be discarded\n", p.cur().Line)
				case "maybe_unused", "fallthrough", "noreturn", "unsequenced", "reproducible", "_Noreturn":
					// Standard C23 attributes: accepted and ignored by this
					// lightweight model (no unused/flow diagnostics to drive).
				default:
					// Unknown attribute: accept silently to avoid breaking code
					// that uses vendor attributes (e.g. gnu::...).
				}
			}
			if p.atPunct("]") { // tolerate a single unpaired ']' (malformed input)
				p.next()
				break
			}
			p.next()
		}
	}
}

func (p *Parser) parseExpr() (Expr, error) { return p.parseComma() }

// parseComma parses the comma operator (the lowest-precedence operator in C,
// below even assignment). "a, b, c" evaluates each operand left to right and
// yields the value of the rightmost one. It only matters where a full
// expression is expected; the comma separating function-call arguments and the
// one separating declarators are handled elsewhere.
func (p *Parser) parseComma() (Expr, error) {
	left, err := p.parseAssign()
	if err != nil {
		return nil, err
	}
	for p.atPunct(",") {
		p.next() // consume ','
		right, err := p.parseAssign()
		if err != nil {
			return nil, err
		}
		left = &CommaExpr{Left: left, Right: right}
	}
	return left, nil
}

// parseBraceInit parses a braced initialiser "{ a, .x = b, [2] = c, { d } }"
// found in the initialiser position of a declaration. Elements are assignment
// expressions, nested braces, or designated members (". name =" for struct/
// union members, "[ N ] =" for array index designators, where N is a
// non-negative integer constant). Elements are separated by commas with an
// optional trailing comma. Mixing positional and designated elements in the
// same (sub-)initialiser is rejected by the checker, mirroring the rule goc
// already applies to struct member designators.
func (p *Parser) parseBraceInit() (Expr, error) {
	line := p.cur().Line
	p.next() // consume '{'
	bi := &BraceInit{Line: line}
	for {
		if p.atPunct("}") {
			p.next()
			break
		}
		el := InitElem{DesigIdx: -1}
		if p.atPunct(".") {
			p.next()
			if p.cur().Kind != TIdent {
				return nil, fmt.Errorf("line %d: expected member name after '.' in initialiser", p.cur().Line)
			}
			el.Desig = p.next().Text
			if err := p.expect("="); err != nil {
				return nil, err
			}
		} else if p.atPunct("[") {
			p.next() // consume '['
			if p.cur().Kind != TNum {
				return nil, fmt.Errorf("line %d: expected integer index after '[' in initialiser", p.cur().Line)
			}
			idx := p.cur().Num
			if idx < 0 {
				return nil, fmt.Errorf("line %d: array designator index must be non-negative", p.cur().Line)
			}
			el.DesigIdx = int(idx)
			p.next() // consume the number
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			if err := p.expect("="); err != nil {
				return nil, err
			}
		}
		if p.atPunct("{") {
			e, err := p.parseBraceInit()
			if err != nil {
				return nil, err
			}
			el.E = e
		} else {
			e, err := p.parseAssign()
			if err != nil {
				return nil, err
			}
			el.E = e
		}
		bi.Elems = append(bi.Elems, el)
		if p.atPunct(",") {
			p.next()
			continue
		}
		if err := p.expect("}"); err != nil {
			return nil, err
		}
		break
	}
	return bi, nil
}

// parseAssign parses an assignment expression. Simple "=" binds the value of
// the right side (and yields it); compound operators (+= -= *= /= %= &= |=
// <<= >>=) are kept as AssignExpr{Op} and compiled with evaluate-once lvalue
// semantics (C11 6.5.16.2). Assignment is the lowest-precedence expression
// operator (below the ternary ?:).
func (p *Parser) parseAssign() (Expr, error) {
	left, err := p.parseCond()
	if err != nil {
		return nil, err
	}
	if p.atPunct("=") {
		p.next()
		right, err := p.parseAssign()
		if err != nil {
			return nil, err
		}
		return &AssignExpr{Lhs: left, Rhs: right}, nil
	}
	if op := p.assignOp(); op != "" {
		p.next()
		right, err := p.parseAssign()
		if err != nil {
			return nil, err
		}
		// "E1 op= E2" is not desugared into "E1 = E1 op E2" (that would
		// evaluate E1 twice, C11 6.5.16.2 requires exactly once). The operator
		// rides on AssignExpr; the checker and codegen implement evaluate-once
		// semantics.
		return &AssignExpr{Op: op, Lhs: left, Rhs: right}, nil
	}
	return left, nil
}

// assignOp returns the binary operator a compound-assignment token stands for,
// or "" if the current token is not a compound-assignment operator.
func (p *Parser) assignOp() string {
	switch {
	case p.atPunct("+="):
		return "+"
	case p.atPunct("-="):
		return "-"
	case p.atPunct("*="):
		return "*"
	case p.atPunct("/="):
		return "/"
	case p.atPunct("%="):
		return "%"
	case p.atPunct("&="):
		return "&"
	case p.atPunct("|="):
		return "|"
	case p.atPunct("^="):
		return "^"
	case p.atPunct("<<="):
		return "<<"
	case p.atPunct(">>="):
		return ">>"
	}
	return ""
}

// parseCond parses the ternary operator (?:), the lowest-precedence operator
// the toy model supports (no comma operator).
func (p *Parser) parseCond() (Expr, error) {
	left, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.atPunct("?") {
		return left, nil
	}
	p.next() // consume '?'
	thenE, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(":"); err != nil {
		return nil, err
	}
	elseE, err := p.parseCond()
	if err != nil {
		return nil, err
	}
	return &CondExpr{Cond: left, Then: thenE, Else: elseE}, nil
}

func (p *Parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.atPunct("||") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "||", L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Expr, error) {
	left, err := p.parseBOr()
	if err != nil {
		return nil, err
	}
	for p.atPunct("&&") {
		p.next()
		right, err := p.parseBOr()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "&&", L: left, R: right}
	}
	return left, nil
}

// parseBOr / parseXor / parseBAnd parse the bitwise operators (| ^ &), which
// sit between logical && and equality in precedence. Prefix & (address-of) is
// handled separately in parseUnary, so the single '&' token here is always the
// bitwise AND; '&&' is a distinct two-character token.
func (p *Parser) parseBOr() (Expr, error) {
	left, err := p.parseXor()
	if err != nil {
		return nil, err
	}
	for p.atPunct("|") {
		p.next()
		right, err := p.parseXor()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "|", L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseXor() (Expr, error) {
	left, err := p.parseBAnd()
	if err != nil {
		return nil, err
	}
	for p.atPunct("^") {
		p.next()
		right, err := p.parseBAnd()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "^", L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseBAnd() (Expr, error) {
	left, err := p.parseEq()
	if err != nil {
		return nil, err
	}
	for p.atPunct("&") {
		p.next()
		right, err := p.parseEq()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "&", L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseEq() (Expr, error) {
	left, err := p.parseRel()
	if err != nil {
		return nil, err
	}
	for p.atPunct("==") || p.atPunct("!=") {
		op := p.next().Text
		right, err := p.parseRel()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseRel() (Expr, error) {
	left, err := p.parseShift()
	if err != nil {
		return nil, err
	}
	for p.atPunct("<") || p.atPunct(">") || p.atPunct("<=") || p.atPunct(">=") {
		op := p.next().Text
		right, err := p.parseShift()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, L: left, R: right}
	}
	return left, nil
}

// parseShift parses the shift operators << and >> (below relational, above
// additive in precedence).
func (p *Parser) parseShift() (Expr, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.atPunct("<<") || p.atPunct(">>") {
		op := p.next().Text
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseAdd() (Expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.atPunct("+") || p.atPunct("-") {
		op := p.next().Text
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseMul() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.atPunct("*") || p.atPunct("/") || p.atPunct("%") {
		op := p.next().Text
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (Expr, error) {
	// sizeof is a unary operator with two forms: "sizeof(Type)" (the operand
	// is a type name, parsed by an abstract declarator) and "sizeof expr"
	// (the operand is an ordinary expression). We peek at the token after a
	// '(' to disambiguate: only when it begins a type specifier do we treat
	// the parentheses as enclosing a type rather than a grouped expression.
	if p.cur().Text == "sizeof" {
		p.next() // consume "sizeof"
		if p.atPunct("(") && (isTypeName(p.peek()) || isQualifier(p.peek())) {
			p.next() // consume '('
			spec, err := p.parseDeclarationSpecifiers()
			if err != nil {
				return nil, err
			}
			dt, err := p.parseDeclarator(spec, true, true)
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return &SizeofExpr{Typ: dt.typ}, nil
		}
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &SizeofExpr{E: e}, nil
	}
	// _Alignof is a unary operator analogous to sizeof, but yields the alignment
	// (always a compile-time constant) of a type or a simple variable. It
	// requires parentheses around its operand, exactly like sizeof.
	if p.cur().Text == "_Alignof" || p.cur().Text == "alignof" {
		p.next() // consume "_Alignof" / "alignof"
		if !p.atPunct("(") {
			return nil, fmt.Errorf("line %d: expected '(' after _Alignof", p.cur().Line)
		}
		p.next() // consume '('
		var t *Type
		if isTypeName(p.cur()) || isQualifier(p.cur()) {
			spec, err := p.parseDeclarationSpecifiers()
			if err != nil {
				return nil, err
			}
			dt, err := p.parseDeclarator(spec, true, true)
			if err != nil {
				return nil, err
			}
			t = dt.typ
		} else if p.cur().Kind == TIdent {
			vt, ok := varTypes[p.cur().Text]
			if !ok {
				return nil, fmt.Errorf("line %d: _Alignof(%s): type not known at parse time", p.cur().Line, p.cur().Text)
			}
			t = vt
			p.next() // consume the identifier
		} else {
			return nil, fmt.Errorf("line %d: only _Alignof(type) and _Alignof(var) are supported", p.cur().Line)
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return &NumLit{Val: int64(alignOf(t)), Kind: TInt}, nil
	}
	// _Generic is the C11/C23 compile-time type switch:
	//   _Generic(control-expr, T1: e1, T2: e2, ..., default: eN)
	// The controlling expression and every branch are parsed here; picking
	// the matching association needs full type information, so that happens
	// in the checker (checkExpr's *GenericExpr case), which stores the
	// selected branch in Chosen. Unselected branches are never evaluated.
	if p.cur().Text == "_Generic" {
		line := p.next().Line // consume "_Generic"
		if err := p.expect("("); err != nil {
			return nil, err
		}
		ctrl, err := p.parseAssign()
		if err != nil {
			return nil, err
		}
		if err := p.expect(","); err != nil {
			return nil, err
		}
		var assocs []GenericAssoc
		for {
			var a GenericAssoc
			if p.cur().Kind == TKeyword && p.cur().Text == "default" {
				p.next()
				a.IsDefault = true
			} else {
				if !isTypeName(p.cur()) && !isQualifier(p.cur()) {
					return nil, fmt.Errorf("line %d: expected a type name or 'default' in _Generic, got %q", p.cur().Line, p.cur().Text)
				}
				spec, err := p.parseDeclarationSpecifiers()
				if err != nil {
					return nil, err
				}
				dt, err := p.parseDeclarator(spec, true, true)
				if err != nil {
					return nil, err
				}
				a.Typ = dt.typ
			}
			if err := p.expect(":"); err != nil {
				return nil, err
			}
			e, err := p.parseAssign()
			if err != nil {
				return nil, err
			}
			a.E = e
			assocs = append(assocs, a)
			if p.atPunct(",") {
				p.next()
				continue
			}
			break
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		if len(assocs) == 0 {
			return nil, fmt.Errorf("line %d: _Generic requires at least one association", line)
		}
		return &GenericExpr{Control: ctrl, Assocs: assocs, ChosenIdx: -1, Line: line}, nil
	}
	if p.atPunct("-") || p.atPunct("!") || p.atPunct("~") {
		op := p.next().Text
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: op, E: e}, nil
	}
	if p.atPunct("*") { // indirection (dereference)
		p.next()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: "*", E: e}, nil
	}
	if p.atPunct("&") { // address-of
		p.next()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: "&", E: e}, nil
	}
	if p.atPunct("++") || p.atPunct("--") {
		op := p.next().Text
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &IncDecExpr{Op: op, E: e, Prefix: true}, nil
	}
	// C-style cast: (type) operand. Only when the token after '(' is a type
	// name (or a qualifier that begins a type); otherwise '(' is an
	// expression-grouping parenthesis.
	if p.atPunct("(") && (isTypeName(p.peek()) || isQualifier(p.peek())) {
		p.next() // consume '('
		line := p.cur().Line
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, err
		}
		dt, err := p.parseDeclarator(spec, true, true)
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		// C99 compound literal: "(T){...}" -- a cast is never followed by a
		// brace in valid C, so the '{' unambiguously starts the literal.
		if p.atPunct("{") {
			bi, err := p.parseBraceInit()
			if err != nil {
				return nil, err
			}
			b, ok := bi.(*BraceInit)
			if !ok {
				return nil, fmt.Errorf("line %d: internal: compound literal initialiser is not a brace list", line)
			}
			// A compound literal is a postfix-expression: ".member", "[i]"
			// and even "(args)" may follow it.
			return p.parsePostfixFrom(&CompoundLit{Typ: dt.typ, Init: b, Line: line})
		}
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &CastExpr{Typ: dt.typ, E: e}, nil
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() (Expr, error) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return p.parsePostfixFrom(e)
}

// parsePostfixFrom applies the trailing postfix operators ("(...)", "[...]",
// ".", "->", "++", "--") to the already-parsed head expression e. Split out
// of parsePostfix so a compound literal -- which the cast branch of
// parseUnary builds -- can take postfix operators too, per the C grammar
// ("(struct P){1, 2}.y" is a valid postfix expression).
func (p *Parser) parsePostfixFrom(e Expr) (Expr, error) {
	for {
		if p.atPunct("(") {
			// Any expression may be called: a plain identifier takes the
			// direct-call path, everything else ((*fp)(x), tab[i](x),
			// s.cb(x)) goes through a computed address.
			id, _ := e.(*Ident)
			p.next()
			// va_arg(ap, Type): the second argument is a type name, not an
			// expression, so it cannot go through the normal comma-expression
			// parsing. Build a dedicated VaArgExpr node instead.
			if id != nil && id.Name == "va_arg" {
				ap, err := p.parseAssign()
				if err != nil {
					return nil, err
				}
				if err := p.expect(","); err != nil {
					return nil, err
				}
				spec, err := p.parseDeclarationSpecifiers()
				if err != nil {
					return nil, err
				}
				dt, err := p.parseDeclarator(spec, true, true)
				if err != nil {
					return nil, err
				}
				if err := p.expect(")"); err != nil {
					return nil, err
				}
				e = &VaArgExpr{Ap: ap, Typ: dt.typ}
				continue
			}
			var args []Expr
			if !p.atPunct(")") {
				for {
					// C89 argument expressions are assignment-expressions,
					// not comma-expressions: the top-level commas separate
					// arguments. Parenthesised sub-expressions still parse
					// the full comma-expression via parseExpr inside.
					a, err := p.parseAssign()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.atPunct(",") {
						p.next()
						continue
					}
					break
				}
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			// A callee that is a plain identifier stays a Call (the fast path
			// every direct call -- including calls through a function-pointer
			// VARIABLE -- takes). Anything else is reached through a computed
			// address: (*fp)(x), tab[i](x), s.cb(x), (f ? g : h)(x).
			if id, ok := e.(*Ident); ok {
				e = &Call{Name: id.Name, Args: args}
			} else {
				e = &IndirectCall{Fn: e, Args: args}
			}
		} else if p.atPunct("[") {
			p.next()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			e = &Index{Base: e, Idx: idx}
		} else if p.atPunct(".") || p.atPunct("->") {
			arrow := p.cur().Text == "->"
			p.next()
			if p.cur().Kind != TIdent {
				sep := "."
				if arrow {
					sep = "->"
				}
				return nil, fmt.Errorf("line %d: expected member name after %q", p.cur().Line, sep)
			}
			name := p.next().Text
			e = &MemberExpr{Base: e, Name: name, Arrow: arrow, Line: p.cur().Line}
		} else if p.atPunct("++") || p.atPunct("--") {
			op := p.next().Text
			e = &IncDecExpr{Op: op, E: e, Prefix: false}
		} else {
			break
		}
	}
	return e, nil
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.cur()
	switch {
	case t.Kind == TNum:
		p.next()
		if t.IsDbl {
			return &NumLit{Kind: TDouble, Fval: t.Fval, IsFloat: t.IsFloat}, nil
		}
		if t.BigWords != nil {
			var lo int64
			if len(t.BigWords) > 0 {
				lo = int64(t.BigWords[0])
			}
			return &NumLit{
				Val:       lo,
				Kind:      TInt,
				Unsig:     !t.BigSigned,
				BigWords:  t.BigWords,
				BigSigned: t.BigSigned,
				BigBits:   t.BigBits,
			}, nil
		}
		return &NumLit{Val: t.Num, Kind: TInt, Unsig: t.IsUnsig, Long: t.IsLong, Wide: t.Wide}, nil
	case t.Kind == TStr:
		// Adjacent string literals concatenate (C translation phase 6), e.g.
		// "a" "b" becomes "ab". This is what lets a pasting macro like
		//   #define GREET(x) "hi " x
		//   GREET("there")   ->   "hi " "there"   ->   "hi there"
		// produce a single usable string.
		b := append([]byte(nil), t.Str...)
		wide := t.Wide
		p.next()
		for p.cur().Kind == TStr {
			// L"a" L"b" and "a" L"b" both concatenate: C says a wide literal
			// in the sequence makes the whole result wide (with the narrow
			// ones converted, which for UTF-8 input is a plain re-encode).
			if p.cur().Wide && !wide {
				b = utf16le(b)
				wide = true
			} else if wide && !p.cur().Wide {
				b = append(b, utf16le(p.cur().Str)...)
				p.next()
				continue
			}
			b = append(b, p.cur().Str...)
			p.next()
		}
		return &StrLit{Bytes: b, Wide: wide}, nil
	case t.Kind == TKeyword && (t.Text == "true" || t.Text == "false"):
		p.next()
		v := int64(0)
		if t.Text == "true" {
			v = 1
		}
		return &NumLit{Val: v, Kind: TInt}, nil
	case t.Kind == TIdent && t.Text == "nullptr":
		// C23 nullptr: a null pointer constant. goc lowers it to the integer
		// literal 0, so it is implicitly convertible to any pointer type
		// (exactly like NULL), while keeping a distinct spelling from "0".
		p.next()
		return &NumLit{Val: 0, Kind: TInt}, nil
	case t.Kind == TIdent:
		p.next()
		return &Ident{Name: t.Text, Line: t.Line}, nil
	case p.atPunct("("):
		p.next()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, fmt.Errorf("line %d: unexpected token %q in expression", t.Line, t.Text)
}
