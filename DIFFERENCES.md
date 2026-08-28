# DIFFERENCES.md — Go vs. go2cpp-transpiled C++

This document describes how programs transpiled with go2cpp differ from the
same programs running under real Go. It covers: what is supported, how
semantics differ, and what is not supported at all.

Everything below has been verified with MSYS2 UCRT64 clang++ 22.1.4
(`-std=c++2a`) against Go 1.26.1, using the testcase suite in `testcases/`.

---

## 1. Supported feature matrix

### Keywords

| Go keyword | Status | Transpiled to |
|---|---|---|
| `break`, `continue`, `fallthrough` | Supported | C++ `break`/`continue`; `fallthrough` via generated labels |
| `case`, `default`, `switch` | Supported | Desugared to `if/else if` chains on a saved expression |
| `const` | Supported | `const auto` / `const T` declarations, incl. `iota` |
| `defer` | Supported (see 3.2) | `std::shared_ptr<void>` with a custom deleter lambda |
| `else`, `if` | Supported | C++ `if`/`else if` |
| `for` (all forms), `range` | Supported | Counting loops (`std::size`), range-for, endless loops |
| `func` | Supported | Trailing-return functions (`auto f(...) -> T`) |
| `goto` + labels | Supported | Passed through, labels kept |
| `import`, `package` | Supported (local packages, see 3.11) | Packages transpiled into the single translation unit, qualifier-prefixed identifiers |
| `map` | Supported | `std::unordered_map` |
| `return` | Supported | Direct return; multiple returns as `std::tuple` |
| `struct` | Supported | `class X { public:` with a generated `_str()` method |
| `type` | Supported | `using` aliases and struct declarations |
| `var` | Supported | Typed declarations incl. zero-value initialization |
| `chan` | Partial (see 3.5) | `gochan<T>` shim (buffered) |
| `go` | Partial (see 3.4) | `std::thread(...).detach()` |
| `interface` | Supported (see 3.11) | Abstract class with pure virtual methods; structs inherit and override |
| `select` | **Not supported** | Requires true multi-channel blocking |
| `iota` | Supported | Integer substitution, incl. expressions (`iota * 2`) and implicit repetition of the previous expression |

### Standard library

| Function | Status | Notes |
|---|---|---|
| `fmt.Println` / `fmt.Print` | Supported | `std::cout`; structs formatted via generated `_str()` |
| `fmt.Printf` | Supported | Variadic shim; verbs `%v %s %d %f %t %x %g %q`, `%%` escape |
| `fmt.Sprintf` | Supported | Same variadic shim, returns `std::string` |
| `println` / `print` | Supported | `std::cerr`, matching Go's builtins |
| `strings.Contains`, `strings.HasPrefix` | Supported | |
| `strings.HasSuffix` | Supported | |
| `strings.Index` | Supported | Returns -1 when absent |
| `strings.Join` | Supported | `std::vector<std::string>` |
| `strings.Replace` / `strings.ReplaceAll` | Supported | `Replace` with count, -1 means all |
| `strings.Split` / `strings.SplitN` | Supported | Returns `std::vector<std::string>`; empty separator splits per character |
| `strings.TrimSpace` | Supported | |
| `strconv.ParseFloat` / `strconv.ParseInt` | Supported | Returns `(value, error)` tuple, error as `std::optional<std::string>` |
| `time.Sleep` | Supported | `timeSleep(ms)`; `time.Second`/`time.Millisecond` constants rewritten to integers |
| `len` | Supported | `.size()` |
| `cap` | Supported (see 3.7) | `.capacity()` |
| `make([]T, n)` / `[]T{...}` literals | Supported | `std::vector<T>` |
| `make(chan T[, n])` | Supported | `gochan<T>(n)` |
| `append` | Partial (see 3.8) | Variadic push_back; spread `append(x, y...)` for one slice |
| Compound assignment `+= -= *= /= %=` `<<=` `>>=` `&=` `|=` `^=` | Supported | Rewritten to `x = x op y`; `&^=` is not supported |
| `strings.NewReader` | **Not supported** | No `io.Reader` abstraction exists |
| Everything else | **Not supported** | No full standard library |

---

## 2. Type mapping

| Go type | C++ type |
|---|---|
| `string` | `std::string` |
| `int` | `int` |
| `uint` | `unsigned int` |
| `int8/16/32/64` | `std::int8_t` ... `std::int64_t` |
| `uint8/16/32/64` | `std::uint8_t` ... `std::uint64_t` |
| `byte` / `rune` | `std::uint8_t` / `std::int32_t` |
| `float32` / `float64` | `float` / `double` |
| `bool` | `bool` |
| `[]T` | `std::vector<T>` |
| `map[K]V` | `std::unordered_map<K, V>` |
| `chan T` | `gochan<T>` |
| Multiple return values | `std::tuple<...>` with structured bindings |
| `*T` (pointers) | `T*`; `&x{...}` composite literals become `new` — no garbage collection |

Notes:

* `x := "literal"` becomes `auto x = std::string("literal")`, so string
  methods and slicing work on it.
