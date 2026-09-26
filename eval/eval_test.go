package eval

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zg/interp/lexer"
	"github.com/zg/interp/object"
	"github.com/zg/interp/parser"
)

func run(t *testing.T, input string) object.Object {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors for %q:\n%s", input, strings.Join(errs, "\n"))
	}
	return New(context.Background(), WithMaxSteps(5_000_000)).Eval(program, object.NewEnvironment())
}

func wantInt(t *testing.T, obj object.Object, want int64) {
	t.Helper()
	i, ok := obj.(*object.Integer)
	if !ok {
		t.Fatalf("got %T (%s), want *object.Integer", obj, obj.Inspect())
	}
	if i.Value != want {
		t.Errorf("got %d, want %d", i.Value, want)
	}
}

func TestIntegerArithmetic(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"5", 5}, {"-5", -5}, {"5 + 5", 10}, {"5 - 5", 0},
		{"2 * 3", 6}, {"10 / 2", 5}, {"7 % 3", 1},
		{"2 * (5 + 10)", 30}, {"3 * 3 * 3 + 10", 37},
		{"(5 + 10 * 2 + 15 / 3) * 2 + -10", 50},
		{"-50 + 100 + -50", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) { wantInt(t, run(t, tt.in), tt.want) })
	}
}

func TestFloatArithmetic(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"1.5 + 1.5", 3.0},
		{"3.0 / 2.0", 1.5},
		{"1 + 1.5", 2.5}, // int promotes to float
		{"3 / 2.0", 1.5}, // mixed division is float division
		{"-1.5", -1.5},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			f, ok := run(t, tt.in).(*object.Float)
			if !ok {
				t.Fatalf("got %T", run(t, tt.in))
			}
			if f.Value != tt.want {
				t.Errorf("got %v, want %v", f.Value, tt.want)
			}
		})
	}
}

// Integer division truncates, but mixed-type division must not — this is
// the boundary where a naive implementation silently loses precision.
func TestIntegerVsFloatDivision(t *testing.T) {
	wantInt(t, run(t, "7 / 2"), 3)
	if f := run(t, "7 / 2.0").(*object.Float); f.Value != 3.5 {
		t.Errorf("7 / 2.0 = %v, want 3.5", f.Value)
	}
}

func TestBooleanExpressions(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"true", true}, {"false", false},
		{"1 < 2", true}, {"1 > 2", false}, {"1 <= 1", true}, {"2 >= 3", false},
		{"1 == 1", true}, {"1 != 1", false},
		{"true == true", true}, {"true != false", true},
		{"(1 < 2) == true", true},
		{"!true", false}, {"!false", true}, {"!!true", true},
		{"!null", true},
		{`"a" == "a"`, true}, {`"a" < "b"`, true},
		{"true && false", false}, {"true && true", true},
		{"false || true", true}, {"false || false", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			b, ok := run(t, tt.in).(*object.Boolean)
			if !ok {
				t.Fatalf("got %T", run(t, tt.in))
			}
			if b.Value != tt.want {
				t.Errorf("got %v, want %v", b.Value, tt.want)
			}
		})
	}
}

// Short-circuiting is observable: the right operand must not run when the
// left already decides the answer. If it did, these would error.
func TestLogicalShortCircuit(t *testing.T) {
	if got := run(t, "false && undefined_variable"); isError(got) {
		t.Errorf("&& should not evaluate its right operand: %s", got.Inspect())
	}
	if got := run(t, "true || undefined_variable"); isError(got) {
		t.Errorf("|| should not evaluate its right operand: %s", got.Inspect())
	}
	// ...but it must when the left does not decide it.
	if got := run(t, "true && undefined_variable"); !isError(got) {
		t.Error("&& should evaluate its right operand when the left is true")
	}
}

func TestIfElse(t *testing.T) {
	tests := []struct {
		in   string
		want any
	}{
		{"if (true) { 10 }", int64(10)},
		{"if (false) { 10 }", nil},
		{"if (1 < 2) { 10 }", int64(10)},
		{"if (1 > 2) { 10 } else { 20 }", int64(20)},
		// 0 and "" are truthy in this language; only null and false are not.
		{"if (0) { 10 } else { 20 }", int64(10)},
		{`if ("") { 10 } else { 20 }`, int64(10)},
		{"if (null) { 10 } else { 20 }", int64(20)},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := run(t, tt.in)
			if tt.want == nil {
				if got != NULL {
					t.Errorf("got %s, want null", got.Inspect())
				}
				return
			}
			wantInt(t, got, tt.want.(int64))
		})
	}
}

