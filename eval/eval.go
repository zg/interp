// Package eval is a tree-walking evaluator for the language.
package eval

import (
	"context"
	"fmt"
	"math"

	"github.com/zg/interp/ast"
	"github.com/zg/interp/object"
)

// Singletons: there is only ever one true, one false and one null, so
// identity comparison works and every boolean does not allocate.
var (
	NULL  = &object.Null{}
	TRUE  = &object.Boolean{Value: true}
	FALSE = &object.Boolean{Value: false}
)

// maxCallDepth bounds recursion. Without it, a runaway recursive function
// takes down the host process with a stack overflow that cannot be
// recovered; with it, the program gets a catchable runtime error.
const maxCallDepth = 2048

// Interpreter holds evaluation state.
type Interpreter struct {
	ctx   context.Context
	depth int
	steps int64
	// maxSteps bounds total work so an infinite loop in user code
	// terminates. Zero means unlimited.
	maxSteps int64
}

// Option configures an Interpreter.
type Option func(*Interpreter)

// WithMaxSteps bounds the number of evaluation steps.
func WithMaxSteps(n int64) Option { return func(i *Interpreter) { i.maxSteps = n } }

// New returns an interpreter. The context allows a long-running program to
// be cancelled from outside.
func New(ctx context.Context, opts ...Option) *Interpreter {
	if ctx == nil {
		ctx = context.Background()
	}
	i := &Interpreter{ctx: ctx}
	for _, o := range opts {
		o(i)
	}
	return i
}

// Eval evaluates node in env.
func (i *Interpreter) Eval(node ast.Node, env *object.Environment) object.Object {
	// Check cancellation and the step budget on every node rather than
	// only at loop boundaries: a deeply recursive expression must be
	// interruptible too.
	i.steps++
	if i.maxSteps > 0 && i.steps > i.maxSteps {
		return newError(0, 0, "evaluation step limit exceeded (%d)", i.maxSteps)
	}
	select {
	case <-i.ctx.Done():
		return newError(0, 0, "evaluation cancelled: %v", i.ctx.Err())
	default:
	}

	switch node := node.(type) {
	case *ast.Program:
		return i.evalProgram(node, env)
	case *ast.ExpressionStatement:
		return i.Eval(node.Expression, env)
	case *ast.BlockStatement:
		return i.evalBlockStatement(node, env)
	case *ast.ReturnStatement:
		if node.ReturnValue == nil {
			return &object.ReturnValue{Value: NULL}
		}
		val := i.Eval(node.ReturnValue, env)
		if isError(val) {
			return val
		}
		return &object.ReturnValue{Value: val}
	case *ast.LetStatement:
		val := i.Eval(node.Value, env)
		if isError(val) {
			return val
		}
		env.Set(node.Name.Value, val)
		return NULL
	case *ast.WhileStatement:
		return i.evalWhile(node, env)

	case *ast.IntegerLiteral:
		return &object.Integer{Value: node.Value}
	case *ast.FloatLiteral:
		return &object.Float{Value: node.Value}
	case *ast.StringLiteral:
		return &object.String{Value: node.Value}
	case *ast.Boolean:
		return nativeBool(node.Value)
	case *ast.Null:
		return NULL

	case *ast.PrefixExpression:
		right := i.Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return evalPrefix(node.Operator, right, node.Token.Line, node.Token.Column)
	case *ast.AssignExpression:
		return i.evalAssign(node, env)
	case *ast.InfixExpression:
		return i.evalInfixExpression(node, env)
	case *ast.IfExpression:
		return i.evalIf(node, env)
	case *ast.Identifier:
		return i.evalIdentifier(node, env)
	case *ast.FunctionLiteral:
		return &object.Function{
			Name: node.Name, Parameters: node.Parameters,
			Body: node.Body, Env: env,
		}
	case *ast.CallExpression:
		return i.evalCall(node, env)
	case *ast.ArrayLiteral:
		els := i.evalExpressions(node.Elements, env)
		if len(els) == 1 && isError(els[0]) {
			return els[0]
		}
		return &object.Array{Elements: els}
	case *ast.IndexExpression:
		return i.evalIndexExpression(node, env)
	case *ast.HashLiteral:
		return i.evalHashLiteral(node, env)
	}
	return newError(0, 0, "unknown AST node %T", node)
}

func (i *Interpreter) evalProgram(program *ast.Program, env *object.Environment) object.Object {
	var result object.Object = NULL
	for _, stmt := range program.Statements {
		result = i.Eval(stmt, env)
		switch result := result.(type) {
		case *object.ReturnValue:
			// A top-level return unwraps: the program's value is the
			// returned value, not the wrapper.
			return result.Value
		case *object.Error:
			return result
		}
	}
	return result
}

