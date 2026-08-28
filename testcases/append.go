package main

import (
	"fmt"
)

func main() {
	nums := []int{1, 2}
	nums = append(nums, 3)
	nums = append(nums, 4, 5)
	fmt.Println(len(nums))
	for _, n := range nums {
		fmt.Println(n)
	}
	more := []int{6, 7}
	nums = append(nums, more...)
	fmt.Println(len(nums))
}
