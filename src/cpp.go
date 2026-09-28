package main

// cpp.go — the C preprocessor for goc.
//
// It runs on the token stream produced by Lex (where '#' is a real token) and
// returns a fully-expanded token stream that Parse can consume. The design is
// token-based (not text-based), so macro arguments, '#' stringisation and '##'
// pasting are handled on real tokens rather than by string hacking.
//
// Supported:
//   - #include "file" / <file>   (local relative + search dirs)
//   - #define object- and function-like macros, with #, ## and __VA_ARGS__
//   - #undef
//   - #if / #ifdef / #ifndef / #else / #elif / #endif  (constant expressions,
//     including the defined() operator)
//   - #error   (raised only when the branch is active)
//   - #line N ["file"] (and the GNU "# N ["file"]" form), affecting __LINE__,
//     __FILE__ and diagnostic line numbers
//   - predefined macros __FILE__, __LINE__, __goc__
//   - backslash line continuations inside macro definitions
//
// Things deliberately left for a later stage: #pragma beyond ignoring it,
// and most of the hosted-header ecosystem.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Macro is a single #define'd entity.
type Macro struct {
	Name       string
	Params     []string // nil/empty for object-like; for variadic includes "__VA_ARGS__"
	IsFunc     bool
	IsVariadic bool
	Body       []Token
}

// condFrame tracks one #if/#else chain.
type condFrame struct {
	branchTaken bool // a branch in this block has already evaluated true
	active      bool // is the current branch selected for output
	seenElse    bool
}

// Preprocessor holds the expandable state shared across a translation unit,
// including files pulled in by #include.
type Preprocessor struct {
	macros     map[string]*Macro
	inExpand   map[string]bool // macro names currently being expanded (recursion guard)
	condStack  []condFrame
	searchDirs []string
	baseDir    string
	// #line state: a logical file name (empty = the real source file) and the
	// offset such that logicalLine = physicalLine + lineDelta. Reset per file
	// in process, so a #line inside an #include cannot leak into the includer.
	logicalFile string
	lineDelta   int
}

// Preprocess runs the full preprocessing pipeline on src (already read from
// filename) and returns the expanded token stream.
func Preprocess(src, filename string) ([]Token, error) {
	src = spliceContinuations(src)
	p := &Preprocessor{
		macros:     map[string]*Macro{},
		inExpand:   map[string]bool{},
		searchDirs: []string{},
	}
	p.baseDir = filepath.Dir(filename)
	p.searchDirs = append(p.searchDirs, p.baseDir, ".")
	return p.process(src, filename)
}

// spliceContinuations removes backslash-newline pairs (C translation phase 2),
// so multi-line macro definitions collapse onto one logical line.
func spliceContinuations(src string) string {
	var b strings.Builder
	i := 0
	n := len(src)
	for i < n {
		if src[i] == '\\' {
			if i+1 < n && src[i+1] == '\n' {
				i += 2
				continue
			}
			if i+2 < n && src[i+1] == '\r' && src[i+2] == '\n' {
				i += 3
				continue
			}
		}
		b.WriteByte(src[i])
		i++
	}
	return b.String()
}

