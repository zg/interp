// Package object defines the runtime values the evaluator produces.
package object

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/zg/interp/ast"
)

type Type string

const (
	INTEGER      Type = "INTEGER"
	FLOAT        Type = "FLOAT"
	BOOLEAN      Type = "BOOLEAN"
	STRING       Type = "STRING"
	NULL         Type = "NULL"
	RETURN_VALUE Type = "RETURN_VALUE"
	ERROR        Type = "ERROR"
	FUNCTION     Type = "FUNCTION"
	BUILTIN      Type = "BUILTIN"
	ARRAY        Type = "ARRAY"
	HASH         Type = "HASH"
)

// Object is any runtime value.
type Object interface {
	Type() Type
	Inspect() string
}

type Integer struct{ Value int64 }

func (i *Integer) Type() Type      { return INTEGER }
func (i *Integer) Inspect() string { return strconv.FormatInt(i.Value, 10) }

type Float struct{ Value float64 }

func (f *Float) Type() Type { return FLOAT }
func (f *Float) Inspect() string {
	// 'g' keeps floats readable without trailing zeros, but a whole number
	// would print as "3" and be indistinguishable from an integer.
	s := strconv.FormatFloat(f.Value, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEn") { // n catches NaN/Inf
		s += ".0"
	}
	return s
}

type Boolean struct{ Value bool }

func (b *Boolean) Type() Type      { return BOOLEAN }
func (b *Boolean) Inspect() string { return strconv.FormatBool(b.Value) }

type String struct{ Value string }

func (s *String) Type() Type      { return STRING }
func (s *String) Inspect() string { return s.Value }

type Null struct{}

func (n *Null) Type() Type      { return NULL }
func (n *Null) Inspect() string { return "null" }

// ReturnValue wraps a value travelling up to the nearest function boundary.
type ReturnValue struct{ Value Object }

func (rv *ReturnValue) Type() Type      { return RETURN_VALUE }
func (rv *ReturnValue) Inspect() string { return rv.Value.Inspect() }

// Error is a runtime error. It is a value rather than a Go error so it
// propagates through evaluation the same way any other object does.
type Error struct {
	Message string
	Line    int
	Column  int
}

func (e *Error) Type() Type { return ERROR }
func (e *Error) Inspect() string {
	if e.Line > 0 {
		return fmt.Sprintf("runtime error at %d:%d: %s", e.Line, e.Column, e.Message)
	}
	return "runtime error: " + e.Message
}

type Function struct {
	Name       string
	Parameters []*ast.Identifier
	Body       *ast.BlockStatement
	Env        *Environment // the closure
}

func (f *Function) Type() Type { return FUNCTION }
func (f *Function) Inspect() string {
	params := make([]string, len(f.Parameters))
	for i, p := range f.Parameters {
		params[i] = p.String()
	}
	name := f.Name
	if name == "" {
		name = "anonymous"
	}
	return "fn " + name + "(" + strings.Join(params, ", ") + ") { ... }"
}

// BuiltinFunction is a function implemented in Go.
type BuiltinFunction func(args ...Object) Object

type Builtin struct {
	Name string
	Fn   BuiltinFunction
}

func (b *Builtin) Type() Type      { return BUILTIN }
func (b *Builtin) Inspect() string { return "builtin function " + b.Name }

type Array struct{ Elements []Object }

func (a *Array) Type() Type { return ARRAY }
func (a *Array) Inspect() string {
	els := make([]string, len(a.Elements))
	for i, e := range a.Elements {
		els[i] = e.Inspect()
	}
	return "[" + strings.Join(els, ", ") + "]"
}

// HashKey is the comparable form of a hashable object. Using an explicit
// key type means two equal strings map to the same bucket even though they
// are distinct *String pointers.
type HashKey struct {
	Type  Type
	Value uint64
}

// Hashable is implemented by objects usable as hash keys.
type Hashable interface{ HashKey() HashKey }

func (b *Boolean) HashKey() HashKey {
	var v uint64
	if b.Value {
		v = 1
	}
	return HashKey{Type: b.Type(), Value: v}
}

func (i *Integer) HashKey() HashKey {
	return HashKey{Type: i.Type(), Value: uint64(i.Value)}
}

func (s *String) HashKey() HashKey {
	h := fnv.New64a()
	h.Write([]byte(s.Value))
	return HashKey{Type: s.Type(), Value: h.Sum64()}
}

type HashPair struct{ Key, Value Object }

type Hash struct {
	Pairs map[HashKey]HashPair
	// Order preserves insertion order so Inspect is deterministic; Go
	// randomises map iteration.
	Order []HashKey
}

func (h *Hash) Type() Type { return HASH }
func (h *Hash) Inspect() string {
	pairs := make([]string, 0, len(h.Order))
	for _, k := range h.Order {
		p := h.Pairs[k]
		key := p.Key.Inspect()
		if p.Key.Type() == STRING {
			key = strconv.Quote(key)
		}
		pairs = append(pairs, key+": "+p.Value.Inspect())
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// Set inserts or replaces a pair, maintaining insertion order.
func (h *Hash) Set(k HashKey, p HashPair) {
	if h.Pairs == nil {
		h.Pairs = map[HashKey]HashPair{}
	}
	if _, exists := h.Pairs[k]; !exists {
		h.Order = append(h.Order, k)
	}
	h.Pairs[k] = p
}
