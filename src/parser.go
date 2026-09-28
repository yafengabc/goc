package main

import "fmt"

// typeKeywords are the keywords the parser treats as the start of a type
// specifier.
var typeKeywords = map[string]bool{
	"void": true, "char": true, "int": true, "long": true, "short": true,
	"unsigned": true, "signed": true, "double": true, "float": true,
	"struct": true, "union": true, "enum": true, "_Bool": true,
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
	"typedef": true, "extern": true, "static": true, "register": true, "auto": true,
}

// typedefs maps a typedef name to the type it aliases. Populated during parsing
// of "typedef" declarations and consulted by isTypeName so later declarations
// can use the alias as a type name.
var typedefs = map[string]*Type{}

// structs maps a struct/union tag to its (possibly incomplete) type. A forward
// declaration "struct S;" creates the type; a later definition "struct S {...}"
// fills in the members in place so every reference shares the same *Type.
var structs = map[string]*Type{}

// enumConsts maps an enumerator name to its integer value. Enumerators are
// file-scope integer constants in C; the checker, the constant-expression
// evaluator and the code generator all consult this table as a fallback when a
// name is not a variable.
var enumConsts = map[string]int64{}

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
	// Storage-class specifiers: typedef / extern / static. These precede the
	// type specifier list. "typedef" creates an alias and produces no symbol;
	// extern/static only affect linkage (ignored by the toy model) and fall
	// through to the normal declaration logic.
	storage := ""
	if p.cur().Kind == TKeyword && (p.cur().Text == "typedef" || p.cur().Text == "extern" || p.cur().Text == "static" || p.cur().Text == "register" || p.cur().Text == "auto") {
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

	spec, err := p.parseDeclarationSpecifiers()
	if err != nil {
		return nil, err
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
	// A function definition: declarator followed by a block.
	if p.atPunct("{") {
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &FuncDecl{
			Name:       d.name,
			Ret:        d.typ.Ret,
			Params:     d.paramNames,
			ParamTypes: d.typ.Params,
			Variadic:   d.variadic,
			Body:       body,
		}, nil
	}
	// A declaration: either a function prototype (no body) or a global
	// variable (with optional initialiser). Both end at ';'.
	if d.typ.Kind == KFunc {
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
		}, nil
	}
	// Global variable declaration with optional initialiser: "T name = expr;".
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
	if !p.atPunct(";") {
		return nil, fmt.Errorf("line %d: expected ';' after global declaration", p.cur().Line)
	}
	p.next()
	p.globals = append(p.globals, &DeclStmt{Name: d.name, Typ: declType(d.typ), Init: init, Line: d.line})
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
	var tdType *Type // a typedef alias, if this specifier list names one
	for {
		if isQualifier(p.cur()) {
			if p.cur().Text == "const" {
				isConst = true
			}
			p.next()
			continue
		}
		if !isTypeName(p.cur()) {
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
				return &t2, nil
			}
			return t, nil
		}
		if p.cur().Text == "_Bool" {
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
			return &t2, nil
		}
		return fp, nil
	}
	if isVoid {
		return VoidType(), nil
	}
	if isBool {
		if isConst {
			return &Type{Kind: KBool, Width: 1, Signed: true, Const: true}, nil
		}
		return &Type{Kind: KBool, Width: 1, Signed: true}, nil
	}
	if tdType != nil {
		// The specifier list was a typedef alias (e.g. va_list, size_t). The
		// pointer/array suffixes are applied later by parseDeclarator, so just
		// hand back the alias's underlying type. A const-qualified typedef use
		// is stamped onto a copy so the alias itself is never polluted.
		if isConst {
			t2 := *tdType
			t2.Const = true
			return &t2, nil
		}
		return tdType, nil
	}
	if width == 0 {
		width = 4 // bare "signed"/"unsigned" means int
	}
	t := &Type{Kind: KInt, Width: width, Signed: signed}
	if isConst {
		t.Const = true
	}
	return t, nil
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
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, err
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
			v, err := p.constAdd()
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
	v, err := p.constAdd()
	if err != nil {
		return 0, err
	}
	return v, nil
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
	for p.atPunct("*") || p.atPunct("/") {
		op := p.next().Text
		r, err := p.constPrim()
		if err != nil {
			return 0, err
		}
		if op == "*" {
			v *= r
		} else {
			if r == 0 {
				return 0, fmt.Errorf("line %d: division by zero in array size", p.cur().Line)
			}
			v /= r
		}
	}
	return v, nil
}

