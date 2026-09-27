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
	Funcs []*FuncDecl // function definitions (Body != nil)
	// Prototypes are forward declarations from #include'd headers (Body ==
	// nil). They are used only by the type checker to validate calls; they
	// are never emitted as code, and they are deliberately kept out of the
	// code generator's "defined functions" set so that a prototype for a clib
	// function (e.g. strlen) still triggers clib inclusion rather than being
	// mistaken for a user definition.
	Prototypes []*FuncDecl
}

type FuncDecl struct {
	Name       string
	Ret        *Type
	Params     []string
	ParamTypes []*Type
	Body       *Block
}

type Block struct {
	Stmts []Stmt
}

// DeclList groups several comma-separated declarations from one statement.
type DeclList struct {
	Decls []*DeclStmt
}

type Stmt interface{}

type DeclStmt struct {
	Name string
	Typ  *Type
	Init Expr // may be nil
	Line int
}

type AssignStmt struct {
	Lhs Expr // must be an lvalue: Ident, Unary("*"), or Index
	Rhs Expr
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
	Line int
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

// Index is the subscript operator: arr[i] or ptr[i]. The base may be an array
// or a pointer; the element type is the base's element type.
type Index struct {
	Base Expr
	Idx  Expr
}
