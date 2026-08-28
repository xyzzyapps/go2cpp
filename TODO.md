# Plans

- [x] Slicing! (basic: s[a:b] on strings/vectors, append, appendSpread, cap; slices are copies, see DIFFERENCES.md)
- [ ] Look into using libgolang.h: https://lab.nexedi.com/kirr/pyglang/master/golang/libgolang.h
- [x] Generate the sprintf function programatically, but only for the needed amount of arguments. (superseded by one variadic template shim)

# Packages and imports plan

- [x] Parse import declarations (single-line, blocks, aliases) instead of discarding them
- [x] Resolve local package imports: enclosing go.mod module path stripped and
      resolved from the module root, or a plain path relative to the importing file;
      anything unresolvable stays stdlib (trigger shims)
- [x] Transpile local packages before the main file: exported identifiers
      (funcs/types/vars/consts) prefixed with the package qualifier, field and
      method accesses skipped; dependencies transpiled recursively (visited set,
      cycles unsupported)
- [x] Rewrite qualified call sites pkg.Ident -> pkgIdent in the importing code
- [x] Single translation unit assembly: package code concatenated before main,
      shims/includes applied once to the combined output
- [x] Testcase: testcases/importpkg (main + utils subpackage), registered in main_test.go
- [x] Update README/TODO/SPEC/DIFFERENCES

Also fixed along the way: `return T{...}` lost its semicolon (ends with a
brace), single return types were not run through TypeReplace (`-> float64`
was emitted verbatim), and the test harness now gives compiled binaries the
compiler DLL directory on PATH and retries once (Windows AV file locks).

# Feature completion plan

1. [x] stdlib strings: strings.HasSuffix, strings.Index, strings.Join,
       strings.Replace/ReplaceAll, strings.Split, strings.SplitN (C++ shims +
       testcases)
2. [x] fmt: variadic fmtSprintf + fmtPrintf shims (mixed argument types,
       correct %v/%s/%d/%f/%x/%q on string/bool/number/struct), transpiler emits
       fmtPrintf/fmtSprintf; replaces the 5 fixed same-type templates
3. [x] iota in const expressions (token substitution + implicit repetition)
4. [x] Slicing: templated slice() for std::string/std::vector, append,
       appendSpread, cap; transpiler rewrites s[a:b]/s[a:]/s[:b] outside
       for-headers and map literals
5. [x] go statement: std::thread(...).detach(), time.Sleep shim
6. [x] chan: minimal buffered chan<T> shim (send/recv/close/ok),
       make(chan T[, n]), ch <- v, <-ch, close(ch). Unbuffered rendezvous
       approximated.
7. [x] Port main_test.go to Windows (os.Remove, .exe-aware output paths,
       ./go2cpp resolution) and register all new testcases
8. [x] Full end-to-end verification with MSYS2 UCRT64 clang++ (50/50 testcases)
9. [x] DIFFERENCES.md: Go vs go2cpp semantics, supported/unsupported matrix
10. [x] Update README.md checkboxes, TODO.md and SPEC.md

Not feasible in this architecture (documented in DIFFERENCES.md instead):
- select, interface, strings.NewReader (no io.Reader abstraction), full
  unbuffered channel rendezvous semantics.

# Calling C++ from Go source plan

- [x] Detect a package-level `const cpp = ` + backtick string; emit its
      content verbatim into the generated C++ (after the include block)
- [x] Go functions whose names are declared in the cpp block are skipped
      during transpilation (brace-tracked skip); "go run" uses the Go
      fallbacks, go2cpp uses the C++ definitions
- [x] Testcase cpp_call.go (valid Go, deterministic, go run == transpiled)
- [x] Docs: DIFFERENCES.md section, README usage section, SPEC FR12

Also fixed: compound assignments (+=, -=, *=, /=, %=, <<=, >>=, &=, |=, ^=)
were transpiled to invalid C++ (`total + = 5`); now rewritten to
`total = total + 5`.

# Go syntax feature candidates (survey, awaiting approval)

Likely easy: if/switch init statements (C++17 native), make(map[K]V),
new(T), strconv.Itoa, fmt.Sprint, os.Exit, panic -> throw, copy(), struct
tag stripping.

Medium: method declarations (value receivers as injected member functions),
range-over-int (Go 1.22), labeled break/continue via goto, variadic user
functions, bare returns with named results, string byte/rune iteration
semantics, err != nil -> has_value() convention.

Hard/not feasible: select, interface, closures capturing mutable state,
generics, method sets with pointer receivers, embedding, type assertions,
recover.

# Interfaces plan (the last challenge)

- [x] Pre-scan: collect interface declarations (name -> method sigs) and
      struct methods before the line loop; compute which structs implement
      which interfaces (by method-name set)
- [x] type X interface { ... } -> abstract class with pure virtual methods
      and a virtual destructor
- [x] Method declarations func (r Rect) Area() ... -> free function
      Rect_Area(Rect r) + injected `virtual ... override;` member at struct
      close + out-of-line glue `inline double Rect::Area() { return
      Rect_Area(*this); }` (value and pointer receivers)
- [x] Structs that implement interfaces inherit them (class Rect : public Shape)
- [x] Interface-typed variable tracking: var s Shape -> Shape* s (= nullptr
      when uninitialized), s = Rect{...} -> s = new Rect{...}, s = nil ->
      nullptr, s.Area() -> s->Area() for tracked vars, interface-typed
      function parameters -> Shape*
- [x] Testcase interfaces.go (value + pointer receivers, interface params,
      reassignment, nil)
- [x] Docs: DIFFERENCES.md section, README, SPEC FR13

Limitations (documented): same-file scope, single return values on methods,
named method parameters, no empty interface, no type assertions/switches, no
interface-to-interface conversion, printing an interface value shows &{...}
(Go shows the value form), passing concrete structs to interface parameters
requires &var (Go pointer receiver conventions apply in both languages).

# Discovered issues

- [x] Windows/MinGW build is broken: `ioutil.TempFile("", "go2cpp*")` creates an
  extension-less temp file, but MinGW clang++/g++ append `.exe` to the `-o` output
  name when it has no suffix. go2cpp then reads back the empty original temp file
  and writes a 0-byte "executable". Fixed by creating the temp file as
  `go2cpp*.exe` so the driver uses the name as-is (harmless on Linux).
  Verified with MSYS2 UCRT64 clang++ 22.1.4: all 36 testcases produce output
  identical to `go run`.
