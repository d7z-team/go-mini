//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg tests -out ffi_struct_ffigen_test.go ffi_struct_test.go
package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

type Point struct {
	X int64
	Y int64
}

type Rect struct {
	A Point
	B Point
}

// ffigen:module shape
type MockShapeAPI interface {
	GetRect() Rect
	Area(r Rect) int64
}

type MockShapeHost struct{}

func (m *MockShapeHost) GetRect() Rect {
	return Rect{A: Point{10, 20}, B: Point{30, 40}}
}

func (m *MockShapeHost) Area(r Rect) int64 {
	return (r.B.X - r.A.X) * (r.B.Y - r.A.Y)
}

func TestFFIStructRoundTrip(t *testing.T) {
	impl := &MockShapeHost{}
	executor := engine.MustNewMiniExecutor()
	if err := executor.UseSurface(SurfaceMockShapeAPI(impl)); err != nil {
		t.Fatal(err)
	}

	code := `
	package main
	import "shape"

	func main() {
		rect := shape.GetRect()
		if rect.A.X != 10 || rect.A.Y != 20 || rect.B.X != 30 || rect.B.Y != 40 {
			panic("GetRect failed")
		}
		if shape.Area(rect) != 400 {
			panic("Area failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}
