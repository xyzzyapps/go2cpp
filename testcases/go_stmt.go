package main

import (
	"fmt"
	"time"
)

func worker() {
	fmt.Println("worker running")
}

func main() {
	go worker()
	time.Sleep(200 * time.Millisecond)
	fmt.Println("main done")
}
