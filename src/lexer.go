package main

import (
	"fmt"
	"strconv"
	"strings"
)

type TokKind int

const (
	TEOF TokKind = iota
	TIdent
	TNum
	TStr
	TPunct
	TKeyword
)

var keywords = map[string]bool{
	"int": true, "double": true, "float": true, "if": true, "else": true, "while": true, "return": true,
	"char": true, "long": true, "short": true, "unsigned": true, "signed": true,
	"void": true, "struct": true, "union": true, "enum": true, "_Bool": true,
	"for": true, "break": true, "continue": true,
	"switch": true, "case": true, "default": true, "do": true, "goto": true,
	"typedef": true, "extern": true, "static": true,
	"register": true, "auto": true,
	"const": true, "volatile": true, "restrict": true,
}

type Token struct {
	Kind    TokKind
	Text    string
	Num     int64
	Fval    float64
	IsDbl   bool
	IsFloat bool // a float constant: a floating literal with the f/F suffix
	IsChar  bool // a 'x' character literal (carried as an integer constant in Num)
	Str     []byte
	Line    int
	Space   bool // true if whitespace preceded this token (separates macro name from '(' etc.)
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isAlpha(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// Lex turns C source into a token slice.
//
// The preprocessor runs on the token stream this produces, so the '#' that
// begins a directive is emitted as a normal punctuation token rather than
// dropped: the preprocessor recognises a directive as a '#' that opens a new
// line. Token.Space records whether whitespace preceded the token, which the
// preprocessor needs to tell a function-like macro "F(x)" from an object-like
// macro "F (x)" (the space between name and '(' is significant in C).
func Lex(src string) ([]Token, error) {
	var toks []Token
	line := 1
	space := false
	push := func(t Token) {
		t.Space = space
		toks = append(toks, t)
		space = false
	}
	i := 0
	n := len(src)
	for i < n {
		c := src[i]
		switch {
		case c == '\n':
			line++
			space = true
			i++
		case c == ' ' || c == '\t' || c == '\r':
			space = true
			i++
		case c == '/' && i+1 < n && src[i+1] == '/':
			space = true
			i += 2
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '#':
			// '##' is the token-paste operator and must be a single token; a
			// lone '#' begins a directive or is the stringisation operator.
			if i+1 < n && src[i+1] == '#' {
				push(Token{Kind: TPunct, Text: "##", Line: line})
				i += 2
			} else {
				push(Token{Kind: TPunct, Text: "#", Line: line})
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i < n && !(src[i] == '*' && i+1 < n && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
		case isDigit(c):
			start := i
			isDbl := false
			// Hex literal: 0x[0-9a-fA-F]+. Size suffixes (u/l/ul) are
			// ignored, so 0xff and 1103515245UL both parse as integers.
			if c == '0' && i+1 < n && (src[i+1] == 'x' || src[i+1] == 'X') {
				i += 2
				for i < n && (isDigit(src[i]) ||
					(src[i] >= 'a' && src[i] <= 'f') ||
					(src[i] >= 'A' && src[i] <= 'F')) {
					i++
				}
				text := src[start:i]
				v, _ := strconv.ParseInt(text[2:], 16, 64)
				for i < n && (src[i] == 'u' || src[i] == 'U' || src[i] == 'l' || src[i] == 'L') {
					i++
				}
				push(Token{Kind: TNum, Text: text, Num: v, Line: line})
				continue
			}
			for i < n && isDigit(src[i]) {
				i++
			}
			if i < n && src[i] == '.' {
				isDbl = true
				i++
				for i < n && isDigit(src[i]) {
					i++
				}
			}
			// Exponent: [eE][+-]?digits. Only taken when at least one digit
			// follows, so "1e" or "1ex" still lex as an integer plus an
			// identifier.
			if i < n && (src[i] == 'e' || src[i] == 'E') {
				j := i + 1
				if j < n && (src[j] == '+' || src[j] == '-') {
					j++
				}
				if j < n && isDigit(src[j]) {
					isDbl = true
					i = j
					for i < n && isDigit(src[i]) {
						i++
					}
				}
			}
			text := src[start:i]
			if isDbl {
				isFloat := false
				for i < n && (src[i] == 'f' || src[i] == 'F' || src[i] == 'l' || src[i] == 'L') {
					if src[i] == 'f' || src[i] == 'F' {
						isFloat = true
					}
					i++
				}
				f, _ := strconv.ParseFloat(text, 64)
				push(Token{Kind: TNum, Text: text, Fval: f, IsDbl: true, IsFloat: isFloat, Line: line})
			} else {
				var v int64
				fmt.Sscanf(text, "%d", &v)
				for i < n && (src[i] == 'u' || src[i] == 'U' || src[i] == 'l' || src[i] == 'L') {
					i++
				}
				push(Token{Kind: TNum, Text: text, Num: v, Line: line})
			}
		case isAlpha(c):
			start := i
			for i < n && (isAlpha(src[i]) || isDigit(src[i])) {
				i++
			}
			text := src[start:i]
			if keywords[text] {
				push(Token{Kind: TKeyword, Text: text, Line: line})
			} else {
				push(Token{Kind: TIdent, Text: text, Line: line})
			}
		case c == '"':
			i++
			var buf []byte
			for i < n && src[i] != '"' {
				if src[i] == '\\' && i+1 < n {
					i++
					switch src[i] {
					case 'n':
						buf = append(buf, '\n')
					case 't':
						buf = append(buf, '\t')
					case 'r':
						buf = append(buf, '\r')
					case '\\':
						buf = append(buf, '\\')
					case '"':
						buf = append(buf, '"')
					case '0':
						buf = append(buf, 0)
					default:
						buf = append(buf, src[i])
					}
					i++
				} else {
					buf = append(buf, src[i])
					i++
				}
			}
			if i >= n {
				return nil, fmt.Errorf("line %d: unterminated string literal", line)
			}
			i++ // closing quote
			push(Token{Kind: TStr, Str: buf, Line: line})
		case c == '\'':
			// Character literal 'x' (or '\n', '\0', ...). Carried as an integer
			// constant (the byte value) so the parser/codegen need no new node.
			i++
			var ch byte
			if i < n && src[i] == '\\' && i+1 < n {
				i++
				switch src[i] {
				case 'n':
					ch = '\n'
				case 't':
					ch = '\t'
				case 'r':
					ch = '\r'
				case '\\':
					ch = '\\'
				case '\'':
					ch = '\''
				case '"':
					ch = '"'
				case '0':
					ch = 0
				default:
					ch = src[i]
				}
				i++
			} else if i < n {
				ch = src[i]
				i++
			}
			if i >= n || src[i] != '\'' {
				return nil, fmt.Errorf("line %d: unterminated character literal", line)
			}
			i++ // closing quote
			push(Token{Kind: TNum, Text: string(rune(ch)), Num: int64(ch), IsChar: true, Line: line})
		default:
			two := ""
			if i+1 < n {
				two = src[i : i+2]
			}
			three := ""
			if i+2 < n {
				three = src[i : i+3]
			}
			switch three {
			case "<<=", ">>=":
				push(Token{Kind: TPunct, Text: three, Line: line})
				i += 3
				continue
			}
			switch two {
			case "==", "!=", "<=", ">=", "&&", "||", "##", "<<", ">>", "++", "--",
				"+=", "-=", "*=", "/=", "%=", "&=", "|=", "->":
				push(Token{Kind: TPunct, Text: two, Line: line})
				i += 2
				continue
			}
			if strings.IndexByte("+-*/%=<>!(){};,.[]&?:|", c) >= 0 {
				push(Token{Kind: TPunct, Text: string(c), Line: line})
				i++
				continue
			}
			return nil, fmt.Errorf("line %d: unexpected character %q", line, c)
		}
	}
	push(Token{Kind: TEOF, Line: line})
	return toks, nil
}
