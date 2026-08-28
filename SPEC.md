# go2cpp — Software Requirements Specification

Version: 0.4.0 (upstream xyproto/go2cpp, with Windows/MinGW build fix)
License: MIT

## 1. Overview

go2cpp is a source-to-source compiler (transpiler) that converts a subset of Go
to C++20 and compiles the generated C++ into a native executable by shelling
out to an external C++ compiler. The tool itself is a single Go package with no
third-party dependencies.

Intended use: help port parts of Go programs to C++, or write programs from
scratch in Go and continually verify they can be converted and compiled as C++.

## 2. Functional requirements

- FR1: Read a Go source file (argument) or stdin.
- FR2: Transform the Go source into a single-translation-unit C++20 source.
- FR3: Format the generated C++ with `clang-format` (Webkit style, column
  limit 99) unless disabled with `-O`; fall back to unformatted output when
  clang-format is unavailable.
- FR4: Compile the generated C++ by piping it to an external compiler
  (`-x c++ -std=c++2a -O2 -pipe -fPIC -Wfatal-errors -fpermissive
  -Wno-address-of-temporary -s`).
- FR5: The C++ compiler is `g++` by default and can be overridden with the
  `CXX` environment variable (e.g. clang++ from MSYS2 UCRT64).
- FR6: Write the linked executable to the filename given after `-o`
  (`go2cpp main.go -o main`); with no output filename, print the generated
  C++ source to stdout.
- FR7: On compilation failure, print the generated C++ source and the
  compiler errors to stdout/stderr and exit non-zero.
- FR8: Support the Go subset enumerated in README (keywords, simple maps,
  structs, defer, goto/labels, switch, range loops, iota expressions, strings
  and fmt stdlib functions, slicing/append, `go` as detached threads, and
  buffered `chan` via a runtime shim).
- FR9 (Windows): Produce a runnable executable when the external compiler is
  a MinGW toolchain (clang++ or g++), which appends `.exe` to extension-less
  `-o` output names.
- FR10: The generated program's output must match `go run` output
  (order-insensitively for map iteration), except for the documented
  differences in DIFFERENCES.md.
- FR11: Local package imports are supported: import paths that resolve to a
  directory with .go files (module-path-relative or file-relative) are
  transpiled before the importing code with qualifier-prefixed exported
  identifiers; standard library imports are handled by trigger shims. The
  result stays a single translation unit.
- FR12: A package-level `const cpp = ` + raw string constant is emitted
  verbatim into the generated C++ (after the include block, before the
  shims), and Go functions whose names are declared in it are not
  transpiled — the C++ definitions replace them (C++ escape hatch).
- FR13: Interfaces are supported: `type X interface {` becomes an abstract
  class with pure virtual methods; structs implementing all methods of an
  interface inherit it with injected `virtual ... override;` members that
  forward to generated free functions; interface-typed variables, fields
  and parameters are pointers; interface-implementing struct literals are
  rewritten to generated constructor calls. Limitations in DIFFERENCES.md.

## 3. Non-functional requirements

- NFR1: Few dependencies — only the Go toolchain is needed to build go2cpp.
- NFR2: Low complexity, short source code, fast operation.
- NFR3: Generated code must be C++20 (compilable with g++ or clang++).

## 4. Architecture

Single Go package `main`. Pipeline executed by `main()`:

