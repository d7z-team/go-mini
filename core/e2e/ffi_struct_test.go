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

type ShapeAPI interface {
	GetRect() Rect
	Area(r Rect) int
}

type MockShapeAPI struct{}

func (m *MockShapeAPI) GetRect() Rect {
	return Rect{A: Point{10, 20}, B: Point{30, 40}}
}

func (m *MockShapeAPI) Area(r Rect) int {
	return (r.B.X - r.A.X) * (r.B.Y - r.A.Y)
}

type MockShapeBridge struct {
	impl *MockShapeAPI
}

func (b *MockShapeBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return ShapeAPIHostRouter(ctx, b.impl, nil, methodID, args)
}

func (b *MockShapeBridge) DestroyHandle(handle uint32) error {
	return nil
}

func TestFFIStruct(t *testing.T) {
	bridge := &MockShapeBridge{impl: &MockShapeAPI{}}
	proxy := &ShapeAPIProxy{bridge: bridge}
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