func (p *Parser) constPrim() (int, error) {
	if p.atPunct("(") {
		p.next()
		v, err := p.constAdd()
		if err != nil {
			return 0, err
		}
		if err := p.expect(")"); err != nil {
			return 0, err
		}
		return v, nil
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
// atEllipsis reports whether the cursor is at a "..." token sequence (three
// consecutive '.' punctuation tokens).
func (p *Parser) atEllipsis() bool {
	return p.cur().Kind == TPunct && p.cur().Text == "." &&
		p.peek().Kind == TPunct && p.peek().Text == "." &&
		p.toks[p.pos+2].Kind == TPunct && p.toks[p.pos+2].Text == "."
}

// consumeEllipsis advances past a "..." token sequence.
func (p *Parser) consumeEllipsis() {
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
	t := p.cur()
	var err error
	switch {
	case isTypeName(t) || isQualifier(t) || isStorageClass(t):
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
			if isTypeName(p.cur()) {
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
		v, err := p.constAdd()
		if err != nil {
			return nil, fmt.Errorf("line %d: case label must be an integer constant", t.Line)
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
	if p.cur().Kind == TKeyword && (p.cur().Text == "typedef" || p.cur().Text == "extern" || p.cur().Text == "static" || p.cur().Text == "register" || p.cur().Text == "auto") {
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
	spec, err := p.parseDeclarationSpecifiers()
	if err != nil {
		return nil, err
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
		decls = append(decls, &DeclStmt{Name: pd.name, Typ: declType(pd.typ), Init: init, Storage: storage, Line: pd.line})
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

// parseBraceInit parses a braced initialiser "{ a, .x = b, { c } }" found in
// the initialiser position of a declaration. Elements are assignment
// expressions, nested braces, or designated members (". name ="), separated by
// commas with an optional trailing comma. Array designators ("[2] = ...") are
// not supported and are rejected with a clear message.
func (p *Parser) parseBraceInit() (Expr, error) {
	line := p.cur().Line
	p.next() // consume '{'
	bi := &BraceInit{Line: line}
	for {
		if p.atPunct("}") {
			p.next()
			break
		}
		el := InitElem{}
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
			return nil, fmt.Errorf("line %d: array designators ([i] = ...) are not supported in initialisers", p.cur().Line)
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

// parseAssign parses an assignment expression. Simple "=" binds the value of the
// right side (and yields it), while the compound operators (+= -= *= /= %= &=
// |= <<= >>=) desugar to "lhs = lhs OP rhs". Assignment is the lowest-precedence
// expression operator (below the ternary ?:).
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
		return &AssignExpr{Lhs: left, Rhs: &Binary{Op: op, L: left, R: right}}, nil
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
	if p.atPunct("-") || p.atPunct("!") {
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
		return &NumLit{Val: t.Num, Kind: TInt}, nil
	case t.Kind == TStr:
		// Adjacent string literals concatenate (C translation phase 6), e.g.
		// "a" "b" becomes "ab". This is what lets a pasting macro like
		//   #define GREET(x) "hi " x
		//   GREET("there")   ->   "hi " "there"   ->   "hi there"
		// produce a single usable string.
		b := append([]byte(nil), t.Str...)
		p.next()
		for p.cur().Kind == TStr {
			b = append(b, p.cur().Str...)
			p.next()
		}
		return &StrLit{Bytes: b}, nil
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
