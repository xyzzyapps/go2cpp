package main

// Plan:
// 1. Read in the source code
// 2. Convert it to C++20
// 3. Compile it

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const cppHasStdFormat = false

const versionString = "xyproto/go2cpp 0.4.0"

const tupleType = "std::tuple"

const (
	hashMapSuffix = "_h__"
	keysSuffix    = "_k__"
	switchPrefix  = "_s__"
	labelPrefix   = "_l__"
	deferPrefix   = "_d__"
)

var includeMap = map[string]string{
	"std::tuple":                       "tuple",
	"std::endl":                        "iostream",
	"std::cout":                        "iostream",
	"std::string":                      "string",
	"std::size":                        "iterator",
	"std::unordered_map":               "unordered_map",
	"std::hash":                        "functional",
	"std::size_t":                      "cstddef",
	"std::int8_t":                      "cinttypes",
	"std::int16_t":                     "cinttypes",
	"std::int32_t":                     "cinttypes",
	"std::int64_t":                     "cinttypes",
	"std::uint8_t":                     "cinttypes",
	"std::uint16_t":                    "cinttypes",
	"std::uint32_t":                    "cinttypes",
	"std::uint64_t":                    "cinttypes",
	"printf":                           "cstdio",
	"fprintf":                          "cstdio",
	"sprintf":                          "cstdio",
	"snprintf":                         "cstdio",
	"std::stringstream":                "sstream",
	"std::is_pointer":                  "type_traits",
	"std::experimental::is_detected_v": "experimental/type_traits",
	"std::shared_ptr":                  "memory",
	"std::nullopt":                     "optional",
	"EXIT_SUCCESS":                     "cstdlib",
	"EXIT_FAILURE":                     "cstdlib",
	"std::vector":                      "vector",
	"std::unique_ptr":                  "memory",
	"std::runtime_error":               "stdexcept",
	"std::regex_replace":               "regex",
	"std::regex_constants":             "regex",
	"std::to_string":                   "string",
	"std::ostringstream":               "sstream",
	"std::true_type":                   "type_traits",
	"std::false_type":                  "type_traits",
	"std::decay_t":                     "type_traits",
	"std::declval":                     "utility",
	"std::thread":                      "thread",
	"std::this_thread":                 "thread",
	"std::chrono":                      "chrono",
	"std::deque":                       "deque",
	"std::mutex":                       "mutex",
	"std::unique_lock":                 "mutex",
	"std::lock_guard":                  "mutex",
	"std::condition_variable":          "condition_variable",
	"std::pair":                        "utility",
	"std::move":                        "utility",
	"std::copy":                        "algorithm",
	"std::min":                         "algorithm",
	"exit(":                            "cstdlib",
	// TODO: complex64, complex128
}

var endings = []string{"{", ",", "}", ":"}

// C++ function definitions inside a const cpp block: name(args) { or
// name(args) -> rettype {
var cppFuncDeclRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*\(([^;{}()]*)\)\s*(?:->\s*[\w:<>&*\s]+)?\{`)

// extractCppBlocks finds package-level `const cpp = ` + raw string
// constants. It returns the combined C++ code, the set of line indices the
// constants occupy (which are skipped during the line transformation), and
// the names of the functions declared in the C++ code. Go functions with
// these names are not transpiled; the C++ definitions are used instead.
func extractCppBlocks(source string) (string, map[int]bool, []string) {
	var code strings.Builder
	cppLines := map[int]bool{}
	lines := strings.Split(source, "\n")
	inBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, "const cpp =") || strings.HasPrefix(trimmed, "const cpp=") {
				cppLines[i] = true
				if idx := strings.Index(line, "`"); idx >= 0 {
					rest := line[idx+1:]
					if end := strings.Index(rest, "`"); end >= 0 {
						code.WriteString(rest[:end])
						code.WriteString("\n")
					} else {
						code.WriteString(rest)
						code.WriteString("\n")
						inBlock = true
					}
				}
			}
			continue
		}
		// Inside the raw string constant
		cppLines[i] = true
		if idx := strings.Index(line, "`"); idx >= 0 {
			code.WriteString(line[:idx])
			code.WriteString("\n")
			inBlock = false
		} else {
			code.WriteString(line)
			code.WriteString("\n")
		}
	}
	codeStr := code.String()
	var funcNames []string
	if codeStr != "" {
		seen := map[string]bool{}
		for _, m := range cppFuncDeclRe.FindAllStringSubmatch(codeStr, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				funcNames = append(funcNames, m[1])
			}
		}
	}
	return codeStr, cppLines, funcNames
}

// functionDeclName returns the name of the function declared on a line
// starting with "func", or an empty string for methods and other lines.
func functionDeclName(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "func ") {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "func "))
	if strings.HasPrefix(rest, "(") || !strings.Contains(rest, "(") {
		// Method receiver or malformed line
		return ""
	}
	return strings.TrimSpace(rest[:strings.Index(rest, "(")])
}

// spliceCppBlock inserts the raw C++ code from const cpp blocks right after
// the include block of the generated program, so that it is defined before
// both the shims and the transpiled code, and is never processed by the
// whole-program passes.
func spliceCppBlock(output, cppBlock string) string {
	if cppBlock == "" {
		return output
	}
	lines := strings.Split(output, "\n")
	idx := 0
	for idx < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[idx]), "#include") {
		idx++
	}
	parts := append([]string{}, lines[:idx]...)
	parts = append(parts, strings.Split(strings.TrimRight(cppBlock, "\n"), "\n")...)
	parts = append(parts, lines[idx:]...)
	return strings.Join(parts, "\n")
}

var (
	switchExpressionCounter = -1
	firstCase               bool
	switchLabel             string
	labelCounter            int
	iotaNumber              int    // used for simple increases of iota constants
	iotaFirstLine           bool   // true until the first line of a const block has been processed
	lastConstExpr           string // right-hand side of the previous const line, for implicit repetition
	deferCounter            int
	unfinishedDeferFunction bool
)

// Go slice expressions like s[a:b], s[a:], s[:b] and s[:] on identifiers
var sliceExpression = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\[([^\[\]:]*):([^\[\]:]*)\]`)

// Go append with a spread argument, after the append rename: _go2cpp_append(x, y...)
var appendSpreadExpression = regexp.MustCompile(`_go2cpp_append\(([^,()]+), ([^,()]+?)\.\.\.\)`)

// Go channel receive on an identifier: <-ch
var channelReceiveExpression = regexp.MustCompile(`<-\s*([A-Za-z_][A-Za-z0-9_]*)`)

// The word "iota" inside a const expression
var iotaWord = regexp.MustCompile(`\biota\b`)

// RewriteSlices rewrites Go slice expressions, append with a spread argument
// and channel receives to calls to the C++ shim functions.
func RewriteSlices(source string) string {
	source = appendSpreadExpression.ReplaceAllString(source, "_go2cpp_appendSpread($1, $2)")
	source = channelReceiveExpression.ReplaceAllString(source, "$1.recv()")
	return sliceExpression.ReplaceAllStringFunc(source, func(match string) string {
		parts := sliceExpression.FindStringSubmatch(match)
		name, from, to := parts[1], strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3])
		if from == "" {
			from = "0"
		}
		if to == "" {
			to = name + ".size()"
		}
		return "_go2cpp_slice(" + name + ", " + from + ", " + to + ")"
	})
}

// between returns the string between two given strings, or the original string
// firstA specifies if the first or last instance of a should be used
// firstB specifies if the first or last instance of b should be used
func between(s, a, b string, lastA, lastB bool) string {
	var apos int
	if lastA {
		apos = strings.LastIndex(s, a)
	} else {
		apos = strings.Index(s, a)
	}
	if apos == -1 {
		return s
	}
	var bpos int
	if lastB {
		bpos = strings.LastIndex(s, b)
	} else {
		bpos = strings.Index(s, b)
	}
	if bpos == -1 {
		return s
	}
	if bpos < apos {
		return s[bpos+len(b) : apos]
	}
	return s[apos+len(a) : bpos]
}

// leftBetween searches from the left and returns the first string that is
// between a and b.
func leftBetween(s, a, b string) string {
	return between(s, a, b, false, false)
}

// like leftBetween, but use the rightmost instance of b
func leftBetweenRightmost(s, a, b string) string {
	return between(s, a, b, false, true)
}

// greedyBetween searches from the left for a then
// searches as far as possible for b, before returning
// the string that is between a and b.
func greedyBetween(s, a, b string) string {
	return between(s, a, b, false, true)
}

// TODO: Make more robust, this easily breaks
func LiteralStrings(source string) string {

	// Temporary
	return source

	//output := source
	//replacements := map[string]string{
	//	"\")":  "\"s)",
	//	"\";":  "\"s;",
	//	"\",":  "\"s,",
	//	"\"}":  "\"s}",
	//	"\" }": "\"s }",
	//	"\" )": "\"s )",
	//	"\":":  "\"s:",
	//}
	//hasLiteral := false
	//for k, v := range replacements {
	//	if strings.Contains(output, k) {
	//		output = strings.Replace(output, k, v, -1)
	//		hasLiteral = true
	//	}
	//}
	//if hasLiteral {
	//	output = "\nusing namespace std::string_literals;\n" + output
	//}
	//return output
}

func initializeVariables(sourceCode string) string {
	varTypes := []string{
		"int", "double", "float", "char \\*", "const char \\*",
		"std::string", "uint64_t", "uint32_t", "uint16_t", "uint8_t",
		"int64_t", "int32_t", "int16_t", "int8_t", "unsigned int",
	}

	for _, varType := range varTypes {
		re := regexp.MustCompile(varType + ` ([a-zA-Z_][a-zA-Z0-9_]*)\s*;`)
		switch varType {
		case "int", "float", "double", "uint64_t", "uint32_t", "uint16_t", "uint8_t", "int64_t", "int32_t", "int16_t", "int8_t", "unsigned int":
			sourceCode = re.ReplaceAllString(sourceCode, varType+" $1 = 0;")
		case "char \\*", "const char \\*":
			sourceCode = re.ReplaceAllString(sourceCode, varType+" $1 = nullptr;")
		case "std::string":
			sourceCode = re.ReplaceAllString(sourceCode, varType+" $1 = \"\";")
		}
	}
	return sourceCode
}

// TODO: Avoid whole-program replacements, if possible
func WholeProgramReplace(source string) (output string) {
	output = source
	// TODO: Add these in a smarter way, with more supported types
	replacements := map[string]string{
		" string ":         " " + TypeReplace("string") + " ",
		"(string ":         "(" + TypeReplace("string") + " ",
		"return string":    "return std::to_string",
		"make([]string, ":  "std::vector<" + TypeReplace("string") + "> (",
		"make([]int, ":     "std::vector<" + TypeReplace("int") + "> (",
		"make([]uint, ":    "std::vector<" + TypeReplace("uint") + "> (",
		"make([]float64, ": "std::vector<" + TypeReplace("float64") + "> (",
		"make([]float32, ": "std::vector<" + TypeReplace("float32") + "> (",
		"-> []string":      "-> std::vector<" + TypeReplace("string") + ">",
		"-> []int":         "-> std::vector<" + TypeReplace("int") + ">",
		"= nil)":           "= std::nullopt)",
		"fmt.Sprintf(":     "fmtSprintf(",
		"fmt.Sprint(":      "fmtSprint(",
		"os.Exit(":         "exit(",
		"time.Sleep(":      "timeSleep(",
		"time.Second":      "1000",
		"time.Millisecond": "1",
		" append(":         " _go2cpp_append(",
		"\nappend(":        "\n_go2cpp_append(",
		" copy(":           " _go2cpp_copy(",
		"\ncopy(":          "\n_go2cpp_copy(",
	}
	for k, v := range replacements {
		output = strings.Replace(output, k, v, -1)
	}
	return initializeVariables(output)
}

// ShimCode associates a trigger string with C++ code that is prepended to
// the generated program when the trigger string is found in it.
type ShimCode struct {
	trigger string
	code    string
}

func AddFunctions(source string, useFormatOutput, haveStructs bool) (output string) {

	// The shims are checked in order. Shims that call other shims must come
	// after the shims they depend on, since the code of each shim is scanned
	// by the triggers of the shims that come after it.

	output = source

	simpleFormatOutput := `template <typename T> void _format_output(std::ostream& out, T x)
{
    if constexpr (std::is_same<T, bool>::value) {
        out << std::boolalpha << x << std::noboolalpha;
    } else if constexpr (std::is_integral<T>::value) {
        out << static_cast<int>(x);
    } else {
        out << x;
    }
}`

	shims := []ShimCode{
		{trigger: "_format_output", code: `template<typename T>