func TestReturnStatements(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"return 10;", 10},
		{"return 10; 9;", 10},
		{"return 2 * 5; 9;", 10},
		{"9; return 2 * 5; 9;", 10},
		// A return inside nested blocks must escape all of them.
		{"if (10 > 1) { if (10 > 1) { return 10; } return 1; }", 10},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) { wantInt(t, run(t, tt.in), tt.want) })
	}
}

func TestLetAndAssignment(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"let a = 5; a;", 5},
		{"let a = 5 * 5; a;", 25},
		{"let a = 5; let b = a; b;", 5},
		{"let a = 5; let b = a; let c = a + b + 5; c;", 15},
		{"let a = 1; a = 2; a;", 2},
		{"let a = 1; a = a + 10; a;", 11},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) { wantInt(t, run(t, tt.in), tt.want) })
	}
}

// Assigning to a name that was never declared is an error, not an implicit
// global — a typo should not silently create a new variable.
func TestAssignToUndeclared(t *testing.T) {
	got := run(t, "x = 5;")
	if !isError(got) {
		t.Fatalf("got %s, want an error", got.Inspect())
	}
	if !strings.Contains(got.Inspect(), "undeclared") {
		t.Errorf("unhelpful error: %s", got.Inspect())
	}
}

func TestWhileLoop(t *testing.T) {
	wantInt(t, run(t, `
		let i = 0;
		let sum = 0;
		while (i < 10) { sum = sum + i; i = i + 1; }
		sum;
	`), 45)
}

// The loop body must share the enclosing scope, or assignments inside it
// would never affect the condition and every loop would be infinite.
func TestWhileBodySharesScope(t *testing.T) {
	wantInt(t, run(t, "let i = 0; while (i < 3) { i = i + 1; } i;"), 3)
}

func TestWhileWithReturn(t *testing.T) {
	wantInt(t, run(t, `
		let f = fn() { let i = 0; while (true) { i = i + 1; if (i == 5) { return i; } } };
		f();
	`), 5)
}

func TestFunctions(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"let identity = fn(x) { x; }; identity(5);", 5},
		{"let identity = fn(x) { return x; }; identity(5);", 5},
		{"let double = fn(x) { x * 2; }; double(5);", 10},
		{"let add = fn(x, y) { x + y; }; add(5, 5);", 10},
		{"let add = fn(x, y) { x + y; }; add(5 + 5, add(5, 5));", 20},
		{"fn(x) { x; }(5)", 5},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) { wantInt(t, run(t, tt.in), tt.want) })
	}
}

// A closure must capture its defining environment, not the caller's.
func TestClosures(t *testing.T) {
	wantInt(t, run(t, `
		let newAdder = fn(x) { fn(y) { x + y }; };
		let addTwo = newAdder(2);
		addTwo(3);
	`), 5)

	// Each closure gets its own captured binding.
	wantInt(t, run(t, `
		let counter = fn() { let n = 0; fn() { n = n + 1; n } };
		let a = counter();
		let b = counter();
		a(); a(); a();
		b();
	`), 1)
}

func TestRecursion(t *testing.T) {
	wantInt(t, run(t, `
		let fact = fn(n) { if (n <= 1) { 1 } else { n * fact(n - 1) } };
		fact(10);
	`), 3628800)

	wantInt(t, run(t, `
		let fib = fn(n) { if (n < 2) { n } else { fib(n - 1) + fib(n - 2) } };
		fib(20);
	`), 6765)
}

// Unbounded recursion must produce a catchable runtime error rather than
// crashing the host process with a stack overflow.
func TestRecursionDepthLimit(t *testing.T) {
	got := run(t, "let f = fn(n) { f(n + 1) }; f(0);")
	if !isError(got) {
		t.Fatalf("got %s, want an error", got.Inspect())
	}
	if !strings.Contains(got.Inspect(), "call depth") {
		t.Errorf("unexpected error: %s", got.Inspect())
	}
}

func TestStrings(t *testing.T) {
	if s := run(t, `"hello" + " " + "world"`).(*object.String); s.Value != "hello world" {
		t.Errorf("got %q", s.Value)
	}
	// Indexing is by rune, so multi-byte characters are not split.
	if s := run(t, `"héllo"[1]`).(*object.String); s.Value != "é" {
		t.Errorf("got %q, want é", s.Value)
	}
	wantInt(t, run(t, `len("héllo")`), 5)
}

func TestArrays(t *testing.T) {
	arr, ok := run(t, "[1, 2 * 2, 3 + 3]").(*object.Array)
	if !ok {
		t.Fatal("not an array")
	}
	if len(arr.Elements) != 3 {
		t.Fatalf("got %d elements", len(arr.Elements))
	}
	wantInt(t, arr.Elements[1], 4)

	wantInt(t, run(t, "[1, 2, 3][0]"), 1)
	wantInt(t, run(t, "let i = 0; [1][i];"), 1)
	wantInt(t, run(t, "let a = [1, 2, 3]; a[0] + a[1] + a[2];"), 6)

	// Out of range yields null, so "walk until null" loops work.
	if got := run(t, "[1, 2, 3][99]"); got != NULL {
		t.Errorf("out of range should be null, got %s", got.Inspect())
	}
	if got := run(t, "[1, 2, 3][-1]"); got != NULL {
		t.Errorf("negative index should be null, got %s", got.Inspect())
	}
}

