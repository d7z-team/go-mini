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

		// 6. Test cap
		arr3 := make([]int64, 5, 10)
		if cap(arr3) != 10 {
			panic("cap array mismatch")
		}
		b2 := make([]byte, 2, 4)
		if cap(b2) != 4 {
			panic("cap bytes mismatch")
		}

		// 7. Test indexing on String/Bytes
		s3 := "abc"
		if s3[1] != 98 { // 'b'
			panic("string indexing failed")
		}
		b3 := []byte("def")
		if b3[2] != 102 { // 'f'
			panic("bytes indexing failed")
		}

		// 8. Test Error to String auto conversion
		// We can't easily create a raw Error in VM without FFI, 
		// but we can test the assignment logic if we have an Error-returning function.
		// For now, verified by semantic check in other tests.

		// 9. Test new on pointer type
		p := new(*int64)
		if p == nil {
			// This part should NOT be reached if p is properly initialized to TypeHandle(0)
			// because handle(0) == nil should be true.
			// Wait, if p == nil is TRUE, then it PANICS.
			// But we WANT p to be non-nil because new() should return a valid pointer.
			// In go, new(*int) returns a **int, which is NOT nil.
			panic("new on pointer failed")
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
