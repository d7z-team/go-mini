package compiler

import "testing"

func TestCompileSourceAllowsNonNegativeIntegerConstAssignedToUint64(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

const maxUint64 = 1<<64 - 1

func Main(bits int) int64 {
	var value uint64 = maxUint64
	shifted := uint64(1) << (bits - 1)
	shiftedAgain := int64(1) << uint(bits - 1)
	if value == maxUint64 {
		return int64(shifted>>(bits-1)) + shiftedAgain + 40
	}
	return 0
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileRejectsUnrepresentableConstantsAtTargetBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{
			name: "typed package const",
			source: `
package main
const Bad int8 = 128
func Main() int64 { return 0 }
`,
		},
		{
			name: "typed local const",
			source: `
package main
func Main() int64 {
	const Bad uint8 = -1
	return 0
}
`,
		},
		{
			name: "assignment target",
			source: `
package main
func Main() int64 {
	var bad int8 = 128
	return int64(bad)
}
`,
		},
		{
			name: "unsigned assignment target rejects negative constant",
			source: `
package main
func Main() uint64 {
	var bad uint64 = -1
	return bad
}
`,
		},
		{
			name: "return target",
			source: `
package main
func Main() int8 {
	return 128
}
`,
		},
		{
			name: "explicit const conversion",
			source: `
package main
func Main() int64 {
	return int64(int8(128))
}
`,
		},
		{
			name: "float to integer",
			source: `
package main
const Bad int8 = 1.5
func Main() int64 { return 0 }
`,
		},
		{
			name: "negative float to unsigned integer",
			source: `
package main
const Bad uint64 = -1.0
func Main() int64 { return 0 }
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected semantic.const.representable diagnostic")
			}
			requireCompileDiagnostic(t, result.Diagnostics, "semantic.const.representable")
		})
	}
}

func TestCompileAllowsRepresentableConstantsAtTargetBoundaries(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

const A int8 = 127
const B uint8 = 255
const C float32 = 1.5

func Main() int8 {
	var value int8 = 126
	return value + 1
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileRejectsConstantIndexRangeErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "array negative",
			source: `
package main
func Main() int64 {
	values := [2]int64{1, 2}
	return values[-1]
}
`,
			code: "semantic.index.range",
		},
		{
			name: "array out of range",
			source: `
package main
func Main() int64 {
	values := [2]int64{1, 2}
	return values[2]
}
`,
			code: "semantic.index.range",
		},
		{
			name: "pointer array out of range",
			source: `
package main
func Main() int64 {
	values := [2]int64{1, 2}
	ptr := &values
	return ptr[2]
}
`,
			code: "semantic.index.range",
		},
		{
			name: "string literal out of range",
			source: `
package main
func Main() uint8 {
	return "go"[2]
}
`,
			code: "semantic.index.range",
		},
		{
			name: "slice negative",
			source: `
package main
func Main() int64 {
	values := []int64{1, 2}
	return values[-1]
}
`,
			code: "semantic.index.range",
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

func TestCompileAllowsConstantIndexWhenRangeIsNotStatic(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

func Main(text string) int64 {
	values := []int64{1, 2}
	table := map[int64]int64{-1: 40}
	if text[2] != 0 {
		return values[1] + table[-1] + 1
	}
	return 0
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileRejectsInvalidNumericOperatorsAndConstantDivisionByZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "float remainder",
			source: `
package main
func Main() float64 { return 4.0 % 2.0 }
`,
			code: "hirgen.binary.integer_operand",
		},
		{
			name: "float shift",
			source: `
package main
func Main() int64 { var value float64 = 1; return int64(value << 2) }
`,
			code: "hirgen.binary.integer_operand",
		},
		{
			name: "float bitwise",
			source: `
package main
func Main() float64 { return 1.0 & 1.0 }
`,
			code: "hirgen.binary.integer_operand",
		},
		{
			name: "constant division by zero",
			source: `
package main
func Main() float64 { return 1.0 / 0.0 }
`,
			code: "hirgen.const.div_zero",
		},
		{
			name: "variable divided by constant zero",
			source: `
package main
func Main(value float64) float64 { return value / 0.0 }
`,
			code: "hirgen.const.div_zero",
		},
		{
			name: "negative constant shift",
			source: `
package main
func Main() int64 { return 1 << -1 }
`,
			code: "hirgen.const.shift.negative",
		},
		{
			name: "too large constant shift",
			source: `
package main
func Main() int64 { return 1 << 4097 }
`,
			code: "hirgen.const.shift.count",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected diagnostic %s", tc.code)
			}
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == tc.code {
					return
				}
			}
			t.Fatalf("expected %s, got %#v", tc.code, result.Diagnostics)
		})
	}
}