// process tokenises one file and scans it. It owns a fresh conditional-compile
// stack and #line state (an #if or #line inside an included file does not
// affect the includer), but shares the macro table so definitions are global.
func (p *Preprocessor) process(src, filename string) ([]Token, error) {
	save := p.condStack
	saveFile := p.logicalFile
	saveDelta := p.lineDelta
	p.condStack = nil
	p.logicalFile = ""
	p.lineDelta = 0
	defer func() {
		p.condStack = save
		p.logicalFile = saveFile
		p.lineDelta = saveDelta
	}()

	raw, err := Lex(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", filename, err)
	}
	out, err := p.scan(raw, filename)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// active reports whether the current position is inside a selected branch of
// every enclosing #if.
func (p *Preprocessor) active() bool {
	for _, f := range p.condStack {
		if !f.active {
			return false
		}
	}
	return true
}

// scan walks the raw token stream, expands identifiers that are macros, and
// handles directives.
func (p *Preprocessor) scan(raw []Token, filename string) ([]Token, error) {
	var out []Token
	i := 0
	n := len(raw)
	for i < n {
		t := raw[i]
		if t.Kind == TEOF {
			break
		}
		if t.Kind == TPunct && t.Text == "#" && isDirectiveStart(raw, i) {
			hashLine := t.Line
			i++ // consume '#'
			if i >= n {
				continue
			}
			name := raw[i].Text
			i++ // consume directive name
			var rest []Token
			for i < n && raw[i].Line == hashLine {
				rest = append(rest, raw[i])
				i++
			}
			app, err := p.execDirective(name, rest, hashLine, filename)
			if err != nil {
				return nil, err
			}
			out = append(out, app...)
			continue
		}
		// Ordinary token (or inactive code): skip entirely when not active.
		if !p.active() {
			i++
			continue
		}
		e, ni := p.expandAt(raw, i, filename, raw[i].Line)
		out = append(out, e...)
		i = ni
	}
	out = append(out, Token{Kind: TEOF, Line: 0})
	return out, nil
}

// isDirectiveStart reports whether the '#' at index i begins a directive: it
// must be the first non-blank token on its line.
func isDirectiveStart(raw []Token, i int) bool {
	if raw[i].Text != "#" {
		return false
	}
	if i == 0 {
		return true
	}
	return raw[i-1].Line != raw[i].Line
}

// execDirective runs one directive and returns any tokens it produces (only
// #include produces tokens).
func (p *Preprocessor) execDirective(name string, rest []Token, line int, filename string) ([]Token, error) {
	// GNU extension: "# 100 \"file\"" is the same as "#line 100 \"file\"".
	if isAllDigits(name) {
		n, _ := strconv.ParseInt(name, 10, 64)
		return p.doLine(n, rest, line, filename)
	}
	switch name {
	case "include":
		if !p.active() {
			return nil, nil
		}
		return p.doInclude(rest, filename)
	case "define":
		if p.active() {
			p.doDefine(rest)
		}
	case "undef":
		if p.active() && len(rest) > 0 {
			delete(p.macros, rest[0].Text)
		}
	case "if":
		expanded := p.expandTokens(rest, filename, line)
		p.pushCond(p.constExpr(expanded) != 0)
	case "ifdef":
		p.pushCond(p.macroDefined(rest))
	case "ifndef":
		p.pushCond(!p.macroDefined(rest))
	case "elif":
		p.doElif(p.expandTokens(rest, filename, line))
	case "else":
		p.doElse()
	case "endif":
		if len(p.condStack) > 0 {
			p.condStack = p.condStack[:len(p.condStack)-1]
		}
	case "error":
		if p.active() {
			return nil, fmt.Errorf("%s:%d: #error", p.logicalFileName(filename), p.logicalLine(line))
		}
	case "line":
		if !p.active() {
			return nil, nil
		}
		if len(rest) == 0 || rest[0].Kind != TNum {
			return nil, fmt.Errorf("%s:%d: #line needs a line number", p.logicalFileName(filename), p.logicalLine(line))
		}
		return p.doLine(rest[0].Num, rest[1:], line, filename)
	case "warning", "pragma":
		// Ignored for now.
	default:
		if p.active() {
			return nil, fmt.Errorf("%s:%d: unknown preprocessing directive #%s", p.logicalFileName(filename), p.logicalLine(line), name)
		}
	}
	return nil, nil
}

// isAllDigits reports whether s is a non-empty digit sequence (the GNU
// "# <digits>" directive name).
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// logicalLine and logicalFileName apply the current #line state to a physical
// source position, for __LINE__ / __FILE__ and for diagnostics.
func (p *Preprocessor) logicalLine(line int) int { return line + p.lineDelta }

func (p *Preprocessor) logicalFileName(filename string) string {
	if p.logicalFile != "" {
		return p.logicalFile
	}
	return filename
}

// doLine implements #line N ["file"]: from the next source line on, __LINE__
// reports N+1 for the line after the directive, and __FILE__ (if given) the
// new name. Applies only when the branch is active.
func (p *Preprocessor) doLine(n int64, rest []Token, line int, filename string) ([]Token, error) {
	if !p.active() {
		return nil, nil
	}
	if n <= 0 {
		return nil, fmt.Errorf("%s:%d: #line line number must be positive", p.logicalFileName(filename), p.logicalLine(line))
	}
	// The directive sits on physical line `line`; the next physical line is
	// logically N+1, so the offset is N - line.
	p.lineDelta = int(n) - line
	if len(rest) > 0 && rest[0].Kind == TStr {
		p.logicalFile = string(rest[0].Str)
	}
	return nil, nil
}

// macroDefined handles #ifdef / #ifndef, both "X" and "(X)" forms.
func (p *Preprocessor) macroDefined(rest []Token) bool {
	if len(rest) == 0 {
		return false
	}
	name := rest[0].Text
	if name == "(" && len(rest) >= 3 {
		name = rest[1].Text
	}
	_, ok := p.macros[name]
	return ok
}

func (p *Preprocessor) pushCond(cond bool) {
	p.condStack = append(p.condStack, condFrame{branchTaken: cond, active: cond})
}

func (p *Preprocessor) doElse() {
	if len(p.condStack) == 0 {
		return
	}
	f := p.condStack[len(p.condStack)-1]
	if f.seenElse {
		return
	}
	f.seenElse = true
	f.active = !f.branchTaken
	p.condStack[len(p.condStack)-1] = f
}

func (p *Preprocessor) doElif(rest []Token) {
	if len(p.condStack) == 0 {
		return
	}
	f := p.condStack[len(p.condStack)-1]
	if f.seenElse {
		return
	}
	cond := p.constExpr(rest) != 0
	if !f.branchTaken && cond {
		f.active = true
	}
	f.branchTaken = f.branchTaken || cond
	p.condStack[len(p.condStack)-1] = f
}

// doDefine parses a #define directive from its body tokens (everything after
// "define").
func (p *Preprocessor) doDefine(rest []Token) {
	if len(rest) == 0 {
		return
	}
	name := rest[0].Text
	m := &Macro{Name: name}
	i := 1
	// A function-like macro has '(' immediately after the name (no space).
	if i < len(rest) && rest[i].Text == "(" && !rest[i].Space {
		m.IsFunc = true
		i++ // skip '('
		for i < len(rest) && rest[i].Text != ")" {
			if rest[i].Text == "..." {
				m.IsVariadic = true
				i++
				if i < len(rest) && rest[i].Text == "," {
					i++
				}
				continue
			}
			if rest[i].Kind == TIdent {
				m.Params = append(m.Params, rest[i].Text)
			}
			i++
			if i < len(rest) && rest[i].Text == "," {
				i++
			}
		}
		if m.IsVariadic {
			m.Params = append(m.Params, "__VA_ARGS__")
		}
		if i < len(rest) && rest[i].Text == ")" {
			i++
		}
	}
	m.Body = rest[i:]
	p.macros[name] = m
}

// doInclude resolves and expands an #include.
func (p *Preprocessor) doInclude(rest []Token, filename string) ([]Token, error) {
	if len(rest) == 0 {
		return nil, fmt.Errorf("%s: #include needs a filename", filename)
	}
	t := rest[0]
	var path string
	angled := false
	if t.Kind == TStr {
		path = string(t.Str)
	} else if t.Text == "<" {
		var sb strings.Builder
		for i := 1; i < len(rest) && rest[i].Text != ">"; i++ {
			sb.WriteString(rest[i].Text)
		}
		path = sb.String()
		angled = true
	} else {
		return nil, fmt.Errorf("%s: malformed #include", filename)
	}

	full, err := p.resolveInclude(path, filename, angled)
	if err != nil {
		if angled {
			// System headers that goc ships (stdio.h, stdlib.h, string.h, ...)
			// are embedded as real files under goclib/ and injected directly,
			// with no disk lookup. An unavailable <file> is skipped rather
			// than fatal.
			if src, rerr := goclibHeaders.ReadFile("goclib/" + path); rerr == nil {
				inc, perr := p.process(string(src), "<builtin:"+path+">")
				if perr != nil {
					return nil, perr
				}
				if len(inc) > 0 && inc[len(inc)-1].Kind == TEOF {
					inc = inc[:len(inc)-1]
				}
				return inc, nil
			}
			fmt.Fprintf(os.Stderr, "%s: note: skipping unavailable system header <%s>\n", filename, path)
			return nil, nil
		}
		return nil, err
	}
	src, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot read %s: %v", filename, path, err)
	}
	inc, err := p.process(string(src), full)
	if err != nil {
		return nil, err
	}
	// Drop the trailing TEOF that process() appends; the includer's scan owns
	// its own terminator.
	if len(inc) > 0 && inc[len(inc)-1].Kind == TEOF {
		inc = inc[:len(inc)-1]
	}
	return inc, nil
}

