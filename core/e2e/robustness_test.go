package e2e

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type RobustPoint struct {
	X int
	Y int
}

type Geometry interface {
	SumX(points []RobustPoint) int
}

type MockGeo struct{}

func (m *MockGeo) SumX(points []RobustPoint) int {
	fmt.Printf("Host received points: %+v\n", points)
	sum := 0
	for _, p := range points {
		sum += p.X
	}
	return sum
}

func TestRobustness(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	// 注册一个处理数组+结构体的 FFI
	mock := &MockGeo{}
	bridge := &engine.HandleBridgeWrapper{
		Registry: ffigo.NewHandleRegistry(),
		Router: func(ctx context.Context, reg *ffigo.HandleRegistry, methodID uint32, args []byte) ([]byte, error) {
			return GeometryHostRouter(ctx, mock, reg, methodID, args)
		},
	}
	executor.RegisterFFI("SumX", bridge, MethodID_Geometry_SumX, "function(Array<RobustPoint>) Int64")

	code := `
	package main
	import "fmt"

	type RobustPoint struct { X Int64; Y Int64 }

	func main() {
		// 1. 测试 len() 的各种场景
		if len("") != 0 { panic("len string empty") }
		if len([]byte("abc")) != 3 { panic("len bytes") }
		
		arr := []Int64{1, 2, 3, 4}
		if len(arr) != 4 { panic("len array") }

		// 2. 测试复合字面量与 FFI 数组传递
		p1 := RobustPoint{X: 10, Y: 20}
		p2 := RobustPoint{X: 30, Y: 40}
		points := []RobustPoint{p1, p2}
		
		totalX := SumX(points)
		if totalX != 40 { 
			panic("FFI array sum failed: got " + string(totalX)) 
		}

		// 3. 测试 nil 安全比较
		unknown := nil
		if unknown != nil { panic("any should be nil") }
		
		fmt.Println("Robustness test passed")
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
