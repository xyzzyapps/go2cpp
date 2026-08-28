package main

import (
	"fmt"
)

func main() {
	nums := []int{1, 2, 3, 4, 5}
	sub := nums[1:3]
	fmt.Println(len(sub))
	fmt.Println(cap(nums))
	for _, n := range sub {
		fmt.Println(n)
	}
	first := nums[:2]
	fmt.Println(len(first))
}
