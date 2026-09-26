package main

import "fmt"

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

func (p *Parser) expectKeyword(kw string) error {
	if p.cur().Kind != TKeyword || p.cur().Text != kw {
		return fmt.Errorf("line %d: expected keyword %q, got %q", p.cur().Line, kw, p.cur().Text)
	}
	p.next()
	return nil
}

func (p *Parser) parseFunc() (*FuncDecl, error) {
	if err := p.expectKeyword("int"); err != nil {
		return nil, err
	}
	if p.cur().Kind != TIdent {
		return nil, fmt.Errorf("line %d: expected function name", p.cur().Line)
	}
	name := p.next().Text
	if err := p.expect("("); err != nil {
		return nil, err
	}
	var params []string
	if p.cur().Text != ")" {
		for {
			if err := p.expectKeyword("int"); err != nil {
				return nil, err
			}
			if p.cur().Kind != TIdent {
				return nil, fmt.Errorf("line %d: expected parameter name", p.cur().Line)
			}
			params = append(params, p.next().Text)
			if p.cur().Text == "," {
				p.next()
				continue
			}
			break
		}
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FuncDecl{Name: name, Params: params, Body: body}, nil
}

func (p *Parser) parseBlock() (*Block, error) {
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	b := &Block{}
	for p.cur().Text != "}" {
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
	case t.Kind == TKeyword && t.Text == "return":
		p.next()
		var e Expr
		if p.cur().Text != ";" {
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
	case t.Kind == TKeyword && t.Text == "int":
		p.next()
		if p.cur().Kind != TIdent {
			return nil, fmt.Errorf("line %d: expected variable name", p.cur().Line)
		}
		name := p.next().Text
		var init Expr
		if p.cur().Text == "=" {
			p.next()
			var err error
			init, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return &DeclStmt{Name: name, Init: init}, nil
	case t.Kind == TIdent:
		name := t.Text
		if p.peek().Text == "=" {
			p.next() // ident
			p.next() // '='
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			return &AssignStmt{Name: name, E: e}, nil
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return &ExprStmt{E: e}, nil
	case t.Kind == TPunct && t.Text == "{":
		return p.parseBlock()
	}
	return nil, fmt.Errorf("line %d: unexpected token %q", t.Line, t.Text)
}

func (p *Parser) parseExpr() (Expr, error) { return p.parseOr() }

func (p *Parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur().Text == "||" {
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
	for p.cur().Text == "&&" {
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
	for p.cur().Text == "==" || p.cur().Text == "!=" {
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
	for p.cur().Text == "<" || p.cur().Text == ">" || p.cur().Text == "<=" || p.cur().Text == ">=" {
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
	for p.cur().Text == "+" || p.cur().Text == "-" {
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
	for p.cur().Text == "*" || p.cur().Text == "/" || p.cur().Text == "%" {
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
	if p.cur().Text == "-" || p.cur().Text == "!" {
		op := p.next().Text
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: op, E: e}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.cur()
	switch {
	case t.Kind == TNum:
		p.next()
		return &NumLit{Val: t.Num}, nil
	case t.Kind == TStr:
		p.next()
		return &StrLit{Bytes: t.Str}, nil
	case t.Kind == TIdent:
		p.next()
		if p.cur().Text == "(" {
			p.next()
			var args []Expr
			if p.cur().Text != ")" {
				for {
					e, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					args = append(args, e)
					if p.cur().Text == "," {
						p.next()
						continue
					}
					break
				}
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return &Call{Name: t.Text, Args: args}, nil
		}
		return &Ident{Name: t.Text}, nil
	case p.cur().Text == "(":
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
