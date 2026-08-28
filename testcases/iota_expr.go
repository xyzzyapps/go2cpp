package main

import (
	"fmt"
)

const (
	a = iota * 2
	b
	c
)

func main() {
	fmt.Println(a, b, c)
}