using _str_t = decltype( std::declval<T&>()._str() );

template<typename T>
using _p_str_t = decltype( std::declval<T&>()->_str() );

template <typename T> void _format_output(std::ostream& out, T x)
{
    if constexpr (std::is_same<T, bool>::value) {
        out << std::boolalpha << x << std::noboolalpha;
    } else if constexpr (std::is_integral<T>::value) {
        out << static_cast<int>(x);
    } else if constexpr (std::is_object<T>::value && !std::is_pointer<T>::value && std::experimental::is_detected_v<_str_t, T>) {
        out << x._str();
    } else if constexpr (std::is_object<T>::value && std::is_pointer<T>::value && std::experimental::is_detected_v<_p_str_t, T>) {
        out << "&" << x->_str();
    } else {
        out << x;
    }
}`},
		{trigger: "strconv.Itoa", code: `inline auto strconvItoa(int x) -> std::string { return std::to_string(x); }`},
		{trigger: "strconv.ParseFloat", code: `using error = std::optional<std::string>;
auto strconvParseFloat(std::string s, int bitSize) -> std::tuple<double, error> {
	try {
		return std::tuple { std::stod(s), std::nullopt };
	} catch (const std::invalid_argument& ia) {
		return std::tuple { 0.0, std::optional { "invalid argument" } };
	}
}
`},
		{trigger: "strconv.ParseInt", code: `using error = std::optional<std::string>;
auto strconvParseInt(std::string s, int base, int bitSize) -> std::tuple<int, error> {
	try {
		return std::tuple { std::stoi(s), std::nullopt };
	} catch (const std::invalid_argument& ia) {
		return std::tuple { 0.0, std::optional { "invalid argument" } };
	}
}
`},
		{trigger: "strings.Contains", code: `inline auto stringsContains(std::string const& haystack, std::string const& needle) -> bool { return haystack.find(needle) != std::string::npos; }`},
		{trigger: "strings.HasPrefix", code: `inline auto stringsHasPrefix(std::string const& haystack, std::string const& prefix) -> auto { return 0 == haystack.find(prefix); }`},
		{trigger: "strings.HasSuffix", code: `inline auto stringsHasSuffix(std::string const& haystack, std::string const& suffix) -> bool { return haystack.size() >= suffix.size() && 0 == haystack.compare(haystack.size() - suffix.size(), suffix.size(), suffix); }`},
		{trigger: "strings.Index", code: `inline auto stringsIndex(std::string const& haystack, std::string const& needle) -> int { std::size_t pos = haystack.find(needle); return pos == std::string::npos ? -1 : static_cast<int>(pos); }`},
		{trigger: "strings.Join", code: `inline auto stringsJoin(std::vector<std::string> const& elems, std::string const& sep) -> std::string { std::string out; for (std::size_t i = 0; i < elems.size(); ++i) { if (i > 0) { out += sep; } out += elems[i]; } return out; }`},
		{trigger: "strings.ReplaceAll", code: `inline auto stringsReplaceAll(std::string s, std::string const& from, std::string const& to) -> std::string { if (from.empty()) { return s; } std::size_t pos = 0; while ((pos = s.find(from, pos)) != std::string::npos) { s.replace(pos, from.size(), to); pos += to.size(); } return s; }`},
		{trigger: "strings.Replace", code: `inline auto stringsReplace(std::string s, std::string const& from, std::string const& to, int count) -> std::string { if (count < 0) { count = static_cast<int>(s.size()) + 1; } if (from.empty()) { return s; } std::size_t pos = 0; for (int i = 0; i < count; ++i) { pos = s.find(from, pos); if (pos == std::string::npos) { break; } s.replace(pos, from.size(), to); pos += to.size(); } return s; }`},
		{trigger: "strings.SplitN", code: `inline auto stringsSplitN(std::string const& s, std::string const& sep, int n) -> std::vector<std::string> { std::vector<std::string> out; if (n == 0) { return out; } if (n < 0 || sep.empty()) { if (sep.empty()) { for (char c : s) { out.push_back(std::string(1, c)); } if (n < 0) { return out; } } else { std::size_t start = 0; while (true) { std::size_t pos = s.find(sep, start); if (pos == std::string::npos) { out.push_back(s.substr(start)); break; } out.push_back(s.substr(start, pos - start)); start = pos + sep.size(); } return out; } } std::size_t start = 0; while (static_cast<int>(out.size()) < n - 1) { std::size_t pos = s.find(sep, start); if (pos == std::string::npos) { break; } out.push_back(s.substr(start, pos - start)); start = pos + sep.size(); } out.push_back(s.substr(start)); return out; }`},
		{trigger: "strings.Split", code: `inline auto stringsSplit(std::string const& s, std::string const& sep) -> std::vector<std::string> { std::vector<std::string> out; if (sep.empty()) { for (char c : s) { out.push_back(std::string(1, c)); } return out; } std::size_t start = 0; while (true) { std::size_t pos = s.find(sep, start); if (pos == std::string::npos) { out.push_back(s.substr(start)); break; } out.push_back(s.substr(start, pos - start)); start = pos + sep.size(); } return out; }`},
		{trigger: "strings.TrimSpace", code: `inline auto stringsTrimSpace(std::string const& s) -> std::string { std::string news {}; for (auto l : s) { if (l != ' ' && l != '\n' && l != '\t' && l != '\v' && l != '\f' && l != '\r') { news += l; } } return news; }`},
		{trigger: "fmtSprint", code: `template <typename... Args>
std::string fmtSprint(Args... args) {
    std::string result;
    bool prevWasString = false;
    auto appendOne = [&](auto&& x) {
        using U = std::decay_t<decltype(x)>;
        bool isStr = std::is_same<U, const char*>::value || std::is_same<U, std::string>::value;
        if (!result.empty() && !isStr && !prevWasString) { result += " "; }
        result += fmtSprintf("%v", x);
        prevWasString = isStr;
    };
    (appendOne(args), ...);
    return result;
}`},
		{trigger: "fmtSprint", code: `template <typename T> auto _fmt_has_str(int) -> decltype(std::declval<T&>()._str(), std::true_type {});
template <typename T> auto _fmt_has_str(long) -> std::false_type;

template <typename T>
std::string _fmt_format_arg(char verb, T x) {
    std::ostringstream ss;
    ss << std::boolalpha;
    if (verb == 'q') {
        ss << '"';
    }
    if constexpr (decltype(_fmt_has_str<T>(0))::value) {
        ss << x._str();
    } else if (verb == 'x') {
        if constexpr (std::is_integral<T>::value && !std::is_same<T, bool>::value) {
            ss << std::hex << static_cast<long long>(x);
        } else {
            ss << x;
        }
    } else if constexpr (std::is_integral<T>::value && !std::is_same<T, bool>::value && (sizeof(T) < sizeof(int))) {
        ss << static_cast<int>(x);
    } else {
        ss << x;
    }
    if (verb == 'q') {
        ss << '"';
    }
    return ss.str();
}

template <typename... Args>
std::string fmtSprintf(std::string const& fmt, Args... args) {
    std::string result;
    std::size_t pos = 0;
    auto flushLiterals = [&](bool stopAtVerb) {
        while (pos < fmt.size()) {
            char c = fmt[pos];
            if (c == '%') {
                if (pos + 1 < fmt.size() && fmt[pos + 1] == '%') { result += '%'; pos += 2; continue; }
                if (stopAtVerb && pos + 1 < fmt.size()) {
                    char verb = fmt[pos + 1];
                    if (verb == 'v' || verb == 's' || verb == 'd' || verb == 'f' || verb == 't' || verb == 'x' || verb == 'g' || verb == 'q') {
                        return true;
                    }
                }
                result += c;
                pos += 1;
                continue;
            }
            result += c;
            pos += 1;
        }
        return false;
    };
    auto consumeArg = [&](auto&& x) {
        if (flushLiterals(true)) {
            char verb = fmt[pos + 1];
            result += _fmt_format_arg(verb, x);
            pos += 2;
        }
    };
    (consumeArg(args), ...);
    flushLiterals(false);
    return result;
}`},
		{trigger: "_go2cpp_slice", code: `template <typename C>
auto _go2cpp_slice(C const& c, std::size_t from, std::size_t to) -> C {
    if (to > c.size()) { to = c.size(); }
    if (from > to) { from = to; }
    return C(c.begin() + from, c.begin() + to);
}`},
		{trigger: "_go2cpp_append", code: `template <typename C, typename... A>
auto _go2cpp_append(C c, A... args) -> C {
    (c.push_back(args), ...);
    return c;
}
template <typename C>
auto _go2cpp_appendSpread(C c, C const& more) -> C {
    for (auto const& x : more) { c.push_back(x); }
    return c;
}`},
		{trigger: "_go2cpp_copy", code: `template <typename D, typename S>
auto _go2cpp_copy(D& dst, S const& src) -> int {
    auto n = std::min(dst.size(), src.size());
    std::copy(src.begin(), src.begin() + n, dst.begin());
    return static_cast<int>(n);
}`},
		{trigger: "cap(", code: `template <typename C>
auto cap(C const& c) -> int { return static_cast<int>(c.capacity()); }`},
		{trigger: "gochan", code: `template <typename T>
class gochan {
    std::deque<T> buf;
    std::size_t cap_;
    bool closed_ = false;
    std::mutex m;
    std::condition_variable cv;

public:
    explicit gochan(std::size_t capacity) : cap_(capacity < 1 ? 1 : capacity) {}
    void send(T v) {
        std::unique_lock<std::mutex> lk(m);
        cv.wait(lk, [&] { return buf.size() < cap_ || closed_; });
        if (closed_) { return; }
        buf.push_back(std::move(v));
        cv.notify_all();
    }
    T recv() {
        std::unique_lock<std::mutex> lk(m);
        cv.wait(lk, [&] { return !buf.empty() || closed_; });
        if (buf.empty()) { return T{}; }
        T v = std::move(buf.front());
        buf.pop_front();
        cv.notify_all();
        return v;
    }
    std::pair<T, bool> recv2() {
        std::unique_lock<std::mutex> lk(m);
        cv.wait(lk, [&] { return !buf.empty() || closed_; });
        if (buf.empty()) { return std::pair<T, bool> { T {}, false }; }
        T v = std::move(buf.front());
        buf.pop_front();
        cv.notify_all();
        return std::pair<T, bool> { v, true };
    }
    void close() {
        std::lock_guard<std::mutex> lk(m);
        closed_ = true;
        cv.notify_all();
    }
};`},
		{trigger: "timeSleep", code: `inline auto timeSleep(int ms) -> void { std::this_thread::sleep_for(std::chrono::milliseconds(ms)); }`},
		{trigger: "len", code: `
