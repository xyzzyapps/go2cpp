package main

import "fmt"

func value() int {
	return 7
}

func main() {
	if v := value(); v > 5 {
		fmt.Println("big:", v)
	} else if w := v * 2; w > 20 {
		fmt.Println("bigger:", w)
	}
	switch n := value() + 1; n {
	case 8:
		fmt.Println("eight")
	default:
		fmt.Println("other:", n)
	}
}
