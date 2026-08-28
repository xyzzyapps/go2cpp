package main

import (
	"fmt"
	"math"
)

// The raw string constant named "cpp" is emitted verbatim into the
// generated C++ code. Go functions with names declared in it are not
// transpiled; the C++ definitions are used instead. When running with
// "go run", the Go fallback implementations below are used instead.
const cpp = `
#include <cmath>

auto cppSqrt(double x) -> double { return std::sqrt(x); }

int cppAddThree(int x) { return x + 3; }

std::string cppRepeat(std::string s, int n) {
	std::string out;
	for (int i = 0; i < n; ++i) { out += s; }
	return out;
}
`

func cppSqrt(x float64) float64 {
	return math.Sqrt(x)
}

func cppAddThree(x int) int {
	return x + 3
}

func cppRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func main() {
	fmt.Println(cppSqrt(16.0))
	fmt.Println(cppAddThree(4))
	fmt.Println(cppRepeat("ab", 3))
	fmt.Println(cppAddThree(cppAddThree(1)))
}
