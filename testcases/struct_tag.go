package main

import "fmt"

type Item struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func main() {
	it := Item{Name: "box", Count: 5}
	fmt.Println(it.Name, it.Count)
	fmt.Println(it)
}
