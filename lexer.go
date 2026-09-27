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
	"int": true, "double": true, "if": true, "else": true, "while": true, "return": true,
}

type Token struct {
	Kind  TokKind
	Text  string
	Num   int64
	Fval  float64
	IsDbl bool
	Str   []byte
	Line  int
	Space bool // true if whitespace preceded this token (separates macro name from '(' etc.)
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
			for i < n && isDigit(src[i]) {
				i++
			}
			isDbl := false
			if i < n && src[i] == '.' {
				isDbl = true
				i++
				for i < n && isDigit(src[i]) {
					i++
				}
			}
			text := src[start:i]
			if isDbl {
				f, _ := strconv.ParseFloat(text, 64)
				push(Token{Kind: TNum, Text: text, Fval: f, IsDbl: true, Line: line})
			} else {
				var v int64
				fmt.Sscanf(text, "%d", &v)
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
		default:
			two := ""
			if i+1 < n {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=", "&&", "||", "##":
				push(Token{Kind: TPunct, Text: two, Line: line})
				i += 2
				continue
			}
			if strings.IndexByte("+-*/%=<>!(){};,.", c) >= 0 {
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
