package repl

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func session(t *testing.T, input string) string {
	t.Helper()
	var out bytes.Buffer
	if err := Start(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestREPLEvaluates(t *testing.T) {
	got := session(t, "1 + 1\nlet x = 5\nx * 2\n")
	for _, want := range []string{"2", "10"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// State must persist between lines, or the REPL is useless.
func TestREPLKeepsState(t *testing.T) {
	got := session(t, "let a = 7\nlet b = 6\na * b\n")
	if !strings.Contains(got, "42") {
		t.Errorf("state not kept across lines:\n%s", got)
	}
}

// A parse error must not kill the session.
func TestREPLSurvivesErrors(t *testing.T) {
	got := session(t, "let = = =\n1 + 1\n")
	if !strings.Contains(got, "parse error") {
		t.Errorf("error not reported:\n%s", got)
	}
	if !strings.Contains(got, "2") {
		t.Errorf("session did not continue after the error:\n%s", got)
	}
}

func TestREPLRuntimeErrorContinues(t *testing.T) {
	got := session(t, "1 + true\n2 + 2\n")
	if !strings.Contains(got, "4") {
		t.Errorf("session did not continue after a runtime error:\n%s", got)
	}
}

func TestREPLCommands(t *testing.T) {
	if got := session(t, ":help\n"); !strings.Contains(got, "commands:") {
		t.Errorf(":help produced:\n%s", got)
	}
	if got := session(t, "let x = 1\n:env\n"); !strings.Contains(got, "x = 1") {
		t.Errorf(":env produced:\n%s", got)
	}
	if got := session(t, ":builtins\n"); !strings.Contains(got, "len") {
		t.Errorf(":builtins produced:\n%s", got)
	}
}

func TestREPLQuit(t *testing.T) {
	got := session(t, ":quit\n1 + 1\n")
	if strings.Contains(got, "2") {
		t.Errorf(":quit did not stop the session:\n%s", got)
	}
}

// `let` yields null; echoing it on every binding would be noise.
func TestREPLDoesNotEchoNull(t *testing.T) {
	if got := session(t, "let x = 1\n"); strings.Contains(got, "null") {
		t.Errorf("let should not echo null:\n%s", got)
	}
}
