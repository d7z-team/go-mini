package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestBuiltins(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
	package main
	
	func main() {
		// 1. Test append on array
		arr := []int{1, 2}
		arr = append(arr, 3, 4)
		if len(arr) != 4 {
			panic("append array len mismatch")
		}
		if arr[2] != 3 || arr[3] != 4 {
			panic("append array value mismatch")
		}

		// 2. Test append on byte slice
		b := []byte("ab")
		b = append(b, 99) // 99 is 'c'
		if len(b) != 3 {
			panic("append byte slice len mismatch")
		}
		if string(b) != "abc" {
			panic("append byte slice value mismatch")
		}

		// 3. Test delete on map
		m := map[string]int{"a": 10, "b": 20}
		if len(m) != 2 {
			panic("map initial len mismatch")
		}
		delete(m, "a")
		if len(m) != 1 {
			panic("delete map len mismatch")
		}
		
		// Map should not panic when deleting non-existent key
		delete(m, "not_exist")
		if len(m) != 1 {
			panic("delete non-existent key changed len")
		}

		// 4. Test delete on map with Any type wrapper
		var mAny any = map[string]int{"k": 1}
		delete(mAny, "k")
		if len(mAny) != 0 {
			panic("delete on Any map failed")
		}

		// 5. Test numeric conversions
		f := 1.9
		i := int(f)
		if i != 1 {
			panic("float to int conversion failed")
		}
		
		i64 := int64(2.5)
		if i64 != 2 {
			panic("float to int64 conversion failed")
		}

		f2 := float64(i64)
		if f2 != 2.0 {
			panic("int to float conversion failed")
		}

		s := "123"
		if int(s) != 123 {
			panic("string to int conversion failed")
		}

		s2 := "3.14"
		if float64(s2) != 3.14 {
			panic("string to float conversion failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}
