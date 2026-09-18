package compiler

import "testing"

func TestCompileSourceRejectsNonFinalVariadicParameters(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "function declaration",
			source: `package main
func Bad(values ...int64, tail int64) int64 {
	return tail
}
`,
			code: "ast.func.param.variadic.position",
		},
		{
			name: "function literal",
			source: `package main
func Main() int64 {
	fn := func(values ...int64, tail int64) int64 {
		return tail
	}
	return fn(1, 2)
}
`,
			code: "ast.func.param.variadic.position",
		},
		{
			name: "named function type",
			source: `package main
type Bad func(values ...int64, tail int64) int64
`,
			code: "ast.type.func.param.variadic.position",
		},
		{
			name: "interface method",
			source: `package main
type Bad interface {
	Call(values ...int64, tail int64) int64
}
`,
			code: "ast.func.param.variadic.position",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/variadic", "main.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected variadic position diagnostic")
			}
			if result.Artifact.Format != "" {
				t.Fatalf("invalid variadic signature must not produce artifact, got %#v", result.Artifact)
			}
			requireCompileDiagnostic(t, result.Diagnostics, test.code)
		})
	}
}
