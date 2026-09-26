# interp

An interpreter: a program that directly executes instructions written in a
programming language, without requiring them to have been compiled into
machine language first.

This is a tree-walking interpreter for a small dynamically typed language
with first-class functions and closures.

## Install

```sh
go install github.com/zg/interp/cmd/interp@latest
```

## Use

```sh
interp program.ip     # run a file
interp                # start the REPL
```

## The language

```
let x = 5;
let name = "world";
let nums = [1, 2, 3];
let person = {"name": "ada", "born": 1815};

let add = fn(a, b) { a + b };
let fact = fn(n) { if (n <= 1) { 1 } else { n * fact(n - 1) } };

// closures capture their defining scope
let makeCounter = fn() { let n = 0; fn() { n = n + 1; n } };

let i = 0;
while (i < 10) { i = i + 1; }

if (x > 3) { puts("big") } else { puts("small") }
```

Types: integer, float, boolean, string, array, hash, function, null.
Operators: `+ - * / %`, `== != < > <= >=`, `&& ||`, `!`, `=`, indexing.
Builtins: `len first last rest push keys values puts str int float type
split join sort`.

`//` and `/* */` comments. Only `null` and `false` are falsey — `0` and `""`
are truthy.

## Design notes

**Errors are values, not panics.** A runtime error is an object that
propagates through evaluation like any other, carrying the line and column
it came from. Division by zero returns an error rather than panicking the
host; unbounded recursion hits a call-depth limit and returns an error
rather than overflowing the Go stack.

**Programs are interruptible.** Evaluation checks its context on every node,
so a long or infinite computation can be cancelled from outside. An optional
step budget (`WithMaxSteps`) bounds total work, which is how the test suite
asserts that `while (true) {}` terminates.

**Closures capture lexically.** A call's scope encloses the function's
*defining* environment, not the caller's. The counter example above returns
1 2 3 from one closure and 1 from another precisely because of this.

**Assignment is explicit.** `x = 5` on an undeclared name is an error rather
than an implicit global — `let` exists for declaration, and a typo silently
creating a variable is a classic source of bugs. Assignment walks outward to
find the declaring scope, which is what lets a `while` body mutate the
variable its condition tests.

**Output is deterministic.** Hash literals keep insertion order for both
evaluation and printing. Go randomises map iteration, so printing straight
from the map would make identical programs produce different output between
runs.

**`&&` binds tighter than `||`.** Both were initially given one precedence
level, which made `a || b && c` parse as `(a || b) && c`. The precedence
test caught it.

**`/*` opens a comment even where division-then-multiplication was meant**,
exactly as in C. That ambiguity is pinned by a test so it stays a documented
choice.

## Tests

```sh
go test -race ./...
go test -bench=. ./...
```

The parser is tested mostly through `String()` round-trips, which pin
precedence and associativity far more legibly than asserting on tree shape.
