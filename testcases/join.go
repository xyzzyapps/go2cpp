package main

import (
	"fmt"
	"strings"
)

func main() {
	parts := []string{"a", "b", "c"}
	fmt.Println(strings.Join(parts, "-"))
	fmt.Println(strings.Join(parts, ""))
	fmt.Println(strings.Join(parts, ", "))
}
