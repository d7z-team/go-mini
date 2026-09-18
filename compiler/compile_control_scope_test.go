package compiler

import "testing"

func TestCompilePackageAllowsIntegerIndexTypes(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{
		ModulePath: "example/index",
		Files: []SourceFile{{
			Path: "index.mgo",
			Text: `
package index

func HexNibble(value uint64) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 1)
	out[0] = digits[value&15]
	return string(out)
}
`,
		}},
	})
	if err != nil {
		t.Fatalf("compileTestPackage failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompilePackageAllowsContinueThroughSwitchToLoop(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{
		ModulePath: "example/control",
		Files: []SourceFile{{
			Path: "control.mgo",
			Text: `
package control

func Count(values []int) int {
	total := 0
	for i := 0; i < len(values); i++ {
		switch values[i] {
		case 0:
			continue
		default:
			total += values[i]
		}
	}
	return total
}
`,
		}},
	})
	if err != nil {
		t.Fatalf("compileTestPackage failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompilePackageKeepsForAndRangeBindingsScopedToStatement(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{
		ModulePath: "example/loopscope",
		Files: []SourceFile{{
			Path: "loopscope.mgo",
			Text: `
package loopscope

func Sum(values [][]int) int {
	total := 0
	for i := 0; i < len(values); i++ {
		for _, value := range values[i] {
			total += value
		}
	}
	i := 1
	for _, row := range values {
		for _, row := range row {
			total += row
		}
	}
	return total + i
}
`,
		}},
	})
	if err != nil {
		t.Fatalf("compileTestPackage failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompilePackageKeepsIfAndSwitchInitBindingsScopedToStatement(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{
		ModulePath: "example/initscope",
		Files: []SourceFile{{
			Path: "initscope.mgo",
			Text: `
package initscope

func Pick(values []int, x any) int {
	total := 0
	if value := len(values); value > 0 {
		total += value
	}
	if value := 1; value > 0 {
		total += value
	}
	value := 3
	switch kind := value; kind {
	case 3:
		total += kind
	}
	switch kind := x.(type) {
	case int:
		total += kind
	default:
		total += value
	}
	return total
}
`,
		}},
	})
	if err != nil {
		t.Fatalf("compileTestPackage failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}
