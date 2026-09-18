//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg tests -out ffi_robustness_ffigen_test.go robustness_test.go
package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type RobustPoint struct {
	X int64
	Y int64
}

// ffigen:module e2e
type MockGeometry interface {
	SumX(points []RobustPoint) int64
}

type MockGeo struct{}

func (m *MockGeo) SumX(points []RobustPoint) int64 {
	var sum int64
	for _, p := range points {
		sum += p.X
	}
	return sum
}

func TestGeneratedRouterSupportsCompositeInputsAndBuiltins(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	mock := &MockGeo{}
	if err := executor.UseSurface(SurfaceMockGeometry(mock)); err != nil {
		t.Fatal(err)
	}

	code := `
	package main
	import "e2e"

	func main() {
		// 1. 测试 len() 的各种场景
		if len("") != 0 { panic("len string empty") }
		if len([]byte("abc")) != 3 { panic("len bytes") }
		
		arr := []Int64{1, 2, 3, 4}
		if len(arr) != 4 { panic("len array") }

		// 2. 测试复合字面量与 FFI 数组传递
		p1 := e2e.RobustPoint{X: 10, Y: 20}
		p2 := e2e.RobustPoint{X: 30, Y: 40}
		points := []e2e.RobustPoint{p1, p2}
		
		totalX := e2e.SumX(points)
		if totalX != 40 { 
			panic("FFI array sum failed: got " + string(totalX)) 
		}

		// 3. 测试 nil 安全比较
		unknown := nil
		if unknown != nil { panic("any should be nil") }
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedRouterRejectsMalformedArgs(t *testing.T) {
	_, err := mockGeometryHostRouter(context.Background(), &MockGeo{}, ffigo.NewHandleRegistry(), methodIDMockGeometrySumX, "", []byte{5})
	if err == nil || !strings.Contains(err.Error(), "decode params") {
		t.Fatalf("expected decode params error, got %v", err)
	}
}
