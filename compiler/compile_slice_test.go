package compiler

import "testing"

func TestCompileRejectsInvalidFullSliceExpressions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "string",
			source: `
package main
func Main() string { return "hello"[1:2:3] }
`,
			code: "hirgen.slice.full_string",
		},
		{
			name: "invalid operand",
			source: `
package main
func Main() int64 {
	value := int64(10)
	return value[0:0:0]
}
`,
			code: "hirgen.slice.full_type",
		},
		{
			name: "negative constant",
			source: `
package main
func Main() int64 {
	values := [2]int64{1, 2}
	return int64(len(values[-1:1:1]))
}
`,
			code: "hirgen.slice.negative",
		},
		{
			name: "order",
			source: `
package main
func Main() int64 {
	values := [2]int64{1, 2}
	return int64(len(values[1:0:1]))
}
`,
			code: "hirgen.slice.order",
		},
		{
			name: "max before high",
			source: `
package main
func Main() int64 {
	values := [3]int64{1, 2, 3}
	return int64(len(values[0:2:1]))
}
`,
			code: "hirgen.slice.order",
		},
		{
			name: "out of range",
			source: `
package main
func Main() int64 {
	values := [2]int64{1, 2}
	return int64(len(values[0:2:3]))
}
`,
			code: "hirgen.slice.range",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, tc.code)
		})
	}
}

func TestCompileAllowsValidFullSliceExpressions(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

func Main(limit int) int64 {
	array := [4]int64{1, 2, 3, 4}
	fromArray := array[1:3:4]
	ptr := &array
	fromPointer := ptr[:2:3]
	slice := []int64{1, 2, 3, 4}
	fromSlice := slice[0:limit:limit]
	return int64(len(fromArray)) + int64(cap(fromArray)) + int64(len(fromPointer)) + int64(cap(fromPointer)) + int64(len(fromSlice))
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileAllowsIntegerTypesAsIndexesAndSliceBounds(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

func Main(values []byte, low uint8, high uint16, max uint32) byte {
	value := values[low]
	window := values[low:high:max]
	return value + window[uint64(0)]
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompilePreservesNestedSliceElementAddressType(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

type cell struct{ value int }

func Main(lines [][]cell) {
	lines[0] = lines[0][:0]
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileRejectsInvalidStringSliceConversions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "string to non text slice",
			source: `
package main
func Main() {
	_ = []int64("go")
}
`,
			code: "semantic.convert.type",
		},
		{
			name: "rune slice to byte slice",
			source: `
package main
func Main() {
	_ = []byte([]rune("go"))
}
`,
			code: "hirgen.convert.string_slice",
		},
		{
			name: "integer to rune slice",
			source: `
package main
func Main() {
	_ = []rune(65)
}
`,
			code: "semantic.convert.type",
		},
		{
			name: "float variable to string",
			source: `
package main
func Main(value float64) {
	_ = string(value)
}
`,
			code: "hirgen.convert.string",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, tc.code)
		})
	}
}
