package main

import "fmt"

func main() {
	src := []int{1, 2, 3, 4, 5}
	dst := make([]int, 3)
	n := copy(dst, src)
	fmt.Println(n)
	for _, v := range dst {
		fmt.Println(v)
	}
}
