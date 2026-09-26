package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// devNull stands in for stdout/stderr in tests that only care about the
// exit code.
func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"valid program", "let x = 1; x + 1;", 0},
		{"parse error", "let = =", 1},
		{"runtime error", "1 + true", 1},
		{"undefined identifier", "nope", 1},
		{"empty program", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			null := devNull(t)
			if got := Run(context.Background(), tt.src, null, null); got != tt.want {
				t.Errorf("exit code = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRunWritesOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), `puts("hello")`, f, f); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	f.Close()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("got %q, want %q", string(got), "hello\n")
	}
}
