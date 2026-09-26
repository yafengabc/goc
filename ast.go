package main

// AST node definitions for the tiny C subset understood by c0.

type Node interface{}

// CType is the type of a value. Everything is 8 bytes on the stack; the only
// distinction is whether an expression lives in a GP register (int) or an XMM
// register (double) and how clib / the ABI treat it.
type CType int

const (
	TInt CType = iota
	TDouble
)

type Program struct {
	Funcs []*FuncDecl
}

type FuncDecl struct {
	Name       string
	Ret        CType
	Params     []string
	ParamTypes []CType
	Body       *Block
}

type Block struct {
	Stmts []Stmt
}

type Stmt interface{}

type DeclStmt struct {
	Name string
	Typ  CType
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
	Val  int64
	Kind CType
	Fval float64
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