template <typename T>
inline auto len(T x) -> int { return x.size(); }
`},
	}
	for _, shim := range shims {
		code := shim.code
		if shim.trigger == "_format_output" && useFormatOutput && !haveStructs {
			code = simpleFormatOutput
		}
		if strings.Contains(output, shim.trigger) {
			output = strings.Replace(output, shim.trigger, strings.Replace(shim.trigger, ".", "", -1), -1)
			output = code + "\n" + output
		}
	}
	return output
}

// Name and type is used to keep a variable name and a variable type
type NameAndType struct {
	name string
	typ  string
}

// FunctionArguments transforms the arguments given to a function
func FunctionArguments(source string) string {
	namesAndTypes := make([]NameAndType, 0)
	// First find all names and all types
	currentType := ""
	currentName := ""
	splitted := strings.Split(source, ",")
	for i := len(splitted) - 1; i >= 0; i-- {
		nameAndMaybeType := strings.TrimSpace(splitted[i])
		if strings.Contains(nameAndMaybeType, " ") {
			nameAndType := strings.Split(nameAndMaybeType, " ")
			currentType = TypeReplace(strings.Join(nameAndType[1:], " "))
			currentName = nameAndType[0]
		} else {
			currentName = nameAndMaybeType
		}
		namesAndTypes = append(namesAndTypes, NameAndType{currentName, currentType})
		//fmt.Println("NAME: " + currentName + ", TYPE: " + currentType)
	}
	cppSignature := ""
	for i := len(namesAndTypes) - 1; i >= 0; i-- {
		//fmt.Println(namesAndTypes[i])
		cppSignature += namesAndTypes[i].typ + " " + namesAndTypes[i].name
		if i > 0 {
			cppSignature += ", "
		}
	}
	return strings.TrimSpace(cppSignature)
}

// FunctionRetvals transforms the return values from a function
func FunctionRetvals(source string) (output string) {
	if len(strings.TrimSpace(source)) == 0 {
		return source
	}
	output = source
	if strings.Contains(output, "(") {
		s := greedyBetween(output, "(", ")")
		retvals := FunctionArguments(s)
		if strings.Contains(retvals, ",") {
			output = "(" + retvals + ")"
		} else {
			output = retvals
		}
	}
	return strings.TrimSpace(output)
}

// CPPTypes picks out the types given a list of C++ arguments with name and type
func CPPTypes(args string) string {
	words := strings.Split(leftBetween(args, "(", ")"), ",")
	var atypes []string
	for _, word := range words {
		elems := strings.Split(strings.TrimSpace(word), " ")
		t := TypeReplace(elems[0])
		atypes = append(atypes, t)
	}
	return strings.Join(atypes, ", ")
}

// FunctionSignature transforms a function signature that spans one line
// Will change the "func main" signature to a main function that returns an int.
func FunctionSignature(source string) (output, returntype, name string) {
	if len(strings.TrimSpace(source)) == 0 {
		return source, "", ""
	}
	output = source
	args := FunctionArguments(leftBetween(output, "(", ")"))
	// Has return values in a parenthesis
	var rets string
	if strings.Contains(output, ") (") {
		// There is a parenthesis with return types in the function signature
		rets = FunctionRetvals(between(output, ")", "{", false, true))
	} else {
		// There is not a parenthesis with return types in the function signature
		rets = FunctionRetvals(between(output, ")", "{", true, true))
	}
	if strings.Contains(rets, ",") {
		// Multiple return
		rets = tupleType + "<" + CPPTypes(rets) + ">"
	} else if strings.TrimSpace(rets) != "" {
		// Single return type
		rets = TypeReplace(rets)
	}
	name = leftBetween(output, "func ", "(")
	if name == "main" {
		rets = "int"
	}
	if len(strings.TrimSpace(rets)) == 0 {
		rets = "void"
	}
	output = "auto " + name + "(" + args + ") -> " + rets + " {"
	return strings.TrimSpace(output), rets, name
}

// Split arguments. Handles quoting 1 level deep.
func SplitArgs(s string) []string {
	inQuote := false
	inSingleQuote := false
	inPar := false
	inCurly := false
	var args []string
	word := ""
	for _, letter := range s {
		switch letter {
		case '"':
			inQuote = !inQuote
		case '\'':
			inSingleQuote = !inSingleQuote
		}
		if letter == '(' && !inQuote && !inSingleQuote && !inPar && !inCurly {
			inPar = true
		}
		if letter == ')' && !inQuote && !inSingleQuote {
			inPar = false
		}
		if letter == '{' && !inQuote && !inSingleQuote && !inPar && !inCurly {
			inCurly = true
		}
		if letter == '}' && !inQuote && !inSingleQuote {
			inCurly = false
		}
		if letter == ',' && !inQuote && !inSingleQuote && !inPar && !inCurly {
			args = append(args, strings.TrimSpace(word))
			word = ""
		} else {
			word += string(letter)
		}
	}
	args = append(args, strings.TrimSpace(word))
	return args
}

// stripSingleLineComment will strip away trailing single-line comments
func stripSingleLineComment(line string) string {
	commentMarker := "//"
	if strings.Count(line, commentMarker) == 1 {
		p := strings.Index(line, commentMarker)
		return strings.TrimSpace(line[:p])
	}
	return line
}

// Will return the transformed string, and a bool if pretty printing may be needed
func PrintStatement(source string) (string, bool) {

	// Pick out and trim all arguments given to the print functon
	args := SplitArgs(greedyBetween(strings.TrimSpace(source), "(", ")"))

	// Identify the print function
	if !strings.Contains(source, "(") {
		// Not a function call
		return source, false
	}

	fname := strings.TrimSpace(source[:strings.Index(source, "(")])
	// fmt.Sprint returns a string and is not a print statement
	if fname == "fmt.Sprint" {
		return source, false
	}
	//fmt.Println("FNAME", fname)

	// Check if the function call ends with "ln" (println, fmt.Println)
	addNewline := strings.HasSuffix(fname, "ln")
	//fmt.Println("NEWLINE", addNewline)

	// Check if the function call starts with "print" (as opposed to "Print")
	lowercasePrint := strings.HasPrefix(fname, "print")
	//fmt.Println("LOWERCASE PRINT", lowercasePrint)

	// Check if all the arguments are literal strings
	allLiteralStrings := true
	for _, arg := range args {
		if !strings.HasPrefix(arg, "\"") {
			allLiteralStrings = false
		}
	}

	// Check if all the arguments are literal numbers
	allLiteralNumbers := true
	for _, arg := range args {
		if !isNum(arg) {
			allLiteralNumbers = false
		}
	}

	mayNeedPrettyPrint := !allLiteralStrings || !allLiteralNumbers

	// --- enough information gathered, it's time to build the output code ---

	if strings.HasSuffix(fname, "rintf") {
		output := source
		// fmt.Printf prints the formatted string to stdout, fmt.Sprintf
		// returns it. The variadic fmtSprintf shim formats the arguments.
		// fmt.Fprintf with FILE handles falls back to C fprintf.
		output = strings.Replace(output, "fmt.Printf", "std::cout << fmtSprintf", 1)
		output = strings.Replace(output, "fmt.Fprintf", "fprintf", 1)
		output = strings.Replace(output, "fmt.Sprintf", "fmtSprintf", 1)
		return output, mayNeedPrettyPrint
	}

	outputName := "std::cout"
	if lowercasePrint {
		// print and println outputs to stderr
		outputName = "std::cerr"
	}
	//fmt.Println("OUTPUT NAME", outputName)

	// Useful values
	pipe := " << "
	blank := "\" \""
	nl := "std::endl"

	// Silence pipeNewline if the print function does not end with "ln"
	pipeNewline := pipe + nl
	if !addNewline {
		pipeNewline = ""
	}

	// No arguments given?
	if len(args) == 0 {
		// Just output a newline
		if addNewline {
			return outputName + pipeNewline, false
		}
	}

	// Only one argument given?
	if len(args) == 1 {
		if strings.TrimSpace(args[0]) == "" {
			// Just output a newline
			if addNewline {
				return outputName + pipeNewline, false
			}
		}
		if allLiteralStrings || allLiteralNumbers {
			return outputName + pipe + args[0] + pipeNewline, false
		}
		output := "_format_output(" + outputName + ", " + args[0] + ")"
		if addNewline {
			output += ";\n" + outputName + pipeNewline
		}
		return output, true
	}

	// Several arguments given
	//fmt.Println("SEVERAL ARGUMENTS", args)

	// HINT: Almost everything should start with "pipe" and almost nothing should end with "pipe"
	output := outputName
	lastIndex := len(args) - 1
	for i, arg := range args {
		//fmt.Println("ARGUMENT", i, arg)
		if strings.HasPrefix(arg, "\"") {
			// Literal string
			output += pipe + arg
		} else if isNum(arg) {
			// Literal number
			output += pipe + arg
		} else {
			if i == 0 {
				output = ""
			} else {
				output += ";\n"
			}
			output += "_format_output(" + outputName + ", " + arg + ");\n" + outputName
		}
		if i < lastIndex {
			output += pipe + blank
		} else {
			output += pipeNewline
		}
	}

	//fmt.Println("GENERATED OUTPUT", output)

	return output, mayNeedPrettyPrint
}

func AddIncludes(source string) (output string) {
	output = source

	includeString := ""
	for k, v := range includeMap {
		if strings.Contains(output, k) {
			newInclude := "#include <" + v + ">\n"
			if !strings.Contains(includeString, newInclude) {
				includeString += newInclude
			}

		}
	}
	if cppHasStdFormat {
		//"std::format":                      "format",
		k := "std::format"
		v := "format"
		if strings.Contains(output, k) {
			newInclude := "#include <" + v + ">\n"
			if !strings.Contains(includeString, newInclude) {
				includeString += newInclude
			}
		}
	}
	return includeString + "\n" + output
}

func after(keyword, line string) string {
	pos := strings.Index(line, keyword)
	if pos == -1 {
		return line
	}
	return line[pos+len(keyword):]
}

func DeferCall(source string) string {
	trimmed := strings.TrimSpace(after("defer", source))

	// This function handles three possibilities:
	// * defer f()
	// * defer func() { asdf }();
	// * "defer func() {" and then later "}()"

	if strings.HasPrefix(trimmed, "func() {") && strings.HasSuffix(trimmed, "}()") {
		// Anonymous function, on one line
		deferCounter++
		trimmed = strings.TrimSpace(leftBetween(trimmed, "func() {", "}()"))
		// TODO: let go2cpp() return pure source code + includes to place at the top, not just one large string
		return "// " + trimmed + "\nstd::shared_ptr<void> _defer" + strconv.Itoa(deferCounter) + "(nullptr, [](...) { " + go2cpp(trimmed) + "; });"
	} else if trimmed == "func() {" {
		// Anonymous function, on multiple lines
		deferCounter++
		unfinishedDeferFunction = true // output "});" later on, when "}()" is encountered in the Go code
		return "// " + trimmed + "\nstd::shared_ptr<void> _defer" + strconv.Itoa(deferCounter) + "(nullptr, [](...) { "
	} else {
		// Assume a regular function call
		return "// " + trimmed + "\nstd::shared_ptr<void> _defer" + strconv.Itoa(deferCounter) + "(nullptr, [](...) { " + trimmed + "; });"
	}
}

// convertInitStatement rewrites a Go if/switch init part `x := expr` to
// the C++17 init statement `auto x = expr`. Multiple variables become a
// structured binding: a, b := f() -> auto [a, b] = f().
func convertInitStatement(initPart string) string {
	initPart = strings.TrimSpace(initPart)
	idx := strings.Index(initPart, ":=")
	if idx < 0 {
		return initPart
	}
	names := strings.TrimSpace(initPart[:idx])
	rest := strings.TrimSpace(initPart[idx+2:])
	if names == "" || strings.ContainsAny(names, "([{") {
		return initPart
	}
	if strings.Contains(names, ",") {
		var clean []string
		for _, n := range strings.Split(names, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				return initPart
			}
			clean = append(clean, n)
		}
		return "auto [" + strings.Join(clean, ", ") + "] = " + rest
	}
	return "auto " + names + " = " + rest
}

// IfSentence transforms an if statement line. The C++17 init statement
// syntax makes Go if-init statements transpile directly:
// if v := f(); v > 2 { -> if (auto v = f(); v > 2) {
func IfSentence(source string) (output string) {
	output = source
	expression := strings.TrimSpace(leftBetweenRightmost(source, "if", "{"))
	if idx := strings.Index(expression, ";"); idx >= 0 {
		initPart := strings.TrimSpace(expression[:idx])
		if strings.Contains(initPart, ":=") {
			expression = convertInitStatement(initPart) + "; " + strings.TrimSpace(expression[idx+1:])
		}
	}
	return "if (" + expression + ") {"
}

func ElseIfSentence(source string) (output string) {
	output = source
	expression := strings.TrimSpace(leftBetweenRightmost(source, "} else if", "{"))
	if idx := strings.Index(expression, ";"); idx >= 0 {
		initPart := strings.TrimSpace(expression[:idx])
		if strings.Contains(initPart, ":=") {
			expression = convertInitStatement(initPart) + "; " + strings.TrimSpace(expression[idx+1:])
		}
	}
	return "} else if (" + expression + ") {"
}

func TypeReplace(source string) string {
	// TODO: uintptr, complex64 and complex128
	trimmed := strings.TrimSpace(source)
	// For pointer types, move the star
	if strings.HasPrefix(trimmed, "*") {
		trimmed = trimmed[1:] + "*"
	}
	switch trimmed {
	case "string":
		return "std::string"
	case "float64":
		return "double"
	case "float32":
		return "float"
	case "uint64":
		return "std::uint64_t"
	case "uint32":
		return "std::uint32_t"
	case "uint16":
		return "std::uint16_t"
	case "uint8":
		return "std::uint8_t"
	case "int64":
		return "std::int64_t"
	case "int32":
		return "std::int32_t"
	case "int16":
		return "std::int16_t"
	case "int8":
		return "std::int8_t"
	case "byte":
		return "std::uint8_t"
	case "rune":
		return "std::int32_t"
	case "uint":
		return "unsigned int"
	default:
		if _, isIface := scanInterfaces[trimmed]; isIface {
			// Interface types are used through pointers
			return trimmed + "*"
		}
		if strings.HasPrefix(trimmed, "chan ") {
			return "gochan<" + TypeReplace(trimmed[5:]) + ">"
		}
		if strings.HasPrefix(trimmed, "[]") {
			innerType := trimmed[2:]
			return "std::vector<" + TypeReplace(innerType) + ">"
		}
		return trimmed
	}
}

// TODO: Make sure all variations are covered:
// * [_] for _ = range list {
// * [_] for i := range list {
// * [_] for i, v := range list {
// * [_] for _, v := range list {
// * [_] for i, _ := range list {
// * [_] for _, _ = range list {
// * [_] for _ = range map {
// * [_] for k := range map {
// * [_] for k, v := range map {
// * [_] for _, v := range map {
// * [_] for k, _ := range map {
// * [_] for _, _ = range map {
// * [_] for i := 0; i < 10; i++ {
// * [_] for {
// * [_] for ;; {
// * not possible: for x := range 10 {
func ForLoop(source string, encounteredHashMaps []string) string {
	expression := strings.TrimSpace(leftBetween(source, "for", "{"))
	if expression == "" {
		// endless loop
		return "for (;;) {"
	}
	// for range, with no comma
	if strings.Count(expression, ",") == 0 && strings.Contains(expression, "range") {
		fields := strings.Split(expression, " ")
		varName := fields[0]
		listName := fields[len(fields)-1]

		// for i := range l {
		// -->
		// for (auto i = 0; i < std::size(l); i++) {

		hashMapName := listName
		if has(encounteredHashMaps, hashMapName) {
			// looping over the key of a hash map, not over the index of a list
			return "for (const auto & [" + varName + ", " + varName + "__" + "] : " + hashMapName + ") {"
		} else if varName == "_" {
			return "for (const auto & [" + varName + "__" + ", " + varName + "___" + "] : " + hashMapName + ") {"
		} else {
			// looping over the index of a list
			return "for (std::size_t " + varName + " = 0; " + varName + " < std::size(" + listName + "); " + varName + "++) {"
		}
	}
	// for range, over index and element, or key and value
	if strings.Count(expression, ",") == 1 && strings.Contains(expression, "range") && strings.Contains(expression, ":=") {
		fields := strings.Split(expression, ":=")
		varnames := strings.Split(fields[0], ",")

		indexvar := varnames[0]
		elemvar := varnames[1]

		fields = strings.Split(expression, " ")
		listName := fields[len(fields)-1]
		hashMapName := listName

		if has(encounteredHashMaps, hashMapName) {
			if indexvar == "_" {
				// looping over the values of a hash map
				hashMapHashKey := hashMapName + hashMapSuffix
				return "for (const auto & " + hashMapHashKey + " : " + hashMapName + ") {" + "\n" + "auto " + elemvar + " = " + hashMapHashKey + ".second"
			}
			// for k, v := range m
			keyvar := indexvar
			//hashMapHashKey := keyvar + hashMapSuffix + keysSuffix
			return "for (const auto & [" + keyvar + ", " + elemvar + "] : " + hashMapName + ") {"
			//return "for (auto " + hashMapHashKey + " : " + hashMapName + keysSuffix + ") {" + "\n" + "auto " + keyvar + " = " + hashMapHashKey + ".second;\nauto " + elemvar + " = " + hashMapName + ".at(" + hashMapHashKey + ".first)"
		}

		if indexvar == "_" {
			return "for (auto " + elemvar + " : " + listName + ") {"
		}
		return "for (std::size_t " + indexvar + " = 0; " + indexvar + " < std::size(" + listName + "); " + indexvar + "++) {" + "\n" + "auto " + elemvar + " = " + listName + "[" + indexvar + "]"
	}
	// not "for" + "range"
	if strings.Contains(expression, ":=") {
		if strings.HasPrefix(expression, "_,") && strings.Contains(expression, "range") {
			// For each, no index
			varname := leftBetween(expression, ",", ":")
			fields := strings.SplitN(expression, "range ", 2)
			listname := fields[1]
			// C++11 and later for each loop
			expression = "auto &" + varname + " : " + listname
		} else {
			expression = "auto " + strings.Replace(expression, ":=", "=", 1)
		}
	}
	return "for (" + expression + ") {"
}

func SwitchExpressionVariable() string {
	return switchPrefix + strconv.Itoa(switchExpressionCounter)
}

func LabelName() string {
	return labelPrefix + strconv.Itoa(labelCounter)
}

func Switch(source string) (output string) {
	output = strings.TrimSpace(source)[len("switch "):]
	if strings.HasSuffix(output, "{") {
		output = strings.TrimSpace(output[:len(output)-1])
	}
	// Switch with init statement: switch n := f(); n {
	initPart := ""
	if idx := strings.Index(output, ";"); idx >= 0 {
		initCandidate := strings.TrimSpace(output[:idx])
		if strings.Contains(initCandidate, ":=") {
			initPart = convertInitStatement(initCandidate)
			output = strings.TrimSpace(output[idx+1:])
		}
	}
	switchExpressionCounter++
	firstCase = true
	result := ""
	if initPart != "" {
		result = initPart + ";\n"
	}
	return result + "auto&& " + SwitchExpressionVariable() + " = " + output + "; // switch on " + output
}

func Case(source string) (output string) {
	output = source
	s := leftBetween(output, " ", ":")
	if firstCase {
		firstCase = false
		output = "if ("
	} else {
		output = "} else if ("
	}
	output += SwitchExpressionVariable() + " == " + s + ") { // case " + s
	if switchLabel != "" {
		output += "\n" + switchLabel + ":"
		switchLabel = ""
	}
	return output
}

// Return transformed line and the variable names
func VarDeclarations(source string) (string, []string) {

	// TODO: This is an ugly function. Refactor.

	if strings.Contains(source, "=") {
		parts := strings.SplitN(strings.TrimSpace(source), "=", 2)
		left := parts[0]
		right := strings.TrimSpace(parts[1])
		fields := strings.Split(strings.TrimSpace(left), " ")
		if fields[0] == "var" {
			fields = fields[1:]
		}
		if len(fields) == 2 {
			declared := TypeReplace(fields[1]) + " " + fields[0] + " = "
			if _, isIface := scanInterfaces[fields[1]]; isIface {
				// Interface values are pointers to heap allocations
				if ctor := newIfImplementingLiteral(right); ctor != "" {
					return declared + ctor, []string{fields[0]}
				}
			}
			return declared + right, []string{fields[0]}
		} else if len(fields) > 2 {
			if strings.Contains(source, ",") {
				leftFields := strings.Fields(left)
				if leftFields[0] == "var" {
					leftFields = leftFields[1:]
				}
				rightFields := strings.Fields(right)
				if len(leftFields)-1 != len(rightFields) {
					panic("var declaration has mismatching number of variables and values: " + left + " VS " + right)
				}
				lastIndex := len(leftFields) - 1
				varType := leftFields[lastIndex]
				var sb strings.Builder
				var varNames []string

				for i, varName := range leftFields[:lastIndex] {
					if i > 0 {
						sb.WriteString(";")
					}
					if strings.HasSuffix(varName, ",") {
						varName = varName[:len(varName)-1]
					}
					varValue := rightFields[i]
					if strings.HasSuffix(varValue, ",") {
						varValue = varValue[:len(varValue)-1]
					}
					sb.WriteString(TypeReplace(varType) + " " + varName + " = " + varValue)
					varNames = append(varNames, varName)
				}
				return sb.String(), varNames
			}
			return TypeReplace(fields[1]) + " " + fields[0] + " " + strings.Join(fields[2:], " ") + " = " + right, []string{fields[0]}
		}
		leftFields := strings.Fields(left)
		if leftFields[0] == "var" {
			leftFields = leftFields[1:]
		}
		if len(leftFields) > 1 {
			panic("unsupported var declaration: " + source)
		}

		varName := leftFields[0]
		varType := right
		varValue := ""
		if strings.Contains(source, "=") {
			varType = "auto"
			lastIndex := len(leftFields) - 1
			if len(leftFields) > 1 && !strings.Contains(leftFields[lastIndex], ",") {
				varType = leftFields[lastIndex]
				//leftFields = leftFields[:lastIndex-1]
			}
			varValue = right
		}

		withBracket := false
		if strings.HasSuffix(right, "{") {
			varType = strings.TrimSpace(right[:len(right)-1])
			withBracket = true
		}

		varValue = strings.TrimPrefix(varValue, varType)

		/*
			fmt.Println("source:", source)
			fmt.Println("varType:", varType)
			fmt.Println("varName:", varName)
			fmt.Println("varValue:", varValue)
			fmt.Println("withBracket:", withBracket)
		*/

		s := TypeReplace(varType) + " " + varName + " = " + varValue
		if withBracket {
			if !strings.HasSuffix(s, "{") {
				s += "{"
			}
		} else {
			if !strings.Contains(source, "`") {
				// Only add a semicolon if it's not a multiline string and not an opening bracket
				s += ";"
			}
		}
		return s, []string{varName}
	}
	fields := strings.Fields(source)
	if fields[0] == "var" {
		fields = fields[1:]
	}
	if len(fields) == 2 {
		if _, isIface := scanInterfaces[fields[1]]; isIface {
			return TypeReplace(fields[1]) + " " + fields[0] + " = nullptr", []string{fields[0]}
		}
		return TypeReplace(fields[1]) + " " + fields[0], []string{fields[0]}
	}
	if strings.Contains(source, ",") {
		// Comma separated variable names, with one common variable type,
		// and no value assignment
		lastIndex := len(fields) - 1
		varType := fields[lastIndex]
		var sb strings.Builder
		var varNames []string

		for i, varName := range fields[:lastIndex] {
			if i > 0 {
				sb.WriteString(";")
			}
			if strings.HasSuffix(varName, ",") {
				varName = varName[:len(varName)-1]
			}
			sb.WriteString(TypeReplace(varType) + " " + varName)
			varNames = append(varNames, varName)
		}

		return sb.String(), varNames
	}

	// Unrecognized
	panic("Unrecognized var declaration: " + source)

}

