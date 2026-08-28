package main

import "fmt"

// Shape is an interface with two methods.
type Shape interface {
	Area() float64
	Name() string
}

// Rect implements Shape with a value receiver.
type Rect struct {
	W     float64
	H     float64
	Title string
}

func (r Rect) Area() float64 {
	return r.W * r.H
}

func (r Rect) Name() string {
	return r.Title
}

// Circle implements Shape too.
type Circle struct {
	R     float64
	Title string
}

func (c Circle) Area() float64 {
	return 3.14159 * c.R * c.R
}

func (c Circle) Name() string {
	return c.Title
}

// area calls the method through the interface.
func area(s Shape) float64 {
	return s.Area()
}

// label returns the name through the interface.
func label(s Shape) string {
	return s.Name()
}

func main() {
	var s Shape
	s = Rect{W: 2, H: 3, Title: "rect"}
	fmt.Println(s.Area())
	fmt.Println(s.Name())
	s = Circle{R: 1, Title: "circle"}
	fmt.Println(area(s))
	fmt.Println(label(s))
	s = nil
	r := Rect{W: 4, H: 5, Title: "r"}
	fmt.Println(area(&r))
	fmt.Println(r.Area())
	fmt.Println(r.Name())
}