```
 .go file / stdin
        |
        v
 go2cppFile(filename, source)        (packages.go, file-level pipeline)
   |- parseImports: split stdlib vs local package imports
   |- resolvePackageDir: module-path-relative or file-relative directory
   |- transpilePackage (recursive): prefix exported identifiers, transpile
   |   with go2cppLines, rewrite pkg.Ident call sites, visited-set dedup
   |
   v
 go2cppLines(source)                 (main.go, line-based transformation)
   |- extractCppBlocks: const cpp raw strings -> verbatim C++ + skipped
   |   function names (Go fallbacks of those names are skipped entirely)
   |- scanTypeDecls: interfaces, struct methods, implemented interfaces,
   |   struct field order (for constructor generation)
   |- strip comments, track curly depth, skip package/import blocks
   |- per-line dispatch: func / for / switch / case / if / defer /
   |   go (goroutine) / chan send/receive/close / var / const (iota) /
   |   type / map literal / print statements
   |- WholeProgramReplace + initializeVariables (type/zero-value rewrites,
   |   fmt.Sprintf/time.Sleep/append renames)
   |- RewriteSlices (s[a:b] -> _go2cpp_slice, append spread, <-ch -> .recv())
   |- AddFunctions (ordered shim list: strings*, fmtSprintf, _go2cpp_slice,
   |   _go2cpp_append, cap, gochan<T>, timeSleep, len, _format_output)
   |- AddIncludes (scan symbols -> standard #include headers)
   |- spliceCppBlock: const cpp content inserted after the include block
        |
        v
 clang-format (optional, -o)  ->  C++ compiler $CXX (default g++)
        |                             pipes C++ source via stdin
        v
 temp file (go2cpp*.exe)  ->  copy to output filename
```

The whole-program passes (WholeProgramReplace, RewriteSlices,
AddFunctions, AddIncludes) run once, on the combined output of all
transpiled packages plus the main file, so shims and includes are never
duplicated.

### 4.1 Files

| File          | Responsibility |
|---------------|----------------|
| `main.go`     | Transpiler core: line dispatcher `go2cppLines()`, statement transformers, C++ escape hatch (`extractCppBlocks`, `spliceCppBlock`), runtime shims, includes, CLI |
| `packages.go` | Imports and local packages: `go2cppFile()`, import parsing, module resolution, identifier prefixing, single-TU assembly |
| `main.go` (interface support) | Pre-scan (`scanTypeDecls`), method declarations, virtual override injection, interface variable tracking, struct-literal constructor rewriting |
| `strutils.go` | String helpers: `lastchar`, `has`, `splitAtAndTrim` |
| `intutils.go` | Number helpers: `isNum`, `trailingNumber` |
| `main_test.go`| End-to-end tests: for every testcase, compare `go run` output with the go2cpp+compiler binary |
| `testcases/`  | Go sample programs used by the tests, incl. `importpkg/` (multi-package) |

### 4.2 Key transformers in main.go

- `FunctionSignature` — Go func to `auto name(args) -> rettype {`; multiple
  return values become `std::tuple`; `main` returns `int`.
- `ForLoop` — range loops over lists (`std::size` counting loop), maps
  (structured bindings), and C-style/endless loops.
- `PrintStatement` — `fmt.Println/Print` to `std::cout`/`std::cerr` chains
  with `_format_output`; `fmt.Printf`/`fmt.Sprintf` to the variadic
  `fmtSprintf` shim; `fmt.Fprintf` falls back to C `fprintf`.
- `VarDeclarations`, `TypeDeclaration` (Go struct -> `class X { public:`),
  `ConstDeclaration` (with full iota support: expressions, implicit
  repetition of the previous expression), `HashElements` (Go map
  literal -> `std::unordered_map` initializer list).
- `DeferCall` — defer to `std::shared_ptr<void>` custom-deleter lambda.
- `go` statements — plain calls to `std::thread(f, args).detach()`;
  multiline anonymous goroutines to thread lambdas.
- Channels — `make(chan T[, n])` to `gochan<T>(n)`, `ch <- v` to
  `ch.send(v)`, `v := <-ch` to `ch.recv()`, `v, ok := <-ch` to
  `ch.recv2()`, `close(ch)` to `ch.close()`.
- `RewriteSlices` — regex pass rewriting `s[a:b]`/`s[a:]`/`s[:b]` to
  `_go2cpp_slice(s, a, b)`, `append(x, y...)` to `_go2cpp_appendSpread`, and
  remaining `<-ch` receives to `.recv()` calls.
