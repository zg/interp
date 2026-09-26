// Package repl provides an interactive read-eval-print loop.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/zg/interp/eval"
	"github.com/zg/interp/lexer"
	"github.com/zg/interp/object"
	"github.com/zg/interp/parser"
)

const prompt = ">> "

// Start runs the REPL until EOF or :quit.
func Start(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	env := object.NewEnvironment()
	interp := eval.New(ctx)

	fmt.Fprintln(out, "interp — type :help for commands, Ctrl-D to exit")
	for {
		fmt.Fprint(out, prompt)
		if !scanner.Scan() {
			fmt.Fprintln(out)
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if done := command(line, out, env); done {
			return nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		p := parser.New(lexer.New(line))
		program := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintln(out, "  parse error:", e)
			}
			continue
		}

		result := interp.Eval(program, env)
		// null from a `let` is not worth echoing; everything else is.
		if result != nil && result != object.Object(nil) {
			if result.Type() != object.NULL {
				fmt.Fprintln(out, result.Inspect())
			}
		}
	}
}

// command handles REPL meta-commands, returning true when the loop should
// stop.
func command(line string, out io.Writer, env *object.Environment) bool {
	switch line {
	case ":quit", ":q", ":exit":
		return true
	case ":help", ":h":
		fmt.Fprint(out, `commands:
  :help     show this message
  :env      list bound names
  :builtins list builtin functions
  :quit     exit

language: let, fn, if/else, while, return, arrays, hashes, strings
`)
	case ":env":
		names := env.Names()
		if len(names) == 0 {
			fmt.Fprintln(out, "  (nothing bound)")
			return false
		}
		for _, n := range names {
			v, _ := env.Get(n)
			fmt.Fprintf(out, "  %s = %s\n", n, v.Inspect())
		}
	case ":builtins":
		fmt.Fprintln(out, "  "+strings.Join(eval.Builtins(), ", "))
	}
	return false
}