// TypeDeclaration returns a transformed string (from Go to C++),
// and a bool if a struct is opened (with {).
func TypeDeclaration(source string) (string, bool) {
	fields := strings.Split(strings.TrimSpace(source), " ")
	if fields[0] == "type" {
		fields = fields[1:]
	}
	left := strings.TrimSpace(fields[0])
	right := strings.TrimSpace(fields[1])
	words := strings.Split(left, " ")
	if len(fields) == 2 {
		// Type alias
		return "using " + left + " = " + TypeReplace(right), false
	} else if len(words) == 2 {
		// Type alias
		return "using " + words[1] + " " + words[0] + " = " + TypeReplace(right), false
	} else if strings.Contains(right, "struct") {
		// type Vec3 struct {
		// to
		// class Vec3 { public:
		// also the closing bracket must end with a semicolon
		return "class " + left + " { public:", true
	} else if len(words) == 1 {
		// Type alias
		return "using " + left + " = " + TypeReplace(right), false
	}
	// Unrecognized
	panic("Unrecognized type declaration: " + source)
}

// ConstDeclaration transforms a const declaration line from Go to C++.
// inBlock is true when the line is part of a "const (" block, where each
// line has an implicit iota index (starting at 0 for the first line of the
// block) and the expression of the previous line is repeated implicitly
// when no value is given.
func ConstDeclaration(source string, inBlock bool) (output string) {
	if inBlock {
		// Each const line in the block increments iota by one
		if iotaFirstLine {
			iotaFirstLine = false
		} else {
			iotaNumber++
		}
	}
	output = source
	fields := strings.SplitN(source, "=", 2)
	if len(fields) == 1 {
		// Only a constant name, with no value assigned
		name := strings.TrimSpace(fields[0])
		if inBlock && lastConstExpr != "" {
			return "const auto " + name + " = " + iotaSubstitute(lastConstExpr)
		}
		return "const auto " + name + " = " + strconv.Itoa(iotaNumber)
	}
	left := strings.TrimSpace(fields[0])
	right := strings.TrimSpace(fields[1])
	if inBlock {
		lastConstExpr = right
	}
	if strings.Contains(right, "iota") {
		if !inBlock {
			// A standalone const line has iota set to 0
			iotaNumber = 0
		}
		right = iotaSubstitute(right)
	}
	words := strings.Split(left, " ")
	if len(words) == 1 {
		// No type
		return "const auto " + left + " = " + right
	} else if len(words) == 2 {
		if words[0] == "const" {
			return "const auto " + words[1] + " = " + right
		}
		return "const " + TypeReplace(words[1]) + " " + words[0] + " = " + right
	}
	// Unrecognized
	panic("go2cpp: unrecognized const expression: " + source)
}

