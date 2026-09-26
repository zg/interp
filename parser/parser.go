// Package parser builds an AST from a token stream using Pratt (top-down
// operator precedence) parsing.
package parser

import (
	"fmt"
	"strconv"

	"github.com/zg/interp/ast"
	"github.com/zg/interp/lexer"
	"github.com/zg/interp/token"
)

// Precedence levels, lowest to highest.
const (
	_ int = iota
	LOWEST
	ASSIGN      // =
	LOGICAL_OR  // ||
	LOGICAL_AND // &&  (binds tighter than ||)
	EQUALS      // == !=
	LESSGREATER // > < >= <=
	SUM         // + -
	PRODUCT     // * / %
	PREFIX      // -x !x
	CALL        // f(x)
	INDEX       // a[i]
)

var precedences = map[token.Type]int{
	token.ASSIGN:   ASSIGN,
	token.OR:       LOGICAL_OR,
	token.AND:      LOGICAL_AND,
	token.EQ:       EQUALS,
	token.NOT_EQ:   EQUALS,
	token.LT:       LESSGREATER,
	token.GT:       LESSGREATER,
	token.LTE:      LESSGREATER,
	token.GTE:      LESSGREATER,
	token.PLUS:     SUM,
	token.MINUS:    SUM,
	token.SLASH:    PRODUCT,
	token.ASTERISK: PRODUCT,
	token.PERCENT:  PRODUCT,
	token.LPAREN:   CALL,
	token.LBRACKET: INDEX,
}

type (
	prefixParseFn func() ast.Expression
	infixParseFn  func(ast.Expression) ast.Expression
)

// Parser turns tokens into an AST, accumulating errors rather than stopping
// at the first one so a single run can report everything wrong with a file.
type Parser struct {
	l      *lexer.Lexer
	errors []string

	cur  token.Token
	peek token.Token

	prefixParseFns map[token.Type]prefixParseFn
	infixParseFns  map[token.Type]infixParseFn
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{l: l}

	p.prefixParseFns = map[token.Type]prefixParseFn{
		token.IDENT:    p.parseIdentifier,
		token.INT:      p.parseIntegerLiteral,
		token.FLOAT:    p.parseFloatLiteral,
		token.STRING:   p.parseStringLiteral,
		token.BANG:     p.parsePrefixExpression,
		token.MINUS:    p.parsePrefixExpression,
		token.TRUE:     p.parseBoolean,
		token.FALSE:    p.parseBoolean,
		token.NULL:     p.parseNull,
		token.LPAREN:   p.parseGroupedExpression,
		token.IF:       p.parseIfExpression,
		token.FUNCTION: p.parseFunctionLiteral,
		token.LBRACKET: p.parseArrayLiteral,
		token.LBRACE:   p.parseHashLiteral,
	}
	p.infixParseFns = map[token.Type]infixParseFn{
		token.PLUS:     p.parseInfixExpression,
		token.MINUS:    p.parseInfixExpression,
		token.SLASH:    p.parseInfixExpression,
		token.ASTERISK: p.parseInfixExpression,
		token.PERCENT:  p.parseInfixExpression,
		token.EQ:       p.parseInfixExpression,
		token.NOT_EQ:   p.parseInfixExpression,
		token.LT:       p.parseInfixExpression,
		token.GT:       p.parseInfixExpression,
		token.LTE:      p.parseInfixExpression,
		token.GTE:      p.parseInfixExpression,
		token.AND:      p.parseInfixExpression,
		token.OR:       p.parseInfixExpression,
		token.LPAREN:   p.parseCallExpression,
		token.LBRACKET: p.parseIndexExpression,
		token.ASSIGN:   p.parseAssignExpression,
	}

	// Prime cur and peek.
	p.nextToken()
	p.nextToken()
	return p
}

// Errors returns every parse error found.
func (p *Parser) Errors() []string { return p.errors }

func (p *Parser) errorf(format string, args ...any) {
	p.errors = append(p.errors, fmt.Sprintf("%d:%d: %s", p.cur.Line, p.cur.Column, fmt.Sprintf(format, args...)))
}

func (p *Parser) nextToken() {
	p.cur = p.peek
	p.peek = p.l.Next()
	// An illegal token is a lexing failure; surface it here rather than
	// letting it masquerade as a syntax error further along.
	if p.peek.Type == token.ILLEGAL {
		p.errors = append(p.errors,
			fmt.Sprintf("%d:%d: illegal token %q", p.peek.Line, p.peek.Column, p.peek.Literal))
	}
}

func (p *Parser) curIs(t token.Type) bool  { return p.cur.Type == t }
func (p *Parser) peekIs(t token.Type) bool { return p.peek.Type == t }

