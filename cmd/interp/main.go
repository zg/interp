// Command interp runs a program, or starts a REPL when given no file.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/zg/interp/eval"
	"github.com/zg/interp/lexer"
	"github.com/zg/interp/object"
	"github.com/zg/interp/parser"
	"github.com/zg/interp/repl"
)

func main() {
	// Ctrl-C cancels a running program rather than killing the process
	// outright, so the evaluator can unwind and report.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		if err := repl.Start(ctx, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "interp:", err)
			os.Exit(1)
		}
		return
	}

	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "interp:", err)
		os.Exit(1)
	}
	if code := Run(ctx, string(src), os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

// Run parses and evaluates src, returning a process exit code.
func Run(ctx context.Context, src string, stdout, stderr *os.File) int {
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		fmt.Fprintln(stderr, "parse errors:")
		for _, e := range errs {
			fmt.Fprintln(stderr, "  "+e)
		}
		return 1
	}

	eval.Stdout = stdout
	result := eval.New(ctx).Eval(program, object.NewEnvironment())
	if result != nil && result.Type() == object.ERROR {
		fmt.Fprintln(stderr, result.Inspect())
		return 1
	}
	// A program's final expression value is printed only when it is not
	// null, so scripts that end in puts() do not emit a stray blank line.
	if result != nil && result.Type() != object.NULL {
		if s := strings.TrimSpace(result.Inspect()); s != "" {
			fmt.Fprintln(stdout, s)
		}
	}
	return 0
}