// iotaSubstitute replaces the word "iota" in the given expression with the
// current iota number.
func iotaSubstitute(expression string) string {
	return iotaWord.ReplaceAllString(expression, strconv.Itoa(iotaNumber))
}

// HashElements transforms the contents of a map in Go to the contents of an unordered_map in C++
// keyType is the type of the key, in C++, for instance "std::string"
// if keyForBoth is true, a hash(key)->key map is created,
// if not, a hash(key)->value map is created.
// This will not work for multiline hash map initializations.
// TODO: Handle keys and values that look like this: "\": \"" (containing quotes, a colon and a space)
func HashElements(source, keyType string, keyForBoth bool) string {
	// Check if the given source line contains either a separating or a trailing comma
	if !strings.Contains(source, ",") {
		return source
	}
	// Check if there is only one pair
	if strings.Count(source, ": ") == 1 {
		pairElements := strings.SplitN(source, ": ", 2)
		if len(pairElements) != 2 {
			panic("This should be two elements, separated by a colon and a space " + source)
		}
		return "{ " + strings.TrimSpace(pairElements[0]) + ", " + strings.TrimSpace(pairElements[1]) + " }, "
	}
	// Multiple pairs
	pairs := strings.Split(source, ",")
	output := "{"
	first := true
	for _, pair := range pairs {
		if !first {
			output += ","
		} else {
			first = false
		}
		pairElements := strings.SplitN(pair, ": ", 2)
		//fmt.Println("HASH ELEMENTS", source)
		//fmt.Println("HASH ELEMENTS", pairs)
		if len(pairElements) != 2 {
			panic("This should be two elements, separated by a colon and a space: " + pair)
		}
		output += "{ " + strings.TrimSpace(pairElements[0]) + ", " + strings.TrimSpace(pairElements[1]) + " }"
	}
	return output + "}"
}

func CreateStrMethod(varNames []string) string {
	var sb strings.Builder
	sb.WriteString("std::string _str() {\n")
	sb.WriteString("  std::stringstream ss;\n")
	sb.WriteString("  ss << \"{\";\n")
	for i, varName := range varNames {
		if i > 0 {
			sb.WriteString("  ss << \" \";\n")
		}
		sb.WriteString("  _format_output(ss, ")
		sb.WriteString(varName)
		sb.WriteString(");\n")
	}
	sb.WriteString("  ss << \"}\";")
	sb.WriteString("  return ss.str();\n")
	sb.WriteString("}\n")
	return sb.String()
}

// --- Interface support ---
// The pre-scan collects interface declarations and struct methods before the
// line transformation, and computes which structs implement which interfaces
// (by method-name set).

type cppMethodSig struct {
	name   string
	params string // Go syntax parameters as written
	ret    string // Go syntax return type, empty for none
}

var (
	scanInterfaces    = map[string][]cppMethodSig{}  // interface name -> methods
	structMethodNames = map[string]map[string]bool{} // struct type -> method names
	implementedIface  = map[string][]string{}        // struct type -> interface names
	structFieldsCpp   = map[string][]cppField{}      // struct type -> fields in declaration order
)

type cppField struct {
	name    string
	cpptype string
}

