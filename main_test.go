package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testcaseDirectory = "testcases"

var testPrograms = []string{
	//"multiline_map",
	"multiline_string",
	"cpp_call",
	"interfaces",
	"if_switch_init",
	"stdlib_misc",
	"copy_vec",
	"struct_tag",
	"var_string",
	"var_multi",
	"string_args",
	"importpkg/main",
	"defer",
	"iota",
	"map_struct",
	"for_range_map_key_value",
	"for_range_map_value",
	"for_range_map_key",
	"has_suffix",
	"index",
	"join",
	"replace",
	"split",
	"splitn",
	"sprintf_mixed",
	"printf_v",
	"iota_expr",
	"slice_str",
	"slice_vec",
	"append",
	"go_stmt",
	"chan_buffered",
	"string_map",
	"struct",
	"var2",
	"for_range_both",
	"for_endless",
	"for_range_single",
	"for_range_list",
	"for_regular",
	"trimspace",
	"printf",
	"multiple",
	"prefix",
	"contains",
	"if",
	"var",
	"output_char2",
	"var_zero",
	"output_char",
	"const",
	"continue",
	"types",
	"if_minus_one",
	"goto",
	"switch",
	"hello",
	"sprintf",
}

// Programs with unordered words as the output
var unorderedOutput = []string{
	"string_map",
	"for_range_map_key_value",
	"for_range_map_value",
	"for_range_map_key",
}

// go2cppBinary returns the path to the go2cpp executable built by "go build".
func go2cppBinary() string {
	if runtime.GOOS == "windows" {
		return ".\\go2cpp.exe"
	}
	return "./go2cpp"
}

// testcaseBinary returns the path of the compiled testcase executable.
// MinGW compilers append .exe when linking on Windows.
func testcaseBinary(program string) string {
	name := filepath.Join(testcaseDirectory, program)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func assertEqual(t *testing.T, a interface{}, b interface{}, message string) {
	if a == b {
		return
	}
	if len(message) == 0 {
		message = fmt.Sprintf("%v != %v", a, b)
	}
	t.Fatal(message)
}

// Run a command and return what has been written to stdout,
// or an empty string, and what has been written to stderr,
// or an empty string.
func Run(cmdString string) (string, string, error) {
	var outputStdout, outputStderr bytes.Buffer
	fields := strings.Fields(cmdString)
	cmd := exec.Command(fields[0], fields[1:]...)
	cmd.Stdout = &outputStdout
	cmd.Stderr = &outputStderr
	err := cmd.Run()
	if err != nil {
		return outputStdout.String(), outputStderr.String(), err
	}
	return outputStdout.String(), outputStderr.String(), nil
}

// RunWithDLLPath runs a command like Run, but with the directory of the CXX
// compiler prepended to PATH in the child environment. MinGW-built binaries
// need runtime DLLs like libstdc++-6.dll from the compiler directory.
func RunWithDLLPath(cmdString string) (string, string, error) {
	var outputStdout, outputStderr bytes.Buffer
	fields := strings.Fields(cmdString)
	cmd := exec.Command(fields[0], fields[1:]...)
	if cxx := os.Getenv("CXX"); cxx != "" {
		cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(cxx)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cmd.Stdout = &outputStdout
	cmd.Stderr = &outputStderr
	err := cmd.Run()
	if err != nil {
		return outputStdout.String(), outputStderr.String(), err
	}
	return outputStdout.String(), outputStderr.String(), nil
}

func exists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// normalize converts CRLF line endings to LF, since the C++ runtime writes
// \r\n in text mode on Windows, while Go writes \n.
func normalize(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func TestPrograms(t *testing.T) {
	Run("go build")
	go2cpp := go2cppBinary()
	for _, program := range testPrograms {
		gofile := filepath.Join(testcaseDirectory, program+".go")
		binary := testcaseBinary(program)

		if !exists(gofile) {
			t.Fatal(gofile + " does not exist!")
		}

		// Program output when running with "go run"
		fmt.Println("[go  ] Compiling and running " + gofile + " (using go run)...")
		stdoutGo, stderrGo, err := Run("go run " + gofile)
		if err != nil {
			t.Fatal(err)
		}

		// Program output when compiling with go2cpp and running the executable
		fmt.Println("[ c++] Compiling and running " + gofile + " (using go2cpp and the C++ compiler in $CXX or g++ default)...")
		Run(go2cpp + " " + gofile + " -o " + binary)
		stdoutTgc, stderrTgc, runErr := RunWithDLLPath(binary)
		if runErr != nil {
			// Check if transpiling works at all, without clang-format
			cmd := go2cpp + " " + gofile + " -O"
			if stdoutT, stderrT, transpileErr := Run(cmd); transpileErr != nil {
				fmt.Println("TRANSPILATION FAILED:", cmd)
				fmt.Println(stdoutT, stderrT)
				fmt.Fprintln(os.Stderr, gofile)
				t.Fatal(transpileErr)
			}
			// Transpilation succeeds, but running the binary failed. On
			// Windows, an antivirus scan may lock the executable right after
			// it is written. Wait a moment and try again.
			time.Sleep(200 * time.Millisecond)
			stdoutTgc, stderrTgc, runErr = RunWithDLLPath(binary)
			if runErr != nil {
				fmt.Fprintln(os.Stderr, gofile)
				t.Fatal("running the compiled binary failed: " + runErr.Error())
			}
		}
		os.Remove(binary)

		// For some test-programs, assume the order of the outputted words are random
		// And only check stdout.
		if has(unorderedOutput, program) {
			words1 := strings.Split(strings.TrimSpace(stdoutGo), " ")
			words2 := strings.Split(strings.TrimSpace(stdoutTgc), " ")
			for _, word := range words1 {
				if !has(words2, word) {
					fmt.Println(words2, "DOES NOT HAVE", word)
					assertEqual(t, stdoutGo, stdoutTgc, "go2cpp and go run should produce the same list of words on stdout")
				}
			}
			for _, word := range words2 {
				if !has(words1, word) {
					fmt.Println(words1, "DOES NOT HAVE", word)
					assertEqual(t, stdoutGo, stdoutTgc, "go2cpp and go run should produce the same list of words on stdout")
				}
			}
			continue
		}

		// Check if they are equal
		assertEqual(t, normalize(stdoutGo), normalize(stdoutTgc), "go2cpp and go run should produce the same output on stdout")
		assertEqual(t, normalize(stderrGo), normalize(stderrTgc), "go2cpp and go run should produce the same output on stderr")
	}
}