* There is no garbage collector: memory is managed by RAII and the shims;
  `new`-allocated structs are leaked unless managed manually.
* Integer overflow: Go defines wraparound for signed integers; in C++,
  signed overflow is undefined behavior (in practice it wraps with the
  flags go2cpp uses, but this is not guaranteed).

---

## 3. Semantic differences

### 3.1 Map iteration order

Go deliberately randomizes map iteration order. `std::unordered_map` has its
own (stable but unspecified) order. Programs that iterate a map produce the
same *set* of outputs but possibly in a different order than `go run`. The
test suite compares such outputs order-insensitively.

### 3.2 `defer` scope

Go runs deferred calls at **function** exit. The generated
`std::shared_ptr<void>` deleter runs at the end of the **block** the `defer`
appears in. For defers directly in a function body the behavior matches;
deferred calls inside inner blocks (`for`, `if`) run earlier than in Go.

### 3.3 Slices are copies, not views

`_go2cpp_slice(s, from, to)` returns a **copy** of the elements. In Go, a
slice shares the backing array, so `sub := x[1:3]; sub[0] = 9` also modifies
`x`. Transpiled code creates independent data — mutations of the slice are
not visible in the original. Out-of-range bounds are **clamped** to the
container size (Go panics with "slice bounds out of range").

### 3.4 Goroutines (`go`)

* `go f(a, b)` becomes `std::thread(f, a, b).detach()`.
* `go func() {` ... `}()` spanning multiple lines becomes a detached thread
  running a lambda.
* One-line anonymous goroutines (`go func() { ... }()` on a single line) are
  not supported.
* There is no scheduler: each goroutine is an OS thread. There is no
  preemption guarantee, no `GOMAXPROCS`, and no way to join.
* Synchronization must be done explicitly, typically with `time.Sleep`
  (milliseconds) — the generated program exits when `main` returns, killing
  any still-running detached threads (same as Go killing goroutines at exit).

### 3.5 Channels (`chan`)

`chan T` is a shim class `gochan<T>` implementing a **blocking bounded
queue** (mutex + condition variable + deque):

| Go behavior | go2cpp behavior |
|---|---|
| `make(chan T, n)` | `gochan<T>(n)` with capacity `n` |
| `make(chan T)` (unbuffered rendezvous) | Approximated as a capacity-1 buffer — sender and receiver do **not** rendezvous |
| `ch <- v` (blocks when full) | `ch.send(v)`, same blocking behavior |
| `v := <-ch` (blocks when empty) | `ch.recv()`, same blocking behavior |
| `v, ok := <-ch` | `ch.recv2()` returning `std::pair<T, bool>`; ok=false when closed and drained (matches Go) |
| `close(ch)` | `ch.close()`; receivers wake up |
| Send on closed channel | Go **panics**; the shim silently drops the value |
| `for v := range ch` | Not supported |
| `select` | Not supported |

### 3.6 `fmt` verb coverage

The variadic formatter supports `%v %s %d %f %t %x %g %q` and `%%`.
Differences from Go:

* `%s` applied to a number prints the number; Go prints `%!s(int=42)`.
* More arguments than verbs: extras are silently dropped; Go appends
  `%!(EXTRA type=value)`.
* Fewer arguments than verbs: the verb stays literal; Go prints
  `%!v(MISSING)`.
* `%x` on a string prints the string as-is; Go hex-encodes it.
* `%q` wraps the value in plain double quotes without Go's escaping.
* Width/precision flags (`%5d`, `%.2f`) are not parsed — the `%` and verb
  are replaced, flags remain literal text.
* Struct values are printed via a generated `_str()` method; pointers print
  with a `&` prefix, matching Go's `&{...}` format.

### 3.7 `cap`

`cap(v)` maps to `v.capacity()`. C++ `std::vector` growth is
implementation-defined, so capacity values can differ between Go and the
transpiled program. `len` always matches.

### 3.8 `append`

* `append(x, a, b, ...)` appends any number of single elements.
* `append(x, y...)` (spread) appends all elements of one slice.
* Mixed forms like `append(x, a, y...)` are not supported.
* Go's append may reallocate and shares memory with the original slice; the
  shim mutates and returns the same vector — observable behavior matches for
  the `x = append(x, ...)` idiom.

### 3.9 Output line endings on Windows

The MinGW C++ runtime writes `\r\n` in text mode, while Go writes `\n`.
Program output therefore contains CRLF line endings on Windows. The test
suite normalizes line endings before comparing. This does not occur on
Linux/macOS.

The generated executables link the MinGW runtime DLLs (`libstdc++-6.dll`,
`libgcc_s_seh-1.dll`, `libwinpthread-1.dll`) dynamically; the compiler's
`bin` directory (for example `C:\msys64\ucrt64\bin`) must be on PATH to run
them. The test suite adds the directory of the `CXX` compiler to the child
PATH automatically when running the compiled binaries.

### 3.10 Calling C++ functions

A package-level raw string constant named `cpp` is emitted verbatim into
the generated C++ program, right after the include block. Go functions
whose names are declared in that block are **not transpiled** — the C++
definitions are used instead. This makes the constant a full C++ escape
hatch:

