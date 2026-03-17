//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg e2e -out ffi_struct_ffigen.go ffi_struct_test.go
package e2e

import (
	"context"
	"testing"
)

type Point struct {
	X int
	Y int
}

type Rect struct {
	A Point
	B Point
}
type MockShapeAPI interface {
	GetRect() Rect
	Area(r Rect) int
}

type MockShapeHost struct{}

func (m *MockShapeHost) GetRect() Rect {
	return Rect{A: Point{10, 20}, B: Point{30, 40}}
}

func (m *MockShapeHost) Area(r Rect) int {
	return (r.B.X - r.A.X) * (r.B.Y - r.A.Y)
}

func TestFFIStruct(t *testing.T) {
	impl := &MockShapeHost{}
	proxy := &MockShapeAPIProxy{bridge: &MockShapeAPI_Bridge{Impl: impl}}
	ctx := context.Background()

	rect := proxy.GetRect(ctx)
	if rect.A.X != 10 || rect.A.Y != 20 || rect.B.X != 30 || rect.B.Y != 40 {
		t.Fatalf("GetRect failed: %+v", rect)
	}

	area := proxy.Area(ctx, rect)
	if area != 400 {
		t.Fatalf("Area failed: %d", area)
	}
}
