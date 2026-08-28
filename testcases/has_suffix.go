package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Println(strings.HasSuffix("hello.txt", ".txt"))
	fmt.Println(strings.HasSuffix("hello.txt", ".md"))
	fmt.Println(strings.HasSuffix("hello", "hello"))
}