- `Switch`/`Case` — switch desugared to `if/else if` chains on a saved
  expression; `goto`/labels pass through with a `_l__` prefix policy.
- `TypeReplace` — Go type to C++ type mapping (`string` -> `std::string`,
  `float64` -> `double`, `int32` -> `std::int32_t`, `[]T` -> `std::vector<T>`,
  `chan T` -> `gochan<T>`, etc.).
- `AddFunctions` — an **ordered list** of trigger/code pairs (ShimCode);
  when the trigger is found in the output, the C++ shim is prepended and the
  trigger name is renamed (dots removed). Order matters: shims that call
  other shims come after their dependencies.

### 4.3 Global state

`switchExpressionCounter`, `deferCounter`, `labelCounter`, `iotaNumber`,
`iotaFirstLine`, `lastConstExpr`, `firstCase`, `switchLabel`,
`unfinishedDeferFunction` — module-level counters and flags shared by the
transformers (single-threaded by design).

### 4.4 CLI and environment

```
go2cpp [--version] [--help]
go2cpp <file.go> [-o | -O] [output-filename]
```

- `-o` format with clang-format (default), `-O` skip formatting.
- `CXX` — override the C++ compiler used for the generated code.
- `clang-format` must be on PATH for formatting (graceful fallback).

## 5. Windows / MinGW notes

MinGW clang++/g++ append `.exe` when the `-o` link output has no extension.
go2cpp therefore creates its temporary file with the pattern `go2cpp*.exe`
(`ioutil.TempFile("", "go2cpp*.exe")`) so the compiler writes to the exact
name that is read back and copied to the output file. Without this, the tool
produces 0-byte executables and orphans `go2cpp*.exe` files in TMP.

Verified toolchain: MSYS2 UCRT64 clang 22.1.4
(`C:\msys64\ucrt64\bin\clang++.exe`), clang-format 22.1.4, Go 1.26.1.
All 57 testcases (including the multi-package `importpkg`, the C++
interop `cpp_call` and the `interfaces` testcase) produce output identical
to `go run`, and `go test` passes on Windows.

Additional Windows notes:

* When `C:\msys64\ucrt64\bin` is prepended to PATH, MSYS2's own trimmed
  `go.exe` shadows the system Go; use the full path to the Go binary for
  `go run` comparisons.
* The MinGW C++ runtime writes `\r\n` in text mode while Go writes `\n`;
  the test suite normalizes line endings before comparing.
* Generated executables link the MinGW runtime DLLs dynamically
  (`libstdc++-6.dll` and friends); the compiler `bin` directory must be on
  PATH to run them. The test harness adds the `CXX` directory to the child
  PATH when running compiled binaries.
* MSYS2 packages a trimmed Go in `ucrt64/bin`; `go test` must run with the
  real Go toolchain on PATH.

## 6. Build and verification

```
go build          # builds go2cpp(.exe)
go test           # end-to-end: go run vs go2cpp+compiler for every testcase
```

The test suite resolves the go2cpp binary and testcase executables in a
platform-aware way (`go2cppBinary`, `testcaseBinary`), removes binaries with
`os.Remove`, and normalizes CRLF line endings, so it runs on both Linux and
Windows. On Windows, set `CXX` to the MinGW clang++/g++ full path:

```
$env:CXX = "C:\msys64\ucrt64\bin\clang++.exe"; go test
```

## 7. Known limitations

- Only simple code samples are supported; see DIFFERENCES.md for the full
  list of supported features and semantic differences (slices are copies,
  `defer` is block-scoped, goroutines are detached threads, `chan` is a
  buffered shim, `select`/`interface` are unsupported, local packages have
  no import cycles or dot imports).
- No plan for full `import` support yet (see README TODO).
- The line-based transformer keeps global mutable state and is not
  concurrency-safe.
- Map iteration order differs between Go and C++ (tests compare unordered).
