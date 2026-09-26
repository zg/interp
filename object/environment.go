package object

// Environment maps names to values. Each function call creates one enclosed
// by the function's defining environment, which is what makes closures work.
type Environment struct {
	store map[string]Object
	outer *Environment
}

func NewEnvironment() *Environment {
	return &Environment{store: map[string]Object{}}
}

// NewEnclosedEnvironment returns an environment whose lookups fall back to
// outer.
func NewEnclosedEnvironment(outer *Environment) *Environment {
	return &Environment{store: map[string]Object{}, outer: outer}
}

// Get looks up name, walking out through enclosing scopes.
func (e *Environment) Get(name string) (Object, bool) {
	obj, ok := e.store[name]
	if !ok && e.outer != nil {
		return e.outer.Get(name)
	}
	return obj, ok
}

// Set binds name in this scope.
func (e *Environment) Set(name string, val Object) Object {
	e.store[name] = val
	return val
}

// Assign rebinds an existing name in whichever scope defined it, reporting
// false when the name is unbound. This is what lets a while loop mutate a
// variable declared outside it, instead of shadowing it in the loop body.
func (e *Environment) Assign(name string, val Object) bool {
	if _, ok := e.store[name]; ok {
		e.store[name] = val
		return true
	}
	if e.outer != nil {
		return e.outer.Assign(name, val)
	}
	return false
}

// Names returns the names bound in this scope only.
func (e *Environment) Names() []string {
	out := make([]string, 0, len(e.store))
	for k := range e.store {
		out = append(out, k)
	}
	return out
}