func (i *Interpreter) evalBlockStatement(block *ast.BlockStatement, env *object.Environment) object.Object {
	var result object.Object = NULL
	for _, stmt := range block.Statements {
		result = i.Eval(stmt, env)
		if result != nil {
			// Return values stay wrapped here so they keep bubbling up
			// through nested blocks until a function call unwraps them.
			if t := result.Type(); t == object.RETURN_VALUE || t == object.ERROR {
				return result
			}
		}
	}
	return result
}

func (i *Interpreter) evalWhile(node *ast.WhileStatement, env *object.Environment) object.Object {
	for {
		cond := i.Eval(node.Condition, env)
		if isError(cond) {
			return cond
		}
		if !isTruthy(cond) {
			return NULL
		}
		// The body shares the enclosing environment so assignments inside
		// the loop are visible to the condition; a fresh scope per
		// iteration would make every loop infinite.
		result := i.Eval(node.Body, env)
		if result != nil {
			if t := result.Type(); t == object.RETURN_VALUE || t == object.ERROR {
				return result
			}
		}
	}
}

func (i *Interpreter) evalIf(ie *ast.IfExpression, env *object.Environment) object.Object {
	cond := i.Eval(ie.Condition, env)
	if isError(cond) {
		return cond
	}
	switch {
	case isTruthy(cond):
		return i.Eval(ie.Consequence, env)
	case ie.Alternative != nil:
		return i.Eval(ie.Alternative, env)
	default:
		return NULL
	}
}

func (i *Interpreter) evalIdentifier(node *ast.Identifier, env *object.Environment) object.Object {
	if val, ok := env.Get(node.Value); ok {
		return val
	}
	if b, ok := builtins[node.Value]; ok {
		return b
	}
	return newError(node.Token.Line, node.Token.Column, "identifier not found: %s", node.Value)
}

// evalInfixExpression handles assignment and short-circuiting operators
// before falling back to ordinary binary evaluation.
func (i *Interpreter) evalInfixExpression(node *ast.InfixExpression, env *object.Environment) object.Object {
	// && and || must not evaluate their right operand when the left
	// already decides the result.
	if node.Operator == "&&" || node.Operator == "||" {
		left := i.Eval(node.Left, env)
		if isError(left) {
			return left
		}
		if node.Operator == "&&" && !isTruthy(left) {
			return FALSE
		}
		if node.Operator == "||" && isTruthy(left) {
			return TRUE
		}
		right := i.Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return nativeBool(isTruthy(right))
	}

	left := i.Eval(node.Left, env)
	if isError(left) {
		return left
	}
	right := i.Eval(node.Right, env)
	if isError(right) {
		return right
	}
	return evalInfix(node.Operator, left, right, node.Token.Line, node.Token.Column)
}

// evalAssign rebinds an existing variable or mutates an array or hash
// element. Assigning to an unbound name is an error rather than an implicit
// declaration: `let` exists for that, and silently creating globals from a
// typo is a classic source of bugs.
func (i *Interpreter) evalAssign(node *ast.AssignExpression, env *object.Environment) object.Object {
	val := i.Eval(node.Value, env)
	if isError(val) {
		return val
	}

	switch target := node.Target.(type) {
	case *ast.Identifier:
		if !env.Assign(target.Value, val) {
			return newError(node.Token.Line, node.Token.Column,
				"cannot assign to undeclared variable: %s (use let)", target.Value)
		}
		return val

	case *ast.IndexExpression:
		left := i.Eval(target.Left, env)
		if isError(left) {
			return left
		}
		index := i.Eval(target.Index, env)
		if isError(index) {
			return index
		}
		switch container := left.(type) {
		case *object.Array:
			idx, ok := index.(*object.Integer)
			if !ok {
				return newError(node.Token.Line, node.Token.Column,
					"array index must be INTEGER, got %s", index.Type())
			}
			if idx.Value < 0 || idx.Value >= int64(len(container.Elements)) {
				return newError(node.Token.Line, node.Token.Column,
					"index out of range: %d (length %d)", idx.Value, len(container.Elements))
			}
			container.Elements[idx.Value] = val
			return val
		case *object.Hash:
			key, ok := index.(object.Hashable)
			if !ok {
				return newError(node.Token.Line, node.Token.Column, "unusable as hash key: %s", index.Type())
			}
			container.Set(key.HashKey(), object.HashPair{Key: index, Value: val})
			return val
		default:
			return newError(node.Token.Line, node.Token.Column,
				"cannot index-assign into %s", left.Type())
		}
	}
	return newError(node.Token.Line, node.Token.Column, "invalid assignment target")
}

func (i *Interpreter) evalExpressions(exps []ast.Expression, env *object.Environment) []object.Object {
	out := make([]object.Object, 0, len(exps))
	for _, e := range exps {
		ev := i.Eval(e, env)
		if isError(ev) {
			return []object.Object{ev}
		}
		out = append(out, ev)
	}
	return out
}