// implementingStructNames returns the sorted names of all structs that
// implement at least one interface.
func implementingStructNames() []string {
	var names []string
	for name := range implementedIface {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// zeroValueFor returns a C++ zero value expression for the given type.
func zeroValueFor(cppType string) string {
	if strings.HasSuffix(cppType, "*") {
		return "nullptr"
	}
	if cppType == "std::string" {
		return "std::string()"
	}
	if cppType == "bool" {
		return "false"
	}
	return "0"
}

// isIdentByte reports whether the byte can be part of an identifier.
func isIdentByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

var structNamedFieldRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*:`)

// buildConstructorArgs converts the contents of a struct literal to a C++
// constructor argument list, using the field declaration order.
func buildConstructorArgs(structName, inner string) string {
	fields := structFieldsCpp[structName]
	if len(fields) == 0 {
		return ""
	}
	inner = strings.TrimSpace(inner)
	if inner == "" {
		var args []string
		for _, f := range fields {
			args = append(args, zeroValueFor(f.cpptype))
		}
		return strings.Join(args, ", ")
	}
	parts := SplitArgs(inner)
	named := true
	values := map[string]string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		m := structNamedFieldRe.FindStringSubmatch(part)
		if m == nil {
			named = false
			break
		}
		values[m[1]] = strings.TrimSpace(part[len(m[0]):])
	}
	var args []string
	if named {
		for _, f := range fields {
			if v, ok := values[f.name]; ok {
				args = append(args, v)
			} else {
				args = append(args, zeroValueFor(f.cpptype))
			}
		}
	} else {
		for i, part := range parts {
			if i >= len(fields) {
				break
			}
			args = append(args, strings.TrimSpace(part))
		}
	}
	return strings.Join(args, ", ")
}

// rewriteStructLiterals rewrites struct literals of interface-implementing
// structs (which are non-aggregates due to inheritance) to constructor
// calls: Rect{W: 2, H: 3} -> Rect(2, 3).
func rewriteStructLiterals(line string) string {
	for _, t := range implementingStructNames() {
		needle := t + "{"
		for {
			idx := -1
			for start := 0; start+len(needle) <= len(line); start++ {
				if line[start:start+len(needle)] == needle && (start == 0 || !isIdentByte(line[start-1])) {
					idx = start
					break
				}
			}
			if idx < 0 {
				break
			}
			open := idx + len(needle) - 1
			depth := 0
			end := -1
			for j := open; j < len(line); j++ {
				if line[j] == '{' {
					depth++
				} else if line[j] == '}' {
					depth--
					if depth == 0 {
						end = j
						break
					}
				}
			}
			if end < 0 {
				break
			}
			inner := line[open+1 : end]
			args := buildConstructorArgs(t, inner)
			line = line[:idx] + t + "(" + args + ")" + line[end+1:]
		}
	}
	return line
}

// newIfImplementingLiteral returns "new T(args)" when the right-hand side is
// a struct literal of an interface-implementing struct, otherwise "".
func newIfImplementingLiteral(right string) string {
	for _, t := range implementingStructNames() {
		if strings.HasPrefix(right, t+"{") {
			return "new " + t + "(" + buildConstructorArgs(t, leftBetween(right, "{", "}")) + ")"
		}
	}
	return ""
}

// constructorFor generates a constructor for an interface-implementing
// struct, since inheritance makes it a non-aggregate.
func constructorFor(structName string) string {
	fields := structFieldsCpp[structName]
	if len(fields) == 0 {
		return ""
	}
	var params, inits []string
	for _, f := range fields {
		params = append(params, f.cpptype+" "+f.name)
		inits = append(inits, f.name+"("+f.name+")")
	}
	return structName + "(" + strings.Join(params, ", ") + ") : " + strings.Join(inits, ", ") + " {}"
}

// scanTypeDecls scans the source for interface declarations and methods.
func scanTypeDecls(source string, cppLines map[int]bool) {
	scanInterfaces = map[string][]cppMethodSig{}
	structMethodNames = map[string]map[string]bool{}
	implementedIface = map[string][]string{}
	structFieldsCpp = map[string][]cppField{}

	lines := strings.Split(source, "\n")
	currentIface := ""
	var currentIfaceMethods []cppMethodSig
	currentStruct := ""
	for i, line := range lines {
		if cppLines[i] {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if currentStruct != "" {
			// Collect the fields of a struct, in declaration order
			if trimmed == "}" {
				currentStruct = ""
				continue
			}
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			idx := strings.IndexAny(trimmed, " \t")
			if idx < 0 {
				continue
			}
			namePart := trimmed[:idx]
			typePart := strings.TrimSpace(trimmed[idx+1:])
			if bt := strings.Index(typePart, "`"); bt >= 0 {
				// Strip struct field tags: Name string `json:"name"`
				typePart = strings.TrimSpace(typePart[:bt])
			}
			cppType := TypeReplace(typePart)
			for _, n := range strings.Split(namePart, ",") {
				n = strings.TrimSpace(n)
				if n != "" {
					structFieldsCpp[currentStruct] = append(structFieldsCpp[currentStruct], cppField{n, cppType})
				}
			}
			continue
		}
		if currentIface == "" {
			if strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, " struct {") {
				currentStruct = typeName(trimmed)
			} else if strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, " interface {") {
				currentIface = typeName(trimmed)
			}
			continue
		}
		if trimmed == "}" {
			if currentIface != "" && len(currentIfaceMethods) > 0 {
				scanInterfaces[currentIface] = currentIfaceMethods
			}
			currentIface = ""
			currentIfaceMethods = nil
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || !strings.Contains(trimmed, "(") {
			continue
		}
		name := strings.TrimSpace(leftBetween(trimmed, "", "("))
		params := leftBetween(trimmed, "(", ")")
		ret := ""
		if idx := strings.LastIndex(trimmed, ")"); idx >= 0 {
			ret = strings.TrimSpace(trimmed[idx+1:])
		}
		if name != "" {
			currentIfaceMethods = append(currentIfaceMethods, cppMethodSig{name, params, ret})
		}
	}
	for i, line := range lines {
		if cppLines[i] {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "func (") {
			continue
		}
		_, recvType, methodName, _, _, _ := parseMethodDecl(trimmed)
		if recvType == "" || methodName == "" {
			continue
		}
		recvType = strings.TrimPrefix(recvType, "*")
		if structMethodNames[recvType] == nil {
			structMethodNames[recvType] = map[string]bool{}
		}
		structMethodNames[recvType][methodName] = true
	}
	for iface, methods := range scanInterfaces {
		for structName, methodSet := range structMethodNames {
			allPresent := true
			for _, m := range methods {
				if !methodSet[m.name] {
					allPresent = false
					break
				}
			}
			if allPresent && len(methods) > 0 {
				implementedIface[structName] = append(implementedIface[structName], iface)
			}
		}
	}
}

// typeName extracts the type name from `type X interface {` / `type X struct {`.
func typeName(trimmed string) string {
	fields := strings.Split(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimmed), "{")), " ")
	if len(fields) >= 2 {
		return fields[1]
	}
	return ""
}

// parseMethodDecl parses `func (r Rect) Area(a int) int {`.
func parseMethodDecl(trimmed string) (recvName, recvType, methodName, params, ret string, isPtr bool) {
	inner := leftBetween(trimmed, "(", ")")
	restIdx := strings.Index(trimmed, ")")
	rest := ""
	if restIdx >= 0 {
		rest = strings.TrimSpace(trimmed[restIdx+1:])
	}
	innerWords := strings.Split(strings.TrimSpace(inner), " ")
	if len(innerWords) >= 2 {
		recvName = strings.TrimSpace(innerWords[0])
		recvType = strings.TrimSpace(innerWords[len(innerWords)-1])
		if strings.HasPrefix(recvType, "*") {
			isPtr = true
			recvType = strings.TrimPrefix(recvType, "*")
		}
	}
	if pidx := strings.Index(rest, "("); pidx >= 0 {
		methodName = strings.TrimSpace(rest[:pidx])
		params = leftBetween(rest, "(", ")")
		if lastIdx := strings.LastIndex(rest, ")"); lastIdx >= 0 {
			ret = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest[lastIdx+1:]), "{"))
		}
	}
	return
}

// wrapIfStructLiteral wraps a struct literal in new when its type implements
// an interface, so that it can be assigned to an interface pointer.
func wrapIfStructLiteral(right string) string {
	for structName := range implementedIface {
		if strings.HasPrefix(right, structName+"{") {
			return "new " + right
		}
	}
	return right
}

