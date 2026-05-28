package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestBuiltinsMutateCollectionsAndConvertValues(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

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

		// 8. Test new on pointer type
		p := new(*int64)
		if p == nil {
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

func TestNilMapBuiltinsAndIndexing(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
	package main

	func main() {
		var m map[string]int
		if m != nil {
			panic("nil map comparison failed")
		}
		if len(m) != 0 {
			panic("nil map len failed")
		}
		delete(m, "missing")
		if m["missing"] != 0 {
			panic("nil map missing value failed")
		}
		total := 0
		for _, v := range m {
			total = total + v
		}
		if total != 0 {
			panic("nil map range failed")
		}

		var a []int
		if a != nil {
			panic("nil array comparison failed")
		}
		if len(a) != 0 || cap(a) != 0 {
			panic("nil array len cap failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}
