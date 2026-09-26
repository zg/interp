package parser

import (
	"strings"
	"testing"

	"github.com/zg/interp/ast"
	"github.com/zg/interp/lexer"
)

func parse(t *testing.T, input string) *ast.Program {
	t.Helper()
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors for %q:\n%s", input, strings.Join(errs, "\n"))
	}
	return program
}

// The String() round-trip pins precedence and associativity far more
// legibly than asserting on tree shape node by node.
func TestOperatorPrecedence(t *testing.T) {
	tests := []struct{ in, want string }{
		{"-a * b", "((-a) * b)"},
		{"!-a", "(!(-a))"},
		{"a + b + c", "((a + b) + c)"},
		{"a + b - c", "((a + b) - c)"},
		{"a * b * c", "((a * b) * c)"},
		{"a * b / c", "((a * b) / c)"},
		{"a + b / c", "(a + (b / c))"},
		{"a + b % c", "(a + (b % c))"},
		{"a + b * c + d / e - f", "(((a + (b * c)) + (d / e)) - f)"},
		{"5 > 4 == 3 < 4", "((5 > 4) == (3 < 4))"},
		{"3 + 4 * 5 == 3 * 1 + 4 * 5", "((3 + (4 * 5)) == ((3 * 1) + (4 * 5)))"},
		{"true", "true"},
		{"3 > 5 == false", "((3 > 5) == false)"},
		{"1 + (2 + 3) + 4", "((1 + (2 + 3)) + 4)"},
		{"(5 + 5) * 2", "((5 + 5) * 2)"},
		{"-(5 + 5)", "(-(5 + 5))"},
		{"!(true == true)", "(!(true == true))"},
		{"a + add(b * c) + d", "((a + add((b * c))) + d)"},
		{"add(a, b, 1, 2 * 3, add(6, 7 * 8))", "add(a, b, 1, (2 * 3), add(6, (7 * 8)))"},
		{"a * [1, 2, 3, 4][b * c] * d", "((a * ([1, 2, 3, 4][(b * c)])) * d)"},
		{"add(a * b[2], b[1], 2 * [1, 2][1])", "add((a * (b[2])), (b[1]), (2 * ([1, 2][1])))"},
		// Logical operators bind looser than comparison.
		{"a < b && c > d", "((a < b) && (c > d))"},
		{"a || b && c", "(a || (b && c))"},
		// Assignment binds loosest of all and is right-associative.
		{"a = b + c", "(a = (b + c))"},
		{"a = b = c", "(a = (b = c))"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := parse(t, tt.in).String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLetStatements(t *testing.T) {
	tests := []struct {
		in   string
		name string
		val  string
	}{
		{"let x = 5;", "x", "5"},
		{"let y = true;", "y", "true"},
		{"let foobar = y;", "foobar", "y"},
		{`let s = "hi";`, "s", `"hi"`},
	}
	for _, tt := range tests {
		program := parse(t, tt.in)
		if len(program.Statements) != 1 {
			t.Fatalf("got %d statements, want 1", len(program.Statements))
		}
		stmt, ok := program.Statements[0].(*ast.LetStatement)
		if !ok {
			t.Fatalf("got %T, want *ast.LetStatement", program.Statements[0])
		}
		if stmt.Name.Value != tt.name {
			t.Errorf("name = %q, want %q", stmt.Name.Value, tt.name)
		}
		if stmt.Value.String() != tt.val {
			t.Errorf("value = %q, want %q", stmt.Value.String(), tt.val)
		}
	}
}

// A function bound by let takes the variable's name, which makes error
// messages and stack traces useful.
func TestLetNamesFunction(t *testing.T) {
	program := parse(t, "let double = fn(x) { x * 2 };")
	stmt := program.Statements[0].(*ast.LetStatement)
	fn, ok := stmt.Value.(*ast.FunctionLiteral)
	if !ok {
		t.Fatalf("got %T", stmt.Value)
	}
	if fn.Name != "double" {
		t.Errorf("name = %q, want %q", fn.Name, "double")
	}
}

func TestReturnStatements(t *testing.T) {
	program := parse(t, "return 5; return true; return;")
	if len(program.Statements) != 3 {
		t.Fatalf("got %d statements, want 3", len(program.Statements))
	}
	// A bare return is legal and carries no value.
	last := program.Statements[2].(*ast.ReturnStatement)
	if last.ReturnValue != nil {
		t.Errorf("bare return should have no value, got %v", last.ReturnValue)
	}
}

func TestFunctionLiteral(t *testing.T) {
	program := parse(t, "fn(x, y) { x + y; }")
	stmt := program.Statements[0].(*ast.ExpressionStatement)
	fn := stmt.Expression.(*ast.FunctionLiteral)
	if len(fn.Parameters) != 2 {
		t.Fatalf("got %d parameters, want 2", len(fn.Parameters))
	}
	if fn.Parameters[0].Value != "x" || fn.Parameters[1].Value != "y" {
		t.Errorf("parameters = %v", fn.Parameters)
	}
}

func TestFunctionParameterVariants(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int
	}{
		{"fn() {}", 0},
		{"fn(x) {}", 1},
		{"fn(x, y, z) {}", 3},
	} {
		program := parse(t, tt.in)
		fn := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.FunctionLiteral)
		if len(fn.Parameters) != tt.want {
			t.Errorf("%s: got %d parameters, want %d", tt.in, len(fn.Parameters), tt.want)
		}
	}
}

func TestIfExpression(t *testing.T) {
	program := parse(t, "if (x < y) { x } else { y }")
	expr := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.IfExpression)
	if expr.Alternative == nil {
		t.Fatal("expected an else branch")
	}

	program = parse(t, "if (x < y) { x }")
	expr = program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.IfExpression)
	if expr.Alternative != nil {
		t.Error("expected no else branch")
	}
}

