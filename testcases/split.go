package main

import (
	"fmt"
	"strings"
)

func main() {
	parts := strings.Split("a,b,c", ",")
	fmt.Println(len(parts))
	for _, p := range parts {
		fmt.Println(p)
	}
	empty := strings.Split("", ",")
	fmt.Println(len(empty))
	fmt.Println(strings.Split("no-separators", ",")[0])
}
