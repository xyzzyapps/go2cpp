package main

import (
	"fmt"
	"strings"
)

func main() {
	parts := strings.SplitN("a,b,c", ",", 2)
	fmt.Println(len(parts))
	fmt.Println(parts[0])
	fmt.Println(parts[1])
	none := strings.SplitN("a,b,c", ",", 0)
	fmt.Println(len(none))
	all := strings.SplitN("a,b,c", ",", -1)
	fmt.Println(len(all))
}
