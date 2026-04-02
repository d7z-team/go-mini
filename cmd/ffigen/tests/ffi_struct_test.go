//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg tests -path gopkg.d7z.net/go-mini/cmd/ffigen/tests -out ffi_struct_ffigen_test.go ffi_struct_test.go
package tests

import (
	"testing"
)

type Point struct {
	X int64
	Y int64
}

type Rect struct {
	A Point
	B Point
}
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

func TestFFIStruct(t *testing.T) {
	impl := &MockShapeHost{}
	proxy := &MockShapeAPIProxy{bridge: &MockShapeAPI_Bridge{Impl: impl}}

	rect := proxy.GetRect()
	if rect.A.X != 10 || rect.A.Y != 20 || rect.B.X != 30 || rect.B.Y != 40 {
		t.Fatalf("GetRect failed: %+v", rect)
	}

	area := proxy.Area(rect)
	if area != 400 {
		t.Fatalf("Area failed: %d", area)
	}
}
