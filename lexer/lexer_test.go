package lexer

import (
	"testing"

	"github.com/zg/interp/token"
)

func TestNextToken(t *testing.T) {
	input := `let five = 5;
let add = fn(x, y) { x + y; };
!-/ *5; 5 < 10 > 5;
if (5 < 10) { return true; } else { return false; }
10 == 10; 10 != 9; 10 <= 10; 10 >= 9;
"foobar" "foo bar" ""
[1, 2]; {"k": 1}; 3.14; a && b || c; 7 % 2;
`
	want := []struct {
		typ token.Type
		lit string
	}{
		{token.LET, "let"}, {token.IDENT, "five"}, {token.ASSIGN, "="}, {token.INT, "5"}, {token.SEMICOLON, ";"},
		{token.LET, "let"}, {token.IDENT, "add"}, {token.ASSIGN, "="}, {token.FUNCTION, "fn"},
		{token.LPAREN, "("}, {token.IDENT, "x"}, {token.COMMA, ","}, {token.IDENT, "y"}, {token.RPAREN, ")"},
		{token.LBRACE, "{"}, {token.IDENT, "x"}, {token.PLUS, "+"}, {token.IDENT, "y"}, {token.SEMICOLON, ";"},
		{token.RBRACE, "}"}, {token.SEMICOLON, ";"},
		{token.BANG, "!"}, {token.MINUS, "-"}, {token.SLASH, "/"}, {token.ASTERISK, "*"}, {token.INT, "5"}, {token.SEMICOLON, ";"},
		{token.INT, "5"}, {token.LT, "<"}, {token.INT, "10"}, {token.GT, ">"}, {token.INT, "5"}, {token.SEMICOLON, ";"},
		{token.IF, "if"}, {token.LPAREN, "("}, {token.INT, "5"}, {token.LT, "<"}, {token.INT, "10"}, {token.RPAREN, ")"},
		{token.LBRACE, "{"}, {token.RETURN, "return"}, {token.TRUE, "true"}, {token.SEMICOLON, ";"}, {token.RBRACE, "}"},
		{token.ELSE, "else"}, {token.LBRACE, "{"}, {token.RETURN, "return"}, {token.FALSE, "false"}, {token.SEMICOLON, ";"}, {token.RBRACE, "}"},
		{token.INT, "10"}, {token.EQ, "=="}, {token.INT, "10"}, {token.SEMICOLON, ";"},
		{token.INT, "10"}, {token.NOT_EQ, "!="}, {token.INT, "9"}, {token.SEMICOLON, ";"},
		{token.INT, "10"}, {token.LTE, "<="}, {token.INT, "10"}, {token.SEMICOLON, ";"},
		{token.INT, "10"}, {token.GTE, ">="}, {token.INT, "9"}, {token.SEMICOLON, ";"},
		{token.STRING, "foobar"}, {token.STRING, "foo bar"}, {token.STRING, ""},
		{token.LBRACKET, "["}, {token.INT, "1"}, {token.COMMA, ","}, {token.INT, "2"}, {token.RBRACKET, "]"}, {token.SEMICOLON, ";"},
		{token.LBRACE, "{"}, {token.STRING, "k"}, {token.COLON, ":"}, {token.INT, "1"}, {token.RBRACE, "}"}, {token.SEMICOLON, ";"},
		{token.FLOAT, "3.14"}, {token.SEMICOLON, ";"},
		{token.IDENT, "a"}, {token.AND, "&&"}, {token.IDENT, "b"}, {token.OR, "||"}, {token.IDENT, "c"}, {token.SEMICOLON, ";"},
		{token.INT, "7"}, {token.PERCENT, "%"}, {token.INT, "2"}, {token.SEMICOLON, ";"},
		{token.EOF, ""},
	}

	l := New(input)
	for i, w := range want {
		tok := l.Next()
		if tok.Type != w.typ || tok.Literal != w.lit {
			t.Fatalf("token %d: got %s %q, want %s %q", i, tok.Type, tok.Literal, w.typ, w.lit)
		}
	}
}