func go2cppLines(source string) lineTransformResult {
	functionVarMap := map[string]string{} // variable names encountered in the function so far, and their corresponding smart names
	inMultilineString := false
	debugOutput := false
	lines := []string{}
	currentReturnType := ""
	currentFunctionName := ""
	inImport := false
	inVar := false
	inType := false
	inConst := false
	inHashMap := false
	inGoFunction := false
	inInterface := false
	currentStructName := ""
	glueLines := []string{}
	var interfaceVars []string
	hashKeyType := ""
	curlyCount := 0
	// Keep track of encountered hash maps
	// TODO: Use reflection instead to loop either one way or the other. The hash map may be defined in another package.
	encounteredHashMaps := []string{}
	// Keep track of encountered struct names
	encounteredStructNames := []string{}
	inStruct := false
	usePrettyPrint := false
	closingBracketNeedsASemicolon := false
	cppCode, cppLines, cppFuncNames := extractCppBlocks(source)
	scanTypeDecls(source, cppLines)
	ifaceMethodNames := []string{}
	for _, ms := range scanInterfaces {
		for _, m := range ms {
			if !has(ifaceMethodNames, m.name) {
				ifaceMethodNames = append(ifaceMethodNames, m.name)
			}
		}
	}
	skipUntilDepth := -1
	for i, line := range strings.Split(source, "\n") {
		if cppLines[i] {
			// Part of a const cpp raw string constant, emitted verbatim later
			continue
		}

		if debugOutput {
			fmt.Fprintf(os.Stderr, "%s\n", line)
		}

		newLine := line

		trimmedLine := stripSingleLineComment(strings.TrimSpace(line))

		// TODO: A multiline string could have lines starting with //, make sure to support this
		if strings.HasPrefix(trimmedLine, "//") {
			lines = append(lines, trimmedLine)
			continue
		}
		if strings.HasSuffix(trimmedLine, ";") {
			trimmedLine = trimmedLine[:len(trimmedLine)-1]
		}
		if len(trimmedLine) == 0 {
			lines = append(lines, newLine)
			continue
		}
		// Keep track of how deep we are into curly brackets
		curlyCount += (strings.Count(trimmedLine, "{") - strings.Count(trimmedLine, "}"))
		if skipUntilDepth >= 0 {
			// Inside a Go function that is replaced by a C++ definition from
			// a const cpp block. Skip lines until the function is closed.
			if curlyCount < skipUntilDepth {
				skipUntilDepth = -1
			}
			continue
		}
		if inImport && strings.Contains(trimmedLine, ")") {
			inImport = false
			continue
		} else if inImport {
			continue
		} else if inVar && strings.Contains(trimmedLine, ")") {
			inVar = false
			continue
		} else if inType && strings.Contains(trimmedLine, ")") {
			inType = false
			continue
		} else if inConst && strings.Contains(trimmedLine, ")") {
			inConst = false
			continue
		} else if inHashMap && trimmedLine == "}" {
			inHashMap = false
			newLine = trimmedLine + ";"
		} else if inVar || (inStruct && trimmedLine != "}") {
			if inStruct {
				// Strip struct field tags: Name string `json:"name"`
				if bt := strings.Index(trimmedLine, "`"); bt >= 0 {
					if bt2 := strings.Index(trimmedLine[bt+1:], "`"); bt2 >= 0 {
						trimmedLine = strings.TrimSpace(trimmedLine[:bt] + trimmedLine[bt+bt2+2:])
					}
				}
			}
			var varNames []string
			newLine, varNames = VarDeclarations(trimmedLine)
			if strings.HasSuffix(newLine, "{") {
				closingBracketNeedsASemicolon = true
			}

			if inStruct {
				// Gathering variable names from this struct
				encounteredStructNames = append(encounteredStructNames, varNames...)
			}
		} else if inType {
			prevInStruct := inStruct
			newLine, inStruct = TypeDeclaration(trimmedLine)
			if !prevInStruct && inStruct {
				// Entering struct, reset the slice that is used to gather variable names
				encounteredStructNames = []string{}
			}
		} else if inConst {
			newLine = ConstDeclaration(line, true)
		} else if inHashMap && !inMultilineString {
			newLine = HashElements(trimmedLine, hashKeyType, false)
		} else if inInterface && trimmedLine == "}" {
			newLine = "};"
			inInterface = false
		} else if inInterface {
			// Interface method: Area() float64 -> virtual double Area() = 0;
			name := strings.TrimSpace(leftBetween(trimmedLine, "", "("))
			params := leftBetween(trimmedLine, "(", ")")
			ret := ""
			if idx := strings.LastIndex(trimmedLine, ")"); idx >= 0 {
				ret = strings.TrimSpace(trimmedLine[idx+1:])
			}
			retCpp := "void"
			if ret != "" {
				retCpp = TypeReplace(ret)
			}
			newLine = "virtual " + retCpp + " " + name + "(" + FunctionArguments(params) + ") = 0;"
		} else if strings.HasPrefix(trimmedLine, "func (") {
			// Method declaration: func (r Rect) Area() float64 {
			recvName, recvType, methodName, goParams, goRet, isPtr := parseMethodDecl(trimmedLine)
			freeName := recvType + "_" + methodName
			recvGo := recvName + " " + recvType
			if isPtr {
				recvGo = recvName + " *" + recvType
			}
			allParams := recvGo
			if goParams != "" {
				allParams = recvGo + ", " + goParams
			}
			retCpp := "void"
			if goRet != "" {
				retCpp = TypeReplace(goRet)
			}
			newLine = "auto " + freeName + "(" + FunctionArguments(allParams) + ") -> " + retCpp + " {"
			// Out-of-line member definition, appended after the transpiled body
			forwardArgs := "*this"
			if isPtr {
				forwardArgs = "this"
			}
			forwardOK := true
			memberParams := ""
			if goParams != "" {
				var paramNames []string
				for _, part := range strings.Split(goParams, ",") {
					words := strings.Fields(strings.TrimSpace(part))
					if len(words) < 2 {
						forwardOK = false
						break
					}
					paramNames = append(paramNames, words[len(words)-1])
				}
				if forwardOK {
					memberParams = FunctionArguments(goParams)
					for _, pn := range paramNames {
						forwardArgs += ", " + pn
					}
				}
			}
			if forwardOK {
				glueLines = append(glueLines, "inline "+retCpp+" "+recvType+"::"+methodName+"("+memberParams+") { return "+freeName+"("+forwardArgs+"); }")
			}
			functionVarMap = map[string]string{}
			interfaceVars = nil
			if goParams != "" {
				for _, part := range strings.Split(goParams, ",") {
					words := strings.Fields(strings.TrimSpace(part))
					if len(words) >= 2 {
						if _, isIface := scanInterfaces[words[len(words)-1]]; isIface {
							interfaceVars = append(interfaceVars, words[0])
						}
					}
				}
			}
		} else if strings.HasPrefix(trimmedLine, "func") {
			if name := functionDeclName(trimmedLine); name != "" && has(cppFuncNames, name) {
				// This function is replaced by a C++ definition from a
				// const cpp block. Skip it entirely.
				if strings.Count(trimmedLine, "{") == strings.Count(trimmedLine, "}") {
					continue
				}
				skipUntilDepth = curlyCount
				continue
			}
			functionVarMap = map[string]string{}
			interfaceVars = nil
			if goArgs := leftBetween(trimmedLine, "(", ")"); goArgs != "" {
				for _, part := range strings.Split(goArgs, ",") {
					words := strings.Fields(strings.TrimSpace(part))
					if len(words) >= 2 {
						if _, isIface := scanInterfaces[words[len(words)-1]]; isIface {
							interfaceVars = append(interfaceVars, words[0])
						}
					}
				}
			}
			newLine, currentReturnType, currentFunctionName = FunctionSignature(trimmedLine)
		} else if strings.HasPrefix(trimmedLine, "for") {
			newLine = ForLoop(line, encounteredHashMaps)
		} else if strings.HasPrefix(trimmedLine, "switch") {
			newLine = Switch(line)
		} else if strings.HasPrefix(trimmedLine, "case") {
			newLine = Case(line)
		} else if strings.HasPrefix(trimmedLine, "return") {
			if strings.HasPrefix(currentReturnType, tupleType) {
				elems := strings.SplitN(newLine, "return ", 2)
				newLine = "return " + currentReturnType + "{" + elems[1] + "};"
				//} else {
				// Just use the standard tuple
			}
			if strings.HasSuffix(strings.TrimSpace(newLine), "}") {
				// Struct literal returns end with a brace, which would otherwise
				// suppress the trailing semicolon
				newLine = strings.TrimSpace(newLine) + ";"
			}
		} else if strings.HasPrefix(trimmedLine, "fmt.Print") || strings.HasPrefix(trimmedLine, "print") {
			// _ is if "pretty print" functionality may be needed, for non-literal strings and numbers
			var pp bool
			newLine, pp = PrintStatement(trimmedLine)
			if pp {
				usePrettyPrint = true
			}
		} else if strings.Contains(trimmedLine, "=") && !strings.HasPrefix(trimmedLine, "var ") && !strings.HasPrefix(trimmedLine, "if ") && !strings.HasPrefix(trimmedLine, "const ") && !strings.HasPrefix(trimmedLine, "type ") && !strings.HasPrefix(trimmedLine, "} else if ") {
			elem := strings.SplitN(trimmedLine, "=", 2)
			left := strings.TrimSpace(elem[0])
			declarationAssignment := false
			if strings.HasSuffix(left, ":") {
				declarationAssignment = true
				left = left[:len(left)-1]
			}
			right := strings.TrimSpace(elem[1])
			if strings.HasPrefix(right, "<-") {
				// Channel receive: v := <-ch or v, ok := <-ch
				chanName := strings.TrimSpace(strings.TrimPrefix(right, "<-"))
				if strings.Contains(left, ",") {
					right = chanName + ".recv2()"
				} else {
					right = chanName + ".recv()"
				}
			}
			if strings.HasPrefix(right, "&") && strings.Contains(right, "{") && strings.Contains(right, "}") {
				right = "new " + right[1:]
			}
			if strings.HasPrefix(right, "\"") && strings.HasSuffix(right, "\"") && !strings.Contains(right, "\" + \"") {
				// A string literal on the right side becomes a std::string,
				// so that methods like size() and slicing work on it.
				right = "std::string(" + right + ")"
			}
			if strings.Contains(left, ",") {
				if strings.Contains(left, "_") {
					varNames := strings.Split(left, ",")
					for _, name := range varNames {
						name = strings.TrimSpace(name)
						if value, found := functionVarMap[name]; found {
							// The key already exists, update the value
							if value == name {
								// Add a "0"
								functionVarMap[name] = value + "0"
							} else {
								// Increase the number in the current value by 1
								num := trailingNumber(value)
								num++
								functionVarMap[name] = name + strconv.Itoa(num)
							}
						} else {
							// The key does not exist, just add the name as it is
							functionVarMap[name] = name
						}
					}
					// The "varInFunction" map should now have been updated correctly, so use that

					useVarNames := []string{}
					for _, name := range varNames {
						name = strings.TrimSpace(name)
						useVarNames = append(useVarNames, functionVarMap[name])
					}
					newLine = "auto [" + strings.Join(useVarNames, ", ") + "] = " + right
					//fmt.Println("function var map:", functionVarMap)
					//panic(newLine)
				} else {
					newLine = "auto [" + left + "] = " + right
				}
			} else if declarationAssignment {
				if strings.HasPrefix(right, "make(chan") {
					// ch := make(chan T) or ch := make(chan T, n)
					hashName := strings.TrimSpace(left)
					inner := strings.TrimSpace(after("make(", right))
					inner = strings.TrimSpace(strings.TrimSuffix(inner, ")"))
					inner = strings.TrimSpace(strings.TrimPrefix(inner, "chan"))
					capacity := "0"
					elemType := inner
					if strings.Contains(inner, ",") {
						typeAndCap := strings.SplitN(inner, ",", 2)
						elemType = strings.TrimSpace(typeAndCap[0])
						capacity = strings.TrimSpace(typeAndCap[1])
					}
					newLine = "auto " + hashName + " = gochan<" + TypeReplace(elemType) + ">(" + capacity + ")"
				} else if strings.HasPrefix(right, "make(map") {
					// m := make(map[K]V)
					hashName := strings.TrimSpace(left)
					inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(after("make(", right)), ")"))
					inner = strings.TrimSpace(strings.TrimPrefix(inner, "map"))
					keyType := ""
					valueType := ""
					if strings.HasPrefix(inner, "[") {
						if idx := strings.Index(inner, "]"); idx >= 0 {
							keyType = TypeReplace(strings.TrimSpace(inner[1:idx]))
							valueType = TypeReplace(strings.TrimSpace(inner[idx+1:]))
						}
					}
					encounteredHashMaps = append(encounteredHashMaps, hashName)
					newLine = "auto " + hashName + " = std::unordered_map<" + keyType + ", " + valueType + ">()"
				} else if strings.HasPrefix(right, "new(") {
					// x := new(T) -> zero-initialized allocation
					inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(after("new(", right)), ")"))
					newLine = "auto " + strings.TrimSpace(left) + " = new " + TypeReplace(inner) + "()"
				} else if strings.HasPrefix(right, "[]") {
					if !strings.Contains(right, "{") {
						fmt.Fprintln(os.Stderr, "UNRECOGNIZED LINE: "+trimmedLine)
						//newLine = line

					}
					theType := TypeReplace(leftBetween(right, "]", "{"))
					fields := strings.SplitN(right, "{", 2)
					newLine = "std::vector<" + theType + "> " + strings.TrimSpace(left) + " {" + fields[1]
				} else if strings.HasPrefix(right, "map[") {
					hashName := strings.TrimSpace(left)
					encounteredHashMaps = append(encounteredHashMaps, hashName)

					keyType := TypeReplace(leftBetween(right, "map[", "]"))
					valueType := TypeReplace(leftBetween(right, "]", "{"))

					closingBracket := strings.HasSuffix(strings.TrimSpace(right), "}")
					if !closingBracket {
						inHashMap = true
						hashKeyType = keyType
						newLine = "std::unordered_map<" + keyType + ", " + valueType + "> " + hashName + " {"
					} else {
						elements := leftBetween(right, "{", "}")
						newLine = "std::unordered_map<" + keyType + ", " + valueType + "> " + hashName + " " + HashElements(elements, keyType, false)
					}
				} else {
					varName := strings.TrimSpace(left)
					if value, found := functionVarMap[varName]; found {
						varName = value
					}

					for k, v := range functionVarMap {
						right = strings.Replace(right, k, v, -1)
					}

					newLine = "auto " + varName + " = " + strings.TrimSpace(right)
				}
			} else if has(interfaceVars, left) {
				// Assignment to an interface-typed variable
				if right == "nil" {
					newLine = left + " = nullptr"
				} else if ctor := newIfImplementingLiteral(right); ctor != "" {
					newLine = left + " = " + ctor
				} else {
					newLine = left + " = " + right
				}
			} else {
				newLine = left + " = " + right
				// Compound assignment: total += x -> total = total + x
				leftTrimmed := strings.TrimSpace(left)
				var op string
				switch {
				case strings.HasSuffix(leftTrimmed, "<<"):
					op = "<<"
				case strings.HasSuffix(leftTrimmed, ">>"):
					op = ">>"
				case strings.HasSuffix(leftTrimmed, "+"), strings.HasSuffix(leftTrimmed, "-"),
					strings.HasSuffix(leftTrimmed, "*"), strings.HasSuffix(leftTrimmed, "/"),
					strings.HasSuffix(leftTrimmed, "%"), strings.HasSuffix(leftTrimmed, "&"),
					strings.HasSuffix(leftTrimmed, "|"), strings.HasSuffix(leftTrimmed, "^"):
					op = lastchar(leftTrimmed)
				}
				if op != "" {
					lhs := strings.TrimSpace(strings.TrimSuffix(leftTrimmed, op))
					if lhs != "" {
						newLine = lhs + " = " + lhs + " " + op + " " + right
					}
				}
			}
		} else if strings.HasPrefix(trimmedLine, "package ") {
			continue
		} else if strings.HasPrefix(trimmedLine, "import") {
			if strings.Contains(trimmedLine, "(") {
				inImport = true
			}
			if strings.Contains(trimmedLine, ")") {
				inImport = false
			}
			continue
		} else if strings.HasPrefix(trimmedLine, "defer ") {
			newLine = DeferCall(line)
		} else if strings.HasPrefix(trimmedLine, "go ") {
			// Goroutine start: go f(), go func() { ... }() spanning multiple lines.
			// One-line anonymous goroutines are not supported.
			rest := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "go "))
			if rest == "func() {" {
				// Anonymous goroutine spanning multiple lines
				inGoFunction = true
				newLine = "std::thread([]() {"
			} else if strings.HasSuffix(rest, ")") && strings.Contains(rest, "(") {
				// Plain function call in a goroutine
				pidx := strings.Index(rest, "(")
				fname := strings.TrimSpace(rest[:pidx])
				args := strings.TrimSpace(rest[pidx+1 : len(rest)-1])
				if args != "" {
					newLine = "std::thread(" + fname + ", " + args + ").detach();"
				} else {
					newLine = "std::thread(" + fname + ").detach();"
				}
			} else {
				fmt.Fprintln(os.Stderr, "UNRECOGNIZED go STATEMENT: "+trimmedLine)
				newLine = line
			}
		} else if strings.Contains(trimmedLine, " <- ") && !strings.HasPrefix(trimmedLine, "<-") {
			// Channel send: ch <- v
			parts := strings.SplitN(trimmedLine, " <- ", 2)
			newLine = strings.TrimSpace(parts[0]) + ".send(" + strings.TrimSpace(parts[1]) + ")"
		} else if strings.HasPrefix(trimmedLine, "close(") && strings.HasSuffix(trimmedLine, ")") {
			// Channel close: close(ch)
			newLine = strings.TrimSpace(leftBetween(trimmedLine, "close(", ")")) + ".close()"
		} else if strings.HasPrefix(trimmedLine, "panic(") && strings.HasSuffix(trimmedLine, ")") {
			// panic(msg) -> throw
			newLine = "throw std::runtime_error(" + strings.TrimSpace(leftBetween(trimmedLine, "panic(", ")")) + ")"
		} else if strings.HasPrefix(trimmedLine, "if ") {
			newLine = IfSentence(line)
			// TODO: Short variable names has the potential to ruin if expressions this way, do a smarter replacement
			// TODO: Also do this for for loops, switches and other cases where this makes sense
			for k, v := range functionVarMap {
				newLine = strings.Replace(newLine, k, v, -1)
			}
		} else if strings.HasPrefix(trimmedLine, "} else if ") {
			newLine = ElseIfSentence(line)
		} else if trimmedLine == "var (" {
			inVar = true
			continue
		} else if trimmedLine == "type (" {
			inType = true
			continue
		} else if trimmedLine == "const (" {
			inConst = true
			iotaNumber = 0
			iotaFirstLine = true
			lastConstExpr = ""
			continue
		} else if strings.HasPrefix(trimmedLine, "var ") {
			// Ignore variable name since it's not in a struct
			var varNames []string
			newLine, varNames = VarDeclarations(line)
			_ = varNames
			fields := strings.Fields(trimmedLine)
			if len(fields) >= 3 {
				if _, isIface := scanInterfaces[fields[2]]; isIface {
					interfaceVars = append(interfaceVars, strings.TrimSuffix(fields[1], ","))
				}
			}
			if strings.HasSuffix(newLine, "{") {
				closingBracketNeedsASemicolon = true
			}
		} else if strings.HasPrefix(trimmedLine, "type ") {
			if strings.Contains(trimmedLine, " interface {") {
				ifaceName := typeName(trimmedLine)
				newLine = "class " + ifaceName + " {\npublic:\nvirtual ~" + ifaceName + "() = default;"
				inInterface = true
			} else {
				newLine, inStruct = TypeDeclaration(trimmedLine)
				if inStruct {
					// Entering struct, reset the slice that is used to gather variable names
					encounteredStructNames = []string{}
					currentStructName = typeName(trimmedLine)
					if len(implementedIface[currentStructName]) > 0 {
						bases := ""
						for _, iface := range implementedIface[currentStructName] {
							bases += " : public " + iface
						}
						newLine = strings.Replace(newLine, " {", bases+" {", 1)
					}
				}
			}
		} else if strings.HasPrefix(trimmedLine, "const ") {
			newLine = ConstDeclaration(trimmedLine, false)
		} else if trimmedLine == "fallthrough" {
			newLine = "goto " + LabelName() + "; // fallthrough"
			switchLabel = LabelName()
			labelCounter++
		} else if unfinishedDeferFunction && trimmedLine == "}()" {
			unfinishedDeferFunction = false
			newLine = "});"
		} else if inGoFunction && trimmedLine == "}()" {
			inGoFunction = false
			newLine = "}).detach();"
		} else if trimmedLine == "default:" {
			newLine = "} else { // default case"
			if switchLabel != "" {
				newLine += "\n" + switchLabel + ":"
				switchLabel = ""
			}
		}

		if cppHasStdFormat {
			// Special case for fmt.Sprintf -> std::format
			if strings.Contains(newLine, "fmt.Sprintf(") && strings.Contains(newLine, "%v") {
				newLine = strings.Replace(strings.Replace(newLine, "%v", "{}", -1), "fmt.Sprintf(", "std::format(", -1)
			}
		}

		if currentFunctionName == "main" && trimmedLine == "}" && curlyCount == 0 { // curlyCount has already been decreased for this line
			newLine = strings.Replace(trimmedLine, "}", "return 0;\n}", 1)
		}

		if strings.HasSuffix(trimmedLine, "}") {
			// If the struct is being closed, add a semicolon
			if inStruct {
				// Create a _str() method for this struct
				injected := ""
				if len(implementedIface[currentStructName]) > 0 {
					// Inheritance makes the struct a non-aggregate, so a
					// constructor replaces the designated initializer syntax
					if ctor := constructorFor(currentStructName); ctor != "" {
						injected += ctor + "\n"
					}
				}
				for _, iface := range implementedIface[currentStructName] {
					for _, m := range scanInterfaces[iface] {
						retCpp := "void"
						if m.ret != "" {
							retCpp = TypeReplace(m.ret)
						}
						injected += "virtual " + retCpp + " " + m.name + "(" + FunctionArguments(m.params) + ") override;\n"
					}
				}
				newLine = injected + CreateStrMethod(encounteredStructNames) + newLine + ";"

				inStruct = false
			} else if closingBracketNeedsASemicolon {
				newLine += ";"
				closingBracketNeedsASemicolon = false
			}
			newLine += "\n"
		}
		if (!strings.HasSuffix(newLine, ";") && !has(endings, lastchar(trimmedLine)) || strings.Contains(trimmedLine, "=")) && !strings.HasPrefix(trimmedLine, "//") && (!has(endings, lastchar(newLine)) && !strings.Contains(newLine, "//")) {
			if !inMultilineString {
				newLine += ";"
			}
		}

		// multiline strings
		for strings.Contains(newLine, "`") {
			if !inMultilineString {
				if strings.HasSuffix(newLine, ";") {
					newLine = newLine[:len(newLine)-1]
				}
				newLine = strings.Replace(newLine, "`", "R\"(", 1)
				inMultilineString = true
			} else {
				newLine = strings.Replace(newLine, "`", ")\"", 1)
				//if !strings.HasSuffix(newLine, ",") {
				newLine += ";"
				//}
				inMultilineString = false
			}
		}

		// Interface-implementing struct literals become constructor calls
		newLine = rewriteStructLiterals(newLine)
		// Interface variables call methods through pointers in C++
		for _, iv := range interfaceVars {
			for _, mn := range ifaceMethodNames {
				newLine = strings.Replace(newLine, iv+"."+mn+"(", iv+"->"+mn+"(", -1)
			}
		}

		lines = append(lines, newLine)
	}
	if len(glueLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, glueLines...)
	}
	return lineTransformResult{
		output:         strings.Join(lines, "\n"),
		usePrettyPrint: usePrettyPrint,
		haveStructs:    len(encounteredStructNames) > 0,
		cppBlock:       cppCode,
	}
}

