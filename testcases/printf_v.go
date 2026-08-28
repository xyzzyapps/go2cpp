package main

import (
	"fmt"
)

func main() {
	fmt.Printf("%v-%v-%v\n", 1, "two", true)
	fmt.Printf("plain line\n")
	fmt.Printf("num=%d hex=%x str=%s end\n", 42, 255, "value")
}
