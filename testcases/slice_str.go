package main

import (
	"fmt"
)

func main() {
	s := "hello world"
	fmt.Println(s[0:5])
	fmt.Println(s[6:])
	fmt.Println(s[:5])
	fmt.Println(len(s[:]))
	over := s[6:11]
	fmt.Println(over)
	fmt.Println(len(over))
}