func TestWhileStatement(t *testing.T) {
	program := parse(t, "while (i < 10) { i = i + 1; }")
	ws, ok := program.Statements[0].(*ast.WhileStatement)
	if !ok {
		t.Fatalf("got %T, want *ast.WhileStatement", program.Statements[0])
	}
	if ws.Condition.String() != "(i < 10)" {
		t.Errorf("condition = %q", ws.Condition.String())
	}
	if len(ws.Body.Statements) != 1 {
		t.Errorf("body has %d statements, want 1", len(ws.Body.Statements))
	}
}

func TestHashLiteral(t *testing.T) {
	program := parse(t, `{"one": 1, "two": 2}`)
	hash := program.Statements[0].(*ast.ExpressionStatement).Expression.(*ast.HashLiteral)
	if len(hash.Pairs) != 2 {
		t.Fatalf("got %d pairs, want 2", len(hash.Pairs))
	}
	// Source order must be preserved so String() is deterministic.
	if got := hash.String(); got != `{"one":1, "two":2}` {
		t.Errorf("got %q", got)
	}
}

func TestEmptyHashAndArray(t *testing.T) {
	if got := parse(t, "{}").String(); got != "{}" {
		t.Errorf("empty hash = %q", got)
	}
	if got := parse(t, "[]").String(); got != "[]" {
		t.Errorf("empty array = %q", got)
	}
}

func TestTrailingCommas(t *testing.T) {
	for _, in := range []string{"[1, 2, 3,]", "add(1, 2,)"} {
		p := New(lexer.New(in))
		p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Errorf("%s should parse: %v", in, errs)
		}
	}
}

// Every one of these is malformed; the parser must report an error rather
// than producing a tree that silently means something else.
func TestParseErrors(t *testing.T) {
	tests := []string{
		"let = 5;",
		"let x 5;",
		"if (x { }",
		"fn(x { }",
		"[1, 2",
		`{"a": }`,
		"foo(",
		"let x = ;",
		"5 = 3",     // cannot assign to a literal
		"if (x) { ", // unclosed brace
		`"unterminated`,
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			p := New(lexer.New(in))
			p.ParseProgram()
			if len(p.Errors()) == 0 {
				t.Errorf("expected a parse error for %q", in)
			}
		})
	}
}

// Errors must carry a position so they can point at the source.
func TestErrorsIncludePosition(t *testing.T) {
	p := New(lexer.New("let x 5;"))
	p.ParseProgram()
	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatal("expected an error")
	}
	if !strings.Contains(errs[0], ":") {
		t.Errorf("error lacks position information: %q", errs[0])
	}
}

// Parsing must not stop at the first error: one run should report all of
// them.
func TestMultipleErrorsReported(t *testing.T) {
	p := New(lexer.New("let = 1; let = 2; let = 3;"))
	p.ParseProgram()
	if n := len(p.Errors()); n < 2 {
		t.Errorf("got %d errors, want several", n)
	}
}