// expectPeek advances when the next token is t, and records an error
// otherwise.
func (p *Parser) expectPeek(t token.Type) bool {
	if p.peekIs(t) {
		p.nextToken()
		return true
	}
	p.errors = append(p.errors, fmt.Sprintf("%d:%d: expected %s, got %s",
		p.peek.Line, p.peek.Column, t, p.peek.Type))
	return false
}

func (p *Parser) peekPrecedence() int {
	if pr, ok := precedences[p.peek.Type]; ok {
		return pr
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if pr, ok := precedences[p.cur.Type]; ok {
		return pr
	}
	return LOWEST
}

// ParseProgram parses the whole input.
func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{Statements: []ast.Statement{}}
	for !p.curIs(token.EOF) {
		if stmt := p.parseStatement(); stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}
	return program
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.cur.Type {
	case token.LET:
		return p.parseLetStatement()
	case token.RETURN:
		return p.parseReturnStatement()
	case token.WHILE:
		return p.parseWhileStatement()
	default:
		return p.parseExpressionStatement()
	}
}

func (p *Parser) parseLetStatement() ast.Statement {
	stmt := &ast.LetStatement{Token: p.cur}
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	stmt.Name = &ast.Identifier{Token: p.cur, Value: p.cur.Literal}
	if !p.expectPeek(token.ASSIGN) {
		return nil
	}
	p.nextToken()
	stmt.Value = p.parseExpression(LOWEST)

	// Naming the function after the variable it is bound to gives useful
	// names in error messages and recursion.
	if fn, ok := stmt.Value.(*ast.FunctionLiteral); ok {
		fn.Name = stmt.Name.Value
	}
	if p.peekIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseReturnStatement() ast.Statement {
	stmt := &ast.ReturnStatement{Token: p.cur}
	p.nextToken()
	// `return;` is legal and yields null.
	if p.curIs(token.SEMICOLON) {
		return stmt
	}
	stmt.ReturnValue = p.parseExpression(LOWEST)
	if p.peekIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseWhileStatement() ast.Statement {
	stmt := &ast.WhileStatement{Token: p.cur}
	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	p.nextToken()
	stmt.Condition = p.parseExpression(LOWEST)
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseExpressionStatement() ast.Statement {
	stmt := &ast.ExpressionStatement{Token: p.cur}
	stmt.Expression = p.parseExpression(LOWEST)
	if p.peekIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseBlockStatement() *ast.BlockStatement {
	block := &ast.BlockStatement{Token: p.cur, Statements: []ast.Statement{}}
	p.nextToken()
	for !p.curIs(token.RBRACE) && !p.curIs(token.EOF) {
		if stmt := p.parseStatement(); stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}
	// Running to EOF means the brace was never closed.
	if p.curIs(token.EOF) {
		p.errorf("unexpected end of input: unclosed '{'")
	}
	return block
}

// parseExpression is the Pratt loop: parse a prefix, then keep folding in
// infix operators while they bind more tightly than the caller's level.
func (p *Parser) parseExpression(precedence int) ast.Expression {
	prefix := p.prefixParseFns[p.cur.Type]
	if prefix == nil {
		p.errorf("no prefix parse function for %s", p.cur.Type)
		return nil
	}
	left := prefix()

	for !p.peekIs(token.SEMICOLON) && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peek.Type]
		if infix == nil {
			return left
		}
		p.nextToken()
		left = infix(left)
	}
	return left
}

func (p *Parser) parseIdentifier() ast.Expression {
	return &ast.Identifier{Token: p.cur, Value: p.cur.Literal}
}

func (p *Parser) parseIntegerLiteral() ast.Expression {
	lit := &ast.IntegerLiteral{Token: p.cur}
	v, err := strconv.ParseInt(p.cur.Literal, 0, 64)
	if err != nil {
		p.errorf("could not parse %q as integer", p.cur.Literal)
		return nil
	}
	lit.Value = v
	return lit
}

func (p *Parser) parseFloatLiteral() ast.Expression {
	lit := &ast.FloatLiteral{Token: p.cur}
	v, err := strconv.ParseFloat(p.cur.Literal, 64)
	if err != nil {
		p.errorf("could not parse %q as float", p.cur.Literal)
		return nil
	}
	lit.Value = v
	return lit
}

func (p *Parser) parseStringLiteral() ast.Expression {
	return &ast.StringLiteral{Token: p.cur, Value: p.cur.Literal}
}

func (p *Parser) parseBoolean() ast.Expression {
	return &ast.Boolean{Token: p.cur, Value: p.curIs(token.TRUE)}
}

func (p *Parser) parseNull() ast.Expression {
	return &ast.Null{Token: p.cur}
}

func (p *Parser) parsePrefixExpression() ast.Expression {
	expr := &ast.PrefixExpression{Token: p.cur, Operator: p.cur.Literal}
	p.nextToken()
	expr.Right = p.parseExpression(PREFIX)
	return expr
}

func (p *Parser) parseInfixExpression(left ast.Expression) ast.Expression {
	expr := &ast.InfixExpression{Token: p.cur, Operator: p.cur.Literal, Left: left}
	precedence := p.curPrecedence()
	p.nextToken()
	expr.Right = p.parseExpression(precedence)
	return expr
}

// parseAssignExpression parses `target = value`. Assignment is
// right-associative, so `a = b = c` parses as `a = (b = c)`; that is why the
// right side is parsed at ASSIGN-1 rather than at ASSIGN.
func (p *Parser) parseAssignExpression(left ast.Expression) ast.Expression {
	switch left.(type) {
	case *ast.Identifier, *ast.IndexExpression:
	default:
		p.errorf("cannot assign to %s", left.String())
		return nil
	}
	expr := &ast.AssignExpression{Token: p.cur, Target: left}
	p.nextToken()
	expr.Value = p.parseExpression(ASSIGN - 1)
	return expr
}

func (p *Parser) parseGroupedExpression() ast.Expression {
	p.nextToken()
	expr := p.parseExpression(LOWEST)
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	return expr
}

func (p *Parser) parseIfExpression() ast.Expression {
	expr := &ast.IfExpression{Token: p.cur}
	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	p.nextToken()
	expr.Condition = p.parseExpression(LOWEST)
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	expr.Consequence = p.parseBlockStatement()

	if p.peekIs(token.ELSE) {
		p.nextToken()
		if !p.expectPeek(token.LBRACE) {
			return nil
		}
		expr.Alternative = p.parseBlockStatement()
	}
	return expr
}

func (p *Parser) parseFunctionLiteral() ast.Expression {
	lit := &ast.FunctionLiteral{Token: p.cur}
	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	lit.Parameters = p.parseFunctionParameters()
	if lit.Parameters == nil {
		return nil
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	lit.Body = p.parseBlockStatement()
	return lit
}

func (p *Parser) parseFunctionParameters() []*ast.Identifier {
	params := []*ast.Identifier{}
	if p.peekIs(token.RPAREN) {
		p.nextToken()
		return params
	}
	p.nextToken()
	if !p.curIs(token.IDENT) {
		p.errorf("expected parameter name, got %s", p.cur.Type)
		return nil
	}
	params = append(params, &ast.Identifier{Token: p.cur, Value: p.cur.Literal})

	for p.peekIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		if !p.curIs(token.IDENT) {
			p.errorf("expected parameter name, got %s", p.cur.Type)
			return nil
		}
		params = append(params, &ast.Identifier{Token: p.cur, Value: p.cur.Literal})
	}
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	return params
}

func (p *Parser) parseCallExpression(fn ast.Expression) ast.Expression {
	expr := &ast.CallExpression{Token: p.cur, Function: fn}
	expr.Arguments = p.parseExpressionList(token.RPAREN)
	return expr
}

func (p *Parser) parseArrayLiteral() ast.Expression {
	arr := &ast.ArrayLiteral{Token: p.cur}
	arr.Elements = p.parseExpressionList(token.RBRACKET)
	return arr
}

func (p *Parser) parseIndexExpression(left ast.Expression) ast.Expression {
	expr := &ast.IndexExpression{Token: p.cur, Left: left}
	p.nextToken()
	expr.Index = p.parseExpression(LOWEST)
	if !p.expectPeek(token.RBRACKET) {
		return nil
	}
	return expr
}

// parseExpressionList parses a comma-separated list terminated by end. A
// trailing comma is accepted, which keeps multi-line literals easy to edit.
func (p *Parser) parseExpressionList(end token.Type) []ast.Expression {
	list := []ast.Expression{}
	if p.peekIs(end) {
		p.nextToken()
		return list
	}
	p.nextToken()
	list = append(list, p.parseExpression(LOWEST))

	for p.peekIs(token.COMMA) {
		p.nextToken()
		if p.peekIs(end) { // trailing comma
			p.nextToken()
			return list
		}
		p.nextToken()
		list = append(list, p.parseExpression(LOWEST))
	}
	if !p.expectPeek(end) {
		return nil
	}
	return list
}

func (p *Parser) parseHashLiteral() ast.Expression {
	hash := &ast.HashLiteral{Token: p.cur, Pairs: map[ast.Expression]ast.Expression{}}
	for !p.peekIs(token.RBRACE) {
		p.nextToken()
		key := p.parseExpression(LOWEST)
		if !p.expectPeek(token.COLON) {
			return nil
		}
		p.nextToken()
		hash.Pairs[key] = p.parseExpression(LOWEST)
		hash.Order = append(hash.Order, key)

		if !p.peekIs(token.RBRACE) && !p.expectPeek(token.COMMA) {
			return nil
		}
	}
	if !p.expectPeek(token.RBRACE) {
		return nil
	}
	return hash
}
