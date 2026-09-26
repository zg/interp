package object

import "testing"

// Equal strings are distinct pointers but must hash identically, or hash
// lookups by a computed key would fail.
func TestStringHashKey(t *testing.T) {
	hello1, hello2 := &String{Value: "Hello World"}, &String{Value: "Hello World"}
	diff1, diff2 := &String{Value: "My name is johnny"}, &String{Value: "My name is johnny"}

	if hello1.HashKey() != hello2.HashKey() {
		t.Error("strings with the same content must have the same hash key")
	}
	if diff1.HashKey() != diff2.HashKey() {
		t.Error("strings with the same content must have the same hash key")
	}
	if hello1.HashKey() == diff1.HashKey() {
		t.Error("strings with different content must not collide here")
	}
}

func TestIntegerAndBooleanHashKeys(t *testing.T) {
	if (&Integer{Value: 1}).HashKey() != (&Integer{Value: 1}).HashKey() {
		t.Error("equal integers must hash equally")
	}
	if (&Boolean{Value: true}).HashKey() == (&Boolean{Value: false}).HashKey() {
		t.Error("true and false must not hash equally")
	}
	// An integer and a boolean with the same numeric value must not collide.
	if (&Integer{Value: 1}).HashKey() == (&Boolean{Value: true}).HashKey() {
		t.Error("types must be part of the hash key")
	}
}

// A whole-valued float must not print as an integer.
func TestFloatInspect(t *testing.T) {
	tests := []struct {
		val  float64
		want string
	}{
		{2.0, "2.0"},
		{1.5, "1.5"},
		{-0.25, "-0.25"},
	}
	for _, tt := range tests {
		if got := (&Float{Value: tt.val}).Inspect(); got != tt.want {
			t.Errorf("Float(%v).Inspect() = %q, want %q", tt.val, got, tt.want)
		}
	}
}

func TestHashSetPreservesInsertionOrder(t *testing.T) {
	h := &Hash{}
	for _, s := range []string{"c", "a", "b"} {
		k := &String{Value: s}
		h.Set(k.HashKey(), HashPair{Key: k, Value: &Integer{Value: 1}})
	}
	if got, want := h.Inspect(), `{"c": 1, "a": 1, "b": 1}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// Overwriting must not append a duplicate entry.
	k := &String{Value: "a"}
	h.Set(k.HashKey(), HashPair{Key: k, Value: &Integer{Value: 9}})
	if got, want := h.Inspect(), `{"c": 1, "a": 9, "b": 1}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestEnvironmentScoping(t *testing.T) {
	outer := NewEnvironment()
	outer.Set("x", &Integer{Value: 1})

	inner := NewEnclosedEnvironment(outer)
	// Lookups fall through to the enclosing scope.
	if v, ok := inner.Get("x"); !ok || v.(*Integer).Value != 1 {
		t.Error("inner scope should see outer bindings")
	}
	// Set shadows rather than overwriting.
	inner.Set("x", &Integer{Value: 2})
	if v, _ := outer.Get("x"); v.(*Integer).Value != 1 {
		t.Error("Set in an inner scope must not change the outer binding")
	}
}

// Assign must find the scope that declared the name, so a loop body can
// mutate a variable declared outside it.
func TestEnvironmentAssignWalksOutward(t *testing.T) {
	outer := NewEnvironment()
	outer.Set("x", &Integer{Value: 1})
	inner := NewEnclosedEnvironment(outer)

	if !inner.Assign("x", &Integer{Value: 99}) {
		t.Fatal("Assign should find x in the outer scope")
	}
	if v, _ := outer.Get("x"); v.(*Integer).Value != 99 {
		t.Error("Assign should have updated the outer binding")
	}
	if inner.Assign("undeclared", &Integer{Value: 1}) {
		t.Error("Assign must fail for an unbound name")
	}
}
