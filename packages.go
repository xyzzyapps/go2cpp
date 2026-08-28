package main

// Support for local packages and imports:
//
// * Import declarations are parsed instead of being discarded.
// * An import path is treated as a local package when it resolves to a
//   directory containing .go files, either relative to the enclosing module
//   root (when the path starts with the go.mod module path) or relative to
//   the importing file. Everything else is a standard library import, which
//   is handled by the trigger-based shims.
// * Local packages are transpiled before the importing code. Exported
//   identifiers (functions, types, vars, consts) are prefixed with the
//   package qualifier, so that `utils.Max` call sites, rewritten to
//   `utilsMax`, resolve to the prefixed definitions.
// * The result is assembled as a single translation unit: dependencies
//   first, then the package code, then the main file. The whole-program
//   passes (shims and includes) run once on the combined output.
//
// Limitations (see also DIFFERENCES.md): no import cycles, no dot imports,
// no methods on package types, no unexported identifier access, and types
// must be declared before use in concatenated file order.

import (
	"errors"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// importSpec is a single parsed import declaration.
type importSpec struct {
	alias string // optional alias, empty if none
	path  string // import path as written in the source
}

// lineTransformResult holds the output of the line-based transformation,
// plus the flags that the whole-program passes need.
type lineTransformResult struct {
	output         string
	usePrettyPrint bool
	haveStructs    bool
	cppBlock       string
}

var (
	importLineRe      = regexp.MustCompile(`^(?:(\w+)\s+)?"([^"]+)"$`)
	packageLineRe     = regexp.MustCompile(`^package\s+([A-Za-z_][A-Za-z0-9_]*)`)
	moduleLineRe      = regexp.MustCompile(`^module\s+(\S+)`)
	funcDeclRe        = regexp.MustCompile(`^func\s+([A-Z][A-Za-z0-9_]*)\s*\(`)
	typeDeclRe        = regexp.MustCompile(`^type\s+([A-Z][A-Za-z0-9_]*)\s`)
	varDeclRe         = regexp.MustCompile(`^var\s+([A-Z][A-Za-z0-9_]*)`)
	constDeclRe       = regexp.MustCompile(`^const\s+([A-Z][A-Za-z0-9_]*)`)
	blockNameRe       = regexp.MustCompile(`^([A-Z][A-Za-z0-9_]*)\s*(?:=|$)`)
	qualifierEscapeRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)
)

// parseImports extracts the import specifications from a Go source string.
func parseImports(source string) []importSpec {
	var specs []importSpec
	inImport := false
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inImport {
			if strings.HasPrefix(trimmed, "import") {
				rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "import"))
				if strings.HasPrefix(rest, "(") {
					inImport = true
					continue
				}
				if m := importLineRe.FindStringSubmatch(rest); m != nil {
					specs = append(specs, importSpec{alias: m[1], path: m[2]})
				}
			}
			continue
		}
		if strings.Contains(trimmed, ")") {
			inImport = false
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if m := importLineRe.FindStringSubmatch(trimmed); m != nil {
			specs = append(specs, importSpec{alias: m[1], path: m[2]})
		}
	}
	return specs
}

// findModule walks upwards from startDir until a go.mod is found, and
// returns the module path and the directory containing the go.mod file.
func findModule(startDir string) (string, string, bool) {
	dir := startDir
	for {
		if data, err := ioutil.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if m := moduleLineRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
					return m[1], dir, true
				}
			}
			return "", dir, false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

// isGoPackageDir reports whether the directory exists and contains .go files.
func isGoPackageDir(dir string) bool {
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			return true
		}
	}
	return false
}

// resolvePackageDir maps an import path to a local directory with .go files.
// Module-qualified paths are resolved from the module root, other paths
// relative to the directory of the importing file.
func resolvePackageDir(importPath, fileDir, modulePath, moduleDir string, hasModule bool) (string, bool) {
	if hasModule && modulePath != "" && strings.HasPrefix(importPath, modulePath+"/") {
		rel := strings.TrimPrefix(importPath, modulePath+"/")
		dir := filepath.Join(moduleDir, filepath.FromSlash(rel))
		if isGoPackageDir(dir) {
			return dir, true
		}
	}
	dir := filepath.Join(fileDir, filepath.FromSlash(importPath))
	if isGoPackageDir(dir) {
		return dir, true
	}
	return "", false
}

