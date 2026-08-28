package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Println(strings.Index("hello world", "world"))
	fmt.Println(strings.Index("hello world", "xyz"))
	fmt.Println(strings.Index("hello", "h"))
}