func (p *Preprocessor) resolveInclude(path, fromFile string, angled bool) (string, error) {
	var candidates []string
	if !angled {
		candidates = append(candidates, filepath.Join(filepath.Dir(fromFile), path))
	}
	for _, d := range p.searchDirs {
		candidates = append(candidates, filepath.Join(d, path))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("include file not found: %s", path)
}

// expandAt expands the token at index i (handling macros and predefined
// macros) and returns the resulting tokens plus the next index to read. line
// is the source line of the token currently being processed (the invocation
// site), so __LINE__ resolves to where the macro is used, not where it was
// defined.
func (p *Preprocessor) expandAt(raw []Token, i int, filename string, line int) ([]Token, int) {
	t := raw[i]
	if t.Kind == TIdent {
		switch t.Text {
		case "__FILE__":
			return []Token{tokStr(p.logicalFileName(filename), t.Line)}, i + 1
		case "__LINE__":
			return []Token{tokNum(int64(p.logicalLine(line)), t.Line)}, i + 1
		case "__goc__":
			return []Token{tokNum(1, t.Line)}, i + 1
		}
		if m, ok := p.macros[t.Text]; ok && !p.inExpand[t.Text] && p.active() {
			if !m.IsFunc {
				p.inExpand[t.Text] = true
				e := p.expandTokens(m.Body, filename, line)
				delete(p.inExpand, t.Text)
				return e, i + 1
			}
			// Function-like: needs an adjacent '('.
			if i+1 < len(raw) && raw[i+1].Text == "(" {
				args, next, err := p.readArgs(raw, i+1)
				if err == nil {
					p.inExpand[t.Text] = true
					e := p.expandFunc(m, args, filename, line)
					delete(p.inExpand, t.Text)
					return e, next
				}
			}
		}
	}
	return []Token{t}, i + 1
}

// expandTokens fully expands a token slice.
func (p *Preprocessor) expandTokens(toks []Token, filename string, line int) []Token {
	var out []Token
	j := 0
	for j < len(toks) {
		e, nj := p.expandAt(toks, j, filename, line)
		out = append(out, e...)
		j = nj
	}
	return out
}

// readArgs collects the comma-separated arguments of a function-like macro
// invocation starting at the '(' at openIdx. Nested parentheses are tracked;
// the top-level comma separates arguments.
func (p *Preprocessor) readArgs(raw []Token, openIdx int) ([][]Token, int, error) {
	if openIdx >= len(raw) || raw[openIdx].Text != "(" {
		return nil, openIdx, fmt.Errorf("expected '('")
	}
	args := [][]Token{}
	depth := 0
	i := openIdx + 1
	n := len(raw)
	cur := []Token{}
	for i < n {
		t := raw[i]
		switch t.Text {
		case "(":
			depth++
			cur = append(cur, t)
			i++
		case ")":
			if depth == 0 {
				args = append(args, cur)
				return args, i + 1, nil
			}
			depth--
			cur = append(cur, t)
			i++
		case ",":
			if depth == 0 {
				args = append(args, cur)
				cur = []Token{}
				i++
			} else {
				cur = append(cur, t)
				i++
			}
		default:
			cur = append(cur, t)
			i++
		}
	}
	return nil, i, fmt.Errorf("unterminated macro arguments")
}

// expandFunc performs parameter substitution, '#' stringisation and '##'
// pasting for a function-like macro, then re-expands the result.
func (p *Preprocessor) expandFunc(m *Macro, args [][]Token, filename string, line int) []Token {
	if m.IsVariadic {
		np := len(m.Params) // includes "__VA_ARGS__"
		if len(args) >= np {
			merged := []Token{}
			for k := np - 1; k < len(args); k++ {
				if k > np-1 {
					merged = append(merged, Token{Kind: TPunct, Text: ","})
				}
				merged = append(merged, args[k]...)
			}
			args = append(args[:np-1], merged)
		}
	}

	var out []Token
	i := 0
	for i < len(m.Body) {
		bt := m.Body[i]
		// Stringisation: # param
		if bt.Kind == TPunct && bt.Text == "#" && i+1 < len(m.Body) && isParam(m.Body[i+1], m) {
			idx := paramIndex(m.Body[i+1].Text, m)
			out = append(out, stringize(args[idx], bt.Line)...)
			i += 2
			continue
		}
		// Pasting: a ## b
		if bt.Kind == TPunct && bt.Text == "##" {
			if len(out) == 0 {
				i++
				continue
			}
			i++
			if i >= len(m.Body) {
				break
			}
			nxt := m.Body[i]
			var nxtToks []Token
			if isParam(nxt, m) {
				nxtToks = args[paramIndex(nxt.Text, m)]
			} else {
				nxtToks = []Token{nxt}
			}
			if len(nxtToks) > 0 {
				merged := lexOne(out[len(out)-1].Text + tokenSpelling(nxtToks[0]))
				out[len(out)-1] = merged
				out = append(out, nxtToks[1:]...)
			}
			i++
			continue
		}
		if isParam(bt, m) {
			idx := paramIndex(bt.Text, m)
			// An argument used as a '##' operand is not pre-expanded.
			if i+1 < len(m.Body) && m.Body[i+1].Kind == TPunct && m.Body[i+1].Text == "##" {
				out = append(out, args[idx]...)
			} else {
				out = append(out, p.expandTokens(args[idx], filename, line)...)
			}
			i++
			continue
		}
		out = append(out, bt)
		i++
	}
	return p.expandTokens(out, filename, line)
}

func isParam(t Token, m *Macro) bool {
	return t.Kind == TIdent && paramIndex(t.Text, m) >= 0
}

func paramIndex(name string, m *Macro) int {
	for i, p := range m.Params {
		if p == name {
			return i
		}
	}
	return -1
}

// stringize turns a token list into a string-literal token (the C '#' operator).
func stringize(toks []Token, line int) []Token {
	var sb strings.Builder
	for idx, t := range toks {
		if idx > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(tokenSpelling(t))
	}
	return []Token{{Kind: TStr, Str: []byte(sb.String()), Line: line}}
}

// tokenSpelling returns the textual form of a token, used for pasting and
// stringisation.
func tokenSpelling(t Token) string {
	switch t.Kind {
	case TStr:
		return "\"" + string(t.Str) + "\""
	case TNum, TIdent, TKeyword:
		return t.Text
	case TPunct:
		return t.Text
	}
	return t.Text
}

// lexOne re-lexes a piece of text into a single token (used by '##' pasting).
func lexOne(s string) Token {
	toks, err := Lex(s)
	if err == nil && len(toks) > 0 && toks[0].Kind != TEOF {
		return toks[0]
	}
	return Token{Kind: TPunct, Text: s}
}

func tokStr(s string, line int) Token {
	return Token{Kind: TStr, Str: []byte(s), Line: line}
}

func tokNum(v int64, line int) Token {
	return Token{Kind: TNum, Num: v, Text: strconv.FormatInt(v, 10), Line: line}
}

// constExpr evaluates a (pre-expanded) integer constant expression used by
// #if / #elif. Undefined identifiers and string constants are 0; character
// literals carry their byte value (the lexer emits them as TNum).
func (p *Preprocessor) constExpr(toks []Token) int64 {
	if len(toks) == 0 {
		return 0
	}
	v, _ := p.ceOr(toks, 0)
	return v
}

func (p *Preprocessor) ceOr(toks []Token, i int) (int64, int) {
	left, i := p.ceAnd(toks, i)
	for i < len(toks) && toks[i].Text == "||" {
		i++
		right, ni := p.ceAnd(toks, i)
		i = ni
		if left != 0 || right != 0 {
			left = 1
		} else {
			left = 0
		}
	}
	return left, i
}

func (p *Preprocessor) ceAnd(toks []Token, i int) (int64, int) {
	left, i := p.ceEq(toks, i)
	for i < len(toks) && toks[i].Text == "&&" {
		i++
		right, ni := p.ceEq(toks, i)
		i = ni
		if left != 0 && right != 0 {
			left = 1
		} else {
			left = 0
		}
	}
	return left, i
}

func (p *Preprocessor) ceEq(toks []Token, i int) (int64, int) {
	left, i := p.ceRel(toks, i)
	for i < len(toks) && (toks[i].Text == "==" || toks[i].Text == "!=") {
		op := toks[i].Text
		i++
		right, ni := p.ceRel(toks, i)
		i = ni
		var r int64
		if op == "==" {
			if left == right {
				r = 1
			} else {
				r = 0
			}
		} else {
			if left != right {
				r = 1
			} else {
				r = 0
			}
		}
		left = r
	}
	return left, i
}

func (p *Preprocessor) ceRel(toks []Token, i int) (int64, int) {
	left, i := p.ceAdd(toks, i)
	for i < len(toks) && (toks[i].Text == "<" || toks[i].Text == ">" ||
		toks[i].Text == "<=" || toks[i].Text == ">=") {
		op := toks[i].Text
		i++
		right, ni := p.ceAdd(toks, i)
		i = ni
		var r int64
		switch op {
		case "<":
			if left < right {
				r = 1
			}
		case ">":
			if left > right {
				r = 1
			}
		case "<=":
			if left <= right {
				r = 1
			}
		case ">=":
			if left >= right {
				r = 1
			}
		}
		left = r
	}
	return left, i
}

func (p *Preprocessor) ceAdd(toks []Token, i int) (int64, int) {
	left, i := p.ceMul(toks, i)
	for i < len(toks) && (toks[i].Text == "+" || toks[i].Text == "-") {
		op := toks[i].Text
		i++
		right, ni := p.ceMul(toks, i)
		i = ni
		if op == "+" {
			left += right
		} else {
			left -= right
		}
	}
	return left, i
}

func (p *Preprocessor) ceMul(toks []Token, i int) (int64, int) {
	left, i := p.ceUnary(toks, i)
	for i < len(toks) && (toks[i].Text == "*" || toks[i].Text == "/" || toks[i].Text == "%") {
		op := toks[i].Text
		i++
		right, ni := p.ceUnary(toks, i)
		i = ni
		switch op {
		case "*":
			left *= right
		case "/":
			if right != 0 {
				left /= right
			}
		case "%":
			if right != 0 {
				left %= right
			}
		}
	}
	return left, i
}

func (p *Preprocessor) ceUnary(toks []Token, i int) (int64, int) {
	if i >= len(toks) {
		return 0, i
	}
	t := toks[i]
	switch t.Text {
	case "!":
		v, ni := p.ceUnary(toks, i+1)
		if v == 0 {
			return 1, ni
		}
		return 0, ni
	case "-":
		v, ni := p.ceUnary(toks, i+1)
		return -v, ni
	case "+":
		return p.ceUnary(toks, i+1)
	}
	return p.cePrimary(toks, i)
}

func (p *Preprocessor) cePrimary(toks []Token, i int) (int64, int) {
	if i >= len(toks) {
		return 0, i
	}
	t := toks[i]
	switch {
	case t.Kind == TNum:
		return t.Num, i + 1
	case t.Kind == TIdent && t.Text == "defined":
		i++
		name := ""
		if i < len(toks) && toks[i].Text == "(" {
			i++
			if i < len(toks) {
				name = toks[i].Text
				i++
			}
			if i < len(toks) && toks[i].Text == ")" {
				i++
			}
		} else if i < len(toks) {
			name = toks[i].Text
			i++
		}
		if _, ok := p.macros[name]; ok {
			return 1, i
		}
		return 0, i
	case t.Kind == TIdent:
		return 0, i + 1
	case t.Text == "(":
		v, ni := p.ceOr(toks, i+1)
		if ni < len(toks) && toks[ni].Text == ")" {
			ni++
		}
		return v, ni
	}
	return 0, i + 1
}
