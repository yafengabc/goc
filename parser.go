package main

import "fmt"

// typeKeywords are the keywords the parser treats as the start of a type
// specifier. "struct" is recognised but rejected (not implemented in stage 2).
var typeKeywords = map[string]bool{
	"void": true, "char": true, "int": true, "long": true, "short": true,
	"unsigned": true, "signed": true, "double": true, "struct": true,
}

func isTypeName(tok Token) bool {
	return tok.Kind == TKeyword && typeKeywords[tok.Text]
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
	line       int
}

type Parser struct {
	toks []Token
	pos  int
}

func Parse(toks []Token) (*Program, error) {
	p := &Parser{toks: toks}
	prog := &Program{}
	for p.cur().Kind != TEOF {
		fd, err := p.parseFunc()
		if err != nil {
			return nil, err
		}
		prog.Funcs = append(prog.Funcs, fd)
	}
	return prog, nil
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
	for isTypeName(p.cur()) {
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
func (p *Parser) parseDeclarator(base *Type, allowFunc bool) (declResult, error) {
	for p.atPunct("*") {
		base = PtrType(base)
		p.next()
	}
	var d declResult
	wasGrouped := false
	if p.atPunct("(") {
		p.next()
		inner, err := p.parseDeclarator(base, allowFunc)
		if err != nil {
			return d, err
		}
		if err := p.expect(")"); err != nil {
			return d, err
		}
		d = inner
		d.line = inner.line
		wasGrouped = true
	} else {
		if p.cur().Kind != TIdent {
			return d, fmt.Errorf("line %d: expected declarator name, got %q", p.cur().Line, p.cur().Text)
		}
		nameTok := p.next()
		d.name = nameTok.Text
		d.line = nameTok.Line
		d.typ = base
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
			params, names, err := p.parseParamList()
			if err != nil {
				return d, err
			}
			d.typ = FuncType(d.typ, params)
			d.paramNames = names
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
func (p *Parser) parseParamList() ([]*Type, []string, error) {
	if err := p.expect("("); err != nil {
		return nil, nil, err
	}
	var types []*Type
	var names []string
	if p.atPunct(")") {
		p.next()
		return types, names, nil
	}
	for {
		spec, err := p.parseDeclarationSpecifiers()
		if err != nil {
			return nil, nil, err
		}
		if p.atPunct(")") {
			// "void" alone => empty parameter list
			if spec.IsVoid() {
				p.next()
				return types, names, nil
			}
			return nil, nil, fmt.Errorf("line %d: expected parameter name", p.cur().Line)
		}
		pd, err := p.parseDeclarator(spec, false)
		if err != nil {
			return nil, nil, err
		}
		if pd.typ.IsArray() {
			pd.typ = PtrType(pd.typ.Elem)
		}
		types = append(types, pd.typ)
		names = append(names, pd.name)
		if p.atPunct(",") {
			p.next()
			continue
		}
		break
	}
	if err := p.expect(")"); err != nil {
		return nil, nil, err
	}
	return types, names, nil
}

// parseFunc parses a single function definition: specifiers, a function
// declarator, and a body block.
func (p *Parser) parseFunc() (*FuncDecl, error) {
	ret, err := p.parseDeclarationSpecifiers()
	if err != nil {
		return nil, err
	}
	d, err := p.parseDeclarator(ret, true)
	if err != nil {
		return nil, err
	}
	if d.typ.Kind != KFunc {
		return nil, fmt.Errorf("line %d: expected function definition", p.cur().Line)
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FuncDecl{
		Name:       d.name,
		Ret:        d.typ.Ret,
		Params:     d.paramNames,
		ParamTypes: d.typ.Params,
		Body:       body,
	}, nil
}

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
		pd, err := p.parseDeclarator(spec, false)
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

func (p *Parser) parseExpr() (Expr, error) { return p.parseOr() }

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
	left, err := p.parseEq()
	if err != nil {
		return nil, err
	}
	for p.atPunct("&&") {
		p.next()
		right, err := p.parseEq()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "&&", L: left, R: right}
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
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.atPunct("<") || p.atPunct(">") || p.atPunct("<=") || p.atPunct(">=") {
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