// `/*` opens a block comment even where division-then-multiplication was
// meant, exactly as in C. Pinning this so the ambiguity is a documented
// choice rather than a surprise.
func TestSlashStarStartsCommentNotDivision(t *testing.T) {
	l := New("1 /* 2 */ 3")
	for _, want := range []string{"1", "3"} {
		if tok := l.Next(); tok.Literal != want {
			t.Fatalf("got %q, want %q", tok.Literal, want)
		}
	}
	// An unterminated block comment runs to end of input rather than
	// looping forever.
	l2 := New("1 /* never closed")
	if tok := l2.Next(); tok.Literal != "1" {
		t.Fatalf("got %q, want 1", tok.Literal)
	}
	if tok := l2.Next(); tok.Type != token.EOF {
		t.Fatalf("got %s, want EOF", tok.Type)
	}
}

func TestComments(t *testing.T) {
	l := New("1 // line comment\n2 /* block\ncomment */ 3")
	for _, want := range []string{"1", "2", "3"} {
		if tok := l.Next(); tok.Literal != want {
			t.Fatalf("got %q, want %q", tok.Literal, want)
		}
	}
	if tok := l.Next(); tok.Type != token.EOF {
		t.Fatalf("got %s, want EOF", tok.Type)
	}
}

func TestStringEscapes(t *testing.T) {
	tests := []struct{ in, want string }{
		{`"a\nb"`, "a\nb"},
		{`"a\tb"`, "a\tb"},
		{`"a\"b"`, `a"b`},
		{`"a\\b"`, `a\b`},
		{`"a\qb"`, `a\qb`}, // unknown escape kept verbatim
	}
	for _, tt := range tests {
		tok := New(tt.in).Next()
		if tok.Type != token.STRING || tok.Literal != tt.want {
			t.Errorf("%s -> %s %q, want STRING %q", tt.in, tok.Type, tok.Literal, tt.want)
		}
	}
}

// An unterminated string must be an error, not a silent EOF that makes the
// parser report something misleading further along.
func TestUnterminatedString(t *testing.T) {
	for _, in := range []string{`"abc`, `"abc\`} {
		if tok := New(in).Next(); tok.Type != token.ILLEGAL {
			t.Errorf("%s -> %s, want ILLEGAL", in, tok.Type)
		}
	}
}

func TestPositionTracking(t *testing.T) {
	l := New("let\n  x")
	first := l.Next()
	if first.Line != 1 {
		t.Errorf("'let' line = %d, want 1", first.Line)
	}
	second := l.Next()
	if second.Line != 2 {
		t.Errorf("'x' line = %d, want 2", second.Line)
	}
}

func TestUnicodeIdentifiersAndStrings(t *testing.T) {
	// Multi-byte characters must not be split mid-rune.
	tok := New(`"héllo wörld"`).Next()
	if tok.Literal != "héllo wörld" {
		t.Errorf("got %q", tok.Literal)
	}
}

func TestIllegalCharacters(t *testing.T) {
	for _, in := range []string{"@", "#", "&", "|"} {
		if tok := New(in).Next(); tok.Type != token.ILLEGAL {
			t.Errorf("%q -> %s, want ILLEGAL", in, tok.Type)
		}
	}
}

// A trailing dot must not be swallowed into the number.
func TestNumberFollowedByDot(t *testing.T) {
	l := New("1.5 2")
	if tok := l.Next(); tok.Type != token.FLOAT || tok.Literal != "1.5" {
		t.Fatalf("got %s %q, want FLOAT 1.5", tok.Type, tok.Literal)
	}
	if tok := l.Next(); tok.Type != token.INT || tok.Literal != "2" {
		t.Fatalf("got %s %q, want INT 2", tok.Type, tok.Literal)
	}
}