// finishCpp runs the whole-program passes on the combined transpiled output.
// The order matters.
func finishCpp(res lineTransformResult) string {
	output := res.output
	output = LiteralStrings(output)
	output = WholeProgramReplace(output)
	output = RewriteSlices(output)

	// The order matters
	output = AddFunctions(output, res.usePrettyPrint, res.haveStructs)
	output = AddIncludes(output)

	return spliceCppBlock(output, res.cppBlock)
}

// go2cpp transforms Go source to C++ in one go, for a single file without
// local package imports (used for debug output and deferred function bodies).
func go2cpp(source string) string {
	return finishCpp(go2cppLines(source))
}

func main() {

	// TODO: Use https://github.com/docopt/docopt.go for parsing arguments

	debug := false
	compile := true
	clangFormat := true

	inputFilename := ""
	if len(os.Args) > 1 {
		if os.Args[1] == "--version" {
			fmt.Println(versionString)
			return
		} else if os.Args[1] == "--help" {
			fmt.Println("supported arguments:")
			fmt.Println(" a .go file as the first argument")
			fmt.Println("supported options:")
			fmt.Println(" -o : Format with clang format")
			fmt.Println(" -O : Don't format with clang format")
			return
		}
		inputFilename = os.Args[1]
	}
	if len(os.Args) > 2 {
		if os.Args[2] == "-o" {
			clangFormat = true
		} else if os.Args[2] == "-O" {
			clangFormat = false
		} else if os.Args[2] != "-o" {
			log.Fatal("The second argument must be -o (format sources with clang-format) or -O (don't format sources with clang-format)")
		}
	}

	var sourceData []byte
	var err error
	if inputFilename != "" {
		sourceData, err = ioutil.ReadFile(inputFilename)
	} else {
		sourceData, err = ioutil.ReadAll(os.Stdin)
	}
	if err != nil {
		log.Fatal(err)
	}
	if debug {
		fmt.Println(go2cpp(string(sourceData)))
		return
	}

	transpiled, transpileErr := go2cppFile(inputFilename, string(sourceData))
	if transpileErr != nil {
		log.Fatal(transpileErr)
	}

	cppSource := ""
	if clangFormat {
		cmd := exec.Command("clang-format", "-style={BasedOnStyle: Webkit, ColumnLimit: 99}")
		cmd.Stdin = strings.NewReader(transpiled)
		var out bytes.Buffer
		cmd.Stdout = &out
		err = cmd.Run()
		if err != nil {
			log.Println("clang-format is not available, the output will look ugly!")
			cppSource = transpiled
		} else {
			cppSource = out.String()
		}
	} else {
		cppSource = transpiled
	}

	if !compile {
		fmt.Println(cppSource)
		return
	}

	// Use a temp file with the .exe suffix, so that MinGW clang++/g++ do not
	// append .exe to the -o output name, which would make go2cpp read back
	// an empty file on Windows. Harmless on other platforms.
	tempFile, err := ioutil.TempFile("", "go2cpp*.exe")
	if err != nil {
		log.Fatal(err)
	}
	tempFileName := tempFile.Name()
	defer os.Remove(tempFileName)

	// Compile the string in cppSource
	cpp := "g++"
	if cppenv := os.Getenv("CXX"); cppenv != "" {
		cpp = cppenv
	}
	cmd2 := exec.Command(cpp, "-x", "c++", "-std=c++2a", "-O2", "-pipe", "-fPIC", "-Wfatal-errors", "-fpermissive", "-Wno-address-of-temporary", "-s", "-o", tempFileName, "-")
	cmd2.Stdin = strings.NewReader(cppSource)
	var compiled bytes.Buffer
	var errors bytes.Buffer
	cmd2.Stdout = &compiled
	cmd2.Stderr = &errors
	err = cmd2.Run()
	if err != nil {
		//fmt.Println("Failed to compile this with g++:")
		fmt.Println(cppSource)
		fmt.Println("Errors:")
		fmt.Println(errors.String())
		log.Fatal(err)
	}
	compiledBytes, err := ioutil.ReadFile(tempFileName)
	if err != nil {
		log.Fatal(err)
	}
	outputFilename := ""
	if len(os.Args) > 3 {
		outputFilename = os.Args[3]
	}
	if outputFilename != "" {
		err = ioutil.WriteFile(outputFilename, compiledBytes, 0755)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		fmt.Println(cppSource)
	}
}
