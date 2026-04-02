package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestNumericTypeMapping(t *testing.T) {
	code := `
package main
func main() int {
	var a int8 = 1
	var b int16 = 2
	var c int32 = 3
	var d uint = 4
	var e uint16 = 5
	var f uint32 = 6
	var g float32 = 7.5
	return int(float64(a + b + c + d + e + f) + float64(g))
}
`
	vm, err := engine.NewMiniExecutor().NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	res, err := vm.Eval(context.Background(), "main()", nil)
	if err != nil {
		t.Fatalf("Failed to run main: %v", err)
	}

	// 1 + 2 + 3 + 4 + 5 + 6 + 7.5 = 28.5 -> cast to int = 28
	if res.I64 != 28 {
		t.Errorf("Expected 28, got %v", res.I64)
	}
}