func (i *Interpreter) evalCall(node *ast.CallExpression, env *object.Environment) object.Object {
	fn := i.Eval(node.Function, env)
	if isError(fn) {
		return fn
	}
	args := i.evalExpressions(node.Arguments, env)
	if len(args) == 1 && isError(args[0]) {
		return args[0]
	}
	return i.apply(fn, args, node.Token.Line, node.Token.Column)
}

func (i *Interpreter) apply(fn object.Object, args []object.Object, line, col int) object.Object {
	switch fn := fn.(type) {
	case *object.Function:
		if len(args) != len(fn.Parameters) {
			return newError(line, col, "wrong number of arguments for %s: want %d, got %d",
				fnName(fn), len(fn.Parameters), len(args))
		}
		i.depth++
		defer func() { i.depth-- }()
		if i.depth > maxCallDepth {
			return newError(line, col, "maximum call depth exceeded (%d)", maxCallDepth)
		}

		// The new scope encloses the function's *defining* environment,
		// not the caller's: that is lexical scoping, and it is what makes
		// closures capture the right variables.
		inner := object.NewEnclosedEnvironment(fn.Env)
		for idx, p := range fn.Parameters {
			inner.Set(p.Value, args[idx])
		}
		result := i.Eval(fn.Body, inner)
		// Unwrap here: the return has reached its function boundary.
		if rv, ok := result.(*object.ReturnValue); ok {
			return rv.Value
		}
		return result

	case *object.Builtin:
		result := fn.Fn(args...)
		if result == nil {
			return NULL
		}
		// Builtins report errors without position; fill it in.
		if e, ok := result.(*object.Error); ok && e.Line == 0 {
			e.Line, e.Column = line, col
		}
		return result

	default:
		return newError(line, col, "not a function: %s", fn.Type())
	}
}

func fnName(f *object.Function) string {
	if f.Name == "" {
		return "anonymous function"
	}
	return f.Name
}

func (i *Interpreter) evalIndexExpression(node *ast.IndexExpression, env *object.Environment) object.Object {
	left := i.Eval(node.Left, env)
	if isError(left) {
		return left
	}
	index := i.Eval(node.Index, env)
	if isError(index) {
		return index
	}
	return evalIndex(left, index, node.Token.Line, node.Token.Column)
}

func evalIndex(left, index object.Object, line, col int) object.Object {
	switch {
	case left.Type() == object.ARRAY && index.Type() == object.INTEGER:
		arr := left.(*object.Array)
		idx := index.(*object.Integer).Value
		// Out of range yields null rather than an error, so idiomatic
		// "walk until null" loops work.
		if idx < 0 || idx >= int64(len(arr.Elements)) {
			return NULL
		}
		return arr.Elements[idx]

	case left.Type() == object.STRING && index.Type() == object.INTEGER:
		// Index by rune, not byte, so multi-byte characters are not split.
		runes := []rune(left.(*object.String).Value)
		idx := index.(*object.Integer).Value
		if idx < 0 || idx >= int64(len(runes)) {
			return NULL
		}
		return &object.String{Value: string(runes[idx])}

	case left.Type() == object.HASH:
		hash := left.(*object.Hash)
		key, ok := index.(object.Hashable)
		if !ok {
			return newError(line, col, "unusable as hash key: %s", index.Type())
		}
		pair, ok := hash.Pairs[key.HashKey()]
		if !ok {
			return NULL
		}
		return pair.Value

	default:
		return newError(line, col, "index operator not supported: %s[%s]", left.Type(), index.Type())
	}
}

func (i *Interpreter) evalHashLiteral(node *ast.HashLiteral, env *object.Environment) object.Object {
	hash := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
	for _, keyNode := range node.Order {
		key := i.Eval(keyNode, env)
		if isError(key) {
			return key
		}
		hashKey, ok := key.(object.Hashable)
		if !ok {
			return newError(node.Token.Line, node.Token.Column, "unusable as hash key: %s", key.Type())
		}
		value := i.Eval(node.Pairs[keyNode], env)
		if isError(value) {
			return value
		}
		hash.Set(hashKey.HashKey(), object.HashPair{Key: key, Value: value})
	}
	return hash
}

func evalPrefix(op string, right object.Object, line, col int) object.Object {
	switch op {
	case "!":
		return nativeBool(!isTruthy(right))
	case "-":
		switch r := right.(type) {
		case *object.Integer:
			return &object.Integer{Value: -r.Value}
		case *object.Float:
			return &object.Float{Value: -r.Value}
		default:
			return newError(line, col, "unknown operator: -%s", right.Type())
		}
	default:
		return newError(line, col, "unknown operator: %s%s", op, right.Type())
	}
}

