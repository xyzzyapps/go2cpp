package main

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Name string
	Size int
}

func main() {
	counts := make(map[string]int)
	counts["apples"] = 4
	counts["pears"] = 2
	fmt.Println(counts["apples"], counts["pears"], len(counts))

	n := new(int)
	*n = 9
	fmt.Println(*n)

	fmt.Println("n=" + strconv.Itoa(42))
	fmt.Println(fmt.Sprint("a", 1, true))

	c := Config{Name: "cfg", Size: 3}
	fmt.Println(c.Name, c.Size)

	if len(counts) > 100 {
		panic("too many")
	}
	if len(counts) > 100 {
		os.Exit(1)
	}
}