func TestArrayIndexAssignment(t *testing.T) {
	wantInt(t, run(t, "let a = [1, 2, 3]; a[1] = 42; a[1];"), 42)
	if got := run(t, "let a = [1]; a[5] = 1;"); !isError(got) {
		t.Error("assigning out of range should error")
	}
}

// push and rest must copy, not alias: otherwise mutating one array would
// silently change another.
func TestArrayBuiltinsDoNotAlias(t *testing.T) {
	wantInt(t, run(t, `
		let a = [1, 2, 3];
		let b = push(a, 4);
		len(a);
	`), 3)

	wantInt(t, run(t, `
		let a = [1, 2, 3];
		let b = rest(a);
		b[0] = 99;
		a[1];
	`), 2)
}

func TestHashes(t *testing.T) {
	hash, ok := run(t, `{"one": 1, "two": 2, 3: 3, true: 4}`).(*object.Hash)
	if !ok {
		t.Fatal("not a hash")
	}
	if len(hash.Pairs) != 4 {
		t.Fatalf("got %d pairs, want 4", len(hash.Pairs))
	}
	wantInt(t, run(t, `{"foo": 5}["foo"]`), 5)
	wantInt(t, run(t, `let key = "foo"; {"foo": 5}[key]`), 5)
	wantInt(t, run(t, `{true: 5}[true]`), 5)

	if got := run(t, `{"foo": 5}["bar"]`); got != NULL {
		t.Errorf("missing key should be null, got %s", got.Inspect())
	}
}

// Two equal strings are distinct pointers; they must still hash to the same
// slot.
func TestHashKeyEquality(t *testing.T) {
	wantInt(t, run(t, `
		let h = {"name": 1};
		let k = "na" + "me";
		h[k];
	`), 1)
}

func TestHashIndexAssignment(t *testing.T) {
	wantInt(t, run(t, `let h = {"a": 1}; h["b"] = 2; h["b"];`), 2)
	wantInt(t, run(t, `let h = {"a": 1}; h["a"] = 9; h["a"];`), 9)
}

func TestUnhashableKey(t *testing.T) {
	if got := run(t, `{[1]: "x"}`); !isError(got) {
		t.Error("an array should not be usable as a hash key")
	}
}

func TestBuiltins(t *testing.T) {
	tests := []struct {
		in   string
		want any
	}{
		{`len("")`, int64(0)},
		{`len("four")`, int64(4)},
		{`len([1, 2, 3])`, int64(3)},
		{`len({"a": 1})`, int64(1)},
		{`first([1, 2, 3])`, int64(1)},
		{`last([1, 2, 3])`, int64(3)},
		{`len(rest([1, 2, 3]))`, int64(2)},
		{`len(push([1], 2))`, int64(2)},
		{`len(keys({"a": 1, "b": 2}))`, int64(2)},
		{`len(values({"a": 1, "b": 2}))`, int64(2)},
		{`int("42")`, int64(42)},
		{`int(3.9)`, int64(3)},
		{`len(split("a,b,c", ","))`, int64(3)},
		{`len(split("a b c", ""))`, int64(3)},
		{`type(1)`, "INTEGER"},
		{`type("x")`, "STRING"},
		{`str(42)`, "42"},
		{`join([1,2,3], "-")`, "1-2-3"},
		{`join(sort([3,1,2]), ",")`, "1,2,3"},
		{`join(sort(["c","a","b"]), ",")`, "a,b,c"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := run(t, tt.in)
			switch want := tt.want.(type) {
			case int64:
				wantInt(t, got, want)
			case string:
				s, ok := got.(*object.String)
				if !ok {
					t.Fatalf("got %T (%s)", got, got.Inspect())
				}
				if s.Value != want {
					t.Errorf("got %q, want %q", s.Value, want)
				}
			}
		})
	}
}

func TestBuiltinErrors(t *testing.T) {
	tests := []string{
		`len(1)`,
		`len("a", "b")`,
		`first(1)`,
		`push(1, 2)`,
		`int("abc")`,
		`sort([1, "a"])`,
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			if got := run(t, in); !isError(got) {
				t.Errorf("expected an error, got %s", got.Inspect())
			}
		})
	}
}

