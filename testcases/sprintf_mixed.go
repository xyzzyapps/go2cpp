package main

import (
	"fmt"
)

func main() {
	s := fmt.Sprintf("name=%s age=%d active=%v", "bob", 42, true)
	fmt.Println(s)
	fmt.Println(fmt.Sprintf("single=%v", "only"))
	fmt.Println(fmt.Sprintf("no-args"))
	fmt.Println(fmt.Sprintf("pct=%%d done"))
}
