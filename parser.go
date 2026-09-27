package main

import "fmt"

// typeKeywords are the keywords the parser treats as the start of a type
// specifier. "struct" is recognised but rejected (not implemented in stage 2).
var typeKeywords = map[string]bool{
	"void": true, "char": true, "int": true, "long": true, "short": true,
	"unsigned": true, "signed": true, "double": true, "struct": true,
}

// qualifierKeywords are type qualifiers that decorate a specifier list but
// carry no codegen meaning for the toy model (const/volatile/restrict).
var qualifierKeywords = map[string]bool{
	"const": true, "volatile": true, "restrict": true,
}

// typedefs maps a typedef name to the type it aliases. Populated during parsing
// of "typedef" declarations and consulted by isTypeName so later declarations
// can use the alias as a type name.
var typedefs = map[string]*Type{}

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
	if p.cur().Kind == TKeyword && (p.cur().Text == "typedef" || p.cur().Text == "extern" || p.cur().Text == "static") {
		storage = p.next().Text
	}
	if storage == "typedef" {
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, err
		}
		for {
			pd, err := p.parseDeclarator(spec, false, false)
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
		return nil, nil
	}

	spec, err := p.parseDeclarationSpecifiers()
	if err != nil {
		return nil, err
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
		init, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	if !p.atPunct(";") {
		return nil, fmt.Errorf("line %d: expected ';' after global declaration", p.cur().Line)
	}
	p.next()
	p.globals = append(p.globals, &DeclStmt{Name: d.name, Typ: d.typ, Init: init, Line: d.line})
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
	isDouble := false
	isVoid := false
	seen := false
	for {
		if isQualifier(p.cur()) {
			p.next()
			continue
		}
		if !isTypeName(p.cur()) {
			break
		}
		if p.cur().Text == "struct" {
			return nil, fmt.Errorf("line %d: struct is not supported in stage 2", p.cur().Line)
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
			isDouble = true
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
	if isDouble {
		return DoubleType(), nil
	}
	if isVoid {
		return VoidType(), nil
	}
	if width == 0 {
		width = 8 // bare "signed"/"unsigned" means int
	}
	return &Type{Kind: KInt, Width: width, Signed: signed}, nil
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
	wasGrouped := false
	if p.atPunct("(") {
		p.next()
		inner, err := p.parseDeclarator(base, allowFunc, abstract)
		if err != nil {
			return d, err
		}
		if err := p.expect(")"); err != nil {
			return d, err
		}
		d = inner
		d.line = inner.line
		wasGrouped = true
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
	// Suffixes: array [...], function (...). A function suffix on a grouped
	// declarator would be a function pointer, which stage 2 does not support.
	for {
		if p.atPunct("[") {
			p.next()
			n, err := p.parseArrayLength()
			if err != nil {
				return d, err
			}
			if err := p.expect("]"); err != nil {
				return d, err
			}
			d.typ = ArrType(d.typ, n)
		} else if p.atPunct("(") {
			if !allowFunc {
				return d, fmt.Errorf("line %d: function type not allowed here", p.cur().Line)
			}
			if wasGrouped {
				return d, fmt.Errorf("line %d: function pointers are not supported in stage 2", p.cur().Line)
			}
			params, names, variadic, err := p.parseParamList()
			if err != nil {
				return d, err
			}
			d.typ = FuncType(d.typ, params)
			d.paramNames = names
			d.variadic = variadic
		} else {
			break
		}
	}
	return d, nil
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
	if p.cur().Kind == TNum && !p.cur().IsDbl {
		v := int(p.next().Num)
		return v, nil
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
			return nil, nil, false, fmt.Errorf("line %d: expected parameter name", p.cur().Line)
		}
		pd, err := p.parseDeclarator(spec, false, false)
		if err != nil {
			return nil, nil, false, err
		}
		if pd.typ.IsArray() {
			pd.typ = PtrType(pd.typ.Elem)
		}
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
	case isTypeName(t):
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
	}

	// Expression statement, or assignment if it is followed by '='.
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.atPunct("=") {
		p.next()
		rhs, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return &AssignStmt{Lhs: e, Rhs: rhs}, nil
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	return &ExprStmt{E: e}, nil
}

// parseDeclaration parses one declaration specifier list followed by a
// comma-separated list of initialised declarators: "int a = 1, b[3];".
func (p *Parser) parseDeclaration() (Stmt, error) {
	spec, err := p.parseDeclarationSpecifiers()
	if err != nil {
		return nil, err
	}
	var decls []*DeclStmt
	for {
		pd, err := p.parseDeclarator(spec, false, false)
		if err != nil {
			return nil, err
		}
		var init Expr
		if p.atPunct("=") {
			p.next()
			init, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		decls = append(decls, &DeclStmt{Name: pd.name, Typ: pd.typ, Init: init, Line: pd.line})
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

func (p *Parser) parseExpr() (Expr, error) { return p.parseCond() }

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
	// name; otherwise '(' is an expression-grouping parenthesis.
	if p.atPunct("(") && isTypeName(p.peek()) {
		p.next() // consume '('
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, err
		}
		dt, err := p.parseDeclarator(spec, false, true)
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
			id, ok := e.(*Ident)
			if !ok {
				return nil, fmt.Errorf("line %d: cannot call a non-function expression", p.cur().Line)
			}
			p.next()
			var args []Expr
			if !p.atPunct(")") {
				for {
					a, err := p.parseExpr()
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
			e = &Call{Name: id.Name, Args: args}
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
			return &NumLit{Kind: TDouble, Fval: t.Fval}, nil
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