// exportedIdentifiers scans package sources for top-level exported names.
// Functions and other identifiers (types, vars, consts) are returned
// separately, since they are renamed under different conditions.
func exportedIdentifiers(sources []string) (funcs []string, idents []string) {
	seen := map[string]bool{}
	add := func(name string, isFunc bool) {
		if seen[name] {
			return
		}
		seen[name] = true
		if isFunc {
			funcs = append(funcs, name)
		} else {
			idents = append(idents, name)
		}
	}
	for _, src := range sources {
		inBlock := false
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if inBlock {
				if strings.Contains(trimmed, ")") {
					inBlock = false
					continue
				}
				if m := blockNameRe.FindStringSubmatch(trimmed); m != nil {
					add(m[1], false)
				}
				continue
			}
			if trimmed == "const (" || trimmed == "var (" || trimmed == "type (" {
				inBlock = true
				continue
			}
			if strings.HasPrefix(trimmed, "func (") {
				// Methods are not supported across packages
				continue
			}
			if m := funcDeclRe.FindStringSubmatch(trimmed); m != nil {
				add(m[1], true)
				continue
			}
			for _, re := range []*regexp.Regexp{typeDeclRe, varDeclRe, constDeclRe} {
				if m := re.FindStringSubmatch(trimmed); m != nil {
					add(m[1], false)
					break
				}
			}
		}
	}
	return funcs, idents
}

