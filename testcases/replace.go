package main

import (
	"fmt"
	"strings"
)

func main() {
	s := "one two one two one"
	fmt.Println(strings.ReplaceAll(s, "one", "1"))
	fmt.Println(strings.Replace(s, "one", "1", 2))
	fmt.Println(strings.Replace(s, "one", "1", -1))
	fmt.Println(strings.Replace(s, "missing", "1", 3))
}