func TestPutsWritesToStdout(t *testing.T) {
	var buf bytes.Buffer
	old := Stdout
	Stdout = &buf
	defer func() { Stdout = old }()

	run(t, `puts("hello", 42)`)
	if got := buf.String(); got != "hello 42\n" {
		t.Errorf("got %q, want %q", got, "hello 42\n")
	}
}

func TestRuntimeErrors(t *testing.T) {
	tests := []struct{ in, contains string }{
		{"5 + true;", "type mismatch"},
		{"-true", "unknown operator"},
		{`"a" - "b"`, "unknown operator"},
		{"foobar", "identifier not found"},
		{"1 / 0", "division by zero"},
		{"1 % 0", "modulo by zero"},
		{"let f = fn(x) { x }; f(1, 2);", "wrong number of arguments"},
		{"let n = 1; n();", "not a function"},
		{"1[0]", "index operator not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := run(t, tt.in)
			if !isError(got) {
				t.Fatalf("got %s, want an error", got.Inspect())
			}
			if !strings.Contains(got.Inspect(), tt.contains) {
				t.Errorf("error %q does not mention %q", got.Inspect(), tt.contains)
			}
		})
	}
}

// Float division by zero is defined (±Inf), unlike integer division.
func TestFloatDivisionByZeroIsInfinity(t *testing.T) {
	got := run(t, "1.0 / 0.0")
	if isError(got) {
		t.Fatalf("float division by zero should not error: %s", got.Inspect())
	}
	if !strings.Contains(got.Inspect(), "Inf") {
		t.Errorf("got %s, want infinity", got.Inspect())
	}
}

// An error must stop evaluation and propagate, not be silently discarded by
// an enclosing construct.
func TestErrorsPropagate(t *testing.T) {
	got := run(t, "let f = fn() { 5 + true; }; f(); 99;")
	if !isError(got) {
		t.Fatalf("got %s, want the error to propagate", got.Inspect())
	}
}

func TestErrorsCarryPosition(t *testing.T) {
	got := run(t, "\n\nfoobar")
	err, ok := got.(*object.Error)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if err.Line != 3 {
		t.Errorf("line = %d, want 3", err.Line)
	}
}

// An infinite loop in user code must terminate rather than hanging forever.
func TestStepLimitStopsInfiniteLoop(t *testing.T) {
	p := parser.New(lexer.New("while (true) { }"))
	program := p.ParseProgram()
	got := New(context.Background(), WithMaxSteps(10000)).Eval(program, object.NewEnvironment())
	if !isError(got) {
		t.Fatalf("got %s, want a step-limit error", got.Inspect())
	}
	if !strings.Contains(got.Inspect(), "step limit") {
		t.Errorf("unexpected error: %s", got.Inspect())
	}
}

// A long-running program must be interruptible from outside.
func TestContextCancellation(t *testing.T) {
	p := parser.New(lexer.New("let i = 0; while (true) { i = i + 1; }"))
	program := p.ParseProgram()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan object.Object, 1)
	go func() { done <- New(ctx).Eval(program, object.NewEnvironment()) }()

	select {
	case got := <-done:
		if !isError(got) {
			t.Fatalf("got %s, want a cancellation error", got.Inspect())
		}
		if !strings.Contains(got.Inspect(), "cancelled") {
			t.Errorf("unexpected error: %s", got.Inspect())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation did not stop on cancellation")
	}
}

func TestInspectFormatting(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1", "1"},
		{"1.5", "1.5"},
		{"2.0", "2.0"}, // must not print as "2" and look like an integer
		{"true", "true"},
		{"null", "null"},
		{`"hi"`, "hi"},
		{"[1, 2]", "[1, 2]"},
		{`{"a": 1}`, `{"a": 1}`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := run(t, tt.in).Inspect(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Hash output order must follow insertion, or it would vary run to run
// thanks to Go's randomised map iteration.
func TestHashInspectIsDeterministic(t *testing.T) {
	const src = `{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5}`
	first := run(t, src).Inspect()
	for range 20 {
		if got := run(t, src).Inspect(); got != first {
			t.Fatalf("hash output varies between runs:\n %s\n %s", first, got)
		}
	}
}

func TestBuiltinsListed(t *testing.T) {
	names := Builtins()
	if len(names) < 10 {
		t.Errorf("got %d builtins, expected more", len(names))
	}
	// Must be sorted for stable REPL help output.
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("builtin names are not sorted: %v", names)
		}
	}
}

func BenchmarkFib(b *testing.B) {
	p := parser.New(lexer.New("let fib = fn(n) { if (n < 2) { n } else { fib(n-1) + fib(n-2) } }; fib(18);"))
	program := p.ParseProgram()
	b.ResetTimer()
	for b.Loop() {
		New(context.Background()).Eval(program, object.NewEnvironment())
	}
}