func evalInfix(op string, left, right object.Object, line, col int) object.Object {
	switch {
	case left.Type() == object.INTEGER && right.Type() == object.INTEGER:
		return evalIntegerInfix(op, left.(*object.Integer), right.(*object.Integer), line, col)

	// Mixed int/float arithmetic promotes to float, as most languages do.
	case isNumeric(left) && isNumeric(right):
		return evalFloatInfix(op, toFloat(left), toFloat(right), line, col)

	case left.Type() == object.STRING && right.Type() == object.STRING:
		return evalStringInfix(op, left.(*object.String), right.(*object.String), line, col)

	// Booleans and null compare by identity thanks to the singletons.
	case op == "==":
		return nativeBool(left == right)
	case op == "!=":
		return nativeBool(left != right)

	case left.Type() != right.Type():
		return newError(line, col, "type mismatch: %s %s %s", left.Type(), op, right.Type())
	default:
		return newError(line, col, "unknown operator: %s %s %s", left.Type(), op, right.Type())
	}
}

func evalIntegerInfix(op string, l, r *object.Integer, line, col int) object.Object {
	switch op {
	case "+":
		return &object.Integer{Value: l.Value + r.Value}
	case "-":
		return &object.Integer{Value: l.Value - r.Value}
	case "*":
		return &object.Integer{Value: l.Value * r.Value}
	case "/":
		// Integer division by zero panics in Go; return a catchable
		// runtime error instead of crashing the host.
		if r.Value == 0 {
			return newError(line, col, "division by zero")
		}
		return &object.Integer{Value: l.Value / r.Value}
	case "%":
		if r.Value == 0 {
			return newError(line, col, "modulo by zero")
		}
		return &object.Integer{Value: l.Value % r.Value}
	case "<":
		return nativeBool(l.Value < r.Value)
	case ">":
		return nativeBool(l.Value > r.Value)
	case "<=":
		return nativeBool(l.Value <= r.Value)
	case ">=":
		return nativeBool(l.Value >= r.Value)
	case "==":
		return nativeBool(l.Value == r.Value)
	case "!=":
		return nativeBool(l.Value != r.Value)
	default:
		return newError(line, col, "unknown operator: INTEGER %s INTEGER", op)
	}
}

func evalFloatInfix(op string, l, r float64, line, col int) object.Object {
	switch op {
	case "+":
		return &object.Float{Value: l + r}
	case "-":
		return &object.Float{Value: l - r}
	case "*":
		return &object.Float{Value: l * r}
	case "/":
		// Float division by zero is defined (±Inf), unlike the integer
		// case, so it is allowed through.
		return &object.Float{Value: l / r}
	case "%":
		return &object.Float{Value: math.Mod(l, r)}
	case "<":
		return nativeBool(l < r)
	case ">":
		return nativeBool(l > r)
	case "<=":
		return nativeBool(l <= r)
	case ">=":
		return nativeBool(l >= r)
	case "==":
		return nativeBool(l == r)
	case "!=":
		return nativeBool(l != r)
	default:
		return newError(line, col, "unknown operator: FLOAT %s FLOAT", op)
	}
}

func evalStringInfix(op string, l, r *object.String, line, col int) object.Object {
	switch op {
	case "+":
		return &object.String{Value: l.Value + r.Value}
	case "==":
		return nativeBool(l.Value == r.Value)
	case "!=":
		return nativeBool(l.Value != r.Value)
	case "<":
		return nativeBool(l.Value < r.Value)
	case ">":
		return nativeBool(l.Value > r.Value)
	case "<=":
		return nativeBool(l.Value <= r.Value)
	case ">=":
		return nativeBool(l.Value >= r.Value)
	default:
		return newError(line, col, "unknown operator: STRING %s STRING", op)
	}
}

func isNumeric(o object.Object) bool {
	return o.Type() == object.INTEGER || o.Type() == object.FLOAT
}

func toFloat(o object.Object) float64 {
	switch o := o.(type) {
	case *object.Integer:
		return float64(o.Value)
	case *object.Float:
		return o.Value
	}
	return 0
}

func nativeBool(b bool) *object.Boolean {
	if b {
		return TRUE
	}
	return FALSE
}

// isTruthy defines the language's notion of truth: null and false are
// falsey, 0 and "" are truthy. Keeping it in one place stops the rule
// drifting between if, while and the logical operators.
func isTruthy(obj object.Object) bool {
	switch obj {
	case NULL, FALSE:
		return false
	case TRUE:
		return true
	}
	if b, ok := obj.(*object.Boolean); ok {
		return b.Value
	}
	return obj != nil
}

func newError(line, col int, format string, args ...any) *object.Error {
	return &object.Error{Message: fmt.Sprintf(format, args...), Line: line, Column: col}
}

func isError(obj object.Object) bool {
	return obj != nil && obj.Type() == object.ERROR
}
