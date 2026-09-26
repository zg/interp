// Package token defines the lexical tokens of the language.
package token

import "fmt"

type Type string

const (
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"

	IDENT  = "IDENT"
	INT    = "INT"
	FLOAT  = "FLOAT"
	STRING = "STRING"

	ASSIGN   = "="
	PLUS     = "+"
	MINUS    = "-"
	BANG     = "!"
	ASTERISK = "*"
	SLASH    = "/"
	PERCENT  = "%"

	LT     = "<"
	GT     = ">"
	LTE    = "<="
	GTE    = ">="
	EQ     = "=="
	NOT_EQ = "!="
	AND    = "&&"
	OR     = "||"

	COMMA     = ","
	SEMICOLON = ";"
	COLON     = ":"

	LPAREN   = "("
	RPAREN   = ")"
	LBRACE   = "{"
	RBRACE   = "}"
	LBRACKET = "["
	RBRACKET = "]"

	FUNCTION = "FUNCTION"
	LET      = "LET"
	TRUE     = "TRUE"
	FALSE    = "FALSE"
	IF       = "IF"
	ELSE     = "ELSE"
	RETURN   = "RETURN"
	WHILE    = "WHILE"
	NULL     = "NULL"
)

// Token is a lexical token together with the position it was read from, so
// errors can point at the source.
type Token struct {
	Type    Type
	Literal string
	Line    int
	Column  int
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%q) at %d:%d", t.Type, t.Literal, t.Line, t.Column)
}

var keywords = map[string]Type{
	"fn":     FUNCTION,
	"let":    LET,
	"true":   TRUE,
	"false":  FALSE,
	"if":     IF,
	"else":   ELSE,
	"return": RETURN,
	"while":  WHILE,
	"null":   NULL,
}

// LookupIdent maps an identifier to its keyword type, or IDENT.
func LookupIdent(ident string) Type {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return IDENT
}
