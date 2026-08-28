package main

import (
	"fmt"

	"github.com/xyproto/go2cpp/testcases/importpkg/utils"
)

func main() {
	p := utils.NewPoint(1.5, 2.5)
	fmt.Println(utils.Pi)
	fmt.Println(utils.Max(3, 7))
	fmt.Println(utils.MinInt)
	fmt.Println(utils.Counter)
	fmt.Println(p.X, p.Y)
	fmt.Println(utils.Norm(p))
}
