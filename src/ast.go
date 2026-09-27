package main

// AST node definitions for the tiny C subset understood by goc.

type Node interface{}

// CType is the type of a value. Everything is 8 bytes on the stack; the only
// distinction is whether an expression lives in a GP register (int) or an XMM
// register (double) and how goclib / the ABI treat it.
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
	// code generator's "defined functions" set so that a prototype for a goclib
	// function (e.g. strlen) still triggers goclib inclusion rather than being
	// mistaken for a user definition.
	Prototypes []*FuncDecl
	// Globals are top-level variable declarations (with optional initialisers).
	// They are emitted into the .data section by the code generator and are
	// visible to every function.
	Globals []*DeclStmt
}

type FuncDecl struct {
	Name       string
	Ret        *Type
	Params     []string
	ParamTypes []*Type
	Variadic   bool
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

// AssignExpr is an assignment used in expression position: "(a = b)" yields the
// value of b. It is produced by the parser wherever an assignment appears inside
// a larger expression (e.g. "while ((*d++ = *src++) != 0)"). Statement-level
// "a = b;" is likewise wrapped as ExprStmt{AssignExpr} so both share one
// code path.
type AssignExpr struct {
	Lhs Expr // must be an lvalue
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

// ForStmt is a C for-loop: for (Init; Cond; Post) Body. Init is a Stmt
// (typically a DeclStmt or ExprStmt); Cond and Post are expressions (either may
// be nil, meaning "absent").
type ForStmt struct {
	Init Stmt
	Cond Expr
	Post Expr
	Body Stmt
}

// DoWhileStmt is a C do-while loop: "do Body while (Cond);". The body always
// runs at least once, and the condition is tested after it -- so continue jumps
// to the condition rather than to the top of the body.
type DoWhileStmt struct {
	Body Stmt
	Cond Expr
}

// SwitchStmt is a C switch. The body is kept flat: CaseStmt and DefaultStmt are
// ordinary statements inside the block, exactly where the source puts them.
// That preserves C's fall-through semantics for free -- control dropping out of
// one case simply runs whatever statement follows it -- and it lets "default"
// sit anywhere in the body, not just at the end.
type SwitchStmt struct {
	Src  Expr
	Body *Block
	Line int
}

// CaseStmt is a "case N:" label. The value is folded to an integer constant by
// the parser (C requires a constant expression here), so codegen can compare
// against an immediate.
type CaseStmt struct {
	Val  int
	Line int
}

// DefaultStmt is a "default:" label.
type DefaultStmt struct {
	Line int
}

// GotoStmt is "goto label;". Labels are function-scoped; the checker verifies
// that the target exists in the same function.
type GotoStmt struct {
	Label string
	Line  int
}

// LabelStmt is "label: statement". Labelling a statement (rather than making a
// label its own statement) keeps the parse tree close to the C grammar and lets
// "x: ;" label an empty statement.
type LabelStmt struct {
	Name string
	Stmt Stmt
	Line int
}

type BreakStmt struct{}

type ContinueStmt struct{}

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
	Op string // "+" "-" "*" "/" "%" "<" ">" "<=" ">=" "==" "!=" "&&" "||" "&" "|" "^" "<<" ">>"
	L  Expr
	R  Expr
}

type Call struct {
	Name string
	Args []Expr
}

// IndirectCall is a call whose callee is computed at run time rather than
// named: (*fp)(x), tab[i](x), s.cb(x). Fn evaluates to the address of the
// function. A call written directly against an identifier is still a Call --
// even when that identifier turns out to be a function-pointer variable, which
// the type checker and code generator resolve later.
type IndirectCall struct {
	Fn   Expr
	Args []Expr
}

// Index is the subscript operator: arr[i] or ptr[i]. The base may be an array
// or a pointer; the element type is the base's element type.
type Index struct {
	Base Expr
	Idx  Expr
}

// CondExpr is the ternary operator a ? b : c.
type CondExpr struct {
	Cond Expr
	Then Expr
	Else Expr
}

// CastExpr is a C-style cast (Type)E. Value-level conversion is handled by the
// code generator (most integer widths share the 8-byte slot); only int<->double
// and width truncation/extension actually move bits.
type CastExpr struct {
	Typ *Type
	E   Expr
}

// IncDecExpr is a prefix (++x) or postfix (x++) increment/decrement. Prefix is
// true for ++x / --x, false for x++ / x--.
type IncDecExpr struct {
	Op     string // "++" or "--"
	E      Expr
	Prefix bool
}

// VaArgExpr is the va_arg(ap, Type) builtin of <stdarg.h>. The second "argument"
// is a type, not an expression, so the parser builds this dedicated node instead
// of a Call. Ap is the va_list cursor; Typ is the requested element type.
type VaArgExpr struct {
	Ap  Expr
	Typ *Type
}

// MemberExpr is the field access operator: "base.member" (. form) when Arrow is
// false, or "base->member" (-> form) when Arrow is true. In the -> form the base
// must have pointer-to-struct type and the compiler dereferences it before
// adding the member offset; in the . form the base must be a struct lvalue.
type MemberExpr struct {
	Base  Expr
	Name  string
	Arrow bool
	Line  int
}

// SizeofExpr is the sizeof operator: either "sizeof(Type)" or "sizeof expr". It
// is folded to a constant by the parser/checker where possible and lowered to a
// plain integer literal by the code generator.
type SizeofExpr struct {
	Typ *Type // non-nil when the operand is a type name
	E   Expr  // non-nil when the operand is an expression
}
