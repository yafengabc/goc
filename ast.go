package main

// AST node definitions for the tiny C subset understood by c0.

type Node interface{}

type Program struct {
	Funcs []*FuncDecl
}

type FuncDecl struct {
	Name   string
	Params []string
	Body   *Block
}

type Block struct {
	Stmts []Stmt
}

type Stmt interface{}

type DeclStmt struct {
	Name string
	Init Expr // may be nil
}

type AssignStmt struct {
	Name string
	E    Expr
}

type ExprStmt struct {
	E Expr
}

type ReturnStmt struct {
	E Expr // may be nil
}

type IfStmt struct {
	Cond Expr
	Then Stmt
	Else Stmt // may be nil
}

type WhileStmt struct {
	Cond Expr
	Body Stmt
}

type Expr interface{}

type NumLit struct {
	Val int64
}

type StrLit struct {
	Bytes []byte
}

type Ident struct {
	Name string
}

type Unary struct {
	Op string // "-" or "!"
	E  Expr
}

type Binary struct {
	Op string // "+" "-" "*" "/" "%" "<" ">" "<=" ">=" "==" "!=" "&&" "||"
	L  Expr
	R  Expr
}

type Call struct {
	Name string
	Args []Expr
}