// renameIdent replaces word-boundary occurrences of id in line with the
// replacement. Occurrences preceded by a dot (field and method accesses)
// are left alone. If requireParen is set, only occurrences followed by an
// opening parenthesis are replaced.
func renameIdent(line, id, replacement string, requireParen bool) string {
	isWord := func(c byte) bool {
		return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	var sb strings.Builder
	i := 0
	for i < len(line) {
		j := strings.Index(line[i:], id)
		if j == -1 {
			sb.WriteString(line[i:])
			break
		}
		start := i + j
		end := start + len(id)
		before := byte(' ')
		if start > 0 {
			before = line[start-1]
		}
		after := byte(' ')
		if end < len(line) {
			after = line[end]
		}
		if isWord(before) || isWord(after) || before == '.' || (requireParen && after != '(') {
			sb.WriteString(line[i:end])
			i = end
			continue
		}
		sb.WriteString(line[i:start])
		sb.WriteString(replacement)
		i = end
	}
	return sb.String()
}

// prefixExported renames the exported identifiers of a package to their
// prefixed versions, so that they match the rewritten call sites in the
// importing code. The package line is dropped and import blocks and method
// declarations are left untouched.
func prefixExported(source string, funcs, idents []string, prefix string) string {
	// Never rename anything inside const cpp raw string constants
	_, cppLines, _ := extractCppBlocks(source)
	lines := strings.Split(source, "\n")
	out := make([]string, 0, len(lines))
	inImport := false
	for i, line := range lines {
		if cppLines[i] {
			out = append(out, line)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "import (" {
			inImport = true
			out = append(out, line)
			continue
		}
		if inImport {
			if strings.Contains(trimmed, ")") {
				inImport = false
			}
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "package ") {
			continue
		}
		if strings.HasPrefix(trimmed, "func (") {
			// Methods are not renamed
			out = append(out, line)
			continue
		}
		newLine := line
		for _, f := range funcs {
			newLine = renameIdent(newLine, f, prefix+f, true)
		}
		for _, id := range idents {
			newLine = renameIdent(newLine, id, prefix+id, false)
		}
		out = append(out, newLine)
	}
	return strings.Join(out, "\n")
}

// rewritePackageQualifiers rewrites qualified call sites `pkg.Ident` to
// `pkgIdent`, using the qualifier map built while transpiling the imports.
func rewritePackageQualifiers(source string, qualifiers map[string]string) string {
	for qualifier, prefix := range qualifiers {
		re := regexp.MustCompile(`\b` + qualifierEscapeRe.ReplaceAllString(qualifier, "_") + `\.([A-Za-z_][A-Za-z0-9_]*)`)
		source = re.ReplaceAllString(source, prefix+"$1")
	}
	return source
}

// transpileLocalImports transpiles all local packages imported by the given
// Go source. It returns the transpiled C++ parts (dependencies first), a map
// from package qualifier to identifier prefix, and whether pretty printing
// or structs were encountered.
func transpileLocalImports(source, baseDir, modulePath, moduleDir string, hasModule bool, depth int, visited map[string]string) ([]string, map[string]string, bool, bool, string, error) {
	specs := parseImports(source)
	var parts []string
	qualifiers := map[string]string{}
	pretty := false
	structs := false
	var cppBlocks []string
	for _, spec := range specs {
		dir, ok := resolvePackageDir(spec.path, baseDir, modulePath, moduleDir, hasModule)
		if !ok {
			// Standard library import, handled by the trigger-based shims
			continue
		}
		cpp, prefix, depPretty, depStructs, depCppBlock, err := transpilePackage(dir, spec.alias, modulePath, moduleDir, hasModule, depth, visited)
		if err != nil {
			return nil, nil, false, false, "", err
		}
		parts = append(parts, cpp)
		pretty = pretty || depPretty
		structs = structs || depStructs
		if depCppBlock != "" {
			cppBlocks = append(cppBlocks, depCppBlock)
		}
		qualifier := spec.alias
		if qualifier == "" || qualifier == "_" {
			// The default qualifier is the package name. It is resolved by
			// the visited map when the package has been imported before.
			qualifier = prefix
		}
		if qualifier == "." {
			continue
		}
		qualifiers[qualifier] = prefix
	}
	return parts, qualifiers, pretty, structs, strings.Join(cppBlocks, "\n"), nil
}

// transpilePackage transpiles one local package directory. The package is
// transpiled only once, even when imported several times; the prefix used
// for its identifiers is returned so that importing code can be rewritten.
func transpilePackage(dir, alias, modulePath, moduleDir string, hasModule bool, depth int, visited map[string]string) (string, string, bool, bool, string, error) {
	if depth > 16 {
		return "", "", false, false, "", errors.New("go2cpp: local package imports nested too deeply (import cycle?)")
	}
	if prefix, seen := visited[dir]; seen {
		return "", prefix, false, false, "", nil
	}
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		return "", "", false, false, "", err
	}
	var goFiles []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		goFiles = append(goFiles, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(goFiles)

	pkgName := ""
	var sources []string
	for _, goFile := range goFiles {
		data, err := ioutil.ReadFile(goFile)
		if err != nil {
			return "", "", false, false, "", err
		}
		src := string(data)
		if pkgName == "" {
			for _, line := range strings.Split(src, "\n") {
				if m := packageLineRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
					pkgName = m[1]
					break
				}
			}
		}
		sources = append(sources, src)
	}
	if pkgName == "" {
		pkgName = filepath.Base(dir)
	}
	prefix := pkgName
	if alias != "" && alias != "_" {
		prefix = alias
	}
	visited[dir] = prefix

	// Transpile the dependencies of this package first
	var depParts []string
	depQualifiers := map[string]string{}
	depPretty := false
	depStructs := false
	var depCppBlocks []string
	for _, src := range sources {
		parts, quals, pretty, structs, block, err := transpileLocalImports(src, dir, modulePath, moduleDir, hasModule, depth+1, visited)
		if err != nil {
			return "", "", false, false, "", err
		}
		depParts = append(depParts, parts...)
		for k, v := range quals {
			depQualifiers[k] = v
		}
		depPretty = depPretty || pretty
		depStructs = depStructs || structs
		if block != "" {
			depCppBlocks = append(depCppBlocks, block)
		}
	}

	funcs, idents := exportedIdentifiers(sources)
	pretty := depPretty
	structs := depStructs
	var outParts []string
	var cppBlocks []string
	outParts = append(outParts, depParts...)
	for _, src := range sources {
		prefixed := prefixExported(src, funcs, idents, prefix)
		res := go2cppLines(prefixed)
		outParts = append(outParts, res.output)
		pretty = pretty || res.usePrettyPrint
		structs = structs || res.haveStructs
		if res.cppBlock != "" {
			cppBlocks = append(cppBlocks, res.cppBlock)
		}
	}
	combined := strings.Join(outParts, "\n")
	combined = rewritePackageQualifiers(combined, depQualifiers)
	cppBlock := strings.Join(cppBlocks, "\n")
	if len(depCppBlocks) > 0 {
		cppBlock = strings.Join(append(depCppBlocks, cppBlock), "\n")
	}
	return combined, prefix, pretty, structs, cppBlock, nil
}

// go2cppFile transpiles a Go file together with any local packages it
// imports. Imported packages are transpiled first and concatenated before
// the main source, keeping everything in a single translation unit. The
// whole-program passes run once on the combined output.
func go2cppFile(inputFilename, source string) (string, error) {
	baseDir := "."
	if inputFilename != "" {
		baseDir = filepath.Dir(inputFilename)
	}
	modulePath, moduleDir, hasModule := findModule(baseDir)

	visited := map[string]string{}
	depParts, qualifiers, depPretty, depStructs, depCppBlock, err := transpileLocalImports(source, baseDir, modulePath, moduleDir, hasModule, 0, visited)
	if err != nil {
		return "", err
	}

	mainResult := go2cppLines(source)
	mainOutput := rewritePackageQualifiers(mainResult.output, qualifiers)

	cppBlock := depCppBlock
	if mainResult.cppBlock != "" {
		if cppBlock != "" {
			cppBlock += "\n"
		}
		cppBlock += mainResult.cppBlock
	}

	res := lineTransformResult{
		output:         strings.Join(append(depParts, mainOutput), "\n"),
		usePrettyPrint: mainResult.usePrettyPrint || depPretty,
		haveStructs:    mainResult.haveStructs || depStructs,
		cppBlock:       cppBlock,
	}
	return finishCpp(res), nil
}
