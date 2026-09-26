package eval

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/zg/interp/object"
)

// builtins are the functions available without being defined in the
// language itself.
var builtins map[string]*object.Builtin

// init rather than a literal, because several builtins call helpers that
// reference the map itself.
func init() {
	builtins = map[string]*object.Builtin{}
	register := func(name string, fn object.BuiltinFunction) {
		builtins[name] = &object.Builtin{Name: name, Fn: fn}
	}

	register("len", func(args ...object.Object) object.Object {
		if err := wantArgs("len", 1, args); err != nil {
			return err
		}
		switch arg := args[0].(type) {
		case *object.String:
			// Count runes, not bytes: len("é") is 1.
			return &object.Integer{Value: int64(len([]rune(arg.Value)))}
		case *object.Array:
			return &object.Integer{Value: int64(len(arg.Elements))}
		case *object.Hash:
			return &object.Integer{Value: int64(len(arg.Pairs))}
		default:
			return argError("len", "STRING, ARRAY or HASH", args[0])
		}
	})

	register("first", func(args ...object.Object) object.Object {
		if err := wantArgs("first", 1, args); err != nil {
			return err
		}
		arr, ok := args[0].(*object.Array)
		if !ok {
			return argError("first", "ARRAY", args[0])
		}
		if len(arr.Elements) == 0 {
			return NULL
		}
		return arr.Elements[0]
	})

	register("last", func(args ...object.Object) object.Object {
		if err := wantArgs("last", 1, args); err != nil {
			return err
		}
		arr, ok := args[0].(*object.Array)
		if !ok {
			return argError("last", "ARRAY", args[0])
		}
		if len(arr.Elements) == 0 {
			return NULL
		}
		return arr.Elements[len(arr.Elements)-1]
	})

	register("rest", func(args ...object.Object) object.Object {
		if err := wantArgs("rest", 1, args); err != nil {
			return err
		}
		arr, ok := args[0].(*object.Array)
		if !ok {
			return argError("rest", "ARRAY", args[0])
		}
		if len(arr.Elements) == 0 {
			return NULL
		}
		// Copy: returning arr.Elements[1:] would alias the original's
		// backing array, so a later push could mutate both.
		out := make([]object.Object, len(arr.Elements)-1)
		copy(out, arr.Elements[1:])
		return &object.Array{Elements: out}
	})

	register("push", func(args ...object.Object) object.Object {
		if err := wantArgs("push", 2, args); err != nil {
			return err
		}
		arr, ok := args[0].(*object.Array)
		if !ok {
			return argError("push", "ARRAY", args[0])
		}
		// Arrays are values here: push returns a new array rather than
		// mutating, so aliasing cannot surprise the caller.
		out := make([]object.Object, len(arr.Elements), len(arr.Elements)+1)
		copy(out, arr.Elements)
		return &object.Array{Elements: append(out, args[1])}
	})

	register("keys", func(args ...object.Object) object.Object {
		if err := wantArgs("keys", 1, args); err != nil {
			return err
		}
		hash, ok := args[0].(*object.Hash)
		if !ok {
			return argError("keys", "HASH", args[0])
		}
		out := make([]object.Object, 0, len(hash.Order))
		for _, k := range hash.Order {
			out = append(out, hash.Pairs[k].Key)
		}
		return &object.Array{Elements: out}
	})

	register("values", func(args ...object.Object) object.Object {
		if err := wantArgs("values", 1, args); err != nil {
			return err
		}
		hash, ok := args[0].(*object.Hash)
		if !ok {
			return argError("values", "HASH", args[0])
		}
		out := make([]object.Object, 0, len(hash.Order))
		for _, k := range hash.Order {
			out = append(out, hash.Pairs[k].Value)
		}
		return &object.Array{Elements: out}
	})

	register("puts", func(args ...object.Object) object.Object {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.Inspect()
		}
		fmt.Fprintln(Stdout, strings.Join(parts, " "))
		return NULL
	})

	register("str", func(args ...object.Object) object.Object {
		if err := wantArgs("str", 1, args); err != nil {
			return err
		}
		return &object.String{Value: args[0].Inspect()}
	})

	register("int", func(args ...object.Object) object.Object {
		if err := wantArgs("int", 1, args); err != nil {
			return err
		}
		switch arg := args[0].(type) {
		case *object.Integer:
			return arg
		case *object.Float:
			return &object.Integer{Value: int64(arg.Value)}
		case *object.String:
			n, err := strconv.ParseInt(strings.TrimSpace(arg.Value), 10, 64)
			if err != nil {
				return &object.Error{Message: fmt.Sprintf("int: cannot parse %q", arg.Value)}
			}
			return &object.Integer{Value: n}
		default:
			return argError("int", "INTEGER, FLOAT or STRING", args[0])
		}
	})

	register("float", func(args ...object.Object) object.Object {
		if err := wantArgs("float", 1, args); err != nil {
			return err
		}
		switch arg := args[0].(type) {
		case *object.Float:
			return arg
		case *object.Integer:
			return &object.Float{Value: float64(arg.Value)}
		case *object.String:
			f, err := strconv.ParseFloat(strings.TrimSpace(arg.Value), 64)
			if err != nil {
				return &object.Error{Message: fmt.Sprintf("float: cannot parse %q", arg.Value)}
			}
			return &object.Float{Value: f}
		default:
			return argError("float", "INTEGER, FLOAT or STRING", args[0])
		}
	})

	register("type", func(args ...object.Object) object.Object {
		if err := wantArgs("type", 1, args); err != nil {
			return err
		}
		return &object.String{Value: string(args[0].Type())}
	})

	register("split", func(args ...object.Object) object.Object {
		if err := wantArgs("split", 2, args); err != nil {
			return err
		}
		s, ok := args[0].(*object.String)
		if !ok {
			return argError("split", "STRING", args[0])
		}
		sep, ok := args[1].(*object.String)
		if !ok {
			return argError("split", "STRING", args[1])
		}
		var parts []string
		if sep.Value == "" {
			parts = strings.Fields(s.Value)
		} else {
			parts = strings.Split(s.Value, sep.Value)
		}
		out := make([]object.Object, len(parts))
		for i, p := range parts {
			out[i] = &object.String{Value: p}
		}
		return &object.Array{Elements: out}
	})

	register("join", func(args ...object.Object) object.Object {
		if err := wantArgs("join", 2, args); err != nil {
			return err
		}
		arr, ok := args[0].(*object.Array)
		if !ok {
			return argError("join", "ARRAY", args[0])
		}
		sep, ok := args[1].(*object.String)
		if !ok {
			return argError("join", "STRING", args[1])
		}
		parts := make([]string, len(arr.Elements))
		for i, e := range arr.Elements {
			parts[i] = e.Inspect()
		}
		return &object.String{Value: strings.Join(parts, sep.Value)}
	})

	register("sort", func(args ...object.Object) object.Object {
		if err := wantArgs("sort", 1, args); err != nil {
			return err
		}
		arr, ok := args[0].(*object.Array)
		if !ok {
			return argError("sort", "ARRAY", args[0])
		}
		out := make([]object.Object, len(arr.Elements))
		copy(out, arr.Elements)
		var sortErr object.Object
		sort.SliceStable(out, func(i, j int) bool {
			l, r := out[i], out[j]
			switch {
			case isNumeric(l) && isNumeric(r):
				return toFloat(l) < toFloat(r)
			case l.Type() == object.STRING && r.Type() == object.STRING:
				return l.(*object.String).Value < r.(*object.String).Value
			default:
				if sortErr == nil {
					sortErr = &object.Error{Message: fmt.Sprintf("sort: cannot compare %s with %s", l.Type(), r.Type())}
				}
				return false
			}
		})
		if sortErr != nil {
			return sortErr
		}
		return &object.Array{Elements: out}
	})
}

// Stdout is where puts writes. It is a variable so tests can capture output.
var Stdout io.Writer = os.Stdout

func wantArgs(name string, n int, args []object.Object) object.Object {
	if len(args) != n {
		return &object.Error{Message: fmt.Sprintf("wrong number of arguments to %s: want %d, got %d", name, n, len(args))}
	}
	return nil
}

func argError(name, want string, got object.Object) object.Object {
	return &object.Error{Message: fmt.Sprintf("argument to %s must be %s, got %s", name, want, got.Type())}
}

// Builtins returns the names of all builtin functions, for the REPL's help.
func Builtins() []string {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
