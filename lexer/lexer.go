// Package lexer turns source text into a stream of tokens.
package lexer

import (
	"strings"

	"github.com/zg/interp/token"
)

// Lexer scans source text. It tracks line and column so that parse and
// runtime errors can point at the offending source rather than just failing.
type Lexer struct {
	input []rune // runes, not bytes, so multi-byte source is handled

	pos     int // current position
	readPos int // next position
	ch      rune

	line   int
	column int
}

func New(input string) *Lexer {
	l := &Lexer{input: []rune(input), line: 1, column: 0}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPos]
	}
	l.pos = l.readPos
	l.readPos++

	if l.ch == '\n' {
		l.line++
		l.column = 0
	} else {
		l.column++
	}
}

func (l *Lexer) peekChar() rune {
	if l.readPos >= len(l.input) {
		return 0
	}
	return l.input[l.readPos]
}

// Next returns the next token.
func (l *Lexer) Next() token.Token {
	l.skipWhitespaceAndComments()

	line, col := l.line, l.column

	make := func(t token.Type, lit string) token.Token {
		return token.Token{Type: t, Literal: lit, Line: line, Column: col}
	}
	// two builds a two-character token, consuming the second character.
	two := func(t token.Type, lit string) token.Token {
		l.readChar()
		return make(t, lit)
	}

	var tok token.Token
	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			tok = two(token.EQ, "==")
		} else {
			tok = make(token.ASSIGN, "=")
		}
	case '!':
		if l.peekChar() == '=' {
			tok = two(token.NOT_EQ, "!=")
		} else {
			tok = make(token.BANG, "!")
		}
	case '<':
		if l.peekChar() == '=' {
			tok = two(token.LTE, "<=")
		} else {
			tok = make(token.LT, "<")
		}
	case '>':
		if l.peekChar() == '=' {
			tok = two(token.GTE, ">=")
		} else {
			tok = make(token.GT, ">")
		}
	case '&':
		if l.peekChar() == '&' {
			tok = two(token.AND, "&&")
		} else {
			tok = make(token.ILLEGAL, string(l.ch))
		}
	case '|':
		if l.peekChar() == '|' {
			tok = two(token.OR, "||")
		} else {
			tok = make(token.ILLEGAL, string(l.ch))
		}
	case '+':
		tok = make(token.PLUS, "+")
	case '-':
		tok = make(token.MINUS, "-")
	case '*':
		tok = make(token.ASTERISK, "*")
	case '/':
		tok = make(token.SLASH, "/")
	case '%':
		tok = make(token.PERCENT, "%")
	case ',':
		tok = make(token.COMMA, ",")
	case ';':
		tok = make(token.SEMICOLON, ";")
	case ':':
		tok = make(token.COLON, ":")
	case '(':
		tok = make(token.LPAREN, "(")
	case ')':
		tok = make(token.RPAREN, ")")
	case '{':
		tok = make(token.LBRACE, "{")
	case '}':
		tok = make(token.RBRACE, "}")
	case '[':
		tok = make(token.LBRACKET, "[")
	case ']':
		tok = make(token.RBRACKET, "]")
	case '"':
		lit, ok := l.readString()
		if !ok {
			return token.Token{Type: token.ILLEGAL, Literal: "unterminated string", Line: line, Column: col}
		}
		tok = make(token.STRING, lit)
	case 0:
		tok = make(token.EOF, "")
	default:
		switch {
		case isLetter(l.ch):
			// Identifiers consume their own characters, so return early
			// rather than falling through to the readChar below.
			lit := l.readIdentifier()
			return token.Token{Type: token.LookupIdent(lit), Literal: lit, Line: line, Column: col}
		case isDigit(l.ch):
			lit, isFloat := l.readNumber()
			t := token.INT
			if isFloat {
				t = token.FLOAT
			}
			return token.Token{Type: token.Type(t), Literal: lit, Line: line, Column: col}
		default:
			tok = make(token.ILLEGAL, string(l.ch))
		}
	}

	l.readChar()
	return tok
}

func (l *Lexer) skipWhitespaceAndComments() {
	for {
		switch {
		case l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r':
			l.readChar()
		case l.ch == '/' && l.peekChar() == '/':
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
		case l.ch == '/' && l.peekChar() == '*':
			l.readChar()
			l.readChar()
			for !(l.ch == '*' && l.peekChar() == '/') && l.ch != 0 {
				l.readChar()
			}
			if l.ch != 0 {
				l.readChar() // *
				l.readChar() // /
			}
		default:
			return
		}
	}
}

func (l *Lexer) readIdentifier() string {
	start := l.pos
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return string(l.input[start:l.pos])
}

// readNumber reads an integer or float, reporting which it found. A trailing
// dot not followed by a digit (as in "1..2" or a method-style "1.") is left
// for the next token rather than swallowed.
func (l *Lexer) readNumber() (string, bool) {
	start := l.pos
	for isDigit(l.ch) {
		l.readChar()
	}
	isFloat := false
	if l.ch == '.' && isDigit(l.peekChar()) {
		isFloat = true
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	return string(l.input[start:l.pos]), isFloat
}

// readString reads a double-quoted string with backslash escapes. It
// reports false when the string is never closed, which must be an error
// rather than a silent EOF.
func (l *Lexer) readString() (string, bool) {
	var sb strings.Builder
	for {
		l.readChar()
		switch l.ch {
		case '"':
			return sb.String(), true
		case 0:
			return "", false
		case '\\':
			l.readChar()
			switch l.ch {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case '"':
				sb.WriteRune('"')
			case '\\':
				sb.WriteRune('\\')
			case 0:
				return "", false
			default:
				// Unknown escape: keep both characters rather than
				// silently dropping the backslash.
				sb.WriteRune('\\')
				sb.WriteRune(l.ch)
			}
		default:
			sb.WriteRune(l.ch)
		}
	}
}

func isLetter(ch rune) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch rune) bool { return '0' <= ch && ch <= '9' }
