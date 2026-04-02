package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestMakeAllocation(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	func main() {
		// Array
		arr1 := make([]int, 5)
		if len(arr1) != 5 {
			panic("arr1 length")
		}
		
		arr2 := make([]string, 2, 10)
		if len(arr2) != 2 {
			panic("arr2 length")
		}

		// Map
		m := make(map[string]int)
		m["hello"] = 42
		if len(m) != 1 || m["hello"] != 42 {
			panic("map make failed")
		}

		// Bytes
		b := make([]byte, 3, 5)
		if len(b) != 3 {
			panic("bytes length")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}