```go
const cpp = `
#include <cmath>
auto cppSqrt(double x) -> double { return std::sqrt(x); }
`

func cppSqrt(x float64) float64 {
	return math.Sqrt(x) // fallback for "go run"
}
```

* `go run` uses the Go fallback implementations; the go2cpp-compiled
  program uses the C++ definitions. Both must produce the same output.
* The C++ code is never processed by the transpiler's passes — use plain
  C++ and its own `#include` directives inside the block.
* Function signatures inside the block must fit on one line (the detection
  regex matches `name(args) {` and `name(args) -> rettype {`).
* Function-pointer arguments and multi-line signatures are not detected.
* The block cannot call the generated shims (`fmtSprintf`, `gochan`...);
  it is emitted before them.
* Within local packages, the block works per package file, but exported
  identifier prefixing skips it — name cross-package C++ helpers carefully.

### 3.11 Packages and imports

Imports are resolved in two categories:

* **Standard library imports** (`fmt`, `strings`, ...) are not looked up on
  disk; the used functions are handled by the trigger-based shims.
* **Local package imports** resolve to a directory with `.go` files: either
  by stripping the enclosing `go.mod` module path from the import path and
  resolving the rest from the module root, or as a plain path relative to
  the importing file.

Local packages are transpiled before the importing code and concatenated
into the same translation unit, so no linking or forward declarations are
needed. The exported identifiers of a package (functions, types, vars,
consts) are prefixed with the package qualifier, and qualified call sites
`pkg.Ident` are rewritten to `pkgIdent` in the importing code. This matches
the way the shims are named (`fmt.Println` internals become `fmtPrintln`
style symbols).

Differences and limitations:

* Import cycles are not supported.
* Dot imports (`import . "pkg"`) are not supported.
* Methods on package types are not renamed or callable across packages.
* Unexported (lowercase) identifiers are not accessible from importing code,
  like in Go, but they are not enforced to be private either.
* Aliased imports work; importing the same package under two aliases maps
  both to the first-seen prefix.
* Within a package spanning several files, types must be declared before
  use in alphabetical file order (files are concatenated sorted).
* The generated program is still a single translation unit; there is no
  separate compilation or linking of packages.

### 3.11 Interfaces

Interfaces are mapped to C++ inheritance with virtual dispatch:

```go
type Shape interface {
	Area() float64
}

type Rect struct { W, H float64 }

func (r Rect) Area() float64 { return r.W * r.H }
```

becomes an abstract class `Shape` with pure virtual methods, and `Rect`
inherits it with injected `virtual ... override;` members that forward to
generated free functions (`Rect_Area`). Both value and pointer receivers
are supported.

* Interface-typed variables are pointers (`Shape*`), initialized to
  `nullptr` (Go's `nil`); assigning a struct literal allocates
  (`s = Rect{...}` becomes `s = new Rect(...)`), and assigning `nil`
  becomes `nullptr`.
* Method calls on interface variables are rewritten from `.` to `->`.
* Interface-typed function parameters become pointers; pass concrete
  structs with `&r` (valid Go, since pointer method sets include value
  receiver methods).
* Calling a method on a `nil` interface crashes the C++ program; Go panics
  with a clear message.
* Printing an interface variable shows `&{...}` (Go shows the value form
  `{...}`); print interface results or concrete values instead.

Limitations: interfaces, structs and methods must be declared in the same
file; methods must have 0 or 1 return values and named parameters; no empty
interface, no type assertions or type switches, no interface-to-interface
assignment, no anonymous types satisfying interfaces; struct literals of
implementing structs are rewritten to generated constructor calls (field
order from the declaration), so partial literals keep Go zero-value
semantics.

### 3.12 Structural limitations of the transpiler

* The transformer is line-based: one statement per line, no statement
  splitting or joining across lines.
* If-initializer statements (`if v := f(); v > 0 {`) are not supported.
* Nested slice expressions (`m[k][1:2]`), slices inside multi-argument
  spreads, and slices of slices are not rewritten.
* `<-ch` inside `if`/`for` headers is not rewritten (only plain statements
  and assignments).
* Multiline map literals are supported; multiline struct literals are not.
* Comments must not appear inside multiline strings.
* No compiler errors for unsupported Go constructs — the generated C++ will
  simply fail to compile, or an "UNRECOGNIZED ..." warning is printed to
  stderr.

---

## 4. Not supported (requires runtime/runtime design)

* **`select`** — needs blocking on multiple channels with fair wakeup; the
  `gochan` shim has no multi-channel wait primitive.
* **Empty interfaces and type assertions** — `interface {}` and runtime type
  inspection have no representation in the inheritance mapping.
* **Full unbuffered channel rendezvous** — senders/receivers do not meet;
  capacity-1 buffering is used instead.
* **Garbage collection** — Go's GC has no equivalent; composite literals
  via `&T{...}` leak by default.
* **Goroutine scheduler** — preemption, work stealing, `runtime` package.
* **The rest of the standard library** — only the functions listed in
  section 1 exist.
* **Packages and imports** — `import`/`package` are parsed and discarded;
  all code is a single translation unit.
