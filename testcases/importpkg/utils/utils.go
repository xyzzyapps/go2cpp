package utils

// Pi is an exported constant.
const Pi = 3.14159

const (
	MaxInt = 100
	MinInt = -100
)

// Counter is an exported package-level variable.
var Counter = 42

// Point is an exported struct type.
type Point struct {
	X float64
	Y float64
}

// NewPoint returns a Point with the given coordinates.
func NewPoint(x, y float64) Point {
	return Point{X: x, Y: y}
}

// Max returns the larger of two ints.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Norm returns the squared length of a point.
func Norm(p Point) float64 {
	return p.X*p.X + p.Y*p.Y
}
